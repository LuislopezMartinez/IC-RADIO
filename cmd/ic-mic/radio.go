package main

import (
	"fmt"
	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	rl "github.com/gen2brain/raylib-go/raylib"
	"math"
	"strconv"
	"strings"
	"time"
)

type radioPage int

const (
	pageOperation radioPage = iota
	pageAudio
	pageConfig
	waterfallRows = 120
)

var scopeSpans = [...]int64{2500, 5000, 10000, 25000, 50000, 100000, 250000, 500000}
var tuningSteps = [...]int64{100, 1000, 10, 1}
var radioModes = [...]string{"LSB", "USB", "AM", "CW", "RTTY", "FM", "WFM", "CW-R", "RTTY-R"}
var radioMeters = [...]struct {
	label string
	code  byte
}{{"S / Po", 0}, {"S", 2}, {"Po", 0x11}, {"SWR", 0x12}, {"ALC", 0x13}, {"COMP", 0x14}, {"Vd", 0x15}, {"Id", 0x16}}

type radioPanel struct {
	txMeters                              txMeterDisplay
	initialVFOPending                     bool
	initialVFOAttempt, initialVFOBaseline time.Time
	output                                outputControls
	swr                                   swrPanel
	bands                                 bandSelector
	receiver                              receiverControls
	cluster                               clusterPanel
	confirmVFOEqualize                    bool
	vfoEqualizeSent, vfoEqualizeBaseline  time.Time
	selectedBand                          byte
	page                                  radioPage
	session                               remote.Radio
	remote                                *remote.Client
	remoteEdit                            int
	remotePassword                        string
	rxAudio                               remoteRXPlayback
	txAudio                               remoteTX
	step, meter                           int
	digitStep                             int64
	wheel                                 wheelTuning
	showPeak                              bool
	zoom                                  rfVerticalZoom
	input                                 string
	editing, modeMenu, cancelled          bool
	editTX                                bool
	editOffset                            bool
	editFilterWidth                       bool
	ifFilter                              radioFilterPanel
	offsetSent                            time.Time
	offsetTarget                          int
	lastRXHz, lastTXHz                    int64
	texture                               rl.Texture2D
	pixels                                []rl.Color
	lastSweep                             civ.Sweep
	trace                                 [civ.Samples]rl.Vector2
	fill                                  [2 * civ.Samples]rl.Vector2
	fillGradient                          rfFillGradient
	peakTrace                             [civ.Samples]rl.Vector2
	envelope                              rfEnvelope
}

func (r *radioPanel) unload() {
	r.cluster.unload()
	r.fillGradient.unload()
	if r.texture.ID != 0 {
		rl.UnloadTexture(r.texture)
	}
}

func (r *radioPanel) cancelInput() {
	r.wheel = wheelTuning{}
	r.receiver = receiverControls{}
	r.cluster.closeMenus()
	r.confirmVFOEqualize = false
	r.rxAudio.menu = 0
	if r.remoteEdit == editRemotePassword {
		r.input = ""
	}
	r.remoteEdit = 0
	r.cancelled = r.editing
	r.editing, r.modeMenu = false, false
	r.editOffset, r.editTX = false, false
	r.editFilterWidth = false
	r.ifFilter = radioFilterPanel{}
	r.bands = bandSelector{}
	r.output.dragging = false
}

func (r *radioPanel) snapshot() (civ.State, bool) {
	if r.session == nil {
		return civ.State{}, false
	}
	state := r.session.Snapshot()
	return state, time.Since(state.LastReply) < 2*time.Second && state.Error == ""
}

func navRect(index int) rl.Rectangle {
	return [...]rl.Rectangle{{X: 558, Y: 11, Width: 194, Height: 43}, {X: 754, Y: 11, Width: 163, Height: 43}, {X: 919, Y: 11, Width: 132, Height: 43}}[index]
}
func audioProfileRect(index int) rl.Rectangle {
	return rl.NewRectangle(31+float32(index)*78, 93, 72, 62)
}
func audioDeviceRect(index int) rl.Rectangle {
	if index == 0 {
		return rl.NewRectangle(210, 92, 617, 64)
	}
	return rl.NewRectangle(855, 92, 715, 64)
}
func frequencyRect() rl.Rectangle { return rxDial }
func modeRect() rl.Rectangle      { return rl.NewRectangle(139, 118, 144, 54) }
func stepRect() rl.Rectangle      { return rl.NewRectangle(369, 148, 64, 35) }
func modeOption(index int) rl.Rectangle {
	return rl.NewRectangle(44+float32(index%3)*108, 187+float32(index/3)*42, 101, 37)
}
func spanRect() rl.Rectangle { return rl.NewRectangle(1390, 240, 189, 36) }
func peakRect() rl.Rectangle { return rl.NewRectangle(1280, 240, 88, 36) }
func rfGraph() rl.Rectangle  { return rl.NewRectangle(99, 295, 1480, 226) }
func rfWaterfall() rl.Rectangle {
	graph := rfGraph()
	return rl.NewRectangle(graph.X, 574, graph.Width, 173)
}
func operationPTT() rl.Rectangle     { return rl.NewRectangle(30, 790, 505, 86) }
func operationMonitor() rl.Rectangle { return rl.NewRectangle(1256, 807, 121, 48) }
func operationReset() rl.Rectangle   { return rl.NewRectangle(1395, 807, 175, 48) }
func meterRect() rl.Rectangle        { return rl.NewRectangle(1033, 98, 536, 102) }

func connectionRect() rl.Rectangle { return rl.NewRectangle(1385, 13, 194, 42) }
func radioPowerRect() rl.Rectangle { return rl.NewRectangle(26, 11, 43, 43) }

func (a *app) updateNavigation(mouse rl.Vector2) bool {
	if clicked(mouse, radioPowerRect()) {
		a.toggleRadioPower()
		return true
	}
	if clicked(mouse, connectionRect()) {
		if a.radio.remote != nil || a.ptt.Connected() {
			a.disconnectRadio()
		} else if a.settings.RemoteMode {
			a.connectRemote()
		} else {
			a.connectFromHeader()
		}
		return true
	}
	for i := range 3 {
		if !clicked(mouse, navRect(i)) {
			continue
		}
		if radioPage(i) == a.radio.page {
			return true
		}
		if a.mousePTTActive || a.keyPTTActive || a.radio.txAudio.active.Load() {
			if a.settings.RemoteMode {
				a.stopRemoteTX()
			} else {
				a.setTX(false)
			}
			a.mousePTTActive, a.keyPTTActive = false, false
		}
		a.txGainDrag = false
		a.openDeviceMenu = -1
		a.radio.cancelInput()
		a.radio.page = radioPage(i)
		return true
	}
	return false
}

func (a *app) toggleRadioPower() {
	if a.transmitting() || a.radio.swr.cancel != nil {
		a.message = "Espera a que la emisora vuelva a recepción"
		return
	}
	if a.settings.RemoteMode && a.radio.remote == nil {
		if a.connectRemote() {
			a.radio.remote.EnsureRadioOn()
		}
		return
	}
	state, online := a.radio.snapshot()
	if state.PowerPending {
		return
	}
	if !a.settings.RemoteMode && (a.radio.session == nil || state.Error != "") {
		if a.settings.COM == "" {
			a.message = "Selecciona el puerto del Icom en CFG"
			a.radio.page = pageConfig
			a.radio.cancelInput()
			return
		}
		var connected bool
		if a.ptt.Connected() && state.Error == "" {
			connected = a.startRadioCIV()
		} else {
			connected = a.openRadioPort(a.settings.COM, true)
		}
		if !connected {
			a.message += ". Revisa el puerto USB en CFG"
			a.radio.page = pageConfig
			a.radio.cancelInput()
			return
		}
		state, online = civ.State{}, false
	}
	var accepted bool
	switch {
	case online:
		accepted = a.radio.session.SetRadioPower(false)
	case state.PoweredOff:
		accepted = a.radio.session.SetRadioPower(true)
	default:
		accepted = a.radio.session.EnsureRadioOn()
	}
	if !accepted {
		a.message = "El control de encendido no está disponible"
	} else {
		a.localRXAudio.enabled.Store(false)
		if !online {
			a.radio.requestInitialVFO()
		}
	}
}

func drawRadioPower(state civ.State, online bool) {
	shade := rl.NewColor(157, 165, 170, 255)
	switch {
	case state.PowerPending:
		shade = rl.NewColor(255, 198, 75, 255)
	case online:
		shade = color.green
	case state.PoweredOff:
		shade = color.red
	}
	rect := radioPowerRect()
	rl.DrawRectangleRounded(rect, .24, 8, rl.NewColor(18, 18, 21, 255))
	rl.DrawRectangleRoundedLinesEx(rect, .24, 8, 2, shade)
	center := rl.NewVector2(rect.X+rect.Width/2, rect.Y+rect.Height/2)
	rl.DrawRing(center, 8, 11, -54, 234, 32, shade)
	rl.DrawLineEx(rl.NewVector2(center.X, center.Y-13), rl.NewVector2(center.X, center.Y-2), 3, shade)
}

func (a *app) drawNavigation() {
	radioState, online := a.radio.snapshot()
	drawRadioPower(radioState, online)
	drawMilitaryText("IC-RADIO", 83, 7, 50, color.text)
	drawText("by EA3IWW", 336, 24, 21, color.muted)
	for i, name := range []string{"OPERACIÓN", "AUDIO", "CFG"} {
		rect := navRect(i)
		fill, edge := color.panel2, consoleEdge
		if radioPage(i) == a.radio.page {
			fill, edge = rl.NewColor(126, 20, 33, 255), color.red
			if a.radio.page == pageOperation {
				fill, edge = rl.NewColor(24, 45, 23, 255), color.green
			}
		}
		drawCutPanel(rect, 9, fill, edge, 1.5)
		drawConsoleText(semiboldFont, name, rect, 23, color.text)
	}
	port := a.settings.COM
	if port == "" {
		port = "USB"
	}
	route := "IC-7300  ·  " + port
	if a.settings.RemoteMode {
		route = "REMOTO"
	}
	drawConsoleText(semiboldFont, route, rl.NewRectangle(1110, 12, 227, 44), 23, color.muted)
	for _, x := range []int32{507, 1094} {
		rl.DrawLine(x, 13, x, 55, consoleEdge)
	}
	state, shade, fill := "DESCONECTADO", color.red, rl.NewColor(52, 13, 19, 255)
	if online || a.ptt.Connected() && a.radio.session == nil && a.settings.DisableCIV {
		state, shade, fill = "CONECTADO", color.green, rl.NewColor(6, 37, 16, 255)
	} else if a.ptt.Connected() {
		state = "COM SIN CI-V"
	}
	if a.radio.remote != nil && !online {
		state = "REMOTO · SIN CI-V"
		if radioState.Error == remote.PasswordError {
			state = "CLAVE INCORRECTA"
		} else if radioState.Error != "" {
			state = "SIN RESPUESTA"
		}
	}
	switch {
	case radioState.PowerPending:
		state, shade, fill = "ESPERANDO RADIO", rl.NewColor(255, 198, 75, 255), rl.NewColor(48, 34, 12, 255)
	case radioState.PowerError != "":
		state, shade, fill = "ORDEN FALLIDA", color.red, rl.NewColor(52, 13, 19, 255)
	case radioState.PoweredOff:
		state = "RADIO APAGADA"
	}
	rect := connectionRect()
	rl.DrawRectangleRounded(rect, .22, 8, fill)
	rl.DrawRectangleRoundedLinesEx(rect, .22, 8, 2, shade)
	rl.DrawCircle(1407, 34, 6, shade)
	drawConsoleText(semiboldFont, state, rl.NewRectangle(1415, 13, 153, 42), 18, shade)
}

func (a *app) connectFromHeader() {
	if a.settings.COM != "" {
		a.ports = ptt.Ports()
		for i, port := range a.ports {
			if port.Name == a.settings.COM {
				a.comIndex = i
				a.connectPTT()
				return
			}
		}
		a.comIndex = 0
		a.message = "Puerto " + a.settings.COM + " no disponible. Revisa CFG"
	} else {
		a.message = "Selecciona el puerto del Icom en CFG"
	}
	a.radio.page = pageConfig
	a.radio.cancelInput()
}

func (a *app) disconnectRadio() {
	a.localRXAudio.enabled.Store(false)
	if a.radio.remote != nil {
		a.stopRemoteTX()
		a.stopRemoteAudio()
		a.radio.remote.Close()
		a.radio.remote = nil
	}
	if a.ptt.TX() {
		a.signalEnding = true
		a.setTX(false)
	}
	a.ptt.Close()
	a.signalEnding = false
	a.keyPTTActive, a.mousePTTActive = false, false
	a.radio.session = nil
	a.radio.vfoEqualizeSent = time.Time{}
	a.radio.selectedBand = 0
	a.radio.initialVFOPending = false
	a.radio.cancelInput()
	a.message = "Puerto desconectado"
}

func (r *radioPanel) requestInitialVFO() {
	r.initialVFOPending = true
	r.initialVFOAttempt, r.initialVFOBaseline = time.Time{}, time.Time{}
}

func (a *app) applyInitialVFO() {
	r := &a.radio
	if r.session == nil {
		return
	}
	state, online := r.snapshot()
	if state.PoweredOff && !r.initialVFOPending {
		r.requestInitialVFO()
	}
	if !r.initialVFOPending || !online || state.PowerPending || !state.TXKnown || state.TX || a.transmitting() || state.Tuner == 2 {
		return
	}
	if !r.initialVFOAttempt.IsZero() && state.VFOKnown && state.VFO == 0 && state.VFOAt.After(r.initialVFOBaseline) {
		if !state.FrequencyAt.Before(state.VFOAt) {
			r.initialVFOPending = false
		}
		return
	}
	if time.Since(r.initialVFOAttempt) >= time.Second && r.session.SelectVFO(0) {
		r.initialVFOAttempt, r.initialVFOBaseline = time.Now(), state.VFOAt
	}
}

func (a *app) civAddress() byte {
	value, err := strconv.ParseUint(a.settings.CIVAddress, 16, 8)
	if err != nil || value == 0 || value >= 0xe0 {
		return 0x94
	}
	return byte(value)
}

func (a *app) updateRadioConfig(mouse rl.Vector2) {
	r := &a.radio
	if a.updateConnectionMode(mouse) {
		return
	}
	if r.cancelled {
		r.cancelled = false
		return
	}
	if r.editing {
		r.collectInput("0123456789ABCDEF", 2)
		if rl.IsKeyPressed(rl.KeyEnter) {
			value, err := strconv.ParseUint(r.input, 16, 8)
			if err == nil && value > 0 && value < 0xe0 {
				a.settings.CIVAddress = fmt.Sprintf("%02X", value)
				r.editing = false
				a.save()
			} else {
				a.message = "Dirección CI-V fuera de rango (01–DF)"
			}
		}
		return
	}
	if a.ptt.Connected() {
		if clicked(mouse, configControl(710, 270)) {
			a.disconnectRadio()
		}
	} else {
		if clicked(mouse, configControl(50, 270)) && len(a.ports) > 0 {
			a.comIndex = (a.comIndex + 1) % len(a.ports)
		}
		if clicked(mouse, configControl(270, 270)) {
			a.ports = ptt.Ports()
			a.comIndex = 0
			for i, port := range a.ports {
				if port.Name == a.settings.COM {
					a.comIndex = i
				}
			}
		}
		if clicked(mouse, configControl(490, 270)) {
			if a.line == ptt.RTS {
				a.line = ptt.DTR
			} else {
				a.line = ptt.RTS
			}
		}
		if clicked(mouse, configControl(710, 270)) {
			a.connectPTT()
		}
		if clicked(mouse, configControl(50, 183)) {
			r.editing, r.input = true, fmt.Sprintf("%02X", a.civAddress())
		}
		if clicked(mouse, configControl(270, 183)) {
			a.settings.DisableCIV = !a.settings.DisableCIV
			a.save()
		}
	}
	if clicked(mouse, configControl(930, 270)) {
		a.pttSettingsOpen = true
		a.capturingPTTKey = false
	}
	if clicked(mouse, configControl(1150, 270)) {
		a.settings.AlwaysOnTop = !a.settings.AlwaysOnTop
		if a.settings.AlwaysOnTop {
			rl.SetWindowState(rl.FlagWindowTopmost)
		} else {
			rl.ClearWindowState(rl.FlagWindowTopmost)
		}
		a.save()
	}
	if clicked(mouse, configControl(50, 337)) {
		a.signalEditorOpen = !a.signalEditorOpen
		a.signalSelected, a.signalDrag = -1, -1
		return
	}
	if a.signalEditorOpen {
		a.updateSignalEditor(mouse)
	}
}

func configControl(x, y float32) rl.Rectangle { return rl.NewRectangle(x, y, 205, 44) }

func (a *app) drawRadioConfig(mouse rl.Vector2) {
	if a.settings.RemoteMode {
		a.drawRemoteConfig()
		return
	}
	drawDarkDisplay(rl.NewRectangle(30, 96, 1540, 282))
	a.drawConnectionMode()
	drawText("CONEXIÓN IC-7300", 50, 114, 28, color.text)
	drawText("CI-V USB 115200 · Unlink from [REMOTE]", 50, 152, 19, color.muted)
	address := fmt.Sprintf("%02X", a.civAddress())
	if a.radio.editing {
		address = a.radio.input + "_"
	}
	drawButton(configControl(50, 183), "CI-V  "+address+"h", a.radio.editing)
	drawButton(configControl(270, 183), "CI-V USB", !a.settings.DisableCIV)
	drawText("VFO INICIAL  A", 500, 193, 21, color.text)
	port := "SIN PUERTOS"
	if len(a.ports) > 0 {
		port = a.ports[a.comIndex].Name
	}
	drawButton(configControl(50, 270), port, false)
	drawButton(configControl(270, 270), "BUSCAR PUERTOS", false)
	drawButton(configControl(490, 270), "PTT  "+string(a.line), false)
	label := "CONECTAR"
	if a.ptt.Connected() {
		label = "DESCONECTAR"
	}
	drawButton(configControl(710, 270), label, a.ptt.Connected())
	drawButton(configControl(930, 270), "AJUSTES PTT", false)
	drawButton(configControl(1150, 270), "SIEMPRE VISIBLE", a.settings.AlwaysOnTop)
	drawButton(configControl(50, 337), "SONIC SEQUENCER", a.signalEditorOpen)
	drawText(ellipsis(a.message, 78), 720, 198, 17, color.muted)
	if a.signalEditorOpen {
		a.drawSignalEditor(mouse)
	}
}

func (r *radioPanel) collectInput(allowed string, limit int) {
	for key := rl.GetCharPressed(); key != 0; key = rl.GetCharPressed() {
		char := strings.ToUpper(string(rune(key)))
		if len(r.input) < limit && strings.Contains(allowed, char) {
			r.input += char
		}
	}
	if rl.IsKeyPressed(rl.KeyBackspace) && len(r.input) > 0 {
		r.input = r.input[:len(r.input)-1]
	}
}

func (a *app) updateOperation(mouse rl.Vector2) {
	r := &a.radio
	if r.cancelled {
		r.cancelled = false
		return
	}
	state, online := r.snapshot()
	canTune := online && state.TXKnown && !state.TX && !state.PowerPending
	canAdjustFilter := canTune && !a.transmitting() && r.swr.cancel == nil
	defer func() {
		if !canTune || a.transmitting() || r.editing || r.modeMenu || r.bands.open || !r.bands.requestAt.IsZero() {
			r.wheel = wheelTuning{}
		} else if r.wheel.target != 0 {
			display := state
			display.Frequency = rxTuningFrequency(state, time.Now())
			if r.wheel.update(time.Now(), display, func(hz int64) bool { return r.commitTuning(state, hz) }) {
				a.message = "La emisora no ha confirmado la frecuencia solicitada"
			}
		}
	}()
	if !r.editing && r.ifFilter.open && rl.CheckCollisionPointRec(mouse, filterMenu) &&
		a.updateRadioFilter(mouse, state, canAdjustFilter) {
		r.wheel = wheelTuning{}
		return
	}
	if r.ifFilter.drag.active && a.updateFilterEdge(mouse, state, canAdjustFilter) {
		r.wheel = wheelTuning{}
		return
	}
	if rl.IsWindowFocused() && !a.transmitting() && !state.TX && !r.editing && !r.modeMenu && !r.bands.open && !r.ifFilter.open &&
		r.bands.requestAt.IsZero() && !r.receiver.ampOpen && !r.receiver.agcOpen && !r.receiver.dragging &&
		a.updateCluster(mouse, state, online, canAdjustFilter) {
		r.wheel = wheelTuning{}
		return
	}
	if a.updateReceiverControls(mouse, state, online && state.TXKnown && !state.TX && !state.PowerPending &&
		!a.transmitting() && !r.editing && !r.modeMenu && !r.bands.open && r.bands.requestAt.IsZero()) {
		return
	}
	if !r.editing && !r.modeMenu && a.updateBandSelector(mouse, state, online) {
		return
	}
	if r.editing {
		if r.editOffset {
			a.editTuningOffset(canTune && !a.transmitting() && r.swr.cancel == nil)
			return
		}
		if r.editFilterWidth {
			a.editRadioFilterWidth(state, canAdjustFilter)
			return
		}
		r.collectInput("0123456789.,", 12)
		if rl.IsKeyPressed(rl.KeyEnter) {
			text := strings.ReplaceAll(r.input, ",", ".")
			if strings.Count(text, ".") == 2 {
				text = strings.ReplaceAll(text, ".", "")
			}
			value, err := strconv.ParseFloat(text, 64)
			if strings.Contains(text, ".") {
				value *= 1000000
			}
			if err == nil && canTune && !a.transmitting() && r.swr.cancel == nil && r.commitTuning(state, int64(math.Round(value))) {
				r.editing, r.editTX = false, false
			} else {
				a.message = "Frecuencia no válida o sintonía no disponible"
			}
		}
		return
	}
	if r.modeMenu {
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			for i := range radioModes {
				if i != 6 && online && state.TXKnown && !state.TX && !a.ptt.TX() && clicked(mouse, modeOption(i)) {
					r.session.SetMode(byte(i))
				}
			}
			r.modeMenu = false
		}
		return
	}
	if a.settings.PTTMode == "hold" {
		if clicked(mouse, operationPTT()) && !a.transmitting() {
			a.mousePTTActive = a.setTX(true)
		}
		if a.mousePTTActive && (!rl.IsMouseButtonDown(rl.MouseLeftButton) || !rl.IsWindowFocused()) {
			a.setTX(false)
			a.mousePTTActive = false
		}
	} else if clicked(mouse, operationPTT()) {
		a.toggleTX()
	}
	a.updateOutputControls(mouse, state, online)
	if clicked(mouse, operationMonitor()) {
		a.toggleOutputMonitor()
	}
	if clicked(mouse, operationReset()) {
		a.txSignal.resetSafety()
		a.message = "Protección TX restablecida"
	}
	if a.transmitting() || online && state.TX {
		return
	}
	if clicked(mouse, meterRect()) {
		r.meter = (r.meter + 1) % len(radioMeters)
	}
	if r.session != nil {
		r.session.SelectMeter(radioMeters[r.meter].code)
	}
	if clicked(mouse, stepRect()) {
		if r.digitStep == 0 {
			r.step = (r.step + 1) % len(tuningSteps)
		}
		r.digitStep = 0
		r.wheel = wheelTuning{}
	}
	if r.zoom.update(mouse) {
		return
	}
	if clicked(mouse, peakRect()) {
		r.showPeak = !r.showPeak
		for i, raw := range state.Sweep.Levels {
			r.envelope.peakLevels[i] = float32(raw)
		}
	}
	if clicked(mouse, spanRect()) {
		a.changeScopeSpan(state, online)
		return
	}
	if !online || state.TX || a.ptt.TX() || !state.TXKnown {
		return
	}
	if a.updateRadioButtons(mouse, state) {
		return
	}
	if canTune && r.swr.cancel == nil && a.updateSplitTuning(mouse, state) {
		return
	}
	if a.updateRadioFilter(mouse, state, canAdjustFilter) {
		r.wheel = wheelTuning{}
		return
	}
	if a.updateFilterEdge(mouse, state, canAdjustFilter) {
		r.wheel = wheelTuning{}
		return
	}
	if rl.CheckCollisionPointRec(mouse, frequencyRect()) && !a.keyPTTActive {
		if rl.IsMouseButtonPressed(rl.MouseRightButton) {
			r.editing, r.editTX, r.input = true, false, ""
			return
		}
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
			shown, _ := r.shownFrequency(state, online, time.Now())
			if digit := splitDigitAt(shown, mouse); digit != 0 {
				r.digitStep = digit
			} else {
				r.editing, r.editTX, r.input = true, false, ""
				return
			}
		}
	}
	if clicked(mouse, modeRect()) && !a.keyPTTActive {
		r.modeMenu = true
	}
	step := tuningSteps[r.step]
	graph, waterfall := rfGraph(), rfWaterfall()
	sweep := state.Sweep
	if time.Since(sweep.At) < 2*time.Second && sweep.High > sweep.Low && (clicked(mouse, graph) || clicked(mouse, waterfall)) {
		r.wheel = wheelTuning{}
		tuneArea := graph
		if rl.CheckCollisionPointRec(mouse, waterfall) {
			tuneArea = waterfall
		}
		clickStep := operationTuningStep(r.digitStep, step, true)
		r.commitTuning(state, quantizeFrequency(rfFrequency(tuneArea, mouse.X, sweep), clickStep))
	}
	wheelStep := operationTuningStep(r.digitStep, step,
		rl.CheckCollisionPointRec(mouse, frequencyRect()) || rl.CheckCollisionPointRec(mouse, graph))
	if delta := rl.GetMouseWheelMove(); canTune && wheelStep != 0 && delta != 0 && state.Frequency > 0 {
		r.wheel.queue(time.Now(), rxTuningFrequency(state, time.Now()), wheelStep, float64(delta))
	}
}

func (a *app) changeScopeSpan(state civ.State, online bool) {
	sweep := state.Sweep
	if !online || time.Since(sweep.At) >= 2*time.Second || sweep.High <= sweep.Low {
		a.message = "SPAN necesita un barrido RF activo"
		return
	}
	if state.TX || a.ptt.TX() {
		a.message = "SPAN disponible en recepcion"
		return
	}
	if sweep.Mode == 1 || sweep.Mode == 3 {
		if a.radio.session.SetScopeCenter() {
			a.message = "Cambiando a CENTER. Pulsa SPAN de nuevo para ajustar el ancho"
		} else {
			a.message = "CI-V ocupado. Vuelve a pulsar SPAN"
		}
		return
	}
	next := nextScopeSpan((sweep.High - sweep.Low) / 2)
	if a.radio.session.SetSpan(next) {
		a.message = fmt.Sprintf("SPAN solicitado: ±%g kHz", float64(next)/1000)
	} else {
		a.message = "CI-V ocupado. Vuelve a pulsar SPAN"
	}
}

func nextScopeSpan(current int64) int64 {
	for _, span := range scopeSpans {
		if span > current {
			return span
		}
	}
	return scopeSpans[0]
}

func rfFrequency(graph rl.Rectangle, x float32, sweep civ.Sweep) int64 {
	fraction := math.Max(0, math.Min(1, float64((x-graph.X)/graph.Width)))
	return sweep.Low + int64(math.Round(fraction*float64(sweep.High-sweep.Low)))
}

func quantizeFrequency(hz, step int64) int64 { return (hz + step/2) / step * step }

func (a *app) drawOperation(mouse rl.Vector2) {
	r := &a.radio
	var state civ.State
	var peaks [civ.Samples]byte
	if r.session != nil {
		state, peaks = r.session.TakeDisplaySnapshot()
	}
	online := time.Since(state.LastReply) < 2*time.Second && state.Error == ""
	drawConsolePanel(rl.NewRectangle(7, 79, 1586, 132), consoleEdge)
	for _, x := range []int32{304, 537, 1028} {
		rl.DrawLine(x, 99, x, 194, consoleEdge)
	}
	status, shade := "—", color.muted
	if online && state.TXKnown {
		status, shade = "RX", color.green
	}
	transmitting := a.transmitting() || online && state.TX
	if transmitting {
		status, shade = "TX", color.red
	}
	statusRect := rl.NewRectangle(32, 119, 81, 52)
	drawConsolePanel(statusRect, shade)
	if transmitting {
		rl.DrawRectangleRounded(statusRect, .18, 8, rl.NewColor(108, 12, 22, 255))
		rl.DrawRectangleRoundedLinesEx(statusRect, .18, 8, 2, color.red)
		shade = color.text
	} else if status == "RX" {
		drawCutPanel(statusRect, 7, color.green, color.green, 1)
		shade = color.bg
	}
	drawConsoleText(displayFont, status, statusRect, 37, shade)
	mode := "MODO"
	if online && state.ModeKnown && int(state.Mode) < len(radioModes) {
		mode = radioModes[state.Mode]
	}
	drawConsoleButtonSize(modeRect(), mode, online, false, 30)
	if online {
		rl.DrawTriangle(rl.NewVector2(255, 139), rl.NewVector2(261, 148), rl.NewVector2(267, 139), color.amber)
	}
	drawText("VFO", 324, 111, 17, color.muted)
	if r.initialVFOPending {
		state.VFOKnown = false
	}
	if online && !state.VFOKnown {
		drawText("?", 358, 110, 17, color.amber)
	}
	drawVFOButtons(state, online)
	filterLabel := "FILTRO RX"
	if online && state.Filter > 0 {
		filterLabel = fmt.Sprintf("FIL%d", state.Filter)
		if widthAvailable(state.IF, time.Now()) && state.IF.Slot == state.Filter {
			filterLabel = fmt.Sprintf("FIL%d · %d Hz", state.Filter, state.IF.WidthHz)
		}
	}
	drawConsoleButton(filterButton, filterLabel, r.ifFilter.open, false)
	drawText("PASO", 324, 155, 17, color.muted)
	step := operationTuningStep(r.digitStep, tuningSteps[r.step], true)
	drawConsoleButton(stepRect(), fmt.Sprintf("%d Hz", step), r.digitStep != 0, false)
	r.drawSplitTuning(state, online, transmitting, mouse)
	if transmitting {
		now := time.Now()
		readings := r.txMeters.update(txReadings(state, online, now), state.TXSince, now)
		drawTXHeaderPower(readings[0])
		drawTXTelemetry(readings, r.txMeters.position)
	} else {
		r.txMeters = txMeterDisplay{}
		a.drawRadioMeter(state, online)
		a.drawRF(state, peaks, online, mouse)
		a.drawBandButton(state, online)
		r.drawFilterMenu(state)
		a.drawBandPanel(state)
		if r.receiver.agcOpen {
			for i := 1; i < len(agcLabels); i++ {
				drawConsoleButton(agcOption(i), agcLabels[i], state.RX[civ.AGC].Value == byte(i), false)
			}
		}
		if r.receiver.ampOpen {
			drawPreampMenu(state)
		}
	}
	if !state.TX && !a.transmitting() {
		r.cluster.drawMenus(mouse)
	}
	r.swr.draw(state.Frequency)
	if r.confirmVFOEqualize {
		drawVFOEqualizeConfirmation()
	}
	drawConsolePanel(rl.NewRectangle(7, 770, 1586, 123), consoleEdge)
	startedAt := state.TXSince
	if a.transmitting() {
		startedAt = a.txSince
	}
	a.drawConsolePTT(mouse, transmitting, startedAt)
	for _, x := range []int32{551, 1228} {
		rl.DrawLine(x, 789, x, 875, consoleEdge)
	}
	a.drawOutputControls(state, online)
	drawMonitorButton(operationMonitor(), a.monitorEnabled())
	drawConsoleButton(operationReset(), "RESET SAFE", a.txSignal.safetyActive.Load(), false)
	if msg := a.message; msg != "" {
		drawText(ellipsis(msg, 100), 565, 865, 13, color.muted)
	}
	if r.modeMenu {
		drawDarkDisplay(rl.NewRectangle(37, 181, 329, 133))
		for i, label := range radioModes {
			if i != 6 {
				drawButton(modeOption(i), label, byte(i) == state.Mode)
			}
		}
	}
}

func (a *app) drawRadioMeter(state civ.State, online bool) {
	meter := radioMeters[a.radio.meter]
	code := meter.code
	if code == 0 {
		code = 2
		if state.TX {
			code = 0x11
		}
	}
	drawConsoleText(semiboldFont, meter.label, rl.NewRectangle(1043, 134, 87, 37), 22, color.text)
	const x, y, width, segments = 1140, 136, 417, 32
	valid := online && time.Since(state.MeterAt[code]) < time.Second
	level := 0.0
	if valid {
		level = float64(state.Meters[code]) / 255
	}
	for i := 0; i < segments; i++ {
		shade := color.signal
		position := float64(i+1) / segments
		if position >= .79 {
			shade = color.red
		} else if position >= .63 {
			shade = color.amber
		}
		if position > level {
			shade = mix(rl.NewColor(31, 31, 35, 255), shade, .29)
		}
		rl.DrawRectangle(x+int32(i)*width/segments, y, width/segments-2, 18, shade)
	}
	if meter.code == 0 || code == 2 {
		for _, tick := range []struct {
			at    int32
			label string
		}{
			{12, "S1"}, {41, "S3"}, {71, "S5"}, {95, "S7"}, {120, "S9"}, {165, "+20"}, {205, "+40"}, {241, "+60"},
		} {
			p := x + width*tick.at/255
			shade := color.text
			if tick.at >= 205 {
				shade = color.red
			} else if tick.at >= 165 {
				shade = color.amber
			}
			drawConsoleText(semiboldFont, tick.label, rl.NewRectangle(float32(p-20), 99, 40, 26), 17, shade)
			rl.DrawLine(p, 122, p, 132, shade)
		}
	}
	if meter.code == 0 || code == 0x11 {
		rl.DrawLine(x, 166, x+width, 166, color.muted)
		for i, label := range []string{"0", "25", "50", "75", "100 %"} {
			p := x + int32(i)*width/4
			rl.DrawLine(p, 166, p, 174, color.muted)
			drawConsoleText(semiboldFont, label, rl.NewRectangle(float32(p-28), 175, 56, 24), 17, color.text)
		}
	} else if code != 2 && valid {
		drawText(fmt.Sprintf("Nivel relativo  %.0f %%", level*100), x, y+25, 15, color.muted)
	}
}

func (a *app) drawRF(state civ.State, peaks [civ.Samples]byte, online bool, mouse rl.Vector2) {
	r := &a.radio
	drawCutPanel(rl.NewRectangle(7, 225, 1586, 533), 11, color.panel, rl.NewColor(120, 29, 42, 255), 1.5)
	r.swr.drawButtons()
	drawRFOption(peakRect(), "PEAK", r.showPeak)
	drawRXButtons(state, online)
	r.receiver.draw(state, online)
	r.cluster.drawButtons()
	r.zoom.draw()
	drawConsoleButton(spanRect(), "SPAN", false, true)
	for _, x := range []int32{152, 540, 668, 980} {
		rl.DrawLine(x, 241, x, 275, consoleEdge)
	}
	graph, waterfall := rfGraph(), rfWaterfall()
	sweep := state.Sweep
	valid := online && time.Since(sweep.At) < 2*time.Second && sweep.Low < sweep.High
	rl.DrawRectangleGradientV(int32(graph.X), int32(graph.Y), int32(graph.Width), int32(graph.Height), rl.NewColor(8, 8, 10, 255), rl.NewColor(16, 16, 19, 255))
	rl.DrawRectangleLinesEx(graph, 1, consoleEdge)
	rl.DrawRectangleRec(waterfall, rl.NewColor(5, 8, 6, 255))
	if valid {
		r.zoom.observe(sweep)
	}
	if r.swr.show {
		drawSWRScale(graph)
	} else {
		r.zoom.drawGrid(graph)
	}
	for i := 0; i <= 5; i++ {
		x := graph.X + float32(i)*graph.Width/5
		rl.DrawLineEx(rl.NewVector2(x, graph.Y), rl.NewVector2(x, graph.Y+graph.Height), 1, rl.NewColor(44, 44, 49, 255))
	}
	if !valid {
		r.lastSweep = civ.Sweep{}
		r.envelope.at = time.Time{}
		r.zoom.lastSweep = civ.Sweep{}
		message := "Conecta el IC-7300 desde CFG"
		if a.ptt.Connected() {
			message = "Esperando espectro · abre SCOPE en el IC-7300"
		}
		if !state.Sweep.At.IsZero() && time.Since(state.Sweep.At) < 2*time.Second {
			message = "Frecuencia fuera del rango del scope"
		}
		if state.Error != "" {
			message = state.Error
		}
		drawCenteredFitted(message, graph, 23, color.muted)
		return
	}
	span := fmt.Sprintf("SPAN  %.1f kHz", float64(sweep.High-sweep.Low)/1000)
	if sweep.Mode == 0 || sweep.Mode == 2 {
		span = fmt.Sprintf("SPAN  ±%.1f kHz", float64(sweep.High-sweep.Low)/2000)
	} else {
		span = fmt.Sprintf("SPAN FIX · %.1f kHz", float64(sweep.High-sweep.Low)/1000)
	}
	drawConsoleButton(spanRect(), span, false, true)
	if time.Since(state.ScopeRefAt) < 3*time.Second {
		drawText(fmt.Sprintf("REF %+.1f dB", state.ScopeRefDB), 238, 278, 13, color.muted)
	}
	r.envelope.update(sweep, peaks, time.Now())
	clipped := false
	for i, sample := range r.envelope.levels {
		x := graph.X + float32(i)*graph.Width/(civ.Samples-1)
		// CI-V supplies the waveform drawn by the radio, including its REF setting.
		level := r.zoom.displayLevel(sample)
		peak := r.zoom.displayLevel(r.envelope.peakLevels[i])
		clipped = clipped || level > 160 || (r.showPeak && peak > 160)
		y := graph.Y + graph.Height*(1-max(0, min(160, level))/160)
		r.trace[i] = rl.NewVector2(x, y)
		r.fill[2*i] = r.trace[i]
		r.fill[2*i+1] = rl.NewVector2(x, graph.Y+graph.Height)
		r.peakTrace[i] = rl.NewVector2(x, graph.Y+graph.Height*(1-max(0, min(160, peak))/160))
	}
	r.fillGradient.draw(r.fill[:], graph, r.zoom)
	if r.showPeak {
		rl.DrawLineStrip(r.peakTrace[:], rl.NewColor(182, 190, 185, 230))
	}
	rl.DrawLineStrip(r.trace[:], color.green)
	if clipped {
		alert := rl.NewRectangle(graph.X+graph.Width-110, graph.Y+5, 104, 26)
		drawConsolePanel(alert, color.amber)
		drawConsoleText(semiboldFont, "FUERA DE EJE", alert, 14, color.amber)
	}
	r.updateWaterfall(sweep)
	rl.DrawLine(int32(waterfall.X), int32(waterfall.Y), int32(waterfall.X+waterfall.Width), int32(waterfall.Y), consoleEdge)
	if r.texture.ID != 0 {
		rl.DrawTexturePro(r.texture, rl.NewRectangle(.5, 0, civ.Samples-1, waterfallRows), waterfall, rl.Vector2{}, 0, rl.White)
	}
	now := time.Now()
	hz, exact := r.shownFrequency(state, online, now)
	confirmedRX, tx := state.EffectiveTuning(now)
	rx := confirmedRX
	cursor := r.wheel.cursorFrequency(hz, now)
	if r.wheel.target != 0 {
		rx = cursor
	}
	r.drawFilterEdge(state, rx, mouse)
	a.drawBandCoverage(graph, waterfall, sweep, state.Frequency, hz, cursor, exact)
	r.swr.overlay(graph, sweep)
	drawSplitMarkers(graph, waterfall, sweep, state, rx, confirmedRX, tx)
	r.drawClusterLabels(state, mouse)
}

func displayedFrequency(state civ.State, online bool, now time.Time) (hz int64, exact bool) {
	if !online {
		return 0, false
	}
	if state.Frequency > 0 && now.Sub(state.FrequencyAt) < 2*time.Second {
		return state.Frequency, true
	}
	if now.Sub(state.Sweep.At) < 2*time.Second {
		return state.Sweep.Center, false
	}
	return 0, false
}

func (r *radioPanel) updateWaterfall(sweep civ.Sweep) {
	if r.lastSweep.At == sweep.At {
		return
	}
	if r.texture.ID == 0 {
		img := rl.GenImageColor(civ.Samples, waterfallRows, rl.Black)
		r.texture = rl.LoadTextureFromImage(img)
		rl.UnloadImage(img)
		r.pixels = make([]rl.Color, civ.Samples*waterfallRows)
	}
	if r.lastSweep.Low != sweep.Low || r.lastSweep.High != sweep.High {
		for i := range r.pixels {
			r.pixels[i] = rl.NewColor(5, 8, 6, 255)
		}
	}
	copy(r.pixels[civ.Samples:], r.pixels[:len(r.pixels)-civ.Samples])
	for i, level := range sweep.Levels {
		r.pixels[i] = waterfallColor(level)
	}
	rl.UpdateTexture(r.texture, r.pixels)
	r.lastSweep = sweep
}

func waterfallColor(level byte) rl.Color {
	floor := rl.NewColor(4, 4, 5, 255)
	weak := rl.NewColor(37, 39, 38, 255)
	signal := rl.NewColor(140, 144, 140, 255)
	strong := rl.NewColor(115, 238, 82, 255)
	switch {
	case level <= 16:
		return mix(floor, weak, float32(level)/16)
	case level <= 54:
		return mix(weak, signal, float32(level-16)/38)
	case level <= 94:
		return mix(signal, strong, float32(level-54)/40)
	default:
		return mix(strong, rl.White, float32(level-94)/66)
	}
}
