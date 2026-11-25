package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var (
	splitButton = rl.NewRectangle(437, 148, 96, 35)
	rxDial      = rl.NewRectangle(546, 94, 334, 63)
	txDial      = rl.NewRectangle(546, 161, 334, 36)
	ritButton   = rl.NewRectangle(886, 94, 61, 39)
	deltaButton = rl.NewRectangle(951, 94, 69, 39)
	offsetDial  = rl.NewRectangle(886, 143, 134, 54)
	offsetValue = rl.NewRectangle(889, 145, 128, 22)
	offsetMinus = rl.NewRectangle(889, 169, 40, 25)
	offsetZero  = rl.NewRectangle(933, 169, 40, 25)
	offsetPlus  = rl.NewRectangle(977, 169, 40, 25)
)

const splitDigitScale = .58

func (a *app) updateSplitTuning(mouse rl.Vector2, state civ.State) bool {
	r, t := &a.radio, state.Tuning
	now := time.Now()
	switch {
	case clicked(mouse, splitButton):
		if recentTuning(now, state.SplitAt) {
			r.session.SetSplit(!state.Split)
		}
		return true
	case clicked(mouse, ritButton):
		if recentTuning(now, t.RITAt) {
			r.session.SetIncrementalTuning(false, !t.RIT)
		}
		return true
	case clicked(mouse, deltaButton):
		if recentTuning(now, t.DeltaTXAt) {
			r.session.SetIncrementalTuning(true, !t.DeltaTX)
		}
		return true
	case rl.CheckCollisionPointRec(mouse, offsetDial):
		if recentTuning(now, t.OffsetAt) {
			target := t.Offset
			if !r.offsetSent.IsZero() && (!t.OffsetAt.After(r.offsetSent) || t.Offset != r.offsetTarget) && now.Sub(r.offsetSent) < time.Second {
				target = r.offsetTarget
			}
			wheel := int(rl.GetMouseWheelMove())
			step := 10
			if rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift) {
				step = 100
			}
			switch {
			case clicked(mouse, offsetValue):
				r.editing, r.editOffset, r.input = true, true, ""
				return true
			case clicked(mouse, offsetZero), rl.IsMouseButtonPressed(rl.MouseRightButton):
				target = 0
			case clicked(mouse, offsetMinus):
				target -= step
			case clicked(mouse, offsetPlus):
				target += step
			case wheel != 0:
				target += wheel * step
			default:
				return true
			}
			target = max(-civ.MaxTuningOffset, min(civ.MaxTuningOffset, target))
			r.sendTuningOffset(target, now)
		}
		return true
	case clicked(mouse, txDial) || rl.CheckCollisionPointRec(mouse, txDial) && rl.IsMouseButtonPressed(rl.MouseRightButton):
		_, tx := state.EffectiveTuning(now)
		if state.Split && tx > 0 {
			r.editing, r.editTX, r.input = true, true, ""
		}
		return true
	}
	return false
}

func (r *radioPanel) sendTuningOffset(hz int, now time.Time) bool {
	if !r.session.SetTuningOffset(hz) {
		return false
	}
	r.offsetTarget, r.offsetSent = hz, now
	return true
}

func (a *app) editTuningOffset(available bool) {
	r := &a.radio
	r.collectInput("+-0123456789", 5)
	if !rl.IsKeyPressed(rl.KeyEnter) && !rl.IsKeyPressed(rl.KeyKpEnter) {
		return
	}
	hz, err := strconv.Atoi(r.input)
	if err != nil || hz < -civ.MaxTuningOffset || hz > civ.MaxTuningOffset {
		a.message = "Introduce un valor entre -9999 y +9999 Hz"
		return
	}
	if !available || !r.sendTuningOffset(hz, time.Now()) {
		a.message = "Ajuste no disponible. Comprueba que la emisora esté en RX"
		return
	}
	r.editing, r.editOffset = false, false
}

func recentTuning(now, at time.Time) bool {
	return !at.IsZero() && now.Sub(at) < 3*time.Second
}

func rxTuningFrequency(state civ.State, now time.Time) int64 {
	if rx, _ := state.EffectiveTuning(now); rx > 0 {
		return rx
	}
	return state.Frequency
}

func (r *radioPanel) commitTuning(state civ.State, hz int64) bool {
	if r.editTX {
		_, tx := state.EffectiveTuning(time.Now())
		if !state.Split || tx == 0 {
			return false
		}
		if state.Tuning.DeltaTX {
			hz -= int64(state.Tuning.Offset)
		}
		return r.session.TuneOtherVFO(hz)
	}
	if rx, _ := state.EffectiveTuning(time.Now()); rx > 0 {
		hz -= rx - state.Frequency
	}
	return r.session.Tune(hz)
}

func tuningLabels(state civ.State) (string, string) {
	rx, tx := "RX", "TX"
	if state.VFOKnown {
		rxVFO, txVFO := state.VFO, state.VFO
		if state.Split {
			txVFO ^= 1
		}
		rx += " " + string(rune('A'+rxVFO))
		tx += " " + string(rune('A'+txVFO))
	}
	return rx, tx
}

func drawTuningButton(rect rl.Rectangle, label string, available, active bool, shade rl.Color) {
	drawConsoleButton(rect, label, false, false)
	if available && active {
		rl.DrawRectangleRoundedLinesEx(rect, .16, 8, 2, shade)
		drawConsoleText(semiboldFont, label, rect, 19, shade)
	}
}

func (r *radioPanel) drawSplitTuning(state civ.State, online, transmitting bool, mouse rl.Vector2) {
	now := time.Now()
	t := state.Tuning
	drawTuningButton(splitButton, "SPLIT", online && recentTuning(now, state.SplitAt), state.Split, color.amber)
	drawTuningButton(ritButton, "RIT", online && recentTuning(now, t.RITAt), t.RIT, color.green)
	ritActive := online && recentTuning(now, t.RITAt) && t.RIT
	deltaActive := online && recentTuning(now, t.DeltaTXAt) && t.DeltaTX
	drawTuningButton(deltaButton, "ΔTX", online && recentTuning(now, t.DeltaTXAt), t.DeltaTX, color.red)
	offset := "— Hz"
	offsetAvailable := online && recentTuning(now, t.OffsetAt)
	if offsetAvailable {
		offset = fmt.Sprintf("%+d Hz", t.Offset)
	}
	adjustable := offsetAvailable && state.TXKnown && !transmitting && !state.PowerPending && r.swr.cancel == nil
	shade, edge := color.text, consoleEdge
	switch {
	case ritActive && deltaActive:
		edge = color.green
	case ritActive:
		shade, edge = color.green, color.green
	case deltaActive:
		shade, edge = color.red, color.red
	}
	if r.editOffset {
		offset = r.input + "_ Hz"
		shade = color.text
	}
	drawConsolePanel(offsetDial, edge)
	if ritActive && deltaActive {
		rl.DrawLineEx(rl.NewVector2(offsetDial.X+offsetDial.Width/2, offsetDial.Y), rl.NewVector2(offsetDial.X+offsetDial.Width-5, offsetDial.Y), 2, color.red)
	}
	if adjustable && rl.CheckCollisionPointRec(mouse, offsetValue) || r.editOffset {
		drawConsolePanel(offsetValue, color.amber)
	}
	drawConsoleText(semiboldFont, offset, offsetValue, 19, shade)
	for _, button := range [...]struct {
		rect  rl.Rectangle
		label string
	}{{offsetMinus, "–"}, {offsetZero, "0"}, {offsetPlus, "+"}} {
		shade := color.muted
		if adjustable {
			shade = color.text
			if rl.CheckCollisionPointRec(mouse, button.rect) {
				shade = color.amber
			}
		}
		drawConsolePanel(button.rect, consoleEdge)
		drawConsoleText(semiboldFont, button.label, button.rect, 18, shade)
	}
	rx, exact := r.shownFrequency(state, online, now)
	if online {
		effectiveRX, effectiveTX := state.EffectiveTuning(now)
		if state.TX {
			r.drawTuningFrequencies(state, r.lastRXHz, r.lastTXHz, true)
			return
		}
		r.lastRXHz, r.lastTXHz = effectiveRX, effectiveTX
		r.drawTuningFrequencies(state, rx, effectiveTX, exact)
	} else {
		r.lastRXHz, r.lastTXHz = 0, 0
		r.drawTuningFrequencies(civ.State{}, rx, 0, false)
	}
}

func (r *radioPanel) drawTuningFrequencies(state civ.State, rx, tx int64, exact bool) {
	rxLabel, txLabel := tuningLabels(state)
	drawText(rxLabel, 548, 115, 17, color.green)
	drawText(txLabel, 548, 170, 17, color.amber)
	if r.editing && !r.editTX {
		drawCenteredFitted(r.input+"_", rl.NewRectangle(604, 101, 271, 49), 39, color.text)
	} else if rx > 0 {
		for _, glyph := range splitFrequencyGlyphs(rx) {
			shade := color.green
			if !exact {
				shade = color.muted
			}
			if glyph.step > 0 && glyph.step == r.digitStep {
				shade = color.amber
			}
			drawDialGlyph(glyph, shade, 47)
		}
		drawText("MHz", 842, 118, 13, color.muted)
		if !exact {
			drawText("CENTRO RF", 774, 150, 11, color.muted)
		}
	} else {
		drawCenteredFitted("—", rl.NewRectangle(604, 101, 271, 49), 39, color.muted)
	}
	txText := "—"
	if r.editing && r.editTX {
		txText = r.input + "_"
	} else if tx > 0 {
		txText = fmt.Sprintf("%d.%03d.%03d MHz", tx/1000000, tx/1000%1000, tx%1000)
	}
	drawCenteredFitted(txText, rl.NewRectangle(604, 161, 271, 36), 27, color.amber)
}

func splitFrequencyGlyphs(hz int64) []frequencyGlyph {
	glyphs := frequencyGlyphs(hz)
	for i := range glyphs {
		cell := &glyphs[i].cell
		cell.X = 603 + (cell.X-542)*splitDigitScale
		cell.Width *= splitDigitScale
		cell.Y, cell.Height = 94, 63
	}
	return glyphs
}

func splitDigitAt(hz int64, mouse rl.Vector2) int64 {
	mouse.X = 542 + (mouse.X-603)/splitDigitScale
	mouse.Y = 130
	return frequencyDigitAt(hz, mouse)
}

func drawSplitMarkers(graph, waterfall rl.Rectangle, sweep civ.Sweep, state civ.State, rx, confirmedRX, tx int64) {
	if rx >= sweep.Low && rx <= sweep.High && rx > 0 {
		x := rfFrequencyX(waterfall, sweep, rx)
		rl.DrawLineEx(rl.NewVector2(x, waterfall.Y), rl.NewVector2(x, waterfall.Y+waterfall.Height), 1, color.green)
	}
	if tx <= 0 || tx < sweep.Low || tx > sweep.High || !state.Split && (confirmedRX == 0 || tx == confirmedRX) {
		return
	}
	for _, area := range [...]rl.Rectangle{graph, waterfall} {
		x := rfFrequencyX(area, sweep, tx)
		for y := area.Y; y < area.Y+area.Height; y += 13 {
			rl.DrawLineEx(rl.NewVector2(x, y), rl.NewVector2(x, min(y+8, area.Y+area.Height)), 2, color.amber)
		}
	}
	_, label := tuningLabels(state)
	x := rfFrequencyX(graph, sweep, tx)
	tag := rl.NewRectangle(max(graph.X, min(x-30, graph.X+graph.Width-60)), graph.Y+4, 60, 24)
	drawConsolePanel(tag, color.amber)
	drawConsoleText(semiboldFont, label, tag, 15, color.amber)
}
