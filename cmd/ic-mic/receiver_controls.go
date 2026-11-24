package main

import (
	"fmt"
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var receiverKnobs = [...]struct {
	name    string
	control civ.RXControl
}{
	{"RF GAIN", civ.RFGain}, {"AF GAIN", civ.AFGain}, {"SQL", civ.Squelch},
}

var agcLabels = [...]string{"OFF", "FAST", "MID", "SLOW"}

type receiverControls struct {
	agcOpen, ampOpen, dragging bool
	knob                       int
	origin                     rl.Vector2
	base, preview              int
}

func receiverKnobRect(index int) rl.Rectangle {
	return rl.NewRectangle(155+float32(index)*128, 226, 124, 67)
}

func agcRect() rl.Rectangle { return rl.NewRectangle(544, 240, 120, 36) }
func agcOption(index int) rl.Rectangle {
	return rl.NewRectangle(544, 278+float32(index-1)*35, 120, 34)
}

func (a *app) updateReceiverControls(mouse rl.Vector2, state civ.State, ready bool) bool {
	c := &a.radio.receiver
	if !ready || !rl.IsWindowFocused() {
		*c = receiverControls{}
		return false
	}
	if c.ampOpen {
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			c.ampOpen = false
			for i := range preampOptions {
				if rl.CheckCollisionPointRec(mouse, preampOptionRect(i)) {
					a.setPreampOption(state, i)
					break
				}
			}
		}
		return true
	}
	if c.agcOpen {
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			c.agcOpen = false
			for i := 1; i < len(agcLabels); i++ {
				if rl.CheckCollisionPointRec(mouse, agcOption(i)) {
					a.radio.session.SetRXControl(civ.AGC, byte(i))
					break
				}
			}
		}
		return true
	}
	if c.dragging {
		delta := mouse.X - c.origin.X + c.origin.Y - mouse.Y
		c.preview = max(0, min(255, c.base+int(math.Round(float64(delta)*2))))
		if !rl.IsMouseButtonDown(rl.MouseLeftButton) {
			c.dragging = false
			if c.base != c.preview {
				a.radio.session.SetRXControl(receiverKnobs[c.knob].control, byte(c.preview))
			}
		}
		return true
	}
	if clicked(mouse, agcRect()) {
		c.agcOpen = true
		return true
	}
	for i, knob := range receiverKnobs {
		rect := receiverKnobRect(i)
		if !rl.CheckCollisionPointRec(mouse, rect) {
			continue
		}
		sample := state.RX[knob.control]
		if time.Since(sample.At) >= 5*time.Second {
			if clicked(mouse, rect) {
				a.message = "Esperando el nivel de " + knob.name
			}
			return true
		}
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			value := max(0, min(255, int(sample.Value)+int(math.Round(float64(wheel)*8))))
			a.radio.session.SetRXControl(knob.control, byte(value))
			return true
		}
		if clicked(mouse, rect) {
			c.dragging, c.knob, c.origin = true, i, mouse
			c.base, c.preview = int(sample.Value), int(sample.Value)
			return true
		}
	}
	return false
}

var preampOptions = [...]string{"OFF", "P.AMP 1", "P.AMP 2", "ATT"}

func preampOptionRect(index int) rl.Rectangle {
	button := rfControls[0].rect
	return rl.NewRectangle(button.X, button.Y+button.Height+2+float32(index)*35, button.Width, 34)
}

func (a *app) setPreampOption(state civ.State, option int) {
	preamp, att := state.RX[civ.Preamp], state.RX[civ.Attenuator]
	if time.Since(preamp.At) >= 5*time.Second || time.Since(att.At) >= 5*time.Second {
		a.message = "Esperando estado de P.AMP / ATT"
		return
	}
	radio := a.radio.session
	if option != 3 && att.Value != 0 && !radio.SetRXControl(civ.Attenuator, 0) {
		a.message = "CI-V ocupado. Vuelve a elegir P.AMP / ATT"
		return
	}
	level := byte(option)
	if option == 3 {
		level = 0
	}
	if preamp.Value != level && !radio.SetRXControl(civ.Preamp, level) {
		a.message = "CI-V ocupado. Vuelve a elegir P.AMP / ATT"
		return
	}
	if option == 3 && att.Value == 0 && !radio.SetRXControl(civ.Attenuator, 0x20) {
		a.message = "CI-V ocupado. Vuelve a elegir P.AMP / ATT"
	}
}

func drawPreampMenu(state civ.State) {
	active := int(state.RX[civ.Preamp].Value)
	if state.RX[civ.Attenuator].Value != 0 {
		active = 3
	}
	for i, label := range preampOptions {
		drawRFOptionSize(preampOptionRect(i), label, i == active, 16)
	}
}

func (c receiverControls) draw(state civ.State, online bool) {
	for i, knob := range receiverKnobs {
		rect := receiverKnobRect(i)
		sample := state.RX[knob.control]
		known := online && time.Since(sample.At) < 5*time.Second
		value := int(sample.Value)
		if c.dragging && c.knob == i {
			value, known = c.preview, true
		}
		center := rl.NewVector2(rect.X+rect.Width/2, rect.Y+34)
		rl.DrawCircleV(center, 19, color.panel2)
		rl.DrawRing(center, 17, 19, 135, 405, 28, consoleEdge)
		label := "--"
		if known {
			label = fmt.Sprintf("%d%%", (value*100+127)/255)
			angle := (135 + 270*float64(value)/255) * math.Pi / 180
			tip := rl.NewVector2(center.X+15*float32(math.Cos(angle)), center.Y+15*float32(math.Sin(angle)))
			rl.DrawLineEx(center, tip, 2, color.text)
		}
		drawConsoleText(semiboldFont, knob.name, rl.NewRectangle(rect.X, rect.Y, rect.Width, 15), 15, color.text)
		drawConsoleText(semiboldFont, label, rl.NewRectangle(rect.X, rect.Y+53, rect.Width, 14), 15, color.muted)
	}
	sample := state.RX[civ.AGC]
	label := "AGC --"
	if online && time.Since(sample.At) < 5*time.Second && int(sample.Value) < len(agcLabels) {
		label = "AGC " + agcLabels[sample.Value]
	}
	drawConsoleButtonSize(agcRect(), label+" ▼", c.agcOpen, false, 16)
}
