//go:build cgo

package audioio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

const sampleRate = 48000

type Engine struct {
	mu        sync.RWMutex
	context   *malgo.AllocatedContext
	device    *malgo.Device
	inputs    []malgo.DeviceInfo
	outputs   []malgo.DeviceInfo
	processor Processor
	status    Status
	mono      []float32
}

func New(processor Processor) (*Engine, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	e := &Engine{context: ctx, processor: processor, status: Status{SampleRate: sampleRate}}
	if err := e.refresh(); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

func (e *Engine) refresh() error {
	var err error
	e.inputs, err = e.context.Devices(malgo.Capture)
	if err != nil {
		return fmt.Errorf("enumerate inputs: %w", err)
	}
	e.outputs, err = e.context.Devices(malgo.Playback)
	if err != nil {
		return fmt.Errorf("enumerate outputs: %w", err)
	}
	return nil
}

func deviceList(source []malgo.DeviceInfo) []Device {
	result := make([]Device, len(source))
	for i := range source {
		result[i] = Device{Name: source[i].Name(), IsDefault: source[i].IsDefault != 0, index: i}
	}
	return result
}
func (e *Engine) Inputs() []Device  { return deviceList(e.inputs) }
func (e *Engine) Outputs() []Device { return deviceList(e.outputs) }
func (e *Engine) Refresh() error {
	e.Stop()
	return e.refresh()
}

func (e *Engine) Start(inputIndex, outputIndex int) error {
	e.Stop()
	if inputIndex < 0 || inputIndex >= len(e.inputs) || outputIndex < 0 || outputIndex >= len(e.outputs) {
		return fmt.Errorf("invalid audio device")
	}
	config := malgo.DefaultDeviceConfig(malgo.Duplex)
	config.SampleRate = sampleRate
	// A 20 ms period gives small ARM systems enough scheduling margin for the
	// UI without making interactive audio controls feel sluggish.
	config.PeriodSizeInMilliseconds = 20
	config.Capture.Format, config.Capture.Channels = malgo.FormatF32, 1
	config.Playback.Format, config.Playback.Channels = malgo.FormatF32, 2
	config.Capture.DeviceID = e.inputs[inputIndex].ID.Pointer()
	config.Playback.DeviceID = e.outputs[outputIndex].ID.Pointer()
	callbacks := malgo.DeviceCallbacks{Data: e.process}
	device, err := malgo.InitDevice(e.context.Context, config, callbacks)
	if err != nil {
		e.setError(err)
		return err
	}
	e.device = device
	if err := device.Start(); err != nil {
		device.Uninit()
		e.device = nil
		e.setError(err)
		return err
	}
	e.mu.Lock()
	e.status.Running, e.status.Error = true, ""
	e.status.SampleRate = int(device.SampleRate())
	e.mu.Unlock()
	return nil
}

func (e *Engine) process(output, input []byte, frameCount uint32) {
	frames := int(frameCount)
	if cap(e.mono) < frames {
		e.mono = make([]float32, frames)
	}
	mono := e.mono[:frames]
	inPeak := float32(0)
	inSquares := float64(0)
	for i := 0; i < frames; i++ {
		v := math.Float32frombits(binary.LittleEndian.Uint32(input[i*4:]))
		mono[i] = v
		inSquares += float64(v * v)
		if a := abs(v); a > inPeak {
			inPeak = a
		}
	}
	started := time.Now()
	e.processor.Process(mono)
	processingTime := time.Since(started).Seconds()
	outPeak := float32(0)
	outSquares := float64(0)
	for i, v := range mono {
		if a := abs(v); a > outPeak {
			outPeak = a
		}
		outSquares += float64(v * v)
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(output[(i*2)*4:], bits)
		binary.LittleEndian.PutUint32(output[(i*2+1)*4:], bits)
	}
	e.mu.Lock()
	e.status.InputDBFS = dbfs(inPeak)
	e.status.OutputDBFS = dbfs(outPeak)
	if frames > 0 {
		e.status.InputRMSDBFS = dbfs(float32(math.Sqrt(inSquares / float64(frames))))
		e.status.OutputRMSDBFS = dbfs(float32(math.Sqrt(outSquares / float64(frames))))
		bufferTime := float64(frames) / sampleRate
		load := float32(100 * processingTime / bufferTime)
		e.status.CPULoad += .12 * (load - e.status.CPULoad)
	}
	e.status.BufferFrames = frames
	e.mu.Unlock()
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
func dbfs(v float32) float32 {
	if v < 0.0001 {
		return -80
	}
	return max(-80, float32(20*math.Log10(float64(v))))
}
func (e *Engine) setError(err error) {
	e.mu.Lock()
	e.status.Running = false
	e.status.Error = err.Error()
	e.mu.Unlock()
}
func (e *Engine) Stop() {
	if e.device != nil {
		_ = e.device.Stop()
		e.device.Uninit()
		e.device = nil
	}
	e.mu.Lock()
	e.status.Running = false
	e.mu.Unlock()
}
func (e *Engine) Status() Status { e.mu.RLock(); defer e.mu.RUnlock(); return e.status }
func (e *Engine) Close() {
	e.Stop()
	if e.context != nil {
		_ = e.context.Uninit()
		e.context.Free()
		e.context = nil
	}
}
