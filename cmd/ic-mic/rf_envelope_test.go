package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestRFEnvelopePreservesPulseBetweenFrames(t *testing.T) {
	var envelope rfEnvelope
	now := time.Now()
	sweep := civ.Sweep{Low: 7080000, High: 7130000, At: now}
	sweep.Levels[32] = 30
	envelope.update(sweep, [civ.Samples]byte{}, now)

	var peaks [civ.Samples]byte
	peaks[32] = 150
	envelope.update(sweep, peaks, now.Add(16*time.Millisecond))
	first := envelope.bins[32].level
	if first <= 30 || first >= 75 || envelope.peakLevels[32] <= 30 || envelope.peakLevels[32] >= 150 {
		t.Fatalf("wrong initial rise or peak: trace=%v peak=%v", first, envelope.peakLevels[32])
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(240*time.Millisecond))
	if got := envelope.bins[32].level; got <= first || got >= 150 {
		t.Fatalf("expected trace still moving towards peak: %v", got)
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(time.Second))
	if got := envelope.bins[32].level; got != 150 || envelope.peakLevels[32] != 150 {
		t.Fatalf("pulse clipped: trace=%v peak=%v", got, envelope.peakLevels[32])
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(1100*time.Millisecond))
	if got := envelope.bins[32].level; got <= 30 || got >= 150 || envelope.peakLevels[32] <= got || envelope.peakLevels[32] >= 150 {
		t.Fatalf("expected the peak to fall more slowly than the trace: trace=%v peak=%v", got, envelope.peakLevels[32])
	}

	sweep.Low, sweep.High = 7090000, 7140000
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(1200*time.Millisecond))
	if got := envelope.bins[32].level; got != 30 || envelope.peakLevels[32] != 30 {
		t.Fatalf("retained old peak on a different frequency axis: %v", got)
	}
}

func TestTuneDigitRoundsInWheelDirection(t *testing.T) {
	for _, tc := range []struct{ hz, step, delta, want int64 }{
		{7105267, 1000, 1, 7106000},
		{7105267, 1000, -1, 7105000},
		{7105267, 1000, 3, 7108000},
		{7105000, 1000, -1, 7104000},
		{7105999, 1000, 1, 7106000},
		{7999999, 1000000, 1, 8000000},
	} {
		if got := tuneDigit(tc.hz, tc.step, tc.delta); got != tc.want {
			t.Errorf("tuneDigit(%d, %d, %d) = %d, want %d", tc.hz, tc.step, tc.delta, got, tc.want)
		}
	}
}
