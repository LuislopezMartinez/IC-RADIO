package main

import (
	"math"
	"testing"

	audio "github.com/LuislopezMartinez/IC-DSP/audio-processing-core"
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
