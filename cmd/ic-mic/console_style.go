package main

import (
	"fmt"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var consoleEdge = rl.NewColor(75, 75, 82, 255)

func drawCutPanel(r rl.Rectangle, cut float32, fill, edge rl.Color, thickness float32) {
	x, y := r.X, r.Y
	right, bottom := x+r.Width, y+r.Height
	vertices := [...]rl.Vector2{
		{X: x + cut, Y: y}, {X: x, Y: y + cut}, {X: x, Y: bottom},
		{X: right - cut, Y: bottom}, {X: right, Y: bottom - cut}, {X: right, Y: y},
	}
	rl.DrawTriangleFan(vertices[:], fill)
	for i := range vertices {
		rl.DrawLineEx(vertices[i], vertices[(i+1)%len(vertices)], thickness, edge)
	}
}

func drawConsolePanel(r rl.Rectangle, edge rl.Color) {
	radius := min(.24, 24/min(r.Width, r.Height))
	rl.DrawRectangleRounded(r, radius, 10, rl.NewColor(13, 13, 16, 255))
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

func drawAudioProfileButton(rect rl.Rectangle, label string, selected bool, accent rl.Color) {
	fill := mix(color.panel, accent, .17)
	border := mix(color.panel, accent, .70)
	shade := mix(color.text, accent, .45)
	if selected {
		fill = mix(color.panel, accent, .32)
		border, shade = accent, color.text
	}
	drawCutPanel(rect, 6, fill, border, 1.5)
	if selected {
		rl.DrawRectangleRec(rl.NewRectangle(rect.X+9, rect.Y+rect.Height-5, rect.Width-18, 3), accent)
	}
	drawConsoleText(semiboldFont, label, rect, 22, shade)
}

func drawConsoleButtonSize(r rl.Rectangle, label string, active, primary bool, size float32) {
	fill := rl.NewColor(23, 23, 27, 255)
	border := rl.NewColor(92, 92, 99, 255)
	shade := color.text
	if primary {
		border = color.red
	}
	if active {
		border, shade = color.red, color.text
		fill = rl.NewColor(69, 18, 27, 255)
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
	{"P.AMP / ATT", rl.Rectangle{X: 673, Y: 240, Width: 126, Height: 36}, civ.Preamp},
	{"NOTCH", rl.Rectangle{X: 804, Y: 240, Width: 77, Height: 36}, civ.AutoNotch},
	{"NB", rl.Rectangle{X: 886, Y: 240, Width: 43, Height: 36}, civ.NoiseBlanker},
	{"NR", rl.Rectangle{X: 934, Y: 240, Width: 44, Height: 36}, civ.NoiseReduction},
}

func drawVFOButtons(state civ.State, online bool) {
	for i, label := range vfoLabels {
		active := online && state.VFOKnown && int(state.VFO) == i
		if i == 2 {
			active = online && time.Since(state.VFOEqualizedAt) < 950*time.Millisecond
		}
		if !active || i == 2 {
			drawRFOption(vfoControls[i], label, active)
			continue
		}
		shade := color.green
		if i == 1 {
			shade = rl.NewColor(104, 190, 255, 255)
		}
		rect := vfoControls[i]
		rl.DrawRectangleRounded(rect, .22, 8, shade)
		drawConsoleText(semiboldFont, label, rect, 20, color.bg)
	}
}

func drawRFOption(rect rl.Rectangle, label string, active bool) {
	drawRFOptionSize(rect, label, active, 20)
}

func drawRFOptionSize(rect rl.Rectangle, label string, active bool, size float32) {
	if !active {
		drawConsoleButtonSize(rect, label, false, false, size)
		return
	}
	rl.DrawRectangleRounded(rect, .22, 8, rl.White)
	rl.DrawRectangleRoundedLinesEx(rect, .22, 8, 1.6, rl.White)
	drawConsoleText(semiboldFont, label, rect, size, color.bg)
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
		drawRFOption(control.rect, label, active)
	}
}

func (a *app) updateRadioButtons(mouse rl.Vector2, state civ.State) bool {
	for i, rect := range vfoControls {
		if clicked(mouse, rect) {
			a.radio.wheel = wheelTuning{}
			if a.radio.initialVFOPending {
				a.message = "Esperando confirmación de VFO A"
				return true
			}
			if i == 2 {
				a.radio.confirmVFOEqualize = true
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
	accent := color.red
	fill := rl.NewColor(34, 13, 18, 255)
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
		accent, fill = color.red, rl.NewColor(129, 20, 35, 255)
		label, hint = "TX", ""
		if !startedAt.IsZero() {
			hint = time.Since(startedAt).Truncate(time.Second).String()
		}
	}
	if rl.CheckCollisionPointRec(mouse, r) {
		fill = mix(fill, rl.White, .08)
	}
	drawCutPanel(r, 18, fill, accent, 3)
	drawMilitaryCentered(label, rl.NewRectangle(r.X+12, r.Y, 188, r.Height), 67, color.text, 0)
	lineX := r.X + 205
	rl.DrawLineEx(rl.NewVector2(lineX, r.Y+28), rl.NewVector2(lineX, r.Y+r.Height-28), 1, consoleEdge)
	drawConsoleText(semiboldFont, hint, rl.NewRectangle(lineX+10, r.Y, r.Width-225, r.Height), 17, color.text)
}
