package main

import (
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

const wheelSettleDelay = 65 * time.Millisecond
const wheelConfirmTimeout = 1500 * time.Millisecond

type wheelTuning struct {
	target, step                 int64
	fraction                     float64
	sendAfter, expires, baseline time.Time
	sent, inFlight               bool
}

func (w *wheelTuning) queue(now time.Time, frequency, step int64, movement float64) {
	if !now.Before(w.expires) {
		*w = wheelTuning{}
	}
	if step != w.step {
		w.fraction = 0
	}
	w.step = step
	w.fraction += movement
	whole, remainder := math.Modf(w.fraction)
	w.fraction = remainder
	if whole == 0 {
		if w.target == 0 {
			w.expires = now.Add(wheelConfirmTimeout)
		}
		return
	}
	if w.target != 0 {
		frequency = w.target
	}
	target := tuneDigit(frequency, step, int64(whole))
	if !civ.CanTune(target) {
		return
	}
	w.target, w.sent = target, false
	w.sendAfter = now.Add(wheelSettleDelay)
	w.expires = now.Add(wheelConfirmTimeout)
}

// update sends only the latest target of a wheel gesture. Earlier CI-V
// readings remain available to the rest of the console while the dial previews it.
func (w *wheelTuning) update(now time.Time, state civ.State, tune func(int64) bool) {
	if w.target == 0 {
		return
	}
	if !now.Before(w.expires) {
		*w = wheelTuning{}
		return
	}
	if w.sent {
		if state.Frequency == w.target && state.FrequencyAt.After(w.baseline) {
			*w = wheelTuning{}
		}
		return
	}
	if now.Before(w.sendAfter) {
		return
	}
	if !w.inFlight && state.Frequency == w.target {
		*w = wheelTuning{}
		return
	}
	if tune(w.target) {
		w.sent, w.inFlight = true, true
		w.baseline = state.FrequencyAt
		w.expires = now.Add(wheelConfirmTimeout)
	}
}

func (r *radioPanel) shownFrequency(state civ.State, online bool, now time.Time) (int64, bool) {
	if online && r.wheel.target != 0 && now.Before(r.wheel.expires) {
		return r.wheel.target, true
	}
	return displayedFrequency(state, online, now)
}
