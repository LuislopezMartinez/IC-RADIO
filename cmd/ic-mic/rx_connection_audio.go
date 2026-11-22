package main

import (
	"sync/atomic"
	"time"

	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

type connectedRXAudio struct {
	enabled atomic.Bool
	dsp     audioio.Processor
}

func (p *connectedRXAudio) Process(samples []float32) {
	if !p.enabled.Load() {
		clear(samples)
		return
	}
	p.dsp.Process(samples)
}

func radioRXAudioReady(state civ.State) bool {
	return state.Error == "" && !state.PowerPending && !state.PoweredOff &&
		!state.TX && time.Since(state.LastReply) < 2*time.Second
}
