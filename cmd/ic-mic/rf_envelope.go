package main

import (
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

type rfEnvelope struct {
	low, high     int64
	at, lastSweep time.Time
	interval      time.Duration
	transitionAt  time.Time
	transition    time.Duration
	from          [civ.Samples]float32
	target        [civ.Samples]byte
	levels        [civ.Samples]float32
	peakLevels    [civ.Samples]float32
}

func (e *rfEnvelope) update(sweep civ.Sweep, peaks [civ.Samples]byte, now time.Time) {
	if e.at.IsZero() || e.low != sweep.Low || e.high != sweep.High {
		*e = rfEnvelope{
			low: sweep.Low, high: sweep.High, at: now, lastSweep: sweep.At,
			target: sweep.Levels,
		}
		for i, raw := range sweep.Levels {
			e.levels[i] = float32(raw)
			e.peakLevels[i] = float32(max(raw, peaks[i]))
		}
		return
	}
	newSweep := sweep.At.After(e.lastSweep)
	progress := float32(1)
	if e.transition > 0 {
		progress = max(0, min(1, float32(now.Sub(e.transitionAt))/float32(e.transition)))
	}
	if newSweep {
		if !e.lastSweep.IsZero() {
			interval := sweep.At.Sub(e.lastSweep)
			if e.interval == 0 {
				e.interval = interval
			} else {
				e.interval = (3*e.interval + interval) / 4
			}
		}
		e.lastSweep = sweep.At
	}
	dt := now.Sub(e.at)
	peakFall := float32(-math.Expm1(-float64(dt) / float64(900*time.Millisecond)))
	for i, raw := range sweep.Levels {
		e.levels[i] = e.from[i] + (float32(e.target[i])-e.from[i])*progress
		if newSweep {
			// Start at the position already displayed, even if another
			// sweep arrived before the previous transition had finished.
			e.from[i], e.target[i] = e.levels[i], raw
		}
		peak := float32(max(raw, peaks[i]))
		held := e.peakLevels[i]
		e.peakLevels[i] = max(peak, held+(peak-held)*peakFall, e.levels[i])
	}
	if newSweep {
		e.transitionAt = now
		e.transition = max(24*time.Millisecond, min(300*time.Millisecond, e.interval*9/10))
	}
	e.at = now
}
