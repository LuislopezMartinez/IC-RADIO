package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestTXTelemetryCalibratesAndRejectsStaleReadings(t *testing.T) {
	now := time.Now()
	state := civ.State{TX: true, TXSince: now.Add(-time.Second)}
	for code, value := range map[byte]int{0x11: 143, 0x12: 48, 0x13: 60, 0x14: 130, 0x15: 13, 0x16: 146} {
		state.Meters[code], state.MeterAt[code] = value, now
	}
	readings := txReadings(state, true, now)
	for i, want := range []float64{50, 1.5, 50, 10, 15, 15} {
		if !readings[i].available || readings[i].value != want {
			t.Fatalf("%s: got %v, want %v", readings[i].label, readings[i], want)
		}
	}
	state.Meters[0x11] = 0
	state.MeterAt[0x16] = state.TXSince.Add(-time.Millisecond)
	readings = txReadings(state, true, now)
	if readings[1].available || readings[4].available {
		t.Fatal("displayed SWR with no RF or reused a previous TX reading")
	}
}

func TestTXMeterDisplayRespondsQuicklyAndLetsPowerFallGradually(t *testing.T) {
	start := time.Now()
	var display txMeterDisplay
	readings := [6]txReading{
		{code: 0x11, value: 100, limit: 100, available: true},
		{code: 0x12, value: 2.5, limit: 3, available: true},
	}
	display.update(readings, start, start)
	peak := display.update(readings, start, start.Add(90*time.Millisecond))
	if peak[0].value < 60 || peak[0].value > 65 || peak[1].value != 2.5 {
		t.Fatalf("unexpected attack or SWR reading: %+v", peak)
	}

	readings[0].value = 0
	falling := display.update(readings, start, start.Add(180*time.Millisecond))
	if falling[0].value < 45 || falling[0].value >= peak[0].value {
		t.Fatalf("power should fall gradually, got %.1f after %.1f", falling[0].value, peak[0].value)
	}

	readings[0].available = false
	missing := display.update(readings, start, start.Add(270*time.Millisecond))
	if missing[0].available || display.position[0] != 0 {
		t.Fatal("display retained unavailable power")
	}
}
