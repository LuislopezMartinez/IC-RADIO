package main

import (
	"math"
	"testing"

	audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"
	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	rl "github.com/gen2brain/raylib-go/raylib"
)

type recordingMonitor struct{ samples []float32 }

func (m *recordingMonitor) Write(samples []float32) {
	m.samples = append(m.samples, samples...)
}

func TestBindingKeyName(t *testing.T) {
	cases := map[int32]string{
		rl.KeySpace: "ESPACIO",
		rl.KeyA:     "A",
		rl.KeyNine:  "9",
		rl.KeyF12:   "F12",
	}
	for key, want := range cases {
		if got := bindingKeyName(key); got != want {
			t.Fatalf("bindingKeyName(%d) = %q, want %q", key, got, want)
		}
	}
}

func TestTXCalibrationToneProducesAudio(t *testing.T) {
	monitor := &recordingMonitor{}
	processor := &testToneProcessor{base: audio.NewProcessor(audio.DefaultSampleRate), monitor: monitor}
	processor.frequencyHz.Store(1000)
	processor.enabled.Store(true)
	samples := make([]float32, 480)
	processor.Process(samples)
	peak := float64(0)
	for _, sample := range samples {
		peak = math.Max(peak, math.Abs(float64(sample)))
	}
	if peak < .02 {
		t.Fatalf("tono de prueba demasiado bajo: pico %.4f", peak)
	}
	if len(monitor.samples) != len(samples) {
		t.Fatalf("MON recibió %d muestras, se esperaban %d", len(monitor.samples), len(samples))
	}
	for i := range samples {
		if monitor.samples[i] != samples[i] {
			t.Fatalf("MON no recibió la señal TX procesada en la muestra %d", i)
		}
	}
}

func TestTXFadeStartsSilentAndReachesFullLevel(t *testing.T) {
	processor := &testToneProcessor{base: audio.NewProcessor(audio.DefaultSampleRate)}
	processor.frequencyHz.Store(1000)
	processor.enabled.Store(true)
	processor.startFade(10)
	samples := make([]float32, 960)
	processor.Process(samples)
	firstEnergy, finalEnergy := 0.0, 0.0
	for _, sample := range samples[:120] {
		firstEnergy += float64(sample * sample)
	}
	for _, sample := range samples[840:] {
		finalEnergy += float64(sample * sample)
	}
	if firstEnergy >= finalEnergy*.25 || finalEnergy < 1e-4 {
		t.Fatalf("fade TX inesperado: inicio=%g final=%g", firstEnergy, finalEnergy)
	}
}

func TestEchoToneProfilesAreAudiblySeparated(t *testing.T) {
	rms := func(tone audio.EchoTone) float64 {
		processor := audio.NewProcessor(audio.DefaultSampleRate)
		config := audio.DefaultConfig()
		config.LowCutHz, config.HighCutHz = 20, 10000
		config.EQEnabled = false
		config.Echo = audio.EchoConfig{Enabled: true, DelayMS: 40, MixPercent: 50, Tone: tone}
		processor.Configure(config)
		samples := make([]float32, 4800)
		for i := 0; i < 960; i++ {
			samples[i] = float32(.4 * math.Sin(2*math.Pi*3000*float64(i)/audio.DefaultSampleRate))
		}
		processor.Process(samples)
		var energy float64
		for _, sample := range samples[1920:2880] {
			energy += float64(sample * sample)
		}
		return math.Sqrt(energy / 960)
	}
	dark, neutral, bright := rms(audio.EchoDark), rms(audio.EchoNeutral), rms(audio.EchoBright)
	if !(dark < neutral*.75 && bright > neutral*1.15) {
		t.Fatalf("perfiles de eco poco separados a 3 kHz: oscuro=%.4f neutro=%.4f brillante=%.4f", dark, neutral, bright)
	}
}

func TestEscapeCannotBeBound(t *testing.T) {
	if validBindingKey(rl.KeyEscape) {
		t.Fatal("Escape debe permanecer reservado para RX de emergencia")
	}
	if !validBindingKey(rl.KeySpace) {
		t.Fatal("Espacio debe ser asignable")
	}
}

func TestInitialDeviceScrollStaysInsideList(t *testing.T) {
	if got := initialDeviceScroll(9, 10); got != 2 {
		t.Fatalf("initialDeviceScroll(9, 10) = %d, want 2", got)
	}
	if got := initialDeviceScroll(2, 6); got != 0 {
		t.Fatalf("initialDeviceScroll(2, 6) = %d, want 0", got)
	}
}

func TestDefaultDeviceIndex(t *testing.T) {
	devices := []audioio.Device{{Name: "Altavoces"}, {Name: "Auriculares", IsDefault: true}}
	if got := defaultDeviceIndex(devices); got != 1 {
		t.Fatalf("defaultDeviceIndex() = %d, want 1", got)
	}
}

func TestRadioStateLabels(t *testing.T) {
	cases := map[string]string{
		stateRX:           "RX ESCUCHANDO",
		stateTXReady:      "TX PREPARADO",
		stateTransmitting: "TRANSMITIENDO",
		stateError:        "ERROR DE RUTA",
	}
	for state, want := range cases {
		a := app{radioState: state}
		if got := a.radioStateLabel(); got != want {
			t.Fatalf("estado %q = %q, want %q", state, got, want)
		}
	}
}

func TestApplyRXEmphasisProfiles(t *testing.T) {
	config := audio.DefaultConfig()
	applyRXEmphasis(&config, true, "ssb", 50)
	if config.Emphasis != audio.EmphasisOff || config.SSBClarityDB != 4.5 {
		t.Fatalf("perfil SSB incorrecto: modo=%v claridad=%.1f", config.Emphasis, config.SSBClarityDB)
	}

	applyRXEmphasis(&config, true, "fm75", 100)
	if config.Emphasis != audio.DeEmphasis || config.EmphasisMicroseconds != 75 || config.SSBClarityDB != 0 {
		t.Fatalf("perfil FM 75 incorrecto: modo=%v tiempo=%d claridad=%.1f", config.Emphasis, config.EmphasisMicroseconds, config.SSBClarityDB)
	}

	applyRXEmphasis(&config, false, "fm75", 100)
	if config.Emphasis != audio.EmphasisOff || config.SSBClarityDB != 0 {
		t.Fatalf("el énfasis desactivado sigue afectando al DSP: %+v", config)
	}
}

func TestViewSnapshotsKeepRXAndTXIndependent(t *testing.T) {
	tx := audio.DefaultConfig()
	tx.OutputGainDB = 6
	rx := audio.DefaultConfig()
	rx.OutputGainDB = -3
	a := app{
		mode:      "tx",
		config:    tx,
		activeTab: 4,
		settings:  settings{FiltersEnabled: true, GainEnabled: true},
		rxView:    viewState{processor: audio.NewProcessor(audio.DefaultSampleRate), config: rx, activeTab: 2},
	}
	a.txView = a.snapshotView()
	a.config.OutputGainDB = 9
	a.storeActiveView()
	if a.txView.config.OutputGainDB != 9 || a.rxView.config.OutputGainDB != -3 {
		t.Fatalf("los perfiles RX/TX se mezclaron: TX %.1f, RX %.1f", a.txView.config.OutputGainDB, a.rxView.config.OutputGainDB)
	}
	if a.txView.activeTab != 4 || a.rxView.activeTab != 2 {
		t.Fatalf("las pestañas no permanecen separadas: TX %d, RX %d", a.txView.activeTab, a.rxView.activeTab)
	}
}

func TestMeterGradientCoversFullSignalRange(t *testing.T) {
	low := meterGradient(0)
	middle := meterGradient(.56)
	high := meterGradient(1)
	if low.B <= low.R || low.G <= low.R {
		t.Fatalf("el inicio del medidor no es cian: %+v", low)
	}
	if middle.G <= middle.R || middle.G <= middle.B {
		t.Fatalf("el centro del medidor no es verde: %+v", middle)
	}
	if high.R <= high.G || high.R <= high.B {
		t.Fatalf("el final del medidor no es rojo: %+v", high)
	}
}

func TestSpectrumLogarithmicScaleRoundTrip(t *testing.T) {
	graph := rl.NewRectangle(98, 195, 1454, 148)
	for _, frequency := range []float64{100, 500, 1000, 2500, 4000, 10000} {
		x := spectrumFrequencyX(graph, frequency)
		got := spectrumXFrequency(graph, x)
		if math.Abs(got-frequency) > frequency*.0001 {
			t.Fatalf("conversión logarítmica de %.0f Hz devolvió %.3f Hz", frequency, got)
		}
	}
	position2500 := spectrumFrequencyPosition(2500)
	if position2500 < .69 || position2500 > .71 {
		t.Fatalf("2500 Hz debería ocupar la zona ampliada de voz, posición=%.3f", position2500)
	}
	if got := spectrumXFrequency(graph, graph.X); got != spectrumMinHz {
		t.Fatalf("el espectro empieza en %.0f Hz, want %.0f Hz", got, spectrumMinHz)
	}
}

func TestSpectrumLevelInterpolationUsesLinearFFTSource(t *testing.T) {
	levels := []float32{-80, -60, -40, -20, 0}
	if got := spectrumLevelAt(levels, 2500); math.Abs(float64(got-(-60))) > .001 {
		t.Fatalf("nivel interpolado a 2500 Hz = %.2f, want -60", got)
	}
}

func TestSpectrumDBScaleUsesVisibleSixtyDBRange(t *testing.T) {
	if got := spectrumDBY(200, 120, 0); got != 80 {
		t.Fatalf("0 dB se dibuja en %.1f, want 80", got)
	}
	if got := spectrumDBY(200, 120, -60); got != 200 {
		t.Fatalf("-60 dB se dibuja en %.1f, want 200", got)
	}
}

func TestSpectrumSegmentColorsCommunicateLevel(t *testing.T) {
	quiet := spectrumSegmentColor(-50)
	nominal := spectrumSegmentColor(-35)
	strong := spectrumSegmentColor(-20)
	peak := spectrumSegmentColor(-5)
	if quiet.B <= quiet.R || nominal.G <= nominal.B || strong.R <= strong.G || peak.R <= peak.G {
		t.Fatalf("gradiente inesperado: quiet=%+v nominal=%+v strong=%+v peak=%+v", quiet, nominal, strong, peak)
	}
}

func TestHighCutUsesButterworthCutoffLevel(t *testing.T) {
	config := audio.DefaultConfig()
	config.LowCutHz, config.HighCutHz = 20, 10000
	config.EQEnabled = false
	got := audio.ResponseDB(config, audio.DefaultSampleRate, 10000)
	if got < -3.3 || got > -2.7 {
		t.Fatalf("respuesta en el corte superior = %.2f dB, want aproximadamente -3 dB", got)
	}
}

func TestTXPresenceRaisesHighBandBroadly(t *testing.T) {
	config := audio.DefaultConfig()
	config.LowCutHz, config.HighCutHz = 20, 10000
	config.EQEnabled = false
	without := audio.ResponseDB(config, audio.DefaultSampleRate, 5000)
	config.TXPresenceDB = 9
	with := audio.ResponseDB(config, audio.DefaultSampleRate, 5000)
	if with-without < 6 {
		t.Fatalf("presencia TX solo elevó 5 kHz en %.2f dB", with-without)
	}
}

func TestEqualizerBandsUseFullLogarithmicRange(t *testing.T) {
	graph := eqGraphRectangle()
	previous := float32(-1)
	for band, frequency := range audio.EQFrequencies {
		x := eqBandX(graph, band)
		want := graph.X + logarithmicFrequencyPosition(frequency, 100, 10000)*graph.Width
		if math.Abs(float64(x-want)) > .001 {
			t.Fatalf("banda %d (%.0f Hz) no coincide con la escala EQ: %.3f != %.3f", band, frequency, x, want)
		}
		if x <= previous {
			t.Fatalf("las bandas no avanzan de forma creciente: banda %d en %.3f", band, x)
		}
		previous = x
	}
	if audio.EQBandCount != 11 || audio.EQFrequencies[9] != 8000 || audio.EQFrequencies[10] != 10000 {
		t.Fatalf("faltan las bandas altas del ecualizador: %v", audio.EQFrequencies)
	}
}

func TestTXEchoProducesDelayedSignal(t *testing.T) {
	energyAfterDelay := func(enabled bool) float64 {
		processor := audio.NewProcessor(audio.DefaultSampleRate)
		config := audio.DefaultConfig()
		config.LowCutHz, config.HighCutHz = 20, 10000
		config.EQEnabled, config.NoiseEnabled, config.Compressor.Enabled = false, false, false
		config.Echo = audio.EchoConfig{Enabled: enabled, DelayMS: 40, Feedback: 0, MixPercent: 50, Tone: audio.EchoNeutral}
		processor.Configure(config)
		samples := make([]float32, 4000)
		samples[0] = 1
		processor.Process(samples)
		energy := 0.0
		for _, sample := range samples[1850:2100] {
			energy += float64(sample * sample)
		}
		return energy
	}
	withoutEcho, withEcho := energyAfterDelay(false), energyAfterDelay(true)
	if withEcho < withoutEcho*10 || withEcho < 1e-5 {
		t.Fatalf("el eco no produjo una repetición clara: sin=%g con=%g", withoutEcho, withEcho)
	}
}
