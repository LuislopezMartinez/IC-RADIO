package main

import (
	"fmt"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var consoleEdge = rl.NewColor(75, 159, 178, 255)

func drawConsolePanel(r rl.Rectangle, edge rl.Color) {
	radius := min(.24, 24/min(r.Width, r.Height))
	rl.DrawRectangleRounded(r, radius, 10, rl.NewColor(6, 19, 26, 255))
	rl.DrawRectangleRoundedLinesEx(r, radius, 10, 1.6, edge)
}

func drawConsoleText(font rl.Font, label string, r rl.Rectangle, size float32, shade rl.Color) {
	const spacing = .7
	dim := rl.MeasureTextEx(font, label, size, spacing)
	if dim.X > r.Width-6 {
		size *= (r.Width - 6) / dim.X
		dim = rl.MeasureTextEx(font, label, size, spacing)
	}
	pos := rl.NewVector2(r.X+(r.Width-dim.X)/2, r.Y+(r.Height-dim.Y)/2)
	rl.DrawTextEx(font, label, pos, size, spacing, shade)
}

func drawConsoleButton(r rl.Rectangle, label string, active, primary bool) {
	drawConsoleButtonSize(r, label, active, primary, 20)
}

func drawConsoleButtonSize(r rl.Rectangle, label string, active, primary bool, size float32) {
	fill := rl.NewColor(11, 26, 33, 255)
	border := rl.NewColor(114, 143, 154, 255)
	shade := color.text
	if primary {
		border = color.cyan
	}
	if active {
		border, shade = color.amber, rl.NewColor(255, 224, 160, 255)
		fill = rl.NewColor(63, 38, 9, 255)
	}
	rl.DrawRectangleRounded(r, .22, 8, fill)
	rl.DrawRectangleRoundedLinesEx(r, .22, 8, 1.6, border)
	drawConsoleText(semiboldFont, label, r, size, shade)
}

var vfoLabels = [...]string{"A", "B", "A=B"}
var vfoControls = [...]rl.Rectangle{
	{X: 376, Y: 103, Width: 45, Height: 35},
	{X: 423, Y: 103, Width: 45, Height: 35},
	{X: 475, Y: 103, Width: 57, Height: 35},
}

var rfControls = [...]struct {
	label   string
	rect    rl.Rectangle
	control civ.RXControl
}{
	{"P.AMP / ATT", rl.Rectangle{X: 387, Y: 240, Width: 232, Height: 36}, civ.Preamp},
	{"NOTCH", rl.Rectangle{X: 638, Y: 240, Width: 127, Height: 36}, civ.AutoNotch},
	{"NB", rl.Rectangle{X: 788, Y: 240, Width: 83, Height: 36}, civ.NoiseBlanker},
	{"NR", rl.Rectangle{X: 894, Y: 240, Width: 83, Height: 36}, civ.NoiseReduction},
}

func drawVFOButtons(state civ.State, online bool) {
	for i, label := range vfoLabels {
		active := online && state.VFOKnown && int(state.VFO) == i
		if i == 2 {
			active = online && time.Since(state.VFOEqualizedAt) < 950*time.Millisecond
		}
		drawConsoleButton(vfoControls[i], label, active, false)
	}
}

func drawRXButtons(state civ.State, online bool) {
	for _, control := range rfControls {
		sample := state.RX[control.control]
		known := online && time.Since(sample.At) < 5*time.Second
		label := control.label
		active := known && sample.Value != 0
		if control.control == civ.Preamp {
			att := state.RX[civ.Attenuator]
			switch {
			case !known || time.Since(att.At) >= 5*time.Second:
				label += "   --"
			case att.Value != 0:
				label += "   ATT"
				active = true
			case sample.Value != 0:
				label += fmt.Sprintf("   P%d", sample.Value)
			default:
				label += "   OFF"
			}
		}
		drawConsoleButton(control.rect, label, active, false)
	}
}

func (a *app) updateRadioButtons(mouse rl.Vector2, state civ.State) bool {
	for i, rect := range vfoControls {
		if clicked(mouse, rect) {
			if a.radio.initialVFOPending {
				a.message = "Esperando confirmación de VFO A"
				return true
			}
			if i == 2 {
				a.radio.session.EqualizeVFO()
			} else {
				a.radio.session.SelectVFO(byte(i))
			}
			return true
		}
	}
	for _, control := range rfControls {
		if !clicked(mouse, control.rect) {
			continue
		}
		sample := state.RX[control.control]
		if time.Since(sample.At) >= 5*time.Second {
			a.message = "Esperando estado del control RX"
			return true
		}
		if control.control == civ.Preamp {
			att := state.RX[civ.Attenuator]
			if time.Since(att.At) >= 5*time.Second {
				return true
			}
			switch {
			case att.Value != 0:
				a.radio.session.SetRXControl(civ.Attenuator, 0)
			case sample.Value < 2:
				a.radio.session.SetRXControl(civ.Preamp, sample.Value+1)
			default:
				a.radio.session.SetRXControl(civ.Attenuator, 0x20)
			}
		} else {
			var value byte
			if sample.Value == 0 {
				value = 1
			}
			a.radio.session.SetRXControl(control.control, value)
		}
		return true
	}
	return false
}

func (a *app) drawConsolePTT(mouse rl.Vector2, transmitting bool, startedAt time.Time) {
	r := operationPTT()
	accent := color.cyan
	fill := rl.NewColor(13, 40, 53, 255)
	label := "PTT"
	hint := "MANTENER"
	if a.settings.PTTMode == "lock" {
		hint = "PULSAR"
	}
	shortcut := a.shortcutLabel()
	if shortcut == "SPACE" {
		shortcut = "ESPACIO"
	}
	hint += " · " + shortcut
	if a.settings.RemoteMode && a.radio.remote == nil {
		hint = "CONECTA AL SERVIDOR EN CFG"
	} else if !a.settings.RemoteMode && !a.ptt.Connected() {
		hint = "CONECTA EL PTT EN CFG"
	}
	if transmitting {
		accent, fill = color.red, rl.NewColor(75, 20, 27, 255)
		label, hint = "TX", ""
		if !startedAt.IsZero() {
			hint = time.Since(startedAt).Truncate(time.Second).String()
		}
	}
	if rl.CheckCollisionPointRec(mouse, r) {
		fill = mix(fill, rl.White, .08)
	}
	rl.DrawRectangleRounded(r, .23, 10, fill)
	rl.DrawRectangleRoundedLinesEx(r, .23, 10, 3.5, accent)
	drawMilitaryCentered(label, rl.NewRectangle(r.X+12, r.Y, 188, r.Height), 67, color.text, 0)
	lineX := r.X + 205
	rl.DrawLineEx(rl.NewVector2(lineX, r.Y+28), rl.NewVector2(lineX, r.Y+r.Height-28), 1, consoleEdge)
	drawConsoleText(semiboldFont, hint, rl.NewRectangle(lineX+10, r.Y, r.Width-225, r.Height), 17, color.text)
}
