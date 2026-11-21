package main

import (
	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"math"
	"time"
)

type rfBin struct{ level, pending float32 }

type rfEnvelope struct {
	low, high     int64
	at, lastSweep time.Time
	attack        time.Duration
	bins          [civ.Samples]rfBin
	peakLevels    [civ.Samples]float32
	peakTargets   [civ.Samples]float32
}

func (e *rfEnvelope) update(sweep civ.Sweep, peaks [civ.Samples]byte, now time.Time) {
	if e.at.IsZero() || e.low != sweep.Low || e.high != sweep.High {
		*e = rfEnvelope{low: sweep.Low, high: sweep.High, at: now, attack: 260 * time.Millisecond}
		for i, raw := range sweep.Levels {
			e.bins[i].level = float32(raw)
			e.peakLevels[i] = float32(raw)
		}
	}
	if sweep.At.After(e.lastSweep) {
		if !e.lastSweep.IsZero() {
			interval := max(210*time.Millisecond, min(650*time.Millisecond, sweep.At.Sub(e.lastSweep)))
			e.attack = (3*e.attack + interval) / 4
		}
		e.lastSweep = sweep.At
	}
	dt := now.Sub(e.at)
	rise := float32(-math.Expm1(-2 * float64(dt) / float64(e.attack)))
	fall := float32(-math.Expm1(-float64(dt) / float64(240*time.Millisecond)))
	peakRise := float32(-math.Expm1(-float64(dt) / float64(105*time.Millisecond)))
	peakFall := float32(-math.Expm1(-float64(dt) / float64(900*time.Millisecond)))
	for i, raw := range sweep.Levels {
		bin := &e.bins[i]
		peak := float32(max(raw, peaks[i]))
		if peak > bin.level {
			bin.pending = max(bin.pending, peak)
		}
		if bin.pending > bin.level {
			// Retain a rising pulse even if the next measured sweep is lower.
			bin.level += (bin.pending - bin.level) * rise
			if bin.pending-bin.level <= .5 {
				bin.level, bin.pending = bin.pending, 0
			}
		} else {
			bin.level += (float32(raw) - bin.level) * fall
		}
		level := e.peakLevels[i]
		target := max(e.peakTargets[i], peak)
		if target > level {
			level += (target - level) * peakRise
			if target-level <= .5 {
				level, target = target, 0
			}
			e.peakTargets[i] = target
		} else {
			level += (peak - level) * peakFall
			e.peakTargets[i] = 0
		}
		e.peakLevels[i] = max(level, bin.level)
	}
	e.at = now
}
