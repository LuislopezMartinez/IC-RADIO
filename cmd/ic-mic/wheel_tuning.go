package main

import (
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

const wheelSettleDelay = 65 * time.Millisecond
const wheelConfirmTimeout = 1500 * time.Millisecond
const wheelRetryDelay = wheelConfirmTimeout / 2
const wheelCursorDuration = 55 * time.Millisecond

type wheelTuning struct {
	target, step                 int64
	fraction                     float64
	sendAfter, expires, baseline time.Time
	fractionUntil, retryAfter    time.Time
	sent, inFlight               bool
	cursorFrom, cursorTo         int64
	cursorStart                  time.Time
}

func (w *wheelTuning) queue(now time.Time, frequency, step int64, movement float64) {
	if w.target != 0 && !now.Before(w.expires) {
		w.clearTarget()
	}
	if step != w.step || !now.Before(w.fractionUntil) {
		w.fraction = 0
	}
	w.step = step
	w.fractionUntil = now.Add(wheelConfirmTimeout)
	w.fraction += movement
	whole, remainder := math.Modf(w.fraction)
	w.fraction = remainder
	if whole == 0 {
		return
	}
	if w.target != 0 {
		frequency = w.target
	}
	target := tuneDigit(frequency, step, int64(whole))
	if !civ.CanTune(target) {
		return
	}
	w.cursorFrom = w.cursorFrequency(frequency, now)
	w.cursorTo, w.cursorStart = target, now
	w.target, w.sent = target, false
	w.sendAfter = now.Add(wheelSettleDelay)
	w.expires = now.Add(wheelConfirmTimeout)
}

func (w *wheelTuning) clearTarget() {
	w.target, w.sent, w.inFlight = 0, false, false
}

func (w *wheelTuning) cursorFrequency(hz int64, now time.Time) int64 {
	if hz != w.cursorTo || w.cursorStart.IsZero() {
		return hz
	}
	progress := float64(now.Sub(w.cursorStart)) / float64(wheelCursorDuration)
	progress = max(0, min(1, progress))
	progress *= 2 - progress
	return w.cursorFrom + int64(math.Round(float64(hz-w.cursorFrom)*progress))
}

// update sends only the latest target of a wheel gesture. Earlier CI-V
// readings remain available to the rest of the console while the dial previews it.
// It reports a timeout when the radio never confirms the requested frequency.
func (w *wheelTuning) update(now time.Time, state civ.State, tune func(int64) bool) (timedOut bool) {
	if w.target == 0 {
		return false
	}
	if w.sent && state.Frequency == w.target && state.FrequencyAt.After(w.baseline) {
		w.clearTarget()
		return false
	}
	if !now.Before(w.expires) {
		w.clearTarget()
		return true
	}
	if w.sent {
		if !w.retryAfter.IsZero() && !now.Before(w.retryAfter) && tune(w.target) {
			w.retryAfter = time.Time{}
		}
		return false
	}
	if now.Before(w.sendAfter) {
		return false
	}
	if !w.inFlight && state.Frequency == w.target {
		w.clearTarget()
		return false
	}
	if tune(w.target) {
		w.sent, w.inFlight = true, true
		w.baseline = state.FrequencyAt
		w.retryAfter = now.Add(wheelRetryDelay)
		w.expires = now.Add(wheelConfirmTimeout)
	}
	return false
}

func (r *radioPanel) shownFrequency(state civ.State, online bool, now time.Time) (int64, bool) {
	if online && r.wheel.target != 0 && now.Before(r.wheel.expires) {
		return r.wheel.target, true
	}
	if online {
		if rx, _ := state.EffectiveTuning(now); rx > 0 {
			return rx, true
		}
	}
	return displayedFrequency(state, online, now)
}
