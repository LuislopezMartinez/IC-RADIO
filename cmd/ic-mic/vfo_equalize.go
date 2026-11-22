package main

import (
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var (
	cancelVFOEqualize = rl.NewRectangle(544, 490, 240, 48)
	acceptVFOEqualize = rl.NewRectangle(812, 490, 240, 48)
)

func (a *app) updateVFOEqualize(mouse rl.Vector2, state civ.State) bool {
	r := &a.radio
	if !r.vfoEqualizeSent.IsZero() {
		switch {
		case state.VFOEqualizedAt.After(r.vfoEqualizeBaseline):
			r.vfoEqualizeSent = time.Time{}
			a.message = "A=B confirmado por la emisora"
		case time.Since(r.vfoEqualizeSent) > 3*time.Second:
			r.vfoEqualizeSent = time.Time{}
			a.message = "La emisora no confirmó A=B"
		}
	}
	if !r.confirmVFOEqualize {
		return false
	}
	if clicked(mouse, cancelVFOEqualize) || rl.IsKeyPressed(rl.KeyEscape) {
		r.confirmVFOEqualize = false
		return true
	}
	if clicked(mouse, acceptVFOEqualize) || rl.IsKeyPressed(rl.KeyEnter) {
		r.confirmVFOEqualize = false
		if r.session == nil || !radioRXAudioReady(state) || !state.TXKnown || a.transmitting() ||
			!r.session.EqualizeVFO() {
			a.message = "A=B no disponible. Comprueba que la emisora esté en RX"
		} else {
			r.vfoEqualizeSent, r.vfoEqualizeBaseline = time.Now(), state.VFOEqualizedAt
			a.message = "Esperando confirmación de A=B"
		}
	}
	return true
}

func drawVFOEqualizeConfirmation() {
	rl.DrawRectangle(0, 72, viewW, viewH-72, rl.NewColor(0, 0, 0, 170))
	panel := rl.NewRectangle(518, 363, 560, 198)
	drawConsolePanel(panel, color.border)
	drawText("Igualar VFO A y B", 544, 392, 26, color.text)
	drawText("¿Quieres enviar la orden A=B a la emisora?", 544, 443, 19, color.muted)
	drawConsoleButton(cancelVFOEqualize, "CANCELAR", false, false)
	drawRFOption(acceptVFOEqualize, "CONFIRMAR", true)
}
