//go:build cgo

package audioio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	"github.com/gen2brain/malgo"
)

// PlaybackMonitor plays a copy of an already processed mono stream through a
// second Windows playback endpoint. Its short ring buffer decouples the two
// audio-device clocks without changing the primary radio path.
type PlaybackMonitor struct {
	mu        sync.Mutex
	context   *malgo.AllocatedContext
	device    *malgo.Device
	outputs   []malgo.DeviceInfo
	buffer    []float32
	read      int
	write     int
	count     int
	readPhase float64
	running   bool
}

func NewPlaybackMonitor() (*PlaybackMonitor, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	outputs, err := ctx.Devices(malgo.Playback)
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("enumerate monitor outputs: %w", err)
	}
	return &PlaybackMonitor{context: ctx, outputs: outputs, buffer: make([]float32, sampleRate/10)}, nil
}

func (m *PlaybackMonitor) Start(outputIndex int) error {
	m.Stop()
	if outputIndex < 0 || outputIndex >= len(m.outputs) {
		return fmt.Errorf("invalid monitor output")
	}
	config := malgo.DefaultDeviceConfig(malgo.Playback)
	config.SampleRate = sampleRate
	config.PeriodSizeInMilliseconds = 20
	config.Playback.Format, config.Playback.Channels = malgo.FormatF32, 2
	config.Playback.DeviceID = m.outputs[outputIndex].ID.Pointer()
	device, err := malgo.InitDevice(m.context.Context, config, malgo.DeviceCallbacks{Data: m.process})
	if err != nil {
		return err
	}
	m.device = device
	if err := device.Start(); err != nil {
		device.Uninit()
		m.device = nil
		return err
	}
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	return nil
}

func (m *PlaybackMonitor) StartNamed(name string) error {
	// Playback endpoints can change while the application is running (for
	// example, when headphones are connected). The monitor used to retain only
	// the list captured at startup, so a newly selected Windows default endpoint
	// could never be found here.
	m.Stop()
	if m.context == nil {
		return fmt.Errorf("monitor engine is closed")
	}
	outputs, err := m.context.Devices(malgo.Playback)
	if err != nil {
		return fmt.Errorf("refresh monitor outputs: %w", err)
	}
	m.outputs = outputs
	for i := range m.outputs {
		if m.outputs[i].Name() == name {
			return m.Start(i)
		}
	}
	return fmt.Errorf("monitor output not found: %s", name)
}

// Write copies processed samples without retaining the caller's audio buffer.
func (m *PlaybackMonitor) Write(samples []float32) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	for _, sample := range samples {
		if m.count == len(m.buffer) {
			m.read = (m.read + 1) % len(m.buffer)
			m.count--
		}
		m.buffer[m.write] = sample
		m.write = (m.write + 1) % len(m.buffer)
		m.count++
	}
}

func (m *PlaybackMonitor) process(output, _ []byte, frameCount uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	target := float64(len(m.buffer)) * .5
	step := 1.0
	if target > 0 {
		step += math.Max(-.003, math.Min(.003, (float64(m.count)-target)/target*.003))
	}
	for i := 0; i < int(frameCount); i++ {
		var sample float32
		position := int(m.readPhase)
		if m.count > position+1 {
			left := m.buffer[(m.read+position)%len(m.buffer)]
			right := m.buffer[(m.read+position+1)%len(m.buffer)]
			fraction := float32(m.readPhase - float64(position))
			sample = left + (right-left)*fraction
			m.readPhase += step
			consumed := int(m.readPhase)
			if consumed > 0 {
				consumed = min(consumed, m.count)
				m.read = (m.read + consumed) % len(m.buffer)
				m.count -= consumed
				m.readPhase -= float64(consumed)
			}
		}
		bits := math.Float32bits(sample)
		binary.LittleEndian.PutUint32(output[(i*2)*4:], bits)
		binary.LittleEndian.PutUint32(output[(i*2+1)*4:], bits)
	}
}

func (m *PlaybackMonitor) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	if m.device != nil {
		_ = m.device.Stop()
		m.device.Uninit()
		m.device = nil
	}
	m.mu.Lock()
	m.read, m.write, m.count, m.readPhase = 0, 0, 0, 0
	m.mu.Unlock()
}

func (m *PlaybackMonitor) Close() {
	if m == nil {
		return
	}
	m.Stop()
	if m.context != nil {
		_ = m.context.Uninit()
		m.context.Free()
		m.context = nil
	}
}
