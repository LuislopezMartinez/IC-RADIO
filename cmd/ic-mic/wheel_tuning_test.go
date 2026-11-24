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
	wheel.queue(now.Add(145*time.Millisecond), state.Frequency, 100, .5)
	state.Frequency, state.FrequencyAt = 14200300, now.Add(150*time.Millisecond)
	wheel.update(now.Add(160*time.Millisecond), state, send)
	if wheel.target != 0 {
		t.Fatal("confirmed preview did not clear")
	}
	wheel.queue(now.Add(165*time.Millisecond), state.Frequency, 100, .5)
	if shown, _ := panel.shownFrequency(state, true, now.Add(165*time.Millisecond)); shown != 14200400 {
		t.Fatalf("confirmation dropped fractional wheel movement: dial preview = %d", shown)
	}
}

func TestWheelTuningRetriesAnUnconfirmedFrequencyOnce(t *testing.T) {
	now := time.Now()
	state := civ.State{Frequency: 14200000, FrequencyAt: now}
	panel := radioPanel{}
	wheel := &panel.wheel
	var commands []int64
	send := func(hz int64) bool {
		commands = append(commands, hz)
		return true
	}
	wheel.queue(now, state.Frequency, 100, 2)
	sentAt := now.Add(wheelSettleDelay)
	wheel.update(sentAt, state, send)
	state.Frequency, state.FrequencyAt = 14200100, sentAt.Add(time.Millisecond)
	retryAt := sentAt.Add(wheelRetryDelay)
	wheel.update(retryAt, state, send)
	wheel.update(retryAt.Add(time.Millisecond), state, send)
	if len(commands) != 2 || commands[0] != 14200200 || commands[1] != 14200200 {
		t.Fatalf("expected a single retry of the latest goal: %v", commands)
	}
	if shown, _ := panel.shownFrequency(state, true, retryAt); shown != 14200200 {
		t.Fatalf("intermediate radio reading replaced dial preview: %d", shown)
	}
	if !wheel.update(sentAt.Add(wheelConfirmTimeout), state, send) || wheel.target != 0 {
		t.Fatal("unconfirmed tuning did not report timeout and clear the preview")
	}
}

func TestWheelCursorUsesPreviewAndContinuesFromItsCurrentPosition(t *testing.T) {
	now := time.Now()
	state := civ.State{Frequency: 14200000, FrequencyAt: now}
	state.Tuning.RITAt = now
	panel := radioPanel{}
	wheel := &panel.wheel
	wheel.queue(now, state.Frequency, 1000, 1)
	midpoint := now.Add(wheelCursorDuration / 2)
	shown, _ := panel.shownFrequency(state, true, midpoint)
	cursor := wheel.cursorFrequency(shown, midpoint)
	if shown != 14201000 || cursor <= state.Frequency || cursor >= shown {
		t.Fatalf("expected exact dial target and intermediate cursor, got %d and %d", shown, cursor)
	}
	wheel.queue(midpoint, state.Frequency, 1000, 1)
	shown, _ = panel.shownFrequency(state, true, midpoint)
	if got := wheel.cursorFrequency(shown, midpoint); got != cursor {
		t.Fatalf("next wheel event jumped from %d to %d", cursor, got)
	}
	if got := wheel.cursorFrequency(shown, midpoint.Add(wheelCursorDuration)); got != 14202000 {
		t.Fatalf("cursor settled at %d", got)
	}
}
