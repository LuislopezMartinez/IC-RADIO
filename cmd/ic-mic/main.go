package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"
	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	appfonts "github.com/LuislopezMartinez/IC-RADIO/assets/fonts"
	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	viewW             = 1600
	viewH             = 900
	stateRX           = "rx_listening"
	stateTXReady      = "tx_ready"
	stateTransmitting = "transmitting"
	stateError        = "error"
	maxTXDuration     = 3 * time.Minute
)

var color = struct {
	bg, panel, panel2, border, text, muted, cyan, green, amber, red rl.Color
}{
	rl.NewColor(8, 16, 25, 255), rl.NewColor(14, 27, 39, 255), rl.NewColor(19, 35, 49, 255),
	rl.NewColor(72, 94, 111, 255), rl.NewColor(241, 244, 247, 255), rl.NewColor(166, 180, 191, 255),
	rl.NewColor(16, 218, 236, 255), rl.NewColor(54, 230, 119, 255), rl.NewColor(255, 190, 40, 255), rl.NewColor(235, 25, 39, 255),
}

var regularFont, semiboldFont, militaryFont rl.Font

type testToneProcessor struct {
	base    *audio.Processor
	monitor sampleMonitor
	enabled atomic.Bool
	phase   float64
}

type sampleMonitor interface {
	Write([]float32)
}

func (p *testToneProcessor) Process(samples []float32) {
	if p.enabled.Load() {
		phaseStep := 2 * math.Pi * 1000 / audio.DefaultSampleRate
		for i := range samples {
			samples[i] = float32(.1 * math.Sin(p.phase))
			p.phase += phaseStep
			if p.phase >= 2*math.Pi {
				p.phase -= 2 * math.Pi
			}
		}
	}
	p.base.Process(samples)
	if p.monitor != nil {
		p.monitor.Write(samples)
	}
}

type settings struct {
	Version        int          `json:"version"`
	Input          string       `json:"input"`
	Output         string       `json:"output"`
	COM            string       `json:"com"`
	PTTLine        ptt.Line     `json:"ptt_line"`
	Config         audio.Config `json:"dsp"`
	FiltersEnabled bool         `json:"filters_enabled"`
	GainEnabled    bool         `json:"gain_enabled"`
	PTTMode        string       `json:"ptt_mode"`
	PTTKey         int32        `json:"ptt_key"`
	PTTCtrl        bool         `json:"ptt_ctrl"`
	PTTAlt         bool         `json:"ptt_alt"`
	PTTShift       bool         `json:"ptt_shift"`
	RXInput        string       `json:"rx_input"`
	RXOutput       string       `json:"rx_output"`
	RXConfig       audio.Config `json:"rx_dsp"`
	RXFilters      bool         `json:"rx_filters_enabled"`
	RXGain         bool         `json:"rx_gain_enabled"`
	RXEmphasis     bool         `json:"rx_emphasis_enabled"`
	RXEmphasisMode string       `json:"rx_emphasis_mode"`
	RXEmphasisMix  float64      `json:"rx_emphasis_amount"`
	TXActiveTab    int          `json:"tx_active_tab"`
	RXActiveTab    int          `json:"rx_active_tab"`
	MonitorEnabled bool         `json:"tx_monitor_enabled"`
}

type viewState struct {
	processor      *audio.Processor
	engine         *audioio.Engine
	config         audio.Config
	inputs         []audioio.Device
	outputs        []audioio.Device
	inIndex        int
	outIndex       int
	spectrum       []float32
	filtersEnabled bool
	gainEnabled    bool
	activeTab      int
}

type app struct {
	processor        *audio.Processor
	engine           *audioio.Engine
	ptt              ptt.Controller
	config           audio.Config
	settings         settings
	inputs           []audioio.Device
	outputs          []audioio.Device
	ports            []ptt.Port
	inIndex          int
	outIndex         int
	comIndex         int
	line             ptt.Line
	message          string
	txSince          time.Time
	spectrum         []float32
	activeTab        int
	dragEQ           int
	cutoffDrag       int
	controlDrag      int
	notchDrag        int
	pttSettingsOpen  bool
	capturingPTTKey  bool
	keyPTTActive     bool
	mousePTTActive   bool
	openDeviceMenu   int
	deviceMenuScroll int
	mode             string
	txView           viewState
	rxView           viewState
	radioState       string
	pttStartedFromRX bool
	txSignal         *testToneProcessor
	monitor          *audioio.PlaybackMonitor
}

func main() {
	p := audio.NewProcessor(audio.DefaultSampleRate)
	monitor, monitorErr := audioio.NewPlaybackMonitor()
	txSignal := &testToneProcessor{base: p, monitor: monitor}
	e, err := audioio.New(txSignal)
	if err != nil {
		panic(err)
	}
	a := &app{processor: p, engine: e, config: audio.DefaultConfig(), spectrum: make([]float32, 257), dragEQ: -1, cutoffDrag: -1, controlDrag: -1, notchDrag: -1, openDeviceMenu: -1, line: ptt.RTS, mode: "tx", radioState: stateTXReady, txSignal: txSignal, monitor: monitor}
	defer func() {
		_ = a.ptt.SetTX(false)
		a.ptt.Close()
		if a.txView.engine != nil {
			a.txView.engine.Close()
		} else {
			e.Close()
		}
		if a.rxView.engine != nil {
			a.rxView.engine.Close()
		}
		if a.monitor != nil {
			a.monitor.Close()
		}
	}()
	a.load()
	a.refreshDevices()
	a.applyDSP()
	a.activeTab = validTab(a.settings.TXActiveTab)
	a.txView = a.snapshotView()
	rxProcessor := audio.NewProcessor(audio.DefaultSampleRate)
	rxEngine, err := audioio.New(rxProcessor)
	if err != nil {
		panic(err)
	}
	rxConfig := a.settings.RXConfig
	if rxConfig.HighCutHz == 0 {
		rxConfig = audio.DefaultConfig()
		rxConfig.LowCutHz, rxConfig.HighCutHz = 100, 4000
	}
	applyRXEmphasis(&rxConfig, a.settings.RXEmphasis, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
	rxInputs, rxOutputs := rxEngine.Inputs(), rxEngine.Outputs()
	a.rxView = viewState{processor: rxProcessor, engine: rxEngine, config: rxConfig, inputs: rxInputs, outputs: rxOutputs,
		inIndex: namedDevice(rxInputs, a.settings.RXInput), outIndex: namedDevice(rxOutputs, a.settings.RXOutput), spectrum: make([]float32, 257),
		filtersEnabled: a.settings.RXFilters, gainEnabled: a.settings.RXGain, activeTab: validTab(a.settings.RXActiveTab)}
	if a.settings.Version < 4 {
		a.rxView.filtersEnabled, a.rxView.gainEnabled = true, true
	}
	rxEffective := a.rxView.config
	if !a.rxView.filtersEnabled {
		rxEffective.LowCutHz, rxEffective.HighCutHz = 20, audio.DefaultSampleRate*.45
	}
	if !a.rxView.gainEnabled {
		rxEffective.OutputGainDB = 0
	}
	rxProcessor.Configure(rxEffective)
	if a.settings.RXInput != "" && a.settings.RXOutput != "" {
		if err := a.startRXMonitor(); err != nil {
			a.message = "Monitor RX: " + err.Error()
		}
	}
	if monitorErr != nil {
		a.settings.MonitorEnabled = false
		a.message = "Monitor TX no disponible: " + monitorErr.Error()
	} else if a.settings.MonitorEnabled {
		if err := a.startOutputMonitor(); err != nil {
			a.settings.MonitorEnabled = false
			a.message = "Monitor TX: " + err.Error()
		}
	}
	defer a.save()

	rl.SetConfigFlags(rl.FlagWindowResizable | rl.FlagVsyncHint | rl.FlagMsaa4xHint | rl.FlagWindowHighdpi)
	rl.InitWindow(1280, 720, "IC-RADIO · Consola RX/TX para transceptores")
	defer rl.CloseWindow()
	codepoints := latinCodepoints()
	regularFont = rl.LoadFontFromMemory(".ttf", appfonts.InterRegular, 64, codepoints)
	semiboldFont = rl.LoadFontFromMemory(".ttf", appfonts.InterSemiBold, 72, codepoints)
	militaryFont = rl.LoadFontFromMemory(".ttf", appfonts.BlackOpsOne, 96, codepoints)
	rl.SetTextureFilter(regularFont.Texture, rl.FilterBilinear)
	rl.SetTextureFilter(semiboldFont.Texture, rl.FilterBilinear)
	rl.SetTextureFilter(militaryFont.Texture, rl.FilterBilinear)
	defer rl.UnloadFont(regularFont)
	defer rl.UnloadFont(semiboldFont)
	defer rl.UnloadFont(militaryFont)
	rl.SetWindowMinSize(960, 540)
	rl.SetTargetFPS(60)

	for !rl.WindowShouldClose() {
		mouse := virtualMouse()
		a.update(mouse)
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		destination := destinationRect()
		camera := rl.NewCamera2D(rl.NewVector2(destination.X, destination.Y), rl.Vector2{}, 0, destination.Width/viewW)
		rl.BeginMode2D(camera)
		rl.DrawRectangle(0, 0, viewW, viewH, color.bg)
		a.draw(mouse)
		rl.EndMode2D()
		rl.EndDrawing()
	}
}

func (a *app) refreshDevices() {
	a.inputs, a.outputs, a.ports = a.engine.Inputs(), a.engine.Outputs(), ptt.Ports()
	a.inIndex = namedDevice(a.inputs, a.settings.Input)
	a.outIndex = namedDevice(a.outputs, a.settings.Output)
	a.comIndex = namedPort(a.ports, a.settings.COM)
	if a.settings.PTTLine == ptt.DTR {
		a.line = ptt.DTR
	}
	if len(a.inputs) > 0 && len(a.outputs) > 0 {
		if err := a.engine.Start(a.inIndex, a.outIndex); err != nil {
			a.message, a.radioState = err.Error(), stateError
		}
	} else {
		a.radioState = stateError
	}
}

func (a *app) snapshotView() viewState {
	return viewState{processor: a.processor, engine: a.engine, config: a.config, inputs: a.inputs, outputs: a.outputs,
		inIndex: a.inIndex, outIndex: a.outIndex, spectrum: a.spectrum,
		filtersEnabled: a.settings.FiltersEnabled, gainEnabled: a.settings.GainEnabled, activeTab: a.activeTab}
}

func (a *app) storeActiveView() {
	view := a.snapshotView()
	if a.mode == "rx" {
		a.rxView = view
	} else {
		a.txView = view
	}
}

func (a *app) loadView(view viewState) {
	a.processor, a.engine, a.config = view.processor, view.engine, view.config
	a.inputs, a.outputs = view.inputs, view.outputs
	a.inIndex, a.outIndex, a.spectrum = view.inIndex, view.outIndex, view.spectrum
	a.settings.FiltersEnabled, a.settings.GainEnabled = view.filtersEnabled, view.gainEnabled
	a.activeTab = validTab(view.activeTab)
}

func (a *app) switchMode(mode string) {
	if mode == a.mode {
		return
	}
	if a.ptt.TX() {
		a.pttStartedFromRX = false
		a.setTX(false)
	}
	if mode == "rx" {
		a.txSignal.enabled.Store(false)
	}
	previousMode := a.mode
	a.storeActiveView()
	if previousMode == "tx" {
		a.engine.Stop()
	}
	if mode == "rx" {
		a.loadView(a.rxView)
	} else {
		a.loadView(a.txView)
	}
	a.mode = mode
	a.applyDSP()
	if len(a.inputs) > 0 && len(a.outputs) > 0 {
		var err error
		if mode == "rx" {
			err = a.startRXMonitor()
		} else if !a.engine.Status().Running {
			err = a.engine.Start(a.inIndex, a.outIndex)
		}
		if err != nil {
			a.message, a.radioState = err.Error(), stateError
		} else {
			a.message = strings.ToUpper(mode) + " activo"
			if mode == "rx" {
				a.radioState = stateRX
			} else {
				a.radioState = stateTXReady
			}
		}
	} else {
		a.radioState = stateError
		a.message = "No hay dispositivos de audio para " + strings.ToUpper(mode)
	}
	a.openDeviceMenu = -1
}

func (a *app) startRXMonitor() error {
	if a.rxView.engine == nil {
		return fmt.Errorf("motor RX no disponible")
	}
	if a.rxView.engine.Status().Running {
		return nil
	}
	if len(a.rxView.inputs) == 0 || len(a.rxView.outputs) == 0 {
		return fmt.Errorf("selecciona entrada y salida RX")
	}
	return a.rxView.engine.Start(a.rxView.inIndex, a.rxView.outIndex)
}

func (a *app) stopRXMonitor() {
	if a.rxView.engine != nil {
		a.rxView.engine.Stop()
	}
}

func (a *app) startOutputMonitor() error {
	if a.monitor == nil {
		return fmt.Errorf("motor de monitor no disponible")
	}
	if a.rxView.outIndex < 0 || a.rxView.outIndex >= len(a.rxView.outputs) {
		return fmt.Errorf("selecciona primero los altavoces de RX")
	}
	return a.monitor.StartNamed(a.rxView.outputs[a.rxView.outIndex].Name)
}

func (a *app) toggleOutputMonitor() {
	if a.settings.MonitorEnabled {
		a.monitor.Stop()
		a.settings.MonitorEnabled = false
		a.message = "Monitor de audio TX desactivado"
		a.save()
		return
	}
	if err := a.startOutputMonitor(); err != nil {
		a.settings.MonitorEnabled = false
		a.message = "No se pudo activar MON: " + err.Error()
		return
	}
	a.settings.MonitorEnabled = true
	a.message = "MON activo · escuchando el audio procesado de TX"
	a.save()
}

func (a *app) reconnectAudio() {
	if len(a.inputs) == 0 || len(a.outputs) == 0 {
		return
	}
	if err := a.engine.Start(a.inIndex, a.outIndex); err != nil {
		a.message, a.radioState = err.Error(), stateError
		return
	}
	if a.mode == "rx" {
		a.settings.RXInput, a.settings.RXOutput = a.inputs[a.inIndex].Name, a.outputs[a.outIndex].Name
		a.rxView = a.snapshotView()
		if a.settings.MonitorEnabled {
			if err := a.startOutputMonitor(); err != nil {
				a.settings.MonitorEnabled = false
				a.message = "Monitor TX: " + err.Error()
			}
		}
	} else {
		a.settings.Input, a.settings.Output = a.inputs[a.inIndex].Name, a.outputs[a.outIndex].Name
		a.txView = a.snapshotView()
	}
	a.message = "Audio conectado"
	if a.mode == "rx" {
		a.radioState = stateRX
	} else {
		a.radioState = stateTXReady
	}
	a.save()
}

func (a *app) connectPTT() {
	if len(a.ports) == 0 {
		a.message = "No se encontraron puertos COM"
		return
	}
	if err := a.ptt.Connect(a.ports[a.comIndex].Name, a.line); err != nil {
		a.message = err.Error()
		return
	}
	a.settings.COM, a.settings.PTTLine = a.ports[a.comIndex].Name, a.line
	a.message = "PTT conectado en " + a.settings.COM + " · " + string(a.line)
	a.save()
}

func (a *app) toggleTX() {
	a.setTX(!a.ptt.TX())
}

func (a *app) setTX(enabled bool) bool {
	if !a.ptt.Connected() {
		a.message = "Conecta primero el control PTT"
		a.radioState = stateError
		return false
	}
	startedFromRX := enabled && a.mode == "rx"
	if startedFromRX {
		a.pttStartedFromRX = true
		a.switchMode("tx")
		if !a.engine.Status().Running {
			a.pttStartedFromRX = false
			a.message, a.radioState = "No se pudo preparar la ruta TX", stateError
			a.switchMode("rx")
			return false
		}
	}
	if enabled {
		a.stopRXMonitor()
	}
	if err := a.ptt.SetTX(enabled); err != nil {
		errorText := err.Error()
		a.message, a.radioState = errorText, stateError
		if enabled && a.pttStartedFromRX {
			a.pttStartedFromRX = false
			_ = a.startRXMonitor()
			a.switchMode("rx")
			a.message = errorText
		} else if enabled {
			_ = a.startRXMonitor()
		}
		return false
	}
	if enabled {
		a.txSince = time.Now()
		a.message, a.radioState = "Emisora en transmisión", stateTransmitting
	} else {
		a.txSignal.enabled.Store(false)
		if err := a.startRXMonitor(); err != nil {
			a.message, a.radioState = "Emisora en RX, pero el monitor falló: "+err.Error(), stateError
			return false
		}
		a.message, a.radioState = "Emisora en recepción · monitor RX activo", stateTXReady
		if a.pttStartedFromRX {
			a.pttStartedFromRX = false
			a.switchMode("rx")
		}
	}
	return true
}

func (a *app) update(mouse rl.Vector2) {
	if rl.IsKeyPressed(rl.KeyEscape) {
		if a.ptt.TX() {
			a.setTX(false)
		}
		a.keyPTTActive, a.mousePTTActive = false, false
		a.message = "TX detenido"
		if a.pttSettingsOpen {
			a.capturingPTTKey, a.pttSettingsOpen = false, false
		}
		a.openDeviceMenu = -1
	}
	if a.ptt.TX() && time.Since(a.txSince) >= maxTXDuration {
		a.setTX(false)
		a.message = "TX detenido: límite de 3 minutos"
	}
	if a.pttSettingsOpen {
		a.updatePTTSettings(mouse)
		a.processor.Spectrum(a.spectrum, 10000)
		return
	}
	a.updateKeyboardPTT()
	if a.openDeviceMenu >= 0 {
		a.updateDeviceMenu(mouse)
		a.processor.Spectrum(a.spectrum, 10000)
		return
	}
	if !rl.IsMouseButtonPressed(rl.MouseLeftButton) && !rl.IsMouseButtonDown(rl.MouseLeftButton) {
		a.dragEQ = -1
		a.cutoffDrag = -1
		a.controlDrag = -1
		a.notchDrag = -1
	}
	if clicked(mouse, rl.NewRectangle(650, 22, 100, 48)) {
		a.switchMode("rx")
		return
	}
	if clicked(mouse, rl.NewRectangle(760, 22, 100, 48)) {
		a.switchMode("tx")
		return
	}
	if clicked(mouse, rl.NewRectangle(30, 92, 710, 64)) && len(a.inputs) > 0 {
		a.openDeviceMenu, a.deviceMenuScroll = 0, initialDeviceScroll(a.inIndex, len(a.inputs))
	}
	if clicked(mouse, rl.NewRectangle(860, 92, 710, 64)) && len(a.outputs) > 0 {
		a.openDeviceMenu, a.deviceMenuScroll = 1, initialDeviceScroll(a.outIndex, len(a.outputs))
	}
	if clicked(mouse, rl.NewRectangle(920, 22, 200, 48)) && len(a.ports) > 0 {
		a.comIndex = (a.comIndex + 1) % len(a.ports)
	}
	if clicked(mouse, rl.NewRectangle(1130, 22, 75, 48)) {
		if a.line == ptt.RTS {
			a.line = ptt.DTR
		} else {
			a.line = ptt.RTS
		}
	}
	if clicked(mouse, rl.NewRectangle(1215, 22, 155, 48)) {
		a.connectPTT()
	}
	if clicked(mouse, rl.NewRectangle(1380, 22, 190, 48)) {
		a.pttSettingsOpen = true
		a.capturingPTTKey = false
	}
	pttRect := rl.NewRectangle(530, 385, 540, 185)
	if a.mode == "tx" && a.settings.PTTMode == "hold" {
		if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, pttRect) {
			a.mousePTTActive = a.setTX(true)
		}
		if a.mousePTTActive && rl.IsMouseButtonReleased(rl.MouseLeftButton) {
			a.setTX(false)
			a.mousePTTActive = false
		}
	} else if a.mode == "tx" && clicked(mouse, pttRect) {
		a.toggleTX()
	}
	for i := 0; i < 5; i++ {
		if clicked(mouse, rl.NewRectangle(float32(20+i*312), 590, 300, 56)) {
			a.activeTab = i
		}
	}
	a.updateCutoffs(mouse)
	a.updateNotchFromSpectrum(mouse)
	a.updateRXEmphasis(mouse)
	if a.mode == "tx" && clicked(mouse, rl.NewRectangle(1170, 665, 180, 46)) {
		enabled := !a.txSignal.enabled.Load()
		a.txSignal.enabled.Store(enabled)
		if enabled {
			a.message = "Tono de prueba TX: 1 kHz a -20 dBFS"
		} else {
			a.message = "Tono de prueba desactivado"
		}
	}
	if a.mode == "tx" && clicked(mouse, rl.NewRectangle(1460, 403, 82, 40)) {
		a.toggleOutputMonitor()
	}
	if clicked(mouse, rl.NewRectangle(1370, 665, 180, 46)) {
		a.toggleActiveSection()
	}
	if a.activeTab == 1 {
		a.updateEQ(mouse)
	}
	a.updatePanelControls(mouse)
	a.processor.Spectrum(a.spectrum, 10000)
}

func (a *app) updateDeviceMenu(mouse rl.Vector2) {
	devices := a.inputs
	x := float32(30)
	selector := rl.NewRectangle(30, 92, 710, 64)
	if a.openDeviceMenu == 1 {
		devices, x, selector = a.outputs, 860, rl.NewRectangle(860, 92, 710, 64)
	}
	visible := min(8, len(devices))
	maxScroll := max(0, len(devices)-visible)
	if wheel := rl.GetMouseWheelMove(); wheel != 0 {
		a.deviceMenuScroll = max(0, min(maxScroll, a.deviceMenuScroll-int(math.Copysign(1, float64(wheel)))))
	}
	list := rl.NewRectangle(x, 160, 710, float32(visible*44+8))
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
		if rl.CheckCollisionPointRec(mouse, selector) {
			a.openDeviceMenu = -1
			return
		}
		if rl.CheckCollisionPointRec(mouse, list) {
			row := int((mouse.Y - list.Y - 4) / 44)
			if row >= 0 && row < visible {
				index := a.deviceMenuScroll + row
				if a.openDeviceMenu == 0 {
					a.inIndex = index
				} else {
					a.outIndex = index
				}
				a.openDeviceMenu = -1
				a.reconnectAudio()
			}
			return
		}
		a.openDeviceMenu = -1
	}
}

func initialDeviceScroll(selected, count int) int {
	return max(0, min(max(0, count-8), selected-3))
}

func (a *app) updateKeyboardPTT() {
	if !rl.IsWindowFocused() {
		if a.keyPTTActive {
			a.setTX(false)
			a.keyPTTActive = false
		}
		return
	}
	key := a.settings.PTTKey
	if key == 0 {
		key = rl.KeySpace
	}
	modifiers := (!a.settings.PTTCtrl || ctrlDown()) && (!a.settings.PTTAlt || altDown()) && (!a.settings.PTTShift || shiftDown())
	if a.settings.PTTMode == "hold" {
		down := modifiers && rl.IsKeyDown(key)
		if down && !a.keyPTTActive {
			a.keyPTTActive = a.setTX(true)
		}
		if !down && a.keyPTTActive {
			a.setTX(false)
			a.keyPTTActive = false
		}
	} else if modifiers && rl.IsKeyPressed(key) {
		a.toggleTX()
	}
}

func (a *app) updatePTTSettings(mouse rl.Vector2) {
	if a.capturingPTTKey {
		for key := rl.GetKeyPressed(); key != 0; key = rl.GetKeyPressed() {
			if validBindingKey(key) {
				if key == rl.KeyF4 && altDown() {
					a.message = "Alt+F4 está reservado por Windows"
					return
				}
				a.settings.PTTKey = key
				a.settings.PTTCtrl, a.settings.PTTAlt, a.settings.PTTShift = ctrlDown(), altDown(), shiftDown()
				a.capturingPTTKey = false
				break
			}
		}
	}
	if clicked(mouse, rl.NewRectangle(500, 330, 400, 60)) {
		a.capturingPTTKey = true
	}
	if clicked(mouse, rl.NewRectangle(920, 330, 180, 60)) {
		a.settings.PTTKey, a.settings.PTTCtrl, a.settings.PTTAlt, a.settings.PTTShift = rl.KeySpace, false, false, false
		a.capturingPTTKey = false
	}
	if clicked(mouse, rl.NewRectangle(500, 465, 280, 105)) {
		if a.ptt.TX() {
			a.setTX(false)
		}
		a.settings.PTTMode = "hold"
	}
	if clicked(mouse, rl.NewRectangle(820, 465, 280, 105)) {
		if a.ptt.TX() {
			a.setTX(false)
		}
		a.settings.PTTMode = "lock"
	}
	if clicked(mouse, rl.NewRectangle(900, 715, 200, 55)) {
		a.save()
		a.pttSettingsOpen, a.capturingPTTKey = false, false
	}
	if clicked(mouse, rl.NewRectangle(500, 715, 200, 55)) || clicked(mouse, rl.NewRectangle(1125, 225, 45, 45)) {
		a.pttSettingsOpen, a.capturingPTTKey = false, false
	}
}

func validBindingKey(key int32) bool {
	switch key {
	case rl.KeyEscape, rl.KeyLeftControl, rl.KeyRightControl, rl.KeyLeftAlt, rl.KeyRightAlt,
		rl.KeyLeftShift, rl.KeyRightShift, rl.KeyLeftSuper, rl.KeyRightSuper:
		return false
	}
	return true
}

func ctrlDown() bool  { return rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl) }
func altDown() bool   { return rl.IsKeyDown(rl.KeyLeftAlt) || rl.IsKeyDown(rl.KeyRightAlt) }
func shiftDown() bool { return rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift) }

func (a *app) updateCutoffs(mouse rl.Vector2) {
	graph := rl.NewRectangle(98, 195, 1454, 148)
	lowX := graph.X + float32(a.config.LowCutHz/10000)*graph.Width
	highX := graph.X + float32(a.config.HighCutHz/10000)*graph.Width
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, graph) {
		lowDistance := float32(math.Abs(float64(mouse.X - lowX)))
		highDistance := float32(math.Abs(float64(mouse.X - highX)))
		if lowDistance <= 16 || highDistance <= 16 {
			if lowDistance < highDistance {
				a.cutoffDrag = 0
			} else {
				a.cutoffDrag = 1
			}
		}
	}
	if a.cutoffDrag >= 0 && rl.IsMouseButtonDown(rl.MouseLeftButton) {
		hz := math.Round(float64((mouse.X-graph.X)/graph.Width*10000)/10) * 10
		if a.cutoffDrag == 0 {
			a.config.LowCutHz = max(20, min(a.config.HighCutHz-50, hz))
		} else {
			a.config.HighCutHz = max(a.config.LowCutHz+50, min(10000, hz))
		}
		a.settings.FiltersEnabled = true
		a.applyDSPAndSave()
	}
}

func (a *app) updateNotchFromSpectrum(mouse rl.Vector2) {
	if a.mode != "rx" {
		return
	}
	graph := rl.NewRectangle(98, 195, 1454, 148)
	centerX := graph.X + float32(a.config.Notch.FrequencyHz/10000)*graph.Width
	halfWidthX := float32(a.config.Notch.WidthHz/20000) * graph.Width
	leftX, rightX := centerX-halfWidthX, centerX+halfWidthX
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && a.cutoffDrag < 0 && rl.CheckCollisionPointRec(mouse, graph) {
		distances := []float32{
			float32(math.Abs(float64(mouse.X - centerX))),
			float32(math.Abs(float64(mouse.X - leftX))),
			float32(math.Abs(float64(mouse.X - rightX))),
		}
		best := 0
		for i := 1; i < len(distances); i++ {
			if distances[i] < distances[best] {
				best = i
			}
		}
		if distances[best] <= 14 {
			a.notchDrag = best
		}
	}
	if a.notchDrag >= 0 && rl.IsMouseButtonDown(rl.MouseLeftButton) {
		hz := max(20.0, min(8000.0, float64((mouse.X-graph.X)/graph.Width*10000)))
		switch a.notchDrag {
		case 0:
			a.config.Notch.FrequencyHz = math.Round(hz/5) * 5
		case 1:
			a.config.Notch.WidthHz = max(20, min(2000, 2*(a.config.Notch.FrequencyHz-hz)))
		case 2:
			a.config.Notch.WidthHz = max(20, min(2000, 2*(hz-a.config.Notch.FrequencyHz)))
		}
		a.config.Notch.Enabled = true
		a.applyDSPAndSave()
	}
	if rl.CheckCollisionPointRec(mouse, graph) && math.Abs(float64(mouse.X-centerX)) <= float64(max(20, halfWidthX+12)) {
		if wheel := rl.GetMouseWheelMove(); wheel != 0 {
			if shiftDown() {
				a.config.Notch.DepthDB = max(-60, min(-6, a.config.Notch.DepthDB-float64(wheel)*2))
			} else {
				a.config.Notch.WidthHz = max(20, min(2000, a.config.Notch.WidthHz+float64(wheel)*20))
			}
			a.config.Notch.Enabled = true
			a.applyDSPAndSave()
		}
	}
}

func (a *app) updateEQ(mouse rl.Vector2) {
	graph := rl.NewRectangle(330, 680, 850, 170)
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, graph) {
		best, distance := 0, float32(1e9)
		for i := 0; i < audio.EQBandCount; i++ {
			x := graph.X + float32(i)*graph.Width/float32(audio.EQBandCount-1)
			if d := float32(math.Abs(float64(mouse.X - x))); d < distance {
				best, distance = i, d
			}
		}
		a.dragEQ = best
	}
	if a.dragEQ >= 0 && rl.IsMouseButtonDown(rl.MouseLeftButton) {
		gain := 12 - float64((mouse.Y-graph.Y)/graph.Height)*24
		a.config.EQGainsDB[a.dragEQ] = max(-12, min(12, gain))
		a.config.EQEnabled = true
		a.applyDSPAndSave()
	}
	if clicked(mouse, rl.NewRectangle(1230, 820, 150, 44)) {
		a.config.EQGainsDB = [audio.EQBandCount]float64{}
		a.applyDSPAndSave()
	}
}

func (a *app) updatePanelControls(mouse rl.Vector2) {
	if a.activeTab == 3 {
		speeds := []audio.NoiseSpeed{audio.NoiseSlow, audio.NoiseNormal, audio.NoiseFast}
		for i, speed := range speeds {
			if clicked(mouse, rl.NewRectangle(float32(1200+i*115), 765, 105, 46)) {
				a.config.NoiseSpeed = speed
				a.config.NoiseEnabled = true
				a.applyDSPAndSave()
			}
		}
	}
	slider := rl.NewRectangle(270, 780, 900, 42)
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, slider) {
		a.controlDrag = a.activeTab
	}
	if a.controlDrag != a.activeTab || !rl.IsMouseButtonDown(rl.MouseLeftButton) {
		return
	}
	percent := max(0.0, min(100.0, float64((mouse.X-slider.X)/slider.Width*100)))
	switch a.activeTab {
	case 2:
		if a.mode == "rx" {
			a.config.Notch.FrequencyHz = math.Round((100+percent*39)/10) * 10
			a.config.Notch.Enabled = true
		} else {
			percent = math.Round(percent/20) * 20
			a.config.Compressor.TargetDBFS = -6 - percent*.24
			a.config.Compressor.Enabled = true
		}
	case 3:
		percent = math.Round(percent/5) * 5
		a.config.NoiseStrength = percent
		a.config.NoiseEnabled = true
	case 4:
		a.config.OutputGainDB = math.Round((-12+percent*.24)*2) / 2
		a.settings.GainEnabled = true
	default:
		return
	}
	a.applyDSPAndSave()
}

func (a *app) updateRXEmphasis(mouse rl.Vector2) {
	if a.mode != "rx" || a.activeTab != 0 {
		return
	}
	changed := false
	if clicked(mouse, rl.NewRectangle(810, 684, 165, 42)) {
		a.settings.RXEmphasis = !a.settings.RXEmphasis
		changed = true
	}
	profiles := []string{"ssb", "fm50", "fm75"}
	for i, profile := range profiles {
		if clicked(mouse, rl.NewRectangle(float32(970+i*135), 684, 125, 42)) {
			a.settings.RXEmphasisMode = profile
			a.settings.RXEmphasis = true
			changed = true
		}
	}
	slider := rl.NewRectangle(900, 794, 500, 34)
	if a.settings.RXEmphasisMode == "ssb" && rl.IsMouseButtonDown(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, slider) {
		a.settings.RXEmphasisMix = math.Round(max(0.0, min(100.0, float64((mouse.X-slider.X)/slider.Width*100)))/5) * 5
		a.settings.RXEmphasis = true
		changed = true
	}
	if !changed {
		return
	}
	applyRXEmphasis(&a.config, a.settings.RXEmphasis, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
	a.applyDSPAndSave()
}

func applyRXEmphasis(config *audio.Config, enabled bool, profile string, amount float64) {
	config.Emphasis, config.SSBClarityDB = audio.EmphasisOff, 0
	if !enabled {
		return
	}
	switch profile {
	case "fm50":
		config.Emphasis, config.EmphasisMicroseconds = audio.DeEmphasis, 50
	case "fm75":
		config.Emphasis, config.EmphasisMicroseconds = audio.DeEmphasis, 75
	default:
		config.SSBClarityDB = max(0, min(100, amount)) * 9 / 100
	}
}

func (a *app) toggleActiveSection() {
	switch a.activeTab {
	case 0:
		a.settings.FiltersEnabled = !a.settings.FiltersEnabled
	case 1:
		a.config.EQEnabled = !a.config.EQEnabled
	case 2:
		if a.mode == "rx" {
			a.config.Notch.Enabled = !a.config.Notch.Enabled
		} else {
			a.config.Compressor.Enabled = !a.config.Compressor.Enabled
		}
	case 3:
		a.config.NoiseEnabled = !a.config.NoiseEnabled
	case 4:
		a.settings.GainEnabled = !a.settings.GainEnabled
	}
	a.applyDSPAndSave()
}

func (a *app) sectionEnabled(index int) bool {
	switch index {
	case 0:
		return a.settings.FiltersEnabled
	case 1:
		return a.config.EQEnabled
	case 2:
		if a.mode == "rx" {
			return a.config.Notch.Enabled
		}
		return a.config.Compressor.Enabled
	case 3:
		return a.config.NoiseEnabled
	case 4:
		return a.settings.GainEnabled
	}
	return false
}

func (a *app) applyDSP() {
	effective := a.config
	if !a.settings.FiltersEnabled {
		effective.LowCutHz, effective.HighCutHz = 20, audio.DefaultSampleRate*.45
	}
	if !a.settings.GainEnabled {
		effective.OutputGainDB = 0
	}
	a.processor.Configure(effective)
}

func (a *app) applyDSPAndSave() {
	a.applyDSP()
	a.save()
}

func (a *app) draw(mouse rl.Vector2) {
	status := a.engine.Status()
	drawHeader(a, status)
	inputLabel, outputLabel := "MICRÓFONO", "SALIDA A EMISORA"
	if a.mode == "rx" {
		inputLabel, outputLabel = "ENTRADA DESDE EMISORA", "ALTAVOCES"
	}
	drawSelector(30, 92, 710, inputLabel, deviceName(a.inputs, a.inIndex), mouse)
	drawSelector(860, 92, 710, outputLabel, deviceName(a.outputs, a.outIndex), mouse)
	a.drawSpectrum(30, 175, 1540, 190)
	a.drawMeters(status)
	if a.mode == "tx" {
		a.drawPTT(mouse)
	} else {
		a.drawRXPanel(status)
	}
	a.drawTabs(mouse)
	a.drawDSPPanel()
	if a.message != "" {
		drawText(a.message, 30, 875, 18, color.muted)
	}
	if a.openDeviceMenu >= 0 {
		a.drawDeviceMenu(mouse)
	}
	if a.pttSettingsOpen {
		a.drawPTTSettings(mouse)
	}
}

func (a *app) drawDeviceMenu(mouse rl.Vector2) {
	devices := a.inputs
	selected, x := a.inIndex, float32(30)
	if a.openDeviceMenu == 1 {
		devices, selected, x = a.outputs, a.outIndex, 860
	}
	visible := min(8, len(devices))
	panel := rl.NewRectangle(x, 160, 710, float32(visible*44+8))
	rl.DrawRectangleRounded(panel, .04, 8, rl.NewColor(10, 23, 34, 255))
	rl.DrawRectangleRoundedLines(panel, .04, 8, color.cyan)
	for row := 0; row < visible; row++ {
		index := a.deviceMenuScroll + row
		item := rl.NewRectangle(x+4, panel.Y+4+float32(row*44), 702, 42)
		if index == selected {
			rl.DrawRectangleRounded(item, .08, 5, rl.NewColor(23, 70, 72, 255))
		} else if rl.CheckCollisionPointRec(mouse, item) {
			rl.DrawRectangleRounded(item, .08, 5, rl.NewColor(27, 46, 61, 255))
		}
		label := devices[index].Name
		if devices[index].IsDefault {
			label += "  ·  PREDETERMINADO"
		}
		drawText(ellipsis(label, 58), int32(x+20), int32(item.Y+9), 17, color.text)
		if index == selected {
			drawText("✓", int32(x+665), int32(item.Y+8), 18, color.cyan)
		}
	}
	if a.deviceMenuScroll > 0 {
		drawText("▲", int32(x+675), 165, 14, color.amber)
	}
	if a.deviceMenuScroll+visible < len(devices) {
		drawText("▼", int32(x+675), int32(panel.Y+panel.Height-23), 14, color.amber)
	}
}

func drawHeader(a *app, status audioio.Status) {
	drawMilitaryText("IC-RADIO", 30, 18, 42, color.text)
	connected := a.ptt.Connected()
	dot := color.amber
	state := "PTT SIN CONECTAR"
	if connected {
		dot, state = color.green, "RADIO CONECTADA"
	}
	rl.DrawCircle(335, 45, 9, dot)
	drawText(state, 355, 30, 21, color.amber)
	drawButton(rl.NewRectangle(650, 22, 100, 48), "RX", a.mode == "rx")
	drawButton(rl.NewRectangle(760, 22, 100, 48), "TX", a.mode == "tx")
	portName := "SELECCIONAR COM"
	if len(a.ports) > 0 {
		portName = a.ports[a.comIndex].Name
	}
	drawButton(rl.NewRectangle(920, 22, 200, 48), portName, false)
	drawButton(rl.NewRectangle(1130, 22, 75, 48), string(a.line), false)
	drawButton(rl.NewRectangle(1215, 22, 155, 48), "CONECTAR", connected)
	drawButton(rl.NewRectangle(1380, 22, 190, 48), "AJUSTES PTT", false)
	_ = status
}

func (a *app) drawRXPanel(status audioio.Status) {
	r := rl.NewRectangle(530, 385, 540, 185)
	rl.DrawRectangleRounded(r, .25, 16, rl.NewColor(16, 55, 70, 255))
	stateColor := color.cyan
	if a.radioState == stateError {
		stateColor = color.red
	}
	for i := 0; i < 3; i++ {
		rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X-float32(i), r.Y-float32(i), r.Width+2*float32(i), r.Height+2*float32(i)), .25, 16, rl.NewColor(stateColor.R, stateColor.G, stateColor.B, uint8(220-i*55)))
	}
	drawMilitaryCentered("RX", r, 68, color.text, -27)
	drawCentered(a.radioStateLabel(), r, 23, stateColor, 31)
	drawCentered(fmt.Sprintf("48 kHz · %.0f %% CPU", status.CPULoad), r, 16, color.muted, 70)
}

func drawSelector(x, y, w float32, label, value string, mouse rl.Vector2) {
	r := rl.NewRectangle(x, y, w, 64)
	fill := color.panel2
	if rl.CheckCollisionPointRec(mouse, r) {
		fill = rl.NewColor(25, 48, 65, 255)
	}
	rl.DrawRectangleRounded(r, .14, 8, fill)
	rl.DrawRectangleRoundedLines(r, .14, 8, color.border)
	drawText(label, int32(x+22), int32(y+10), 18, color.muted)
	drawText(ellipsis(value, 42), int32(x+210), int32(y+17), 25, color.text)
	drawText("⌄", int32(x+w-42), int32(y+14), 28, color.text)
}

func (a *app) drawSpectrum(x, y, w, h float32) {
	r := rl.NewRectangle(x, y, w, h)
	rl.DrawRectangleRounded(r, .04, 8, color.panel)
	for i := 0; i <= 8; i++ {
		xx := x + 68 + float32(i)*(w-86)/8
		rl.DrawLine(int32(xx), int32(y+18), int32(xx), int32(y+h-22), rl.NewColor(24, 60, 78, 255))
	}
	for i := 0; i <= 6; i++ {
		yy := y + 20 + float32(i)*(h-40)/6
		rl.DrawLine(int32(x+65), int32(yy), int32(x+w-18), int32(yy), rl.NewColor(30, 69, 87, 255))
		drawText(fmt.Sprintf("%d dB", -i*10), int32(x+12), int32(yy-9), 15, color.muted)
	}
	base := y + h - 22
	barW := (w - 90) / float32(len(a.spectrum))
	for i, value := range a.spectrum {
		norm := float32(math.Max(0, math.Min(1, float64((value+80)/80))))
		bh := norm * (h - 55)
		xx := x + 68 + float32(i)*barW
		brightHeight := bh * .42
		rl.DrawRectangle(int32(xx), int32(base-bh), max(2, int32(barW-1)), int32(brightHeight), rl.NewColor(29, 235, 247, 245))
		rl.DrawRectangle(int32(xx), int32(base-bh+brightHeight), max(2, int32(barW-1)), int32(bh-brightHeight), rl.NewColor(0, 151, 183, 235))
		if bh > 3 {
			rl.DrawRectangle(int32(xx), int32(base-bh), max(2, int32(barW-1)), 2, rl.NewColor(175, 252, 255, 255))
		}
	}
	labels := []string{"100 Hz", "500 Hz", "1 kHz", "2 kHz", "3 kHz", "5 kHz", "10 kHz"}
	for i, label := range labels {
		drawText(label, int32(x+70+float32(i)*(w-150)/6), int32(y+h-20), 14, color.muted)
	}
	graphX, graphW := x+68, w-86
	lowX := graphX + float32(a.config.LowCutHz/10000)*graphW
	highX := graphX + float32(a.config.HighCutHz/10000)*graphW
	if a.settings.FiltersEnabled {
		rl.DrawRectangle(int32(graphX), int32(y+20), int32(max(0, lowX-graphX)), int32(h-42), rl.NewColor(2, 7, 12, 155))
		rl.DrawRectangle(int32(highX), int32(y+20), int32(max(0, graphX+graphW-highX)), int32(h-42), rl.NewColor(2, 7, 12, 155))
		rl.DrawLineEx(rl.NewVector2(lowX, y+18), rl.NewVector2(lowX, y+h-22), 4, color.amber)
		rl.DrawLineEx(rl.NewVector2(highX, y+18), rl.NewVector2(highX, y+h-22), 4, color.amber)
		drawText(fmt.Sprintf("%.0f Hz", a.config.LowCutHz), int32(lowX+7), int32(y+23), 14, color.amber)
		drawText(fmt.Sprintf("%.0f Hz", a.config.HighCutHz), int32(highX+7), int32(y+23), 14, color.amber)
	}
	if a.mode == "rx" {
		centerX := graphX + float32(a.config.Notch.FrequencyHz/10000)*graphW
		halfWidthX := float32(a.config.Notch.WidthHz/20000) * graphW
		leftX, rightX := max(graphX, centerX-halfWidthX), min(graphX+graphW, centerX+halfWidthX)
		fillAlpha, lineAlpha := uint8(40), uint8(105)
		if a.config.Notch.Enabled {
			fillAlpha, lineAlpha = 95, 255
		}
		rl.DrawRectangle(int32(leftX), int32(y+20), max(1, int32(rightX-leftX)), int32(h-42), rl.NewColor(220, 42, 55, fillAlpha))
		for hatchX := leftX; hatchX <= rightX; hatchX += 8 {
			rl.DrawLine(int32(hatchX), int32(y+20), int32(hatchX), int32(y+h-22), rl.NewColor(255, 126, 50, fillAlpha))
		}
		rl.DrawLineEx(rl.NewVector2(leftX, y+18), rl.NewVector2(leftX, y+h-22), 3, rl.NewColor(color.amber.R, color.amber.G, color.amber.B, lineAlpha))
		rl.DrawLineEx(rl.NewVector2(rightX, y+18), rl.NewVector2(rightX, y+h-22), 3, rl.NewColor(color.amber.R, color.amber.G, color.amber.B, lineAlpha))
		rl.DrawLineEx(rl.NewVector2(centerX, y+18), rl.NewVector2(centerX, y+h-22), 4, rl.NewColor(color.red.R, color.red.G, color.red.B, lineAlpha))
		labelColor := rl.NewColor(color.amber.R, color.amber.G, color.amber.B, lineAlpha)
		drawText(fmt.Sprintf("NOTCH %.0f Hz · %.0f Hz", a.config.Notch.FrequencyHz, a.config.Notch.WidthHz), int32(centerX+8), int32(y+52), 13, labelColor)
		a.drawEmphasisCurve(rl.NewRectangle(graphX, y+20, graphW, h-42))
	}
}

func (a *app) drawEmphasisCurve(graph rl.Rectangle) {
	if !a.settings.RXEmphasis {
		return
	}
	curveColor := rl.NewColor(191, 112, 255, 245)
	baseline := graph.Y + graph.Height*.42
	var previous rl.Vector2
	for i := 0; i <= 80; i++ {
		frequency := 20 + float64(i)/80*9980
		cfg := audio.DefaultConfig()
		cfg.LowCutHz, cfg.HighCutHz, cfg.EQEnabled, cfg.Notch.Enabled = 20, audio.DefaultSampleRate*.45, false, false
		applyRXEmphasis(&cfg, true, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
		off := cfg
		applyRXEmphasis(&off, false, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
		db := audio.ResponseDB(cfg, audio.DefaultSampleRate, frequency) - audio.ResponseDB(off, audio.DefaultSampleRate, frequency)
		point := rl.NewVector2(graph.X+float32(i)/80*graph.Width, baseline-float32(db)*3.2)
		if i > 0 {
			rl.DrawLineEx(previous, point, 3, curveColor)
		}
		previous = point
	}
	label := "ÉNFASIS SSB"
	if a.settings.RXEmphasisMode == "fm50" {
		label = "DEÉNFASIS FM · 50 µs"
	} else if a.settings.RXEmphasisMode == "fm75" {
		label = "DEÉNFASIS FM · 75 µs"
	}
	drawText(label, int32(graph.X+graph.Width-230), int32(graph.Y+8), 14, curveColor)
}

func (a *app) drawMeters(status audioio.Status) {
	drawMeter(30, 385, 440, 185, "ENTRADA", status.InputRMSDBFS)
	drawMeter(1130, 385, 440, 185, "SALIDA", status.OutputRMSDBFS)
	if a.mode == "tx" {
		drawMonitorButton(rl.NewRectangle(1460, 403, 82, 40), a.settings.MonitorEnabled)
	}
}

func drawMonitorButton(r rl.Rectangle, enabled bool) {
	fill := rl.NewColor(75, 25, 31, 255)
	border := rl.NewColor(162, 54, 64, 255)
	if enabled {
		fill, border = rl.NewColor(190, 17, 31, 255), rl.NewColor(255, 73, 84, 255)
		for i := 4; i >= 1; i-- {
			expand := float32(i * 2)
			rl.DrawRectangleRounded(rl.NewRectangle(r.X-expand, r.Y-expand, r.Width+expand*2, r.Height+expand*2), .2, 8, rl.NewColor(235, 25, 39, uint8(8+i*4)))
		}
	}
	rl.DrawRectangleRounded(r, .2, 8, fill)
	rl.DrawRectangleRoundedLines(r, .2, 8, border)
	drawCentered("MON", r, 18, color.text, 0)
}

func drawMeter(x, y, w, h float32, label string, db float32) {
	r := rl.NewRectangle(x, y, w, h)
	rl.DrawRectangleRounded(r, .09, 8, color.panel)
	rl.DrawRectangleRoundedLines(r, .09, 8, color.border)
	drawText(label, int32(x+25), int32(y+22), 28, color.text)
	bar := rl.NewRectangle(x+25, y+88, w-50, 46)
	rl.DrawRectangleRounded(bar, .15, 6, rl.NewColor(8, 15, 22, 255))
	norm := float32(math.Max(0, math.Min(1, float64((db+60)/60))))
	for i := 0; i < 30; i++ {
		xx := bar.X + 7 + float32(i)*(bar.Width-14)/30
		c := color.green
		if i > 23 {
			c = color.amber
		}
		if i > 27 {
			c = color.red
		}
		if float32(i)/30 > norm {
			c = rl.NewColor(48, 60, 69, 255)
		}
		rl.DrawRectangle(int32(xx), int32(bar.Y+8), int32((bar.Width-20)/32), int32(bar.Height-16), c)
	}
	drawText("-60       -40       -20       -10    -3  0 dB", int32(x+28), int32(y+145), 15, color.muted)
}

func (a *app) drawPTT(mouse rl.Vector2) {
	r := rl.NewRectangle(530, 385, 540, 185)
	tx := a.ptt.TX()
	fill := rl.NewColor(25, 47, 62, 255)
	outline := color.cyan
	if tx {
		fill, outline = rl.NewColor(173, 5, 18, 255), color.red
	}
	if rl.CheckCollisionPointRec(mouse, r) {
		fill = mix(fill, rl.White, .08)
	}
	if tx {
		for i := 9; i >= 1; i-- {
			expand := float32(i * 4)
			glow := rl.NewRectangle(r.X-expand, r.Y-expand, r.Width+expand*2, r.Height+expand*2)
			rl.DrawRectangleRounded(glow, .25, 16, rl.NewColor(235, 25, 39, uint8(5+i*2)))
		}
	}
	rl.DrawRectangleRounded(r, .25, 16, fill)
	for i := 0; i < 4; i++ {
		rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X-float32(i), r.Y-float32(i), r.Width+2*float32(i), r.Height+2*float32(i)), .25, 16, rl.NewColor(outline.R, outline.G, outline.B, uint8(255-i*45)))
	}
	mainText, sub := "PTT", "PULSAR PARA TRANSMITIR"
	if tx {
		mainText, sub = "TX", "PULSAR PARA VOLVER A RX"
	}
	drawMilitaryCentered(mainText, r, 84, color.text, -34)
	drawCentered(sub, r, 23, color.text, 42)
	if tx {
		drawCentered(time.Since(a.txSince).Truncate(time.Second).String(), r, 27, color.text, 82)
	} else {
		mode := "MANTENER"
		if a.settings.PTTMode == "lock" {
			mode = "PTT LOCK"
		}
		drawCentered(a.radioStateLabel()+" · "+mode+" · "+ellipsis(a.shortcutLabel(), 18), r, 17, color.muted, 82)
	}
}

func (a *app) radioStateLabel() string {
	switch a.radioState {
	case stateRX:
		return "RX ESCUCHANDO"
	case stateTXReady:
		if a.rxView.engine != nil && a.rxView.engine.Status().Running {
			return "TX LISTO · RX ACTIVO"
		}
		return "TX PREPARADO"
	case stateTransmitting:
		return "TRANSMITIENDO"
	case stateError:
		return "ERROR DE RUTA"
	default:
		return "INICIALIZANDO"
	}
}

func (a *app) drawPTTSettings(mouse rl.Vector2) {
	rl.DrawRectangle(0, 0, viewW, viewH, rl.NewColor(0, 0, 0, 190))
	panel := rl.NewRectangle(400, 200, 800, 590)
	rl.DrawRectangleRounded(panel, .05, 12, rl.NewColor(15, 29, 41, 255))
	rl.DrawRectangleRoundedLines(panel, .05, 12, color.cyan)
	drawText("CONFIGURACIÓN PTT", 450, 235, 30, color.text)
	drawButton(rl.NewRectangle(1125, 225, 45, 45), "X", false)
	drawText("ATAJO DE TECLADO", 500, 295, 18, color.muted)
	captureLabel := a.shortcutLabel()
	if a.capturingPTTKey {
		captureLabel = "PULSA UNA TECLA O COMBINACIÓN…"
	}
	drawButton(rl.NewRectangle(500, 330, 400, 60), captureLabel, a.capturingPTTKey)
	drawButton(rl.NewRectangle(920, 330, 180, 60), "RESTABLECER", false)
	drawText("Escape siempre queda reservado para volver inmediatamente a RX.", 500, 405, 17, color.muted)
	drawText("COMPORTAMIENTO", 500, 435, 18, color.muted)
	drawModeCard(rl.NewRectangle(500, 465, 280, 105), "MANTENER", "TX mientras se mantiene pulsado", a.settings.PTTMode == "hold", mouse)
	drawModeCard(rl.NewRectangle(820, 465, 280, 105), "PTT LOCK", "Una pulsación TX, otra RX", a.settings.PTTMode == "lock", mouse)
	drawText("SEGURIDAD", 500, 610, 18, color.muted)
	drawWrappedText("Al perder el foco, cerrar IC-MIC o desconectar el puerto, el modo Mantener vuelve automáticamente a RX.", 500, 642, 620, 17, 26, color.text)
	drawButton(rl.NewRectangle(500, 715, 200, 55), "CERRAR", false)
	drawButton(rl.NewRectangle(900, 715, 200, 55), "GUARDAR", true)
}

func drawModeCard(r rl.Rectangle, title, description string, selected bool, mouse rl.Vector2) {
	fill := color.panel2
	if selected {
		fill = rl.NewColor(22, 74, 70, 255)
	} else if rl.CheckCollisionPointRec(mouse, r) {
		fill = rl.NewColor(28, 48, 63, 255)
	}
	rl.DrawRectangleRounded(r, .12, 8, fill)
	border := color.border
	if selected {
		border = color.cyan
	}
	rl.DrawRectangleRoundedLines(r, .12, 8, border)
	rl.DrawCircle(int32(r.X+28), int32(r.Y+31), 8, border)
	if selected {
		rl.DrawCircle(int32(r.X+28), int32(r.Y+31), 4, color.cyan)
	}
	drawText(title, int32(r.X+48), int32(r.Y+17), 21, color.text)
	drawWrappedText(description, int32(r.X+20), int32(r.Y+59), int32(r.Width-40), 15, 22, color.muted)
}

func drawWrappedText(text string, x, y, maxWidth, size, lineHeight int32, c rl.Color) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}
	line := words[0]
	lineY := y
	for _, word := range words[1:] {
		candidate := line + " " + word
		actualSize := readableSize(size)
		measurement := rl.MeasureTextEx(interfaceFont(actualSize), candidate, float32(actualSize), fontSpacing(actualSize))
		if measurement.X <= float32(maxWidth) {
			line = candidate
			continue
		}
		drawText(line, x, lineY, size, c)
		line, lineY = word, lineY+lineHeight
	}
	drawText(line, x, lineY, size, c)
}

func (a *app) drawTabs(mouse rl.Vector2) {
	labels := []string{"FILTROS", "ECUALIZADOR", "COMPRESOR", "REDUCCIÓN DE RUIDO", "GANANCIA"}
	if a.mode == "rx" {
		labels[2] = "NOTCH"
	}
	for i, label := range labels {
		r := rl.NewRectangle(float32(20+i*312), 590, 300, 56)
		active := i == a.activeTab
		fill := color.panel
		if active {
			fill = color.panel2
		}
		rl.DrawRectangleRounded(r, .12, 6, fill)
		stateColor := color.muted
		if a.sectionEnabled(i) {
			stateColor = color.green
		}
		rl.DrawCircle(int32(r.X+20), int32(r.Y+r.Height/2), 6, stateColor)
		if active {
			rl.DrawRectangle(int32(r.X+10), int32(r.Y+r.Height-4), int32(r.Width-20), 4, color.amber)
		}
		c := color.text
		if active {
			c = color.amber
		}
		drawCentered(label, r, 20, c, 0)
		_ = mouse
	}
}

func (a *app) drawDSPPanel() {
	r := rl.NewRectangle(20, 650, 1560, 220)
	rl.DrawRectangleRounded(r, .04, 8, color.panel)
	rl.DrawRectangleRoundedLines(r, .04, 8, color.border)
	if a.mode == "tx" {
		drawButton(rl.NewRectangle(1170, 665, 180, 46), "TONO 1 kHz", a.txSignal.enabled.Load())
	}
	drawSectionToggle(a.sectionEnabled(a.activeTab))
	switch a.activeTab {
	case 0:
		a.drawFilters()
	case 1:
		a.drawEQ()
	case 2:
		if a.mode == "rx" {
			a.drawNotch()
		} else {
			a.drawCompressor()
		}
	case 3:
		a.drawNoiseReduction()
	case 4:
		a.drawGain()
	}
}

func (a *app) drawFilters() {
	drawText("PASO DE BANDA PARA VOZ", 55, 680, 25, color.amber)
	drawText(fmt.Sprintf("Corte inferior: %.0f Hz", a.config.LowCutHz), 55, 730, 22, color.text)
	drawText(fmt.Sprintf("Corte superior: %.0f Hz", a.config.HighCutHz), 55, 775, 22, color.text)
	drawText("Arrastra las líneas ámbar sobre el espectro.", 55, 835, 18, color.muted)
	if a.mode == "rx" {
		a.drawRXEmphasisControls()
	}
}

func (a *app) drawRXEmphasisControls() {
	rl.DrawLine(775, 675, 775, 850, color.border)
	drawText("ÉNFASIS RX", 810, 742, 22, color.amber)
	drawButton(rl.NewRectangle(810, 684, 165, 42), "ACTIVADO", a.settings.RXEmphasis)
	profiles := []struct{ key, label string }{{"ssb", "SSB"}, {"fm50", "FM 50 µs"}, {"fm75", "FM 75 µs"}}
	for i, profile := range profiles {
		drawButton(rl.NewRectangle(float32(970+i*135), 684, 125, 42), profile.label, a.settings.RXEmphasisMode == profile.key)
	}
	if a.settings.RXEmphasisMode == "ssb" {
		drawText(fmt.Sprintf("Presencia SSB: %.0f %%", a.settings.RXEmphasisMix), 810, 775, 17, color.muted)
		drawCompactSlider(rl.NewRectangle(900, 803, 500, 14), a.settings.RXEmphasisMix)
		drawText("0 %", 810, 797, 15, color.muted)
		drawText("100 %", 1415, 797, 15, color.muted)
	} else {
		drawText("Deénfasis FM normalizado · intensidad fija", 810, 790, 18, color.muted)
		drawText("La curva violeta muestra la respuesta aplicada.", 810, 830, 17, color.text)
	}
}

func drawCompactSlider(track rl.Rectangle, percent float64) {
	percent = max(0, min(100, percent))
	rl.DrawRectangleRounded(track, .5, 8, rl.NewColor(44, 61, 73, 255))
	fill := rl.NewRectangle(track.X, track.Y, track.Width*float32(percent/100), track.Height)
	rl.DrawRectangleRounded(fill, .5, 8, rl.NewColor(191, 112, 255, 255))
	x := track.X + track.Width*float32(percent/100)
	rl.DrawCircle(int32(x), int32(track.Y+track.Height/2), 10, color.text)
	rl.DrawCircle(int32(x), int32(track.Y+track.Height/2), 6, rl.NewColor(191, 112, 255, 255))
}

func (a *app) drawEQ() {
	graph := rl.NewRectangle(330, 680, 850, 170)
	for i := 0; i <= 4; i++ {
		y := graph.Y + float32(i)*graph.Height/4
		rl.DrawLine(int32(graph.X), int32(y), int32(graph.X+graph.Width), int32(y), rl.NewColor(47, 76, 91, 255))
		drawText(fmt.Sprintf("%+d dB", 12-i*6), 260, int32(y-9), 15, color.muted)
	}
	points := make([]rl.Vector2, audio.EQBandCount)
	for i, gain := range a.config.EQGainsDB {
		x := graph.X + float32(i)*graph.Width/float32(audio.EQBandCount-1)
		y := graph.Y + float32((12-gain)/24)*graph.Height
		points[i] = rl.NewVector2(x, y)
		if i > 0 {
			rl.DrawLineEx(points[i-1], points[i], 4, color.cyan)
		}
		rl.DrawCircle(int32(x), int32(y), 8, color.text)
		rl.DrawCircle(int32(x), int32(y), 5, color.cyan)
		drawText(fmt.Sprintf("%.0f", audio.EQFrequencies[i]), int32(x-15), 852, 13, color.muted)
	}
	drawText("ARRASTRA LOS PUNTOS", 1230, 710, 21, color.amber)
	drawText("Ecualización de voz", 1230, 750, 20, color.text)
	drawText("-12 a +12 dB", 1230, 785, 18, color.muted)
	drawButton(rl.NewRectangle(1230, 820, 150, 44), "RESET EQ", false)
}

func (a *app) drawCompressor() {
	percent := (-(a.config.Compressor.TargetDBFS) - 6) / 24 * 100
	drawText("COMPRESIÓN", 55, 680, 25, color.amber)
	drawText("Pasos predefinidos: 0 · 20 · 40 · 60 · 80 · 100 %", 55, 730, 20, color.muted)
	drawValueSlider(percent, fmt.Sprintf("%.0f %%", percent), "SUAVE", "MÁXIMA")
	drawText(fmt.Sprintf("Objetivo automático: %.0f dBFS", a.config.Compressor.TargetDBFS), 1210, 835, 19, color.text)
}

func (a *app) drawNotch() {
	percent := (a.config.Notch.FrequencyHz - 100) / 3900 * 100
	drawText("FILTRO NOTCH", 55, 680, 25, color.amber)
	drawText("Arrastra el centro rojo y los bordes ámbar directamente sobre el espectro", 55, 730, 20, color.muted)
	drawValueSlider(percent, fmt.Sprintf("%.0f Hz", a.config.Notch.FrequencyHz), "100 Hz", "4 kHz")
	drawText(fmt.Sprintf("Ancho %.0f Hz · %.0f dB · Rueda: ancho · Shift+rueda: profundidad", a.config.Notch.WidthHz, a.config.Notch.DepthDB), 640, 835, 16, color.text)
}

func (a *app) drawNoiseReduction() {
	drawText("REDUCCIÓN DE RUIDO", 55, 680, 25, color.amber)
	drawText("Nivel de reducción", 55, 730, 20, color.muted)
	drawValueSlider(a.config.NoiseStrength, fmt.Sprintf("%.0f %%", a.config.NoiseStrength), "0 %", "100 %")
	drawText("INTEGRACIÓN", 1200, 720, 19, color.muted)
	labels := []string{"LENTO", "MEDIO", "RÁPIDO"}
	speeds := []audio.NoiseSpeed{audio.NoiseSlow, audio.NoiseNormal, audio.NoiseFast}
	for i, label := range labels {
		drawButton(rl.NewRectangle(float32(1200+i*115), 765, 105, 46), label, a.config.NoiseSpeed == speeds[i])
	}
}

func (a *app) drawGain() {
	percent := (a.config.OutputGainDB + 12) / 24 * 100
	drawText("GANANCIA DE SALIDA", 55, 680, 25, color.amber)
	drawText("Calibra el nivel enviado a la entrada de audio USB de la radio", 55, 730, 20, color.muted)
	drawValueSlider(percent, fmt.Sprintf("%+.1f dB", a.config.OutputGainDB), "-12 dB", "+12 dB")
}

func drawValueSlider(percent float64, value, left, right string) {
	percent = max(0, min(100, percent))
	track := rl.NewRectangle(270, 792, 900, 16)
	rl.DrawRectangleRounded(track, .5, 8, rl.NewColor(45, 65, 77, 255))
	rl.DrawRectangleRounded(rl.NewRectangle(track.X, track.Y, track.Width*float32(percent/100), track.Height), .5, 8, color.amber)
	handleX := track.X + track.Width*float32(percent/100)
	rl.DrawCircle(int32(handleX), int32(track.Y+track.Height/2), 13, color.text)
	rl.DrawCircle(int32(handleX), int32(track.Y+track.Height/2), 8, color.amber)
	for i := 0; i <= 5; i++ {
		rl.DrawLine(int32(track.X+float32(i)*track.Width/5), int32(track.Y+24), int32(track.X+float32(i)*track.Width/5), int32(track.Y+32), color.border)
	}
	drawText(left, 270, 833, 16, color.muted)
	drawText(right, 1095, 833, 16, color.muted)
	drawText(value, 1210, 775, 38, color.text)
}

func drawSectionToggle(enabled bool) {
	label := "DESACTIVADO"
	if enabled {
		label = "ACTIVADO"
	}
	drawButton(rl.NewRectangle(1370, 665, 180, 46), label, enabled)
}

func drawButton(r rl.Rectangle, label string, active bool) {
	fill := color.panel2
	if active {
		fill = rl.NewColor(25, 75, 60, 255)
	}
	rl.DrawRectangleRounded(r, .18, 6, fill)
	rl.DrawRectangleRoundedLines(r, .18, 6, color.border)
	drawCentered(label, r, 18, color.text, 0)
}

func drawCentered(text string, r rl.Rectangle, size int32, c rl.Color, offsetY float32) {
	actualSize := readableSize(size)
	font := interfaceFont(actualSize)
	measurement := rl.MeasureTextEx(font, text, float32(actualSize), fontSpacing(actualSize))
	drawTextAtSize(text, int32(r.X+(r.Width-measurement.X)/2), int32(r.Y+(r.Height-measurement.Y)/2+offsetY), actualSize, c)
}

func drawMilitaryCentered(text string, r rl.Rectangle, size int32, c rl.Color, offsetY float32) {
	measurement := rl.MeasureTextEx(militaryFont, text, float32(size), 1)
	rl.DrawTextEx(militaryFont, text, rl.NewVector2(r.X+(r.Width-measurement.X)/2, r.Y+(r.Height-measurement.Y)/2+offsetY), float32(size), 1, c)
}

func drawText(text string, x, y, size int32, c rl.Color) {
	drawTextAtSize(text, x, y, readableSize(size), c)
}

func drawTextAtSize(text string, x, y, size int32, c rl.Color) {
	rl.DrawTextEx(interfaceFont(size), text, rl.NewVector2(float32(x), float32(y)), float32(size), fontSpacing(size), c)
}

func drawMilitaryText(text string, x, y, size int32, c rl.Color) {
	rl.DrawTextEx(militaryFont, text, rl.NewVector2(float32(x), float32(y)), float32(size), 1, c)
}

func interfaceFont(size int32) rl.Font {
	if size <= 17 {
		return regularFont
	}
	return semiboldFont
}

func readableSize(size int32) int32 {
	if size <= 15 {
		return size + 4
	}
	if size <= 18 {
		return size + 3
	}
	if size <= 24 {
		return size + 2
	}
	return size
}

func fontSpacing(size int32) float32 {
	if size <= 18 {
		return .35
	}
	return .6
}

func latinCodepoints() []rune {
	result := make([]rune, 0, 228)
	for value := rune(32); value <= 255; value++ {
		result = append(result, value)
	}
	return append(result, '…', '⌄', '·', '✓', '▲', '▼')
}

func clicked(mouse rl.Vector2, r rl.Rectangle) bool {
	return rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, r)
}

func destinationRect() rl.Rectangle {
	w, h := float32(rl.GetScreenWidth()), float32(rl.GetScreenHeight())
	scale := min(w/viewW, h/viewH)
	return rl.NewRectangle((w-viewW*scale)/2, (h-viewH*scale)/2, viewW*scale, viewH*scale)
}

func virtualMouse() rl.Vector2 {
	d := destinationRect()
	m := rl.GetMousePosition()
	return rl.NewVector2((m.X-d.X)*viewW/d.Width, (m.Y-d.Y)*viewH/d.Height)
}

func deviceName(devices []audioio.Device, index int) string {
	if len(devices) == 0 {
		return "Sin dispositivos"
	}
	return devices[index].Name
}
func namedDevice(devices []audioio.Device, name string) int {
	for i, d := range devices {
		if d.Name == name {
			return i
		}
	}
	for i, d := range devices {
		if d.IsDefault {
			return i
		}
	}
	return 0
}
func namedPort(ports []ptt.Port, name string) int {
	for i, p := range ports {
		if p.Name == name {
			return i
		}
	}
	return 0
}
func ellipsis(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit-1]) + "…"
}
func mix(a, b rl.Color, amount float32) rl.Color {
	f := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*amount) }
	return rl.NewColor(f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255)
}

func (a *app) shortcutLabel() string {
	parts := ""
	if a.settings.PTTCtrl {
		parts += "CTRL + "
	}
	if a.settings.PTTAlt {
		parts += "ALT + "
	}
	if a.settings.PTTShift {
		parts += "SHIFT + "
	}
	key := a.settings.PTTKey
	if key == 0 {
		key = rl.KeySpace
	}
	return parts + bindingKeyName(key)
}

func bindingKeyName(key int32) string {
	if key >= rl.KeyA && key <= rl.KeyZ {
		return string(rune('A' + key - rl.KeyA))
	}
	if key >= rl.KeyZero && key <= rl.KeyNine {
		return string(rune('0' + key - rl.KeyZero))
	}
	if key >= rl.KeyF1 && key <= rl.KeyF12 {
		return fmt.Sprintf("F%d", key-rl.KeyF1+1)
	}
	switch key {
	case rl.KeySpace:
		return "ESPACIO"
	case rl.KeyEnter:
		return "ENTER"
	case rl.KeyTab:
		return "TAB"
	case rl.KeyBackspace:
		return "RETROCESO"
	case rl.KeyInsert:
		return "INSERT"
	case rl.KeyDelete:
		return "SUPR"
	case rl.KeyHome:
		return "INICIO"
	case rl.KeyEnd:
		return "FIN"
	case rl.KeyPageUp:
		return "RE PÁG"
	case rl.KeyPageDown:
		return "AV PÁG"
	case rl.KeyUp:
		return "FLECHA ARRIBA"
	case rl.KeyDown:
		return "FLECHA ABAJO"
	case rl.KeyLeft:
		return "FLECHA IZQUIERDA"
	case rl.KeyRight:
		return "FLECHA DERECHA"
	}
	return fmt.Sprintf("TECLA %d", key)
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "settings.json"
	}
	return filepath.Join(dir, "IC-MIC", "settings.json")
}

func validTab(tab int) int {
	if tab < 0 || tab > 4 {
		return 0
	}
	return tab
}

func (a *app) load() {
	a.config = audio.DefaultConfig()
	a.config.LowCutHz, a.config.HighCutHz = 100, 3000
	a.config.Compressor.Enabled, a.config.Compressor.TargetDBFS = true, -18
	a.settings.FiltersEnabled, a.settings.GainEnabled = true, true
	a.settings.PTTMode, a.settings.PTTKey = "hold", rl.KeySpace
	a.settings.RXEmphasisMode, a.settings.RXEmphasisMix = "ssb", 50
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return
	}
	if json.Unmarshal(data, &a.settings) == nil {
		a.config = a.settings.Config
		if a.settings.Version < 2 {
			a.settings.FiltersEnabled, a.settings.GainEnabled = true, true
		}
		if a.settings.Version < 3 || (a.settings.PTTMode != "hold" && a.settings.PTTMode != "lock") {
			a.settings.PTTMode, a.settings.PTTKey = "hold", rl.KeySpace
			a.settings.PTTCtrl, a.settings.PTTAlt, a.settings.PTTShift = false, false, false
		}
		if a.config.HighCutHz == 0 {
			a.config = audio.DefaultConfig()
			a.config.LowCutHz, a.config.HighCutHz = 100, 3000
		}
		if a.config.NoiseSpeed == "" {
			a.config.NoiseSpeed = audio.NoiseNormal
		}
		if a.settings.Version < 5 || (a.settings.RXEmphasisMode != "ssb" && a.settings.RXEmphasisMode != "fm50" && a.settings.RXEmphasisMode != "fm75") {
			a.settings.RXEmphasis, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix = false, "ssb", 50
		}
	}
}

func (a *app) save() {
	// Capture the active editor before serializing both independent profiles.
	// This prevents an RX adjustment from ever being written into the TX slot.
	if a.txView.processor != nil && a.rxView.processor != nil {
		a.storeActiveView()
		a.settings.Config = a.txView.config
		a.settings.FiltersEnabled = a.txView.filtersEnabled
		a.settings.GainEnabled = a.txView.gainEnabled
		a.settings.TXActiveTab = validTab(a.txView.activeTab)
		a.settings.RXConfig = a.rxView.config
		a.settings.RXFilters = a.rxView.filtersEnabled
		a.settings.RXGain = a.rxView.gainEnabled
		a.settings.RXActiveTab = validTab(a.rxView.activeTab)
		if a.txView.inIndex >= 0 && a.txView.inIndex < len(a.txView.inputs) {
			a.settings.Input = a.txView.inputs[a.txView.inIndex].Name
		}
		if a.txView.outIndex >= 0 && a.txView.outIndex < len(a.txView.outputs) {
			a.settings.Output = a.txView.outputs[a.txView.outIndex].Name
		}
		if a.rxView.inIndex >= 0 && a.rxView.inIndex < len(a.rxView.inputs) {
			a.settings.RXInput = a.rxView.inputs[a.rxView.inIndex].Name
		}
		if a.rxView.outIndex >= 0 && a.rxView.outIndex < len(a.rxView.outputs) {
			a.settings.RXOutput = a.rxView.outputs[a.rxView.outIndex].Name
		}
	}
	a.settings.Version = 7
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}
