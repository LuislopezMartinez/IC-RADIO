package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestRFEnvelopeInterpolatesSweepsWithImmediatePeak(t *testing.T) {
	var envelope rfEnvelope
	now := time.Now()
	sweep := civ.Sweep{Low: 7080000, High: 7130000, At: now}
	sweep.Levels[32] = 30
	envelope.update(sweep, [civ.Samples]byte{}, now)

	var peaks [civ.Samples]byte
	peaks[32] = 150
	envelope.update(sweep, peaks, now.Add(16*time.Millisecond))
	if got := envelope.levels[32]; got != 30 || envelope.peakLevels[32] != 150 {
		t.Fatalf("intermediate maximum should be shown only on PEAK: trace=%v peak=%v", got, envelope.peakLevels[32])
	}
	sweep.At, sweep.Levels[32] = now.Add(200*time.Millisecond), 140
	envelope.update(sweep, [civ.Samples]byte{}, sweep.At)
	if got := envelope.levels[32]; got != 30 || envelope.interval != 200*time.Millisecond || envelope.peakLevels[32] < 140 {
		t.Fatalf("new sweep jumped or peak was delayed: trace=%v interval=%v peak=%v", got, envelope.interval, envelope.peakLevels[32])
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(290*time.Millisecond))
	if got := envelope.levels[32]; got != 85 || envelope.transition != 180*time.Millisecond {
		t.Fatalf("expected an intermediate frame: %v", got)
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(388*time.Millisecond))
	sweep.At, sweep.Levels[32] = now.Add(400*time.Millisecond), 30
	envelope.update(sweep, [civ.Samples]byte{}, sweep.At)
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(430*time.Millisecond))
	if got := envelope.levels[32]; got <= 30 || got >= 140 || envelope.peakLevels[32] <= got {
		t.Fatalf("expected the peak to fall more slowly than the trace: trace=%v peak=%v", got, envelope.peakLevels[32])
	}
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(585*time.Millisecond))
	if got := envelope.levels[32]; got >= 35 {
		t.Fatalf("fall is too persistent: %v", got)
	}

	sweep.Low, sweep.High = 7090000, 7140000
	envelope.update(sweep, [civ.Samples]byte{}, now.Add(600*time.Millisecond))
	if got := envelope.levels[32]; got != 30 || envelope.peakLevels[32] != 30 {
		t.Fatalf("retained old peak on a different frequency axis: %v", got)
	}
}

func TestRFEnvelopeRestartsFromDisplayedPosition(t *testing.T) {
	var e rfEnvelope
	now := time.Now()
	sweep := civ.Sweep{Low: 7080000, High: 7130000, At: now}
	e.update(sweep, [civ.Samples]byte{}, now)
	sweep.At, sweep.Levels[0] = now.Add(30*time.Millisecond), 120
	e.update(sweep, [civ.Samples]byte{}, sweep.At)
	sweep.At, sweep.Levels[0] = now.Add(39*time.Millisecond), 20
	e.update(sweep, [civ.Samples]byte{}, sweep.At)
	if e.levels[0] != 40 {
		t.Fatalf("new sweep caused a discontinuity: %v", e.levels[0])
	}
	e.update(sweep, [civ.Samples]byte{}, now.Add(75*time.Millisecond))
	if e.levels[0] != 20 {
		t.Fatalf("did not reach latest measurement: %v", e.levels[0])
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
