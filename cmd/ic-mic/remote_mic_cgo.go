//go:build cgo

package main

// #include <stdlib.h>
import "C"

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	"github.com/gen2brain/malgo"
)

func openRemoteMic(name string, process func([]float32), blocks chan []byte) (func(), error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	closeContext := func() {
		_ = ctx.Uninit()
		ctx.Free()
	}
	inputs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		closeContext()
		return nil, err
	}
	var cfg = malgo.DefaultDeviceConfig(malgo.Capture)
	for i := range inputs {
		if inputs[i].Name() == name {
			cfg.Capture.DeviceID = inputs[i].ID.Pointer()
			break
		}
	}
	if cfg.Capture.DeviceID == nil {
		closeContext()
		return nil, fmt.Errorf("micrófono no encontrado: %s", name)
	}
	freeID := func() { C.free(cfg.Capture.DeviceID) }
	cfg.SampleRate = remote.AudioSampleRate
	cfg.PeriodSizeInMilliseconds = 20
	cfg.Capture.Format, cfg.Capture.Channels = malgo.FormatF32, 1
	var samples []float32
	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(_, pcm []byte, frames uint32) {
		if frames == 0 {
			return
		}
		if cap(samples) < int(frames) {
			samples = make([]float32, frames)
		}
		samples = samples[:frames]
		for i := range samples {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:]))
		}
		process(samples)
		if blocks == nil {
			return
		}
		block := make([]byte, len(samples)*2)
		for i, sample := range samples {
			if math.IsNaN(float64(sample)) {
				sample = 0
			}
			value := int16(max(-32768, min(32767, float64(sample)*32768)))
			binary.LittleEndian.PutUint16(block[i*2:], uint16(value))
		}
		select {
		case blocks <- block:
		default:
			// Discard stale audio when the network cannot keep up.
			select {
			case <-blocks:
			default:
			}
			blocks <- block
		}
	}})
	if err != nil {
		freeID()
		closeContext()
		return nil, err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		freeID()
		closeContext()
		return nil, err
	}
	return func() {
		device.Uninit()
		freeID()
		closeContext()
	}, nil
}
