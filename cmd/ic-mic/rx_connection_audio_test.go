package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

type amplifyRX struct{}

func (amplifyRX) Process(samples []float32) { samples[0] *= 2 }

func TestRXAudioWaitsForRadio(t *testing.T) {
	gate := &connectedRXAudio{dsp: amplifyRX{}}
	for _, test := range []struct {
		name  string
		state civ.State
		want  float32
	}{
		{"disconnected", civ.State{}, 0},
		{"receiving", civ.State{LastReply: time.Now()}, .5},
		{"power pending", civ.State{LastReply: time.Now(), PowerPending: true}, 0},
		{"powered off", civ.State{LastReply: time.Now(), PoweredOff: true}, 0},
		{"connection lost", civ.State{LastReply: time.Now().Add(-3 * time.Second)}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			gate.enabled.Store(radioRXAudioReady(test.state))
			block := []float32{.25}
			gate.Process(block)
			if block[0] != test.want {
				t.Fatalf("audio output = %v, want %v", block[0], test.want)
			}
		})
	}
}
