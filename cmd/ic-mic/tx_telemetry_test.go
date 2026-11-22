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
