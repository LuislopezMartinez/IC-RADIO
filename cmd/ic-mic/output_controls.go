package main

import (
	"fmt"
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

type outputControls struct {
	dragging                       bool
	preview                        int
	powerRequested, tunerRequested time.Time
	tunerTarget                    byte
}

func outputPowerTrack() rl.Rectangle { return rl.NewRectangle(577, 838, 298, 10) }
func outputPowerValue() rl.Rectangle { return rl.NewRectangle(742, 792, 133, 35) }
func tunerControl() rl.Rectangle     { return rl.NewRectangle(903, 806, 151, 50) }
func tuneControl() rl.Rectangle      { return rl.NewRectangle(1068, 806, 139, 50) }

func nominalRFPower(state civ.State, level int) float64 {
	fullScale := 100.0
	if state.Frequency >= 70000000 {
		fullScale = 50
	}
	minimum := 2.0
	if state.ModeKnown && state.Mode == 2 {
		fullScale /= 4
		minimum = 1
	}
	return max(minimum, float64(level)*fullScale/255)
}

func (a *app) updateOutputControls(mouse rl.Vector2, state civ.State, online bool) {
	control := &a.radio.output
	now := time.Now()
	if !control.powerRequested.IsZero() && (state.PowerAt.After(control.powerRequested) || now.Sub(control.powerRequested) > 2*time.Second) {
		control.powerRequested = time.Time{}
	}
	if !control.tunerRequested.IsZero() && (state.TunerAt.After(control.tunerRequested) || now.Sub(control.tunerRequested) > 2*time.Second) {
		control.tunerRequested = time.Time{}
	}
	canAdjust := online && state.TXKnown && !state.TX && !a.ptt.TX() && state.Tuner != 2
	if !canAdjust || !rl.IsWindowFocused() {
		control.dragging = false
		return
	}
	track := outputPowerTrack()
	if clicked(mouse, expandedSliderHitbox(track)) && now.Sub(state.PowerAt) < 4*time.Second {
		control.dragging = true
	}
	if control.dragging {
		control.preview = int(math.Round(sliderPercent(mouse.X, track) * 255))
		if rl.IsMouseButtonReleased(rl.MouseLeftButton) {
			control.dragging = false
			if control.preview != state.PowerSetting && a.radio.session.SetPower(control.preview) {
				control.powerRequested = now
			}
		}
		if !rl.IsMouseButtonDown(rl.MouseLeftButton) {
			control.dragging = false
		}
	}
	if now.Sub(state.TunerAt) >= 4*time.Second || !control.tunerRequested.IsZero() {
		return
	}
	if clicked(mouse, tunerControl()) {
		var next byte
		if state.Tuner == 0 {
			next = 1
		}
		if a.radio.session.SetTuner(next) {
			control.tunerTarget, control.tunerRequested = next, now
		}
	} else if clicked(mouse, tuneControl()) && a.radio.session.SetTuner(2) {
		control.tunerTarget, control.tunerRequested = 2, now
	}
}

func (a *app) drawOutputControls(state civ.State, online bool) {
	control := &a.radio.output
	now := time.Now()
	drawText("RF POWER", 575, 798, 22, color.text)
	value, level, shade := "-- W", state.PowerSetting, color.muted
	hasPower := online && now.Sub(state.PowerAt) < 4*time.Second
	if control.dragging {
		level, shade = control.preview, color.amber
	} else if hasPower {
		shade = color.text
		if !control.powerRequested.IsZero() {
			shade = color.amber
		}
	}
	if control.dragging || hasPower {
		watts := nominalRFPower(state, level)
		format := "~%.0f W"
		if watts != math.Round(watts) && state.ModeKnown && state.Mode == 2 && state.Frequency >= 70000000 {
			format = "~%.1f W"
		}
		value = fmt.Sprintf(format, watts)
	}
	box := outputPowerValue()
	drawConsolePanel(box, consoleEdge)
	drawConsoleText(semiboldFont, value, box, 22, shade)
	track := outputPowerTrack()
	if hasPower {
		drawMechanicalSlider(track, float64(level)*100/255, color.amber)
	} else {
		rl.DrawRectangleRounded(track, .5, 8, color.panel2)
	}

	label, tuneLabel := "TUNER --", "TUNE"
	known := online && now.Sub(state.TunerAt) < 4*time.Second
	if known {
		label = "TUNER OFF"
		if state.Tuner > 0 {
			label = "TUNER ON"
		}
	}
	tuning := known && state.Tuner == 2
	pending := !control.tunerRequested.IsZero()
	if tuning {
		tuneLabel = "AJUSTANDO"
	} else if pending && control.tunerTarget == 2 {
		tuneLabel = "INICIANDO"
	}
	r := tunerControl()
	if known && state.Tuner != 0 {
		rl.DrawRectangleRounded(r, .18, 8, rl.NewColor(5, 43, 28, 255))
		rl.DrawRectangleRoundedLinesEx(r, .18, 8, 2, color.green)
		drawConsoleText(semiboldFont, label, r, 19, color.green)
	} else {
		drawConsoleButton(r, label, pending && control.tunerTarget != 2, false)
	}
	drawConsoleButton(tuneControl(), tuneLabel, tuning || pending && control.tunerTarget == 2, false)
}
