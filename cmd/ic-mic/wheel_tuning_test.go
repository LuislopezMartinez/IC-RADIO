package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestWheelTuningCoalescesAndWaitsForConfirmation(t *testing.T) {
	now := time.Now()
	state := civ.State{Frequency: 14200000, FrequencyAt: now}
	panel := radioPanel{}
	wheel := &panel.wheel
	var commands []int64
	send := func(hz int64) bool {
		commands = append(commands, hz)
		return true
	}
	wheel.queue(now, state.Frequency, 100, .5)
	wheel.queue(now.Add(time.Millisecond), state.Frequency, 100, .5)
	wheel.queue(now.Add(20*time.Millisecond), state.Frequency, 100, 2)
	if shown, _ := panel.shownFrequency(state, true, now.Add(20*time.Millisecond)); shown != 14200300 {
		t.Fatalf("dial preview = %d", shown)
	}
	wheel.update(now.Add(70*time.Millisecond), state, send)
	if len(commands) != 0 {
		t.Fatal("sent an intermediate setting")
	}
	wheel.update(now.Add(100*time.Millisecond), state, send)
	wheel.update(now.Add(140*time.Millisecond), state, send)
	if len(commands) != 1 || commands[0] != 14200300 || wheel.target == 0 {
		t.Fatalf("unexpected command or premature confirmation: %v", commands)
	}
	state.Frequency, state.FrequencyAt = 14200300, now.Add(150*time.Millisecond)
	wheel.update(now.Add(160*time.Millisecond), state, send)
	if wheel.target != 0 {
		t.Fatal("confirmed preview did not clear")
	}
}
