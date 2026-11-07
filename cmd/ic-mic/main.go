package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"
	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	appfonts "github.com/LuislopezMartinez/IC-RADIO/assets/fonts"
	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	viewW              = 1600
	viewH              = 900
	spectrumMinHz      = 100.0
	spectrumMaxHz      = 10000.0
	spectrumVisualBars = 96
	stateRX            = "rx_listening"
	stateTXReady       = "tx_ready"
	stateTransmitting  = "transmitting"
	stateError         = "error"
	maxTXDuration      = 3 * time.Minute
)

var color = struct {
	bg, panel, panel2, border, text, muted, cyan, green, amber, red rl.Color
}{
	rl.NewColor(166, 170, 171, 255), rl.NewColor(8, 19, 24, 255), rl.NewColor(18, 28, 33, 255),
	rl.NewColor(89, 99, 102, 255), rl.NewColor(246, 248, 248, 255), rl.NewColor(181, 190, 192, 255),
	rl.NewColor(16, 218, 236, 255), rl.NewColor(54, 230, 119, 255), rl.NewColor(255, 190, 40, 255), rl.NewColor(235, 25, 39, 255),
}

var dspColor = struct {
	text, muted, amber, border rl.Color
}{
	rl.NewColor(8, 11, 12, 255), rl.NewColor(35, 40, 42, 255),
	rl.NewColor(128, 72, 0, 255), rl.NewColor(73, 79, 80, 225),
}

var controlColor = struct {
	well, track, inactive, active, activeEdge, eqLine rl.Color
}{
	rl.NewColor(182, 187, 186, 255), rl.NewColor(38, 52, 58, 255), rl.NewColor(52, 69, 77, 255),
	rl.NewColor(255, 193, 40, 255), rl.NewColor(180, 91, 0, 255), rl.NewColor(31, 205, 220, 255),
}

var regularFont, semiboldFont, militaryFont rl.Font

type testToneProcessor struct {
	base          *audio.Processor
	monitor       sampleMonitor
	enabled       atomic.Bool
	frequencyHz   atomic.Int64
	phase         float64
	fadeMu        sync.Mutex
	fadeRemaining int64
	fadeTotal     int64
}

type sampleMonitor interface {
	Write([]float32)
}

func (p *testToneProcessor) Process(samples []float32) {
	if p.enabled.Load() {
		frequency := max(int64(0), min(int64(5000), p.frequencyHz.Load()))
		phaseStep := 2 * math.Pi * float64(frequency) / audio.DefaultSampleRate
		for i := range samples {
			if frequency == 0 {
				samples[i] = 0
			} else {
				samples[i] = float32(.1 * math.Sin(p.phase))
			}
			p.phase += phaseStep
			if p.phase >= 2*math.Pi {
				p.phase -= 2 * math.Pi
			}
		}
	}
	p.base.Process(samples)
	p.fadeMu.Lock()
	for i := range samples {
		if p.fadeRemaining <= 0 || p.fadeTotal <= 0 {
			break
		}
		gain := 1 - float64(p.fadeRemaining)/float64(p.fadeTotal)
		samples[i] *= float32(max(0.0, min(1.0, gain)))
		p.fadeRemaining--
	}
	p.fadeMu.Unlock()
	if p.monitor != nil {
		p.monitor.Write(samples)
	}
}

func (p *testToneProcessor) startFade(milliseconds float64) {
	p.fadeMu.Lock()
	defer p.fadeMu.Unlock()
	p.fadeTotal = int64(max(0.0, milliseconds) * audio.DefaultSampleRate / 1000)
	p.fadeRemaining = p.fadeTotal
}

func (p *testToneProcessor) cancelFade() {
	p.fadeMu.Lock()
	p.fadeRemaining, p.fadeTotal = 0, 0
	p.fadeMu.Unlock()
}

type settings struct {
	Version         int          `json:"version"`
	Input           string       `json:"input"`
	Output          string       `json:"output"`
	COM             string       `json:"com"`
	PTTLine         ptt.Line     `json:"ptt_line"`
	Config          audio.Config `json:"dsp"`
	FiltersEnabled  bool         `json:"filters_enabled"`
	GainEnabled     bool         `json:"gain_enabled"`
	PTTMode         string       `json:"ptt_mode"`
	PTTKey          int32        `json:"ptt_key"`
	PTTCtrl         bool         `json:"ptt_ctrl"`
	PTTAlt          bool         `json:"ptt_alt"`
	PTTShift        bool         `json:"ptt_shift"`
	RXInput         string       `json:"rx_input"`
	RXOutput        string       `json:"rx_output"`
	RXFollowDefault bool         `json:"rx_follow_default_output"`
	RXConfig        audio.Config `json:"rx_dsp"`
	RXFilters       bool         `json:"rx_filters_enabled"`
	RXGain          bool         `json:"rx_gain_enabled"`
	RXEmphasis      bool         `json:"rx_emphasis_enabled"`
	RXEmphasisMode  string       `json:"rx_emphasis_mode"`
	RXEmphasisMix   float64      `json:"rx_emphasis_amount"`
	TXActiveTab     int          `json:"tx_active_tab"`
	RXActiveTab     int          `json:"rx_active_tab"`
	MonitorEnabled  bool         `json:"tx_monitor_enabled"`
	AlwaysOnTop     bool         `json:"always_on_top"`
	TestToneHz      float64      `json:"test_tone_hz"`
	TXFadeEnabled   bool         `json:"tx_fade_enabled"`
	TXFadeMS        float64      `json:"tx_fade_ms"`
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
	processor               *audio.Processor
	engine                  *audioio.Engine
	ptt                     ptt.Controller
	config                  audio.Config
	settings                settings
	inputs                  []audioio.Device
	outputs                 []audioio.Device
	ports                   []ptt.Port
	inIndex                 int
	outIndex                int
	comIndex                int
	line                    ptt.Line
	message                 string
	txSince                 time.Time
	spectrum                []float32
	spectrumDisplay         []float32
	spectrumAverage         []float32
	spectrumPeak            []float32
	spectrumPeakHold        []float32
	activeTab               int
	dragEQ                  int
	cutoffDrag              int
	controlDrag             int
	notchDrag               int
	echoDrag                int
	txGainDrag              bool
	pttSettingsOpen         bool
	capturingPTTKey         bool
	keyPTTActive            bool
	mousePTTActive          bool
	openDeviceMenu          int
	deviceMenuScroll        int
	mode                    string
	txView                  viewState
	rxView                  viewState
	radioState              string
	pttStartedFromRX        bool
	txSignal                *testToneProcessor
	monitor                 *audioio.PlaybackMonitor
	defaultOutputName       string
	lastDefaultEndpointName string
	lastDefaultCheck        time.Time
}

func main() {
	p := audio.NewProcessor(audio.DefaultSampleRate)
	monitor, monitorErr := audioio.NewPlaybackMonitor()
	txSignal := &testToneProcessor{base: p, monitor: monitor}
	e, err := audioio.New(txSignal)
	if err != nil {
		panic(err)
	}
	a := &app{processor: p, engine: e, config: audio.DefaultConfig(), spectrum: make([]float32, 257), dragEQ: -1, cutoffDrag: -1, controlDrag: -1, notchDrag: -1, echoDrag: -1, openDeviceMenu: -1, line: ptt.RTS, mode: "tx", radioState: stateTXReady, txSignal: txSignal, monitor: monitor}
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
	a.txSignal.frequencyHz.Store(int64(math.Round(a.settings.TestToneHz)))
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
	rxConfig.LowCutHz = max(spectrumMinHz, rxConfig.LowCutHz)
	applyRXEmphasis(&rxConfig, a.settings.RXEmphasis, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
	rxInputs, rxOutputs := rxEngine.Inputs(), rxEngine.Outputs()
	rxOutputIndex := namedDevice(rxOutputs, a.settings.RXOutput)
	if a.settings.RXFollowDefault {
		rxOutputIndex = defaultDeviceIndex(rxOutputs)
		if len(rxOutputs) > 0 {
			a.defaultOutputName = rxOutputs[rxOutputIndex].Name
		}
	}
	a.rxView = viewState{processor: rxProcessor, engine: rxEngine, config: rxConfig, inputs: rxInputs, outputs: rxOutputs,
		inIndex: namedDevice(rxInputs, a.settings.RXInput), outIndex: rxOutputIndex, spectrum: make([]float32, 257),
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

	windowFlags := uint32(rl.FlagWindowResizable | rl.FlagVsyncHint | rl.FlagMsaa4xHint | rl.FlagWindowHighdpi)
	if a.settings.AlwaysOnTop {
		windowFlags |= rl.FlagWindowTopmost
	}
	rl.SetConfigFlags(windowFlags)
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
		drawRackChassis()
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
	a.spectrumDisplay, a.spectrumAverage, a.spectrumPeak, a.spectrumPeakHold = nil, nil, nil, nil
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
		if a.settings.TXFadeEnabled {
			a.txSignal.startFade(a.settings.TXFadeMS)
		} else {
			a.txSignal.cancelFade()
		}
	} else {
		a.txSignal.cancelFade()
	}
	if err := a.ptt.SetTX(enabled); err != nil {
		a.txSignal.cancelFade()
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
	a.updateDefaultRXOutput()
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
		a.echoDrag = -1
		a.txGainDrag = false
	}
	if clicked(mouse, rl.NewRectangle(650, 22, 100, 48)) {
		a.switchMode("rx")
		return
	}
	if clicked(mouse, rl.NewRectangle(760, 22, 100, 48)) {
		a.switchMode("tx")
		return
	}
	if clicked(mouse, onTopButtonRectangle()) {
		a.settings.AlwaysOnTop = !a.settings.AlwaysOnTop
		if a.settings.AlwaysOnTop {
			rl.SetWindowState(rl.FlagWindowTopmost)
			a.message = "Ventana siempre en primer plano"
		} else {
			rl.ClearWindowState(rl.FlagWindowTopmost)
			a.message = "Ventana en modo normal"
		}
		a.save()
	}
	if clicked(mouse, rl.NewRectangle(30, 92, 710, 64)) && len(a.inputs) > 0 {
		a.openDeviceMenu, a.deviceMenuScroll = 0, initialDeviceScroll(a.inIndex, len(a.inputs))
	}
	if clicked(mouse, rl.NewRectangle(860, 92, 710, 64)) && len(a.outputs) > 0 {
		selected, count := a.outIndex, len(a.outputs)
		if a.mode == "rx" {
			count++
			if a.settings.RXFollowDefault {
				selected = 0
			} else {
				selected++
			}
		}
		a.openDeviceMenu, a.deviceMenuScroll = 1, initialDeviceScroll(selected, count)
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
	pttRect := pttRectangle()
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
		if clicked(mouse, tabRectangle(i)) {
			a.activeTab = i
		}
	}
	a.updateCutoffs(mouse)
	a.updateNotchFromSpectrum(mouse)
	a.updateRXEmphasis(mouse)
	a.updateTXFilterControls(mouse)
	a.updatePersistentTXGain(mouse)
	if a.mode == "tx" && clicked(mouse, rl.NewRectangle(1460, 403, 82, 40)) {
		a.toggleOutputMonitor()
	}
	if a.activeTab != 1 && !(a.mode == "tx" && (a.activeTab == 0 || a.activeTab == 4)) && clicked(mouse, rl.NewRectangle(1370, 665, 180, 46)) {
		a.toggleActiveSection()
	}
	if a.activeTab == 1 {
		a.updateEQ(mouse)
	}
	a.updatePanelControls(mouse)
	a.processor.Spectrum(a.spectrum, 10000)
}

func (a *app) updateDefaultRXOutput() {
	if !a.settings.RXFollowDefault || time.Since(a.lastDefaultCheck) < time.Second {
		return
	}
	a.lastDefaultCheck = time.Now()
	name, err := audioio.DefaultPlaybackEndpointName()
	if err != nil || name == "" || name == a.lastDefaultEndpointName {
		return
	}

	view := &a.rxView
	if a.mode == "rx" {
		view = nil
	}
	engine := a.rxView.engine
	inputs, outputs := a.rxView.inputs, a.rxView.outputs
	inIndex := a.rxView.inIndex
	wasRunning := engine != nil && engine.Status().Running
	if a.mode == "rx" {
		engine, inputs, outputs, inIndex = a.engine, a.inputs, a.outputs, a.inIndex
		wasRunning = engine != nil && engine.Status().Running
	}
	if engine == nil {
		return
	}
	inputName := deviceName(inputs, inIndex)
	if err := engine.Refresh(); err != nil {
		a.message = "No se pudo actualizar la salida predeterminada: " + err.Error()
		return
	}
	inputs, outputs = engine.Inputs(), engine.Outputs()
	if len(inputs) == 0 || len(outputs) == 0 {
		a.message = "La salida predeterminada de Windows no está disponible"
		return
	}
	inIndex = namedDevice(inputs, inputName)
	outIndex := namedDevice(outputs, name)
	if wasRunning {
		if err := engine.Start(inIndex, outIndex); err != nil {
			a.message = "Cambiar salida RX: " + err.Error()
			return
		}
	}
	if view == nil {
		a.inputs, a.outputs, a.inIndex, a.outIndex = inputs, outputs, inIndex, outIndex
		a.rxView = a.snapshotView()
	} else {
		view.inputs, view.outputs, view.inIndex, view.outIndex = inputs, outputs, inIndex, outIndex
	}
	a.defaultOutputName = outputs[outIndex].Name
	a.lastDefaultEndpointName = name
	a.settings.RXOutput = a.defaultOutputName
	if a.settings.MonitorEnabled {
		if err := a.startOutputMonitor(); err != nil {
			a.settings.MonitorEnabled = false
			a.message = "Salida RX cambiada, pero MON falló: " + err.Error()
			return
		}
	}
	a.message = "Salida RX predeterminada → " + a.defaultOutputName
	a.save()
}

func (a *app) updateDeviceMenu(mouse rl.Vector2) {
	devices := a.inputs
	x := float32(30)
	selector := rl.NewRectangle(30, 92, 710, 64)
	includeWindowsDefault := false
	if a.openDeviceMenu == 1 {
		devices, x, selector = a.outputs, 860, rl.NewRectangle(860, 92, 710, 64)
		includeWindowsDefault = a.mode == "rx"
	}
	count := len(devices)
	if includeWindowsDefault {
		count++
	}
	visible := min(8, count)
	maxScroll := max(0, count-visible)
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
				itemIndex := a.deviceMenuScroll + row
				if a.openDeviceMenu == 0 {
					a.inIndex = itemIndex
				} else if includeWindowsDefault && itemIndex == 0 {
					a.settings.RXFollowDefault = true
					a.outIndex = defaultDeviceIndex(a.outputs)
					a.defaultOutputName = deviceName(a.outputs, a.outIndex)
					a.lastDefaultEndpointName = ""
				} else {
					deviceIndex := itemIndex
					if includeWindowsDefault {
						deviceIndex--
						a.settings.RXFollowDefault = false
						a.defaultOutputName = ""
					}
					a.outIndex = deviceIndex
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
	lowX := spectrumFrequencyX(graph, a.config.LowCutHz)
	highX := spectrumFrequencyX(graph, a.config.HighCutHz)
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
		hz := math.Round(spectrumXFrequency(graph, mouse.X)/10) * 10
		if a.cutoffDrag == 0 {
			a.config.LowCutHz = max(spectrumMinHz, min(a.config.HighCutHz-50, hz))
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
	centerX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz)
	leftX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz-a.config.Notch.WidthHz/2)
	rightX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz+a.config.Notch.WidthHz/2)
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
		hz := max(spectrumMinHz, min(8000.0, spectrumXFrequency(graph, mouse.X)))
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
	if rl.CheckCollisionPointRec(mouse, graph) && mouse.X >= leftX-12 && mouse.X <= rightX+12 {
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
	graph := eqGraphRectangle()
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, graph) {
		best, distance := 0, float32(1e9)
		for i := 0; i < audio.EQBandCount; i++ {
			x := eqBandX(graph, i)
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
	if clicked(mouse, rl.NewRectangle(1130, 654, 190, 42)) {
		a.config.EQGainsDB = [audio.EQBandCount]float64{}
		a.applyDSPAndSave()
	}
	if clicked(mouse, rl.NewRectangle(1330, 654, 190, 42)) {
		a.config.EQEnabled = !a.config.EQEnabled
		a.applyDSPAndSave()
	}
}

func (a *app) updatePanelControls(mouse rl.Vector2) {
	if a.mode == "tx" && a.activeTab == 4 {
		a.updateEchoControls(mouse)
		return
	}
	if a.activeTab == 3 {
		speeds := []audio.NoiseSpeed{audio.NoiseSlow, audio.NoiseNormal, audio.NoiseFast}
		for i, speed := range speeds {
			if clicked(mouse, rl.NewRectangle(float32(1200+i*115), 742, 105, 42)) {
				a.config.NoiseSpeed = speed
				a.config.NoiseEnabled = true
				a.applyDSPAndSave()
			}
		}
	}
	slider := rl.NewRectangle(270, 753, 900, 42)
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

func (a *app) updateEchoControls(mouse rl.Vector2) {
	changed := false
	if clicked(mouse, rl.NewRectangle(55, 650, 170, 38)) {
		a.config.Echo.Enabled = !a.config.Echo.Enabled
		changed = true
	}
	toneRects := []rl.Rectangle{
		rl.NewRectangle(95, 810, 150, 30),
		rl.NewRectangle(260, 810, 150, 30),
		rl.NewRectangle(425, 810, 150, 30),
	}
	tones := []audio.EchoTone{audio.EchoDark, audio.EchoNeutral, audio.EchoBright}
	for i, r := range toneRects {
		if clicked(mouse, r) {
			a.config.Echo.Tone = tones[i]
			a.config.Echo.Enabled = true
			changed = true
		}
	}
	presetRects := []rl.Rectangle{
		rl.NewRectangle(690, 810, 140, 30),
		rl.NewRectangle(845, 810, 140, 30),
		rl.NewRectangle(1000, 810, 140, 30),
	}
	presets := [][3]float64{{80, 8, 8}, {140, 15, 12}, {260, 25, 18}}
	for i, r := range presetRects {
		if clicked(mouse, r) {
			a.config.Echo.DelayMS, a.config.Echo.Feedback, a.config.Echo.MixPercent = presets[i][0], presets[i][1], presets[i][2]
			a.config.Echo.Enabled = true
			changed = true
		}
	}
	tracks := []rl.Rectangle{
		rl.NewRectangle(365, 700, 775, 24),
		rl.NewRectangle(365, 738, 775, 24),
		rl.NewRectangle(365, 776, 775, 24),
	}
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) {
		for i, track := range tracks {
			if rl.CheckCollisionPointRec(mouse, track) {
				a.echoDrag = i
				break
			}
		}
	}
	if a.echoDrag >= 0 && a.echoDrag < len(tracks) && rl.IsMouseButtonDown(rl.MouseLeftButton) {
		track := tracks[a.echoDrag]
		percent := max(0.0, min(100.0, float64((mouse.X-track.X)/track.Width*100)))
		switch a.echoDrag {
		case 0:
			a.config.Echo.DelayMS = math.Round((40+percent*4.6)/5) * 5
			a.config.Echo.Enabled = true
		case 1:
			a.config.Echo.Feedback = math.Round(percent * .7)
			a.config.Echo.Enabled = true
		case 2:
			a.config.Echo.MixPercent = math.Round(percent * .5)
			a.config.Echo.Enabled = true
		}
		changed = true
	}
	if changed {
		a.applyDSPAndSave()
	}
}

func (a *app) updatePersistentTXGain(mouse rl.Vector2) {
	if a.mode != "tx" {
		return
	}
	track := txGainTrackRectangle()
	if rl.IsMouseButtonPressed(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, rl.NewRectangle(track.X-12, track.Y-12, track.Width+24, track.Height+24)) {
		a.txGainDrag = true
	}
	if !a.txGainDrag || !rl.IsMouseButtonDown(rl.MouseLeftButton) {
		return
	}
	percent := max(0.0, min(100.0, float64((mouse.X-track.X)/track.Width*100)))
	a.config.OutputGainDB = math.Round((-12+percent*.24)*2) / 2
	a.settings.GainEnabled = true
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
	slider := rl.NewRectangle(900, 780, 500, 34)
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

func (a *app) updateTXFilterControls(mouse rl.Vector2) {
	if a.mode != "tx" || a.activeTab != 0 {
		return
	}
	if clicked(mouse, txFilterToggleRectangle()) {
		a.settings.FiltersEnabled = !a.settings.FiltersEnabled
		a.applyDSPAndSave()
	}
	if clicked(mouse, txToneToggleRectangle()) {
		enabled := !a.txSignal.enabled.Load()
		a.txSignal.enabled.Store(enabled)
		if enabled {
			a.message = fmt.Sprintf("Tono de prueba TX: %.0f Hz a -20 dBFS", a.settings.TestToneHz)
		} else {
			a.message = "Tono de prueba desactivado"
		}
	}
	if clicked(mouse, txFadeToggleRectangle()) {
		a.settings.TXFadeEnabled = !a.settings.TXFadeEnabled
		a.save()
	}
	if rl.IsMouseButtonDown(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, expandedSliderHitbox(txToneSliderRectangle())) {
		percent := sliderPercent(mouse.X, txToneSliderRectangle())
		a.settings.TestToneHz = math.Round(percent*500) * 10
		a.txSignal.frequencyHz.Store(int64(a.settings.TestToneHz))
		a.save()
	}
	if rl.IsMouseButtonDown(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, expandedSliderHitbox(txFadeSliderRectangle())) {
		percent := sliderPercent(mouse.X, txFadeSliderRectangle())
		a.settings.TXFadeMS = math.Round(percent*300) * 10
		a.save()
	}
	if rl.IsMouseButtonDown(rl.MouseLeftButton) && rl.CheckCollisionPointRec(mouse, expandedSliderHitbox(txPresenceSliderRectangle())) {
		percent := sliderPercent(mouse.X, txPresenceSliderRectangle())
		a.config.TXPresenceDB = math.Round(percent*20) / 20 * 9
		a.applyDSPAndSave()
	}
}

func expandedSliderHitbox(slider rl.Rectangle) rl.Rectangle {
	return rl.NewRectangle(slider.X-10, slider.Y-10, slider.Width+20, slider.Height+20)
}

func sliderPercent(x float32, slider rl.Rectangle) float64 {
	return max(0.0, min(1.0, float64((x-slider.X)/slider.Width)))
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
		if a.mode == "tx" {
			a.config.Echo.Enabled = !a.config.Echo.Enabled
		} else {
			a.settings.GainEnabled = !a.settings.GainEnabled
		}
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
		if a.mode == "tx" {
			return a.config.Echo.Enabled || a.settings.GainEnabled
		}
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
	outputValue := deviceName(a.outputs, a.outIndex)
	if a.mode == "rx" && a.settings.RXFollowDefault {
		outputValue = "WINDOWS → " + outputValue
	}
	drawSelector(30, 92, 710, inputLabel, deviceName(a.inputs, a.inIndex), mouse)
	drawSelector(860, 92, 710, outputLabel, outputValue, mouse)
	a.drawSpectrum(30, 175, 1540, 190)
	a.drawMeters(status)
	if a.mode == "tx" {
		a.drawPTT(mouse)
		a.drawPersistentTXGain()
	} else {
		a.drawRXPanel(status)
	}
	a.drawTabs(mouse)
	a.drawDSPPanel()
	if a.message != "" {
		drawText(a.message, 35, 870, 18, dspColor.muted)
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
	includeWindowsDefault := false
	if a.openDeviceMenu == 1 {
		devices, selected, x = a.outputs, a.outIndex, 860
		includeWindowsDefault = a.mode == "rx"
		if includeWindowsDefault {
			if a.settings.RXFollowDefault {
				selected = 0
			} else {
				selected++
			}
		}
	}
	count := len(devices)
	if includeWindowsDefault {
		count++
	}
	visible := min(8, count)
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
		label := "PREDETERMINADO DE WINDOWS → " + deviceName(devices, defaultDeviceIndex(devices))
		if !includeWindowsDefault || index > 0 {
			deviceIndex := index
			if includeWindowsDefault {
				deviceIndex--
			}
			label = devices[deviceIndex].Name
			if devices[deviceIndex].IsDefault {
				label += "  ·  ACTUAL DE WINDOWS"
			}
		}
		drawText(ellipsis(label, 58), int32(x+20), int32(item.Y+9), 17, color.text)
		if index == selected {
			drawText("✓", int32(x+665), int32(item.Y+8), 18, color.cyan)
		}
	}
	if a.deviceMenuScroll > 0 {
		drawText("▲", int32(x+675), 165, 14, color.amber)
	}
	if a.deviceMenuScroll+visible < count {
		drawText("▼", int32(x+675), int32(panel.Y+panel.Height-23), 14, color.amber)
	}
}

func drawHeader(a *app, status audioio.Status) {
	drawMilitaryText("IC-RADIO", 38, 18, 42, rl.NewColor(18, 23, 25, 255))
	drawButton(onTopButtonRectangle(), "ON TOP", a.settings.AlwaysOnTop)
	connected := a.ptt.Connected()
	dot := color.amber
	state := "PTT SIN CONECTAR"
	if connected {
		dot, state = color.green, "RADIO CONECTADA"
	}
	drawDarkDisplay(rl.NewRectangle(325, 18, 300, 54))
	rl.DrawCircle(350, 45, 10, rl.NewColor(dot.R/4, dot.G/4, dot.B/4, 255))
	rl.DrawCircle(350, 45, 7, dot)
	drawText(state, 372, 30, 21, color.amber)
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
	labelWidth := float32(245)
	if strings.Contains(label, "ENTRADA DESDE") || strings.Contains(label, "SALIDA A EMISORA") {
		labelWidth = 285
	}
	// The label belongs to the metal faceplate; only the selected device sits in
	// the dark glass display, as it would on physical rack hardware.
	drawText(label, int32(x+18), int32(y+18), 23, rl.NewColor(27, 31, 32, 255))
	field := rl.NewRectangle(x+labelWidth, y+5, w-labelWidth-8, 54)
	drawDarkDisplay(field)
	if rl.CheckCollisionPointRec(mouse, r) {
		rl.DrawRectangleRoundedLines(rl.NewRectangle(field.X-2, field.Y-2, field.Width+4, field.Height+4), .18, 8, rl.NewColor(126, 224, 230, 175))
	}
	drawText(ellipsis(value, 34), int32(field.X+22), int32(field.Y+13), 23, color.text)
	drawText("⌄", int32(field.X+field.Width-42), int32(field.Y+10), 28, color.text)
}

func (a *app) drawSpectrum(x, y, w, h float32) {
	r := rl.NewRectangle(x, y, w, h)
	drawInsetPanel(r, .04)
	graph := rl.NewRectangle(x+68, y+20, w-86, h-42)
	frequencyLabels := []struct {
		hz    float64
		label string
	}{{100, "100 Hz"}, {500, "500 Hz"}, {1000, "1 kHz"}, {2000, "2 kHz"}, {3000, "3 kHz"}, {5000, "5 kHz"}, {10000, "10 kHz"}}
	for _, tick := range frequencyLabels {
		xx := spectrumFrequencyX(graph, tick.hz)
		rl.DrawLine(int32(xx), int32(y+18), int32(xx), int32(y+h-22), rl.NewColor(24, 60, 78, 255))
	}
	for i := 0; i <= 6; i++ {
		yy := y + 20 + float32(i)*(h-40)/6
		rl.DrawLine(int32(x+65), int32(yy), int32(x+w-18), int32(yy), rl.NewColor(30, 69, 87, 255))
		drawText(fmt.Sprintf("%d dB", -i*10), int32(x+12), int32(yy-9), 15, color.muted)
	}
	a.updateSpectrumDisplay(float32(rl.GetFrameTime()))
	base := graph.Y + graph.Height
	step := graph.Width / spectrumVisualBars
	barWidth := max(float32(3), step*.68)
	plotHeight := graph.Height
	points := make([]rl.Vector2, spectrumVisualBars)
	for i := 0; i < spectrumVisualBars; i++ {
		xx := graph.X + (float32(i)+.5)*step
		averageY := spectrumDBY(base, plotHeight, a.spectrumAverage[i])
		if averageY < base {
			rl.DrawRectangle(int32(xx-barWidth*.46), int32(averageY), max(2, int32(barWidth*.92)), int32(base-averageY), rl.NewColor(83, 48, 146, 48))
		}

		value := max(float32(-60), min(float32(0), a.spectrumDisplay[i]))
		topY := spectrumDBY(base, plotHeight, value)
		points[i] = rl.NewVector2(xx, topY)
		for segmentDB := float32(-60); segmentDB < value; segmentDB += 5 {
			segmentTop := min(value, segmentDB+5)
			segmentY := spectrumDBY(base, plotHeight, segmentTop)
			segmentBottom := spectrumDBY(base, plotHeight, segmentDB)
			segmentHeight := max(float32(2), segmentBottom-segmentY-2)
			segmentColor := spectrumSegmentColor(segmentTop)
			rl.DrawRectangle(int32(xx-barWidth/2), int32(segmentY), max(2, int32(barWidth)), int32(segmentHeight), segmentColor)
		}
		if value > -60 {
			rl.DrawRectangle(int32(xx-barWidth*.7), int32(topY-2), max(2, int32(barWidth*1.4)), 4, rl.NewColor(26, 229, 246, 38))
		}

		peak := max(float32(-60), min(float32(0), a.spectrumPeak[i]))
		peakY := spectrumDBY(base, plotHeight, peak)
		if peak > -60 {
			rl.DrawRectangle(int32(xx-barWidth/2), int32(peakY-1), max(2, int32(barWidth)), 2, rl.NewColor(255, 190, 40, 230))
		}
	}
	for i := 1; i < len(points); i++ {
		rl.DrawLineEx(points[i-1], points[i], 6, rl.NewColor(20, 218, 236, 28))
	}
	for i := 1; i < len(points); i++ {
		rl.DrawLineEx(points[i-1], points[i], 2, rl.NewColor(120, 245, 255, 220))
	}
	for _, tick := range frequencyLabels {
		xx := spectrumFrequencyX(graph, tick.hz)
		labelX := xx - 22
		if tick.hz == spectrumMaxHz {
			labelX = xx - 52
		}
		drawText(tick.label, int32(labelX), int32(y+h-20), 14, color.muted)
	}
	graphX, graphW := x+68, w-86
	lowX := spectrumFrequencyX(graph, a.config.LowCutHz)
	highX := spectrumFrequencyX(graph, a.config.HighCutHz)
	if a.settings.FiltersEnabled {
		rl.DrawRectangle(int32(graphX), int32(y+20), int32(max(0, lowX-graphX)), int32(h-42), rl.NewColor(2, 7, 12, 155))
		rl.DrawRectangle(int32(highX), int32(y+20), int32(max(0, graphX+graphW-highX)), int32(h-42), rl.NewColor(2, 7, 12, 155))
		rl.DrawLineEx(rl.NewVector2(lowX, y+18), rl.NewVector2(lowX, y+h-22), 4, color.amber)
		rl.DrawLineEx(rl.NewVector2(highX, y+18), rl.NewVector2(highX, y+h-22), 4, color.amber)
		drawText(fmt.Sprintf("%.0f Hz", a.config.LowCutHz), int32(lowX+7), int32(y+23), 14, color.amber)
		drawText(fmt.Sprintf("%.0f Hz", a.config.HighCutHz), int32(highX+7), int32(y+23), 14, color.amber)
	}
	if a.mode == "rx" {
		centerX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz)
		leftX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz-a.config.Notch.WidthHz/2)
		rightX := spectrumFrequencyX(graph, a.config.Notch.FrequencyHz+a.config.Notch.WidthHz/2)
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

func (a *app) updateSpectrumDisplay(dt float32) {
	if len(a.spectrumDisplay) != spectrumVisualBars {
		a.spectrumDisplay = make([]float32, spectrumVisualBars)
		a.spectrumAverage = make([]float32, spectrumVisualBars)
		a.spectrumPeak = make([]float32, spectrumVisualBars)
		a.spectrumPeakHold = make([]float32, spectrumVisualBars)
		for i := 0; i < spectrumVisualBars; i++ {
			a.spectrumDisplay[i], a.spectrumAverage[i], a.spectrumPeak[i] = -80, -80, -80
		}
	}
	dt = max(float32(0), min(float32(.1), dt))
	attack := float32(1 - math.Exp(-float64(dt)/.035))
	decay := float32(1 - math.Exp(-float64(dt)/.22))
	averageRate := float32(1 - math.Exp(-float64(dt)/1.4))
	for i := 0; i < spectrumVisualBars; i++ {
		position := float32(i) / float32(spectrumVisualBars-1)
		target := max(float32(-80), min(float32(0), spectrumLevelAt(a.spectrum, spectrumPositionFrequency(position))))
		rate := decay
		if target > a.spectrumDisplay[i] {
			rate = attack
		}
		a.spectrumDisplay[i] += (target - a.spectrumDisplay[i]) * rate
		a.spectrumAverage[i] += (target - a.spectrumAverage[i]) * averageRate
		if a.spectrumDisplay[i] >= a.spectrumPeak[i] {
			a.spectrumPeak[i] = a.spectrumDisplay[i]
			a.spectrumPeakHold[i] = .5
		} else if a.spectrumPeakHold[i] > 0 {
			a.spectrumPeakHold[i] -= dt
		} else {
			a.spectrumPeak[i] = max(a.spectrumDisplay[i], a.spectrumPeak[i]-32*dt)
		}
	}
}

func spectrumDBY(base, height, db float32) float32 {
	db = max(float32(-60), min(float32(0), db))
	return base - (db+60)/60*height
}

func spectrumSegmentColor(db float32) rl.Color {
	switch {
	case db >= -10:
		return rl.NewColor(255, 93, 78, 245)
	case db >= -25:
		return rl.NewColor(255, 190, 40, 245)
	case db >= -40:
		return rl.NewColor(52, 230, 105, 245)
	default:
		return rl.NewColor(20, 218, 236, 240)
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
		position := float32(i) / 80
		frequency := spectrumPositionFrequency(position)
		cfg := audio.DefaultConfig()
		cfg.LowCutHz, cfg.HighCutHz, cfg.EQEnabled, cfg.Notch.Enabled = 20, audio.DefaultSampleRate*.45, false, false
		applyRXEmphasis(&cfg, true, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
		off := cfg
		applyRXEmphasis(&off, false, a.settings.RXEmphasisMode, a.settings.RXEmphasisMix)
		db := audio.ResponseDB(cfg, audio.DefaultSampleRate, frequency) - audio.ResponseDB(off, audio.DefaultSampleRate, frequency)
		point := rl.NewVector2(graph.X+position*graph.Width, baseline-float32(db)*3.2)
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
	drawInsetPanel(r, .09)
	drawText(label, int32(x+25), int32(y+22), 28, color.text)
	bar := rl.NewRectangle(x+25, y+88, w-50, 46)
	rl.DrawRectangleRounded(bar, .15, 6, rl.NewColor(8, 15, 22, 255))
	norm := float32(math.Max(0, math.Min(1, float64((db+60)/60))))
	for i := 0; i < 30; i++ {
		xx := bar.X + 7 + float32(i)*(bar.Width-14)/30
		c := meterGradient(float32(i) / 29)
		if float32(i)/30 > norm {
			c = rl.NewColor(48, 60, 69, 255)
		}
		rl.DrawRectangle(int32(xx), int32(bar.Y+8), int32((bar.Width-20)/32), int32(bar.Height-16), c)
	}
	drawText("-60       -40       -20       -10    -3  0 dB", int32(x+28), int32(y+145), 15, color.muted)
}

func meterGradient(position float32) rl.Color {
	position = max(float32(0), min(float32(1), position))
	stops := []struct {
		at    float32
		color rl.Color
	}{
		{0, rl.NewColor(0, 224, 238, 255)},
		{.24, rl.NewColor(28, 232, 129, 255)},
		{.56, rl.NewColor(91, 235, 48, 255)},
		{.78, rl.NewColor(230, 235, 32, 255)},
		{.92, rl.NewColor(255, 183, 24, 255)},
		{1, rl.NewColor(239, 61, 35, 255)},
	}
	for i := 1; i < len(stops); i++ {
		if position <= stops[i].at {
			span := stops[i].at - stops[i-1].at
			amount := (position - stops[i-1].at) / span
			return mix(stops[i-1].color, stops[i].color, amount)
		}
	}
	return stops[len(stops)-1].color
}

func (a *app) drawPTT(mouse rl.Vector2) {
	r := pttRectangle()
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
	drawMilitaryCentered(mainText, r, 70, color.text, -27)
	drawCentered(sub, r, 20, color.text, 25)
	if tx {
		drawCentered(time.Since(a.txSince).Truncate(time.Second).String(), r, 20, color.text, 51)
	} else {
		mode := "MANTENER"
		if a.settings.PTTMode == "lock" {
			mode = "PTT LOCK"
		}
		drawCentered(a.radioStateLabel()+" · "+mode+" · "+ellipsis(a.shortcutLabel(), 18), r, 15, color.muted, 51)
	}
}

func (a *app) drawPersistentTXGain() {
	r := txGainPanelRectangle()
	percent := (a.config.OutputGainDB + 12) / 24 * 100
	drawRecessedControlWell(r, false)
	drawText("GANANCIA TX", int32(r.X+18), int32(r.Y+15), 17, dspColor.text)
	drawMechanicalSlider(txGainTrackRectangle(), percent, controlColor.active)
	drawText(fmt.Sprintf("%+.1f dB", a.config.OutputGainDB), int32(r.X+r.Width-88), int32(r.Y+14), 18, dspColor.amber)
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
	} else {
		labels[4] = "EFECTOS"
	}
	for i, label := range labels {
		r := tabRectangle(i)
		active := i == a.activeTab
		fill := rl.NewColor(27, 31, 32, 255)
		hovered := rl.CheckCollisionPointRec(mouse, r)
		if hovered {
			fill = rl.NewColor(20, 38, 52, 255)
		}
		if active {
			fill = rl.NewColor(57, 45, 25, 255)
			for glow := 5; glow >= 1; glow-- {
				expand := float32(glow)
				gr := rl.NewRectangle(r.X-expand, r.Y-expand, r.Width+expand*2, r.Height+expand*2)
				rl.DrawRectangleRounded(gr, .22, 10, rl.NewColor(color.amber.R, color.amber.G, color.amber.B, uint8(3+glow*3)))
			}
		}
		rl.DrawRectangleRounded(rl.NewRectangle(r.X+2, r.Y+5, r.Width, r.Height), .22, 10, rl.NewColor(5, 7, 8, 190))
		rl.DrawRectangleRounded(r, .22, 10, fill)
		rl.DrawLineEx(rl.NewVector2(r.X+18, r.Y+4), rl.NewVector2(r.X+r.Width-18, r.Y+4), 2, rl.NewColor(155, 160, 160, 90))
		border := color.border
		if active {
			border = color.amber
		} else if hovered {
			border = rl.NewColor(105, 128, 143, 255)
		}
		rl.DrawRectangleRoundedLines(r, .22, 10, border)
		stateColor := color.muted
		if a.sectionEnabled(i) {
			stateColor = color.green
		}
		rl.DrawCircle(int32(r.X+20), int32(r.Y+r.Height/2), 6, stateColor)
		if active {
			rl.DrawRectangleRounded(rl.NewRectangle(r.X+12, r.Y+r.Height-6, r.Width-24, 5), 1, 5, color.amber)
		}
		c := color.text
		if active {
			c = color.amber
		}
		drawCentered(label, r, 20, c, 0)
	}
}

func (a *app) drawDSPPanel() {
	r := rl.NewRectangle(35, 650, 1530, 210)
	drawMetalControlPanel(r)
	if a.activeTab != 1 && !(a.mode == "tx" && (a.activeTab == 0 || a.activeTab == 4)) {
		drawSectionToggle(a.sectionEnabled(a.activeTab))
	}
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
		if a.mode == "tx" {
			a.drawEffects()
		} else {
			a.drawGain()
		}
	}
}

func (a *app) drawFilters() {
	if a.mode == "tx" {
		a.drawTXFilterControls()
		return
	}
	drawText("PASO DE BANDA PARA VOZ", 55, 680, 25, dspColor.amber)
	drawText(fmt.Sprintf("Corte inferior: %.0f Hz", a.config.LowCutHz), 55, 720, 22, dspColor.text)
	drawText(fmt.Sprintf("Corte superior: %.0f Hz", a.config.HighCutHz), 55, 755, 22, dspColor.text)
	drawText("Arrastra las líneas ámbar sobre el espectro.", 55, 810, 18, dspColor.muted)
	a.drawRXEmphasisControls()
}

func (a *app) drawTXFilterControls() {
	drawRecessedControlWell(rl.NewRectangle(45, 662, 520, 184), false)
	drawText("PASO DE BANDA PARA VOZ", 65, 680, 22, dspColor.amber)
	drawButton(txFilterToggleRectangle(), "ACTIVADO", a.settings.FiltersEnabled)
	rl.DrawLine(65, 718, 545, 718, rl.NewColor(110, 117, 118, 180))
	drawText(fmt.Sprintf("Corte inferior: %.0f Hz", a.config.LowCutHz), 65, 730, 20, dspColor.text)
	drawText(fmt.Sprintf("Corte superior: %.0f Hz", a.config.HighCutHz), 65, 765, 20, dspColor.text)
	rl.DrawLine(65, 802, 545, 802, rl.NewColor(110, 117, 118, 180))
	drawText("Arrastra las líneas ámbar sobre el espectro.", 65, 812, 16, dspColor.muted)

	drawRecessedControlWell(rl.NewRectangle(575, 662, 975, 56), false)
	drawText("GENERADOR DE TONO", 590, 674, 18, dspColor.amber)
	drawButton(txToneToggleRectangle(), "TONO", a.txSignal.enabled.Load())
	drawControlValue(rl.NewRectangle(945, 670, 140, 38), formatToneFrequency(a.settings.TestToneHz))
	drawText("0 Hz", 1095, 675, 14, dspColor.text)
	drawMechanicalSlider(txToneSliderRectangle(), a.settings.TestToneHz/5000*100, controlColor.active)
	drawText("5 kHz", 1510, 675, 14, dspColor.text)

	drawRecessedControlWell(rl.NewRectangle(575, 722, 975, 56), false)
	drawText("FADE ON PTT", 590, 731, 18, dspColor.amber)
	drawText("Entrada progresiva de audio al iniciar TX", 590, 753, 14, dspColor.text)
	drawButton(txFadeToggleRectangle(), "ACTIVADO", a.settings.TXFadeEnabled)
	drawControlValue(rl.NewRectangle(945, 730, 140, 38), fmt.Sprintf("%.0f ms", a.settings.TXFadeMS))
	drawText("0 ms", 1095, 735, 14, dspColor.text)
	drawMechanicalSlider(txFadeSliderRectangle(), a.settings.TXFadeMS/3000*100, controlColor.active)
	drawText("3000 ms", 1490, 735, 14, dspColor.text)

	drawRecessedControlWell(rl.NewRectangle(575, 782, 975, 64), false)
	drawText("PRESENCIA TX", 590, 791, 18, dspColor.amber)
	drawText("Realce amplio desde 1,8 kHz", 590, 815, 14, dspColor.text)
	drawControlValue(rl.NewRectangle(945, 790, 140, 42), fmt.Sprintf("%+.1f dB", a.config.TXPresenceDB))
	drawText("0 dB", 1095, 797, 14, dspColor.text)
	drawMechanicalSlider(txPresenceSliderRectangle(), a.config.TXPresenceDB/9*100, controlColor.active)
	drawText("+9 dB", 1510, 797, 14, dspColor.text)
}

func drawControlValue(r rl.Rectangle, value string) {
	drawDarkDisplay(r)
	drawCentered(value, r, 18, color.text, 0)
}

func formatToneFrequency(hz float64) string {
	if hz >= 1000 {
		return fmt.Sprintf("%.2f kHz", hz/1000)
	}
	return fmt.Sprintf("%.0f Hz", hz)
}

func (a *app) drawRXEmphasisControls() {
	rl.DrawLine(775, 675, 775, 825, dspColor.border)
	drawText("ÉNFASIS RX", 810, 730, 22, dspColor.amber)
	drawButton(rl.NewRectangle(810, 684, 165, 42), "ACTIVADO", a.settings.RXEmphasis)
	profiles := []struct{ key, label string }{{"ssb", "SSB"}, {"fm50", "FM 50 µs"}, {"fm75", "FM 75 µs"}}
	for i, profile := range profiles {
		drawButton(rl.NewRectangle(float32(970+i*135), 684, 125, 42), profile.label, a.settings.RXEmphasisMode == profile.key)
	}
	if a.settings.RXEmphasisMode == "ssb" {
		drawText(fmt.Sprintf("Presencia SSB: %.0f %%", a.settings.RXEmphasisMix), 810, 765, 17, dspColor.muted)
		drawRecessedControlWell(rl.NewRectangle(875, 782, 555, 34), false)
		drawCompactSlider(rl.NewRectangle(900, 790, 500, 14), a.settings.RXEmphasisMix)
		drawText("0 %", 810, 784, 15, dspColor.muted)
		drawText("100 %", 1415, 784, 15, dspColor.muted)
	} else {
		drawText("Deénfasis FM normalizado · intensidad fija", 810, 775, 18, dspColor.muted)
		drawText("La curva violeta muestra la respuesta aplicada.", 810, 810, 17, dspColor.text)
	}
}

func drawCompactSlider(track rl.Rectangle, percent float64) {
	drawCompactSliderColor(track, percent, rl.NewColor(191, 112, 255, 255))
}

func drawCompactSliderColor(track rl.Rectangle, percent float64, active rl.Color) {
	percent = max(0, min(100, percent))
	drawMechanicalSlider(track, percent, active)
}

func (a *app) drawEQ() {
	graph := eqGraphRectangle()
	drawRecessedControlWell(rl.NewRectangle(42, 700, 1518, 132), true)
	for _, frequency := range []float64{100, 500, 1000, 2000, 5000, 10000} {
		x := spectrumFrequencyX(graph, frequency)
		rl.DrawLine(int32(x), int32(graph.Y), int32(x), int32(graph.Y+graph.Height), rl.NewColor(37, 74, 83, 215))
	}
	for i := 0; i <= 4; i++ {
		y := graph.Y + float32(i)*graph.Height/4
		rl.DrawLine(int32(graph.X), int32(y), int32(graph.X+graph.Width), int32(y), rl.NewColor(42, 70, 76, 220))
		label := fmt.Sprintf("%+d dB", 12-i*6)
		labelSize := readableSize(15)
		labelWidth := rl.MeasureTextEx(interfaceFont(labelSize), label, float32(labelSize), fontSpacing(labelSize)).X
		drawText(label, int32(graph.X-labelWidth-8), int32(y-9), 15, color.muted)
	}
	points := make([]rl.Vector2, audio.EQBandCount)
	for i, gain := range a.config.EQGainsDB {
		x := eqBandX(graph, i)
		y := graph.Y + float32((12-gain)/24)*graph.Height
		points[i] = rl.NewVector2(x, y)
		if i > 0 {
			rl.DrawLineEx(points[i-1], points[i], 8, rl.NewColor(controlColor.eqLine.R, controlColor.eqLine.G, controlColor.eqLine.B, 30))
			rl.DrawLineEx(points[i-1], points[i], 3, controlColor.eqLine)
		}
		drawMechanicalHandle(x, y, controlColor.active, 10)
		labelX := x - 18
		if i == audio.EQBandCount-1 {
			labelX = x - 34
		}
		drawText(formatEQFrequency(audio.EQFrequencies[i]), int32(labelX), int32(graph.Y+graph.Height+5), 13, color.muted)
	}
	drawText("ECUALIZADOR · 11 BANDAS · 100 Hz - 10 kHz", 55, 670, 20, dspColor.amber)
	drawButton(rl.NewRectangle(1130, 654, 190, 42), "RESET EQ", false)
	drawButton(rl.NewRectangle(1330, 654, 190, 42), "BYPASS", !a.config.EQEnabled)
}

func (a *app) drawCompressor() {
	percent := (-(a.config.Compressor.TargetDBFS) - 6) / 24 * 100
	drawText("COMPRESIÓN", 55, 680, 25, dspColor.amber)
	drawText("Pasos predefinidos: 0 · 20 · 40 · 60 · 80 · 100 %", 55, 715, 20, dspColor.muted)
	drawValueSlider(percent, fmt.Sprintf("%.0f %%", percent), "SUAVE", "MÁXIMA")
	drawText(fmt.Sprintf("Objetivo automático: %.0f dBFS", a.config.Compressor.TargetDBFS), 1210, 810, 19, dspColor.text)
}

func (a *app) drawNotch() {
	percent := (a.config.Notch.FrequencyHz - 100) / 3900 * 100
	drawText("FILTRO NOTCH", 55, 680, 25, dspColor.amber)
	drawText("Arrastra el centro rojo y los bordes ámbar directamente sobre el espectro", 55, 715, 20, dspColor.muted)
	drawValueSlider(percent, fmt.Sprintf("%.0f Hz", a.config.Notch.FrequencyHz), "100 Hz", "4 kHz")
	drawCentered(fmt.Sprintf("Ancho %.0f Hz · %.0f dB · Rueda: ancho · Shift+rueda: profundidad", a.config.Notch.WidthHz, a.config.Notch.DepthDB), rl.NewRectangle(270, 822, 900, 18), 16, dspColor.text, 0)
}

func (a *app) drawNoiseReduction() {
	drawText("REDUCCIÓN DE RUIDO", 55, 680, 25, dspColor.amber)
	drawText("Nivel de reducción", 55, 715, 20, dspColor.muted)
	drawValueSlider(a.config.NoiseStrength, fmt.Sprintf("%.0f %%", a.config.NoiseStrength), "0 %", "100 %")
	drawText("INTEGRACIÓN", 1200, 710, 19, dspColor.muted)
	labels := []string{"LENTO", "MEDIO", "RÁPIDO"}
	speeds := []audio.NoiseSpeed{audio.NoiseSlow, audio.NoiseNormal, audio.NoiseFast}
	for i, label := range labels {
		drawButton(rl.NewRectangle(float32(1200+i*115), 742, 105, 42), label, a.config.NoiseSpeed == speeds[i])
	}
}

func (a *app) drawGain() {
	percent := (a.config.OutputGainDB + 12) / 24 * 100
	drawText("GANANCIA DE SALIDA", 55, 680, 25, dspColor.amber)
	drawText("Calibra el nivel enviado a la entrada de audio USB de la radio", 55, 715, 20, dspColor.muted)
	drawValueSlider(percent, fmt.Sprintf("%+.1f dB", a.config.OutputGainDB), "-12 dB", "+12 dB")
}

func (a *app) drawEffects() {
	drawRecessedControlWell(rl.NewRectangle(75, 680, 1090, 122), false)
	drawText("ECO TX", 95, 650, 25, dspColor.text)
	box := rl.NewRectangle(55, 652, 28, 28)
	boxFill, boxBorder := color.panel2, color.border
	if a.config.Echo.Enabled {
		boxFill, boxBorder = color.amber, color.amber
	}
	rl.DrawRectangleRounded(box, .22, 6, boxFill)
	rl.DrawRectangleRoundedLines(box, .22, 6, boxBorder)
	if a.config.Echo.Enabled {
		drawText("✓", 59, 651, 22, dspColor.text)
	}
	drawEffectTrack(700, "RETARDO", fmt.Sprintf("%.0f ms", a.config.Echo.DelayMS), (a.config.Echo.DelayMS-40)/460*100, 365, 775)
	drawEffectTrack(738, "REALIMENTACIÓN", fmt.Sprintf("%.0f %%", a.config.Echo.Feedback), a.config.Echo.Feedback/70*100, 365, 775)
	drawEffectTrack(776, "MEZCLA", fmt.Sprintf("%.0f %%", a.config.Echo.MixPercent), a.config.Echo.MixPercent/50*100, 365, 775)

	toneLabels := []string{"OSCURO", "NEUTRO", "BRILLANTE"}
	tones := []audio.EchoTone{audio.EchoDark, audio.EchoNeutral, audio.EchoBright}
	for i, label := range toneLabels {
		drawButton(rl.NewRectangle(float32(95+i*165), 810, 150, 30), label, a.config.Echo.Tone == tones[i])
	}
	rl.DrawLine(625, 810, 625, 840, dspColor.border)
	presets := [][3]float64{{80, 8, 8}, {140, 15, 12}, {260, 25, 18}}
	for i, label := range []string{"CORTO", "MEDIO", "LARGO"} {
		preset := presets[i]
		selected := a.config.Echo.Enabled && math.Abs(a.config.Echo.DelayMS-preset[0]) < .5 &&
			math.Abs(a.config.Echo.Feedback-preset[1]) < .5 && math.Abs(a.config.Echo.MixPercent-preset[2]) < .5
		drawButton(rl.NewRectangle(float32(690+i*155), 810, 140, 30), label, selected)
	}
	rl.DrawLineEx(rl.NewVector2(1200, 670), rl.NewVector2(1200, 840), 2, dspColor.border)

}

func drawEffectTrack(y float32, label, value string, percent float64, x, width float32) {
	percent = max(0, min(100, percent))
	if label != "" {
		drawText(label, 95, int32(y-7), 18, dspColor.text)
		drawText(value, 260, int32(y-7), 18, dspColor.muted)
	}
	track := rl.NewRectangle(x, y+7, width, 10)
	drawMechanicalSlider(track, percent, controlColor.active)
}

func drawValueSlider(percent float64, value, left, right string) {
	percent = max(0, min(100, percent))
	drawRecessedControlWell(rl.NewRectangle(205, 742, 980, 100), false)
	track := rl.NewRectangle(270, 765, 900, 16)
	drawMechanicalSlider(track, percent, controlColor.active)
	for i := 0; i <= 5; i++ {
		x := int32(track.X + float32(i)*track.Width/5)
		rl.DrawLine(x, int32(track.Y+24), x, int32(track.Y+33), rl.NewColor(62, 70, 72, 255))
	}
	drawText(left, 270, 802, 16, dspColor.text)
	rightSize := readableSize(16)
	rightWidth := rl.MeasureTextEx(interfaceFont(rightSize), right, float32(rightSize), fontSpacing(rightSize)).X
	drawText(right, int32(track.X+track.Width-rightWidth), 802, 16, dspColor.text)
	drawText(value, 1210, 748, 38, dspColor.text)
}

func drawRecessedControlWell(r rl.Rectangle, dark bool) {
	fill := controlColor.well
	upper, lower := rl.NewColor(72, 80, 82, 170), rl.NewColor(245, 247, 246, 180)
	if dark {
		fill = rl.NewColor(22, 34, 39, 255)
		upper, lower = rl.NewColor(3, 7, 9, 220), rl.NewColor(115, 137, 141, 120)
	}
	rl.DrawRectangleRounded(rl.NewRectangle(r.X+1, r.Y+3, r.Width, r.Height), .12, 8, rl.NewColor(255, 255, 255, 85))
	rl.DrawRectangleRounded(r, .12, 8, fill)
	rl.DrawLineEx(rl.NewVector2(r.X+12, r.Y+2), rl.NewVector2(r.X+r.Width-12, r.Y+2), 2, upper)
	rl.DrawLineEx(rl.NewVector2(r.X+12, r.Y+r.Height-2), rl.NewVector2(r.X+r.Width-12, r.Y+r.Height-2), 2, lower)
	rl.DrawRectangleRoundedLines(r, .12, 8, rl.NewColor(88, 96, 98, 190))
}

func drawMechanicalSlider(track rl.Rectangle, percent float64, active rl.Color) {
	percent = max(0, min(100, percent))
	rl.DrawRectangleRounded(rl.NewRectangle(track.X, track.Y+2, track.Width, track.Height), .5, 8, rl.NewColor(8, 13, 15, 125))
	rl.DrawRectangleRounded(track, .5, 8, controlColor.track)
	fill := rl.NewRectangle(track.X, track.Y, track.Width*float32(percent/100), track.Height)
	if fill.Width > 0 {
		rl.DrawRectangleRounded(rl.NewRectangle(fill.X-2, fill.Y-2, fill.Width+4, fill.Height+4), .5, 8, rl.NewColor(active.R, active.G, active.B, 32))
		rl.DrawRectangleRounded(fill, .5, 8, active)
		rl.DrawRectangleRounded(rl.NewRectangle(fill.X, fill.Y+fill.Height-3, fill.Width, 3), .5, 8, controlColor.activeEdge)
	}
	handleX := track.X + track.Width*float32(percent/100)
	drawMechanicalHandle(handleX, track.Y+track.Height/2, active, max(float32(10), track.Height*.82))
}

func drawMechanicalHandle(x, y float32, active rl.Color, radius float32) {
	rl.DrawCircle(int32(x+2), int32(y+3), radius+2, rl.NewColor(0, 0, 0, 100))
	rl.DrawCircle(int32(x), int32(y), radius+1, rl.NewColor(8, 12, 14, 255))
	rl.DrawRing(rl.NewVector2(x, y), radius-2, radius+1, 0, 360, 32, active)
	rl.DrawCircle(int32(x), int32(y), radius-4, controlColor.track)
	rl.DrawCircle(int32(x-radius*.28), int32(y-radius*.28), max(float32(1), radius*.14), rl.NewColor(255, 255, 255, 220))
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
	border := color.border
	textColor := color.text
	if active {
		fill = rl.NewColor(73, 51, 17, 255)
		border = color.amber
		textColor = rl.NewColor(255, 224, 151, 255)
		for glow := 5; glow >= 1; glow-- {
			expand := float32(glow * 2)
			glowRect := rl.NewRectangle(r.X-expand, r.Y-expand, r.Width+expand*2, r.Height+expand*2)
			rl.DrawRectangleRounded(glowRect, .18, 8, rl.NewColor(color.amber.R, color.amber.G, color.amber.B, uint8(6+glow*6)))
		}
	}
	rl.DrawRectangleRounded(rl.NewRectangle(r.X+2, r.Y+4, r.Width, r.Height), .18, 6, rl.NewColor(3, 5, 6, 180))
	rl.DrawRectangleRounded(r, .18, 6, fill)
	rl.DrawLineEx(rl.NewVector2(r.X+10, r.Y+3), rl.NewVector2(r.X+r.Width-10, r.Y+3), 2, rl.NewColor(190, 196, 197, 80))
	rl.DrawRectangleRoundedLines(r, .18, 6, border)
	if active {
		rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X+2, r.Y+2, r.Width-4, r.Height-4), .18, 6, rl.NewColor(255, 211, 91, 150))
	}
	drawCentered(label, r, 18, textColor, 0)
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
func defaultDeviceIndex(devices []audioio.Device) int {
	for i, device := range devices {
		if device.IsDefault {
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

func spectrumFrequencyPosition(hz float64) float32 {
	return logarithmicFrequencyPosition(hz, spectrumMinHz, spectrumMaxHz)
}

func logarithmicFrequencyPosition(hz, minimum, maximum float64) float32 {
	hz = max(minimum, min(maximum, hz))
	return float32(math.Log(hz/minimum) / math.Log(maximum/minimum))
}

func spectrumPositionFrequency(position float32) float64 {
	position = max(float32(0), min(float32(1), position))
	return spectrumMinHz * math.Pow(spectrumMaxHz/spectrumMinHz, float64(position))
}

func spectrumFrequencyX(graph rl.Rectangle, hz float64) float32 {
	return graph.X + spectrumFrequencyPosition(hz)*graph.Width
}

func spectrumXFrequency(graph rl.Rectangle, x float32) float64 {
	if graph.Width <= 0 {
		return spectrumMinHz
	}
	return spectrumPositionFrequency((x - graph.X) / graph.Width)
}

func spectrumLevelAt(levels []float32, hz float64) float32 {
	if len(levels) == 0 {
		return -80
	}
	position := max(0.0, min(1.0, hz/spectrumMaxHz)) * float64(len(levels)-1)
	lower := min(int(position), len(levels)-1)
	upper := min(lower+1, len(levels)-1)
	fraction := float32(position - float64(lower))
	return levels[lower] + fraction*(levels[upper]-levels[lower])
}

func eqBandX(graph rl.Rectangle, band int) float32 {
	band = max(0, min(audio.EQBandCount-1, band))
	return graph.X + logarithmicFrequencyPosition(audio.EQFrequencies[band], 100, 10000)*graph.Width
}

func eqGraphRectangle() rl.Rectangle {
	return rl.NewRectangle(100, 710, 1450, 90)
}

func pttRectangle() rl.Rectangle {
	return rl.NewRectangle(530, 385, 540, 130)
}

func txGainPanelRectangle() rl.Rectangle {
	return rl.NewRectangle(530, 523, 540, 54)
}

func txGainTrackRectangle() rl.Rectangle {
	return rl.NewRectangle(685, 546, 275, 10)
}

func onTopButtonRectangle() rl.Rectangle {
	return rl.NewRectangle(38, 62, 140, 26)
}

func txFilterToggleRectangle() rl.Rectangle {
	return rl.NewRectangle(390, 674, 150, 38)
}

func txToneToggleRectangle() rl.Rectangle {
	return rl.NewRectangle(820, 670, 115, 38)
}

func txFadeToggleRectangle() rl.Rectangle {
	return rl.NewRectangle(820, 730, 115, 38)
}

func txToneSliderRectangle() rl.Rectangle {
	return rl.NewRectangle(1150, 682, 345, 14)
}

func txFadeSliderRectangle() rl.Rectangle {
	return rl.NewRectangle(1150, 742, 325, 14)
}

func txPresenceSliderRectangle() rl.Rectangle {
	return rl.NewRectangle(1150, 802, 345, 14)
}

func tabRectangle(index int) rl.Rectangle {
	return rl.NewRectangle(float32(35+index*306), 590, 296, 56)
}

func formatEQFrequency(hz float64) string {
	if hz < 1000 {
		return fmt.Sprintf("%.0f", hz)
	}
	if math.Mod(hz, 1000) == 0 {
		return fmt.Sprintf("%.0fk", hz/1000)
	}
	return fmt.Sprintf("%.1fk", hz/1000)
}

func drawRackChassis() {
	rl.DrawRectangleGradientV(0, 0, viewW, viewH, rl.NewColor(211, 214, 214, 255), rl.NewColor(137, 142, 144, 255))
	for y := int32(1); y < viewH; y += 3 {
		shade := uint8(18 + (y%11)*2)
		rl.DrawLine(0, y, viewW, y, rl.NewColor(255, 255, 255, shade))
	}
	for y := int32(2); y < viewH; y += 7 {
		rl.DrawLine(0, y, viewW, y, rl.NewColor(45, 49, 50, 18))
	}
	rl.DrawRectangleGradientH(0, 0, 24, viewH, rl.NewColor(92, 97, 99, 255), rl.NewColor(182, 186, 186, 255))
	rl.DrawRectangleGradientH(viewW-24, 0, 24, viewH, rl.NewColor(182, 186, 186, 255), rl.NewColor(89, 94, 96, 255))
	rl.DrawRectangleLinesEx(rl.NewRectangle(3, 3, viewW-6, viewH-6), 3, rl.NewColor(68, 73, 75, 255))
	rl.DrawLine(24, 0, 24, viewH, rl.NewColor(65, 70, 72, 190))
	rl.DrawLine(viewW-25, 0, viewW-25, viewH, rl.NewColor(238, 240, 240, 170))
	for _, p := range []rl.Vector2{{X: 14, Y: 42}, {X: viewW - 14, Y: 42}, {X: 14, Y: viewH - 42}, {X: viewW - 14, Y: viewH - 42}} {
		drawRackScrew(p.X, p.Y)
	}
}

func drawRackScrew(x, y float32) {
	rl.DrawCircle(int32(x+2), int32(y+3), 13, rl.NewColor(45, 47, 47, 110))
	rl.DrawCircleGradient(rl.NewVector2(x, y), 12, rl.NewColor(74, 78, 79, 255), rl.NewColor(8, 10, 11, 255))
	rl.DrawCircleLines(int32(x), int32(y), 12, rl.NewColor(212, 215, 214, 180))
	rl.DrawLineEx(rl.NewVector2(x-6, y-6), rl.NewVector2(x+6, y+6), 3, rl.NewColor(5, 6, 7, 255))
}

func drawDarkDisplay(r rl.Rectangle) {
	rl.DrawRectangleRounded(rl.NewRectangle(r.X+2, r.Y+4, r.Width, r.Height), .18, 8, rl.NewColor(25, 27, 28, 150))
	rl.DrawRectangleRounded(r, .18, 8, rl.NewColor(8, 15, 18, 255))
	rl.DrawRectangleRoundedLines(r, .18, 8, rl.NewColor(48, 53, 55, 255))
	rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X+2, r.Y+2, r.Width-4, r.Height-4), .18, 8, rl.NewColor(185, 193, 194, 100))
	rl.DrawLineEx(rl.NewVector2(r.X+12, r.Y+5), rl.NewVector2(r.X+r.Width-12, r.Y+5), 2, rl.NewColor(222, 231, 231, 65))
}

func drawInsetPanel(r rl.Rectangle, roundness float32) {
	rl.DrawRectangleRounded(rl.NewRectangle(r.X-3, r.Y-3, r.Width+6, r.Height+8), roundness, 8, rl.NewColor(65, 69, 70, 160))
	rl.DrawRectangleRounded(r, roundness, 8, color.panel)
	rl.DrawRectangleRoundedLines(r, roundness, 8, rl.NewColor(42, 49, 52, 255))
	rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X+2, r.Y+2, r.Width-4, r.Height-4), roundness, 8, rl.NewColor(130, 141, 143, 90))
}

func drawMetalControlPanel(r rl.Rectangle) {
	rl.DrawRectangleRounded(rl.NewRectangle(r.X-3, r.Y-3, r.Width+6, r.Height+7), .04, 8, rl.NewColor(61, 65, 66, 155))
	// Scissor rectangles use physical screen coordinates and therefore do not
	// follow the virtual 1600x900 camera when the window is resized. Drawing the
	// panel directly keeps its bounds stable at every window scale.
	rl.DrawRectangleRounded(r, .04, 8, rl.NewColor(205, 208, 207, 255))
	for y := int32(r.Y) + 2; y < int32(r.Y+r.Height); y += 3 {
		rl.DrawLine(int32(r.X+2), y, int32(r.X+r.Width-2), y, rl.NewColor(255, 255, 255, 25))
	}
	for y := int32(r.Y) + 4; y < int32(r.Y+r.Height); y += 8 {
		rl.DrawLine(int32(r.X+2), y, int32(r.X+r.Width-2), y, rl.NewColor(40, 44, 45, 13))
	}
	rl.DrawRectangleRoundedLines(r, .04, 8, rl.NewColor(55, 60, 61, 255))
	rl.DrawRectangleRoundedLines(rl.NewRectangle(r.X+2, r.Y+2, r.Width-4, r.Height-4), .04, 8, rl.NewColor(232, 234, 233, 150))
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
	a.settings.RXFollowDefault = true
	a.settings.TestToneHz, a.settings.TXFadeMS = 1000, 750
	a.settings.TXFadeEnabled = true
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
		if a.settings.Version < 8 {
			a.config.Echo = audio.DefaultConfig().Echo
		}
		if a.settings.Version < 9 {
			a.settings.RXFollowDefault = true
		}
		if a.settings.Version < 10 {
			a.settings.TestToneHz, a.settings.TXFadeMS = 1000, 750
			a.settings.TXFadeEnabled = true
		}
		a.settings.TestToneHz = max(0, min(5000, a.settings.TestToneHz))
		a.settings.TXFadeMS = max(0, min(3000, a.settings.TXFadeMS))
		a.config.LowCutHz = max(spectrumMinHz, a.config.LowCutHz)
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
	a.settings.Version = 10
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
