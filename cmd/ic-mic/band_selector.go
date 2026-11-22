package main

import (
	"fmt"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var radioBands = [...]struct {
	name      string
	code      byte
	low, high int64
}{
	{"1.8", 0x01, 1800000, 1999999},
	{"3.5", 0x02, 3400000, 4099999},
	{"7", 0x03, 6900000, 7499999},
	{"10", 0x04, 9900000, 10499999},
	{"14", 0x05, 13900000, 14499999},
	{"18", 0x06, 17900000, 18499999},
	{"21", 0x07, 20900000, 21499999},
	{"24", 0x08, 24400000, 25099999},
	{"28", 0x09, 28000000, 29999999},
	{"50", 0x10, 50000000, 54000000},
	{"GENE", 0x11, 0, 0},
}

func bandAtFrequency(hz int64) int {
	if hz <= 0 {
		return -1
	}
	for i, band := range radioBands {
		if hz >= band.low && hz <= band.high {
			return i
		}
	}
	return len(radioBands) - 1
}

type bandSelector struct {
	open      bool
	band      byte
	pressed   byte
	pressAt   time.Time
	register  byte
	requestAt time.Time
	tuneAt    time.Time
}

func bandControl() rl.Rectangle { return rl.NewRectangle(24, 240, 126, 36) }
func bandPanel() rl.Rectangle   { return rl.NewRectangle(228, 289, 527, 240) }

func bandKey(index int) rl.Rectangle {
	return rl.NewRectangle(247+float32(index%4)*124, 345+float32(index/4)*54, 114, 46)
}

func (a *app) updateBandSelector(mouse rl.Vector2, state civ.State, online bool) bool {
	r := &a.radio
	s := &r.bands
	available := online && state.TXKnown && !state.TX && !a.ptt.TX() && !a.keyPTTActive && !a.mousePTTActive
	now := time.Now()
	if !s.requestAt.IsZero() {
		if !available || now.Sub(s.requestAt) > 3*time.Second {
			a.message = "El Icom no ha confirmado el cambio de banda"
			*s = bandSelector{}
			return true
		}
		memory := state.Band
		if memory.At.After(s.requestAt) && memory.Band == s.band && memory.Register == s.register {
			if s.tuneAt.IsZero() && r.session.Tune(memory.Frequency) {
				s.tuneAt = now
			} else if !s.tuneAt.IsZero() && state.FrequencyAt.After(s.tuneAt) && state.Frequency == memory.Frequency &&
				r.session.RestoreBandMode(memory) {
				r.selectedBand = s.band
				a.message = fmt.Sprintf("BAND %s - Registro %d", radioBands[bandAtFrequency(memory.Frequency)].name, s.register)
				*s = bandSelector{}
			}
		}
		return true
	}
	if clicked(mouse, bandControl()) {
		if s.open {
			*s = bandSelector{}
		} else if available {
			s.open = true
		} else {
			a.message = "BAND disponible con el Icom conectado en RX"
		}
		return true
	}
	if !s.open {
		return false
	}
	if !available {
		*s = bandSelector{}
		return true
	}
	if s.band != 0 {
		for i := range 3 {
			if clicked(mouse, bandKey(i)) {
				a.requestBandMemory(s.band, byte(i+1), now)
				return true
			}
		}
		if clicked(mouse, bandKey(3)) {
			s.band = 0
		} else if rl.IsMouseButtonPressed(rl.MouseLeftButton) && !rl.CheckCollisionPointRec(mouse, bandPanel()) {
			*s = bandSelector{}
		}
		return true
	}
	if s.pressed != 0 {
		if rl.IsMouseButtonDown(rl.MouseLeftButton) && now.Sub(s.pressAt) >= time.Second {
			s.band, s.pressed = s.pressed, 0
		} else if rl.IsMouseButtonReleased(rl.MouseLeftButton) {
			code := s.pressed
			s.pressed = 0
			for i, band := range radioBands {
				if code == band.code && rl.CheckCollisionPointRec(mouse, bandKey(i)) {
					a.requestBandMemory(code, 1, now)
					break
				}
			}
		} else if !rl.IsMouseButtonDown(rl.MouseLeftButton) {
			s.pressed = 0
		}
		return true
	}
	for i, band := range radioBands {
		if clicked(mouse, bandKey(i)) {
			s.pressed, s.pressAt = band.code, now
			return true
		}
	}
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && !rl.CheckCollisionPointRec(mouse, bandPanel()) {
		*s = bandSelector{}
	}
	return true
}

func (a *app) requestBandMemory(band, register byte, now time.Time) {
	r := &a.radio
	if r.session.ReadBandMemory(band, register) {
		r.bands = bandSelector{band: band, register: register, requestAt: now}
		r.wheel = wheelTuning{}
		a.message = "Leyendo el registro de banda del Icom"
	} else {
		a.message = "CI-V ocupado. Vuelve a elegir la banda"
	}
}

func (a *app) drawBandButton(state civ.State, online bool) {
	label := "BAND"
	if i := a.radio.coverageBand(state.Frequency); online && i >= 0 && time.Since(state.FrequencyAt) < 2*time.Second {
		label += " " + radioBands[i].name
	}
	drawConsoleButton(bandControl(), label+"  ▼", a.radio.bands.open, false)
}

func (a *app) drawBandPanel(state civ.State) {
	s := a.radio.bands
	if !s.open {
		return
	}
	drawConsolePanel(bandPanel(), color.signal)
	if s.band != 0 {
		drawText("REGISTROS DEL ICOM", 247, 306, 22, color.text)
		for i := range 3 {
			drawConsoleButton(bandKey(i), fmt.Sprintf("REG %d", i+1), false, false)
		}
		drawConsoleButton(bandKey(3), "VOLVER", false, false)
		return
	}
	drawText("BAND", 247, 306, 23, color.text)
	drawText("Mantener para elegir registro", 450, 310, 15, color.muted)
	current := a.radio.coverageBand(state.Frequency)
	for i, band := range radioBands {
		drawConsoleButton(bandKey(i), band.name, i == current || s.pressed == band.code, false)
	}
}
