package main

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	rl "github.com/gen2brain/raylib-go/raylib"
)

type remoteRXPlayback struct {
	cancel context.CancelFunc
	done   chan struct{}
	status atomic.Value
	menu   int
	scroll int
	meter  remoteAudioMeter
}

type remoteAudioMeter struct {
	value atomic.Value
}

func (m *remoteAudioMeter) snapshot() audioio.Status {
	if status, ok := m.value.Load().(audioio.Status); ok {
		return status
	}
	return audioio.Status{InputRMSDBFS: -120, OutputRMSDBFS: -120}
}

func (m *remoteAudioMeter) reset() {
	m.value.Store(audioio.Status{InputRMSDBFS: -120, OutputRMSDBFS: -120})
}

func (m *remoteAudioMeter) process(samples []float32, process func([]float32)) {
	if len(samples) == 0 {
		return
	}
	input := blockRMSDBFS(samples)
	start := time.Now()
	process(samples)
	load := float32(100 * time.Since(start).Seconds() * remote.AudioSampleRate / float64(len(samples)))
	m.value.Store(audioio.Status{
		Running: true, SampleRate: remote.AudioSampleRate, BufferFrames: len(samples),
		InputRMSDBFS: input, OutputRMSDBFS: blockRMSDBFS(samples), CPULoad: load,
	})
}

func blockRMSDBFS(samples []float32) float32 {
	sum := 0.0
	for _, sample := range samples {
		value := float64(sample)
		sum += value * value
	}
	return float32(10 * math.Log10(max(1e-12, sum/float64(len(samples)))))
}

func (a *app) remoteDevices(input bool) []audioio.Device {
	devices := a.rxView.outputs
	if input {
		devices = a.txView.inputs
	}
	var filtered []audioio.Device
	for _, device := range devices {
		if !isRadioUSBCodec(device.Name) && (!input || !isLoopbackDevice(device.Name)) {
			filtered = append(filtered, device)
		}
	}
	return filtered
}

func remoteDeviceIndex(devices []audioio.Device, saved string) int {
	if saved != "" {
		for i, device := range devices {
			if device.Name == saved {
				return i
			}
		}
		return -1
	}
	if len(devices) == 0 {
		return -1
	}
	return defaultDeviceIndex(devices)
}

func (a *app) startRemoteAudio() {
	a.stopRemoteAudio()
	rx := &a.radio.rxAudio
	client := a.radio.remote
	if client == nil {
		return
	}
	outputs := a.remoteDevices(false)
	index := remoteDeviceIndex(outputs, a.settings.RemoteRXOutput)
	if index < 0 || a.monitor == nil {
		rx.status.Store("Selecciona una salida de audio disponible")
		return
	}
	if err := a.monitor.StartNamed(outputs[index].Name); err != nil {
		rx.status.Store("No se pudo abrir la salida de audio: " + err.Error())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	processor := a.rxWDSP
	rx.cancel, rx.done = cancel, make(chan struct{})
	rx.status.Store("Esperando audio del servidor")
	go func(done chan struct{}) {
		defer close(done)
		for {
			playing := false
			err := client.PlayRX(ctx, func(samples []float32) {
				if !playing {
					rx.status.Store("Recibiendo audio de la emisora")
					playing = true
				}
				if !a.radio.txAudio.active.Load() {
					rx.meter.process(samples, processor.Process)
					a.monitor.Write(samples)
				}
			})
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				rx.meter.reset()
				rx.status.Store("Esperando audio del servidor")
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}(rx.done)
}

func (a *app) stopRemoteAudio() {
	rx := &a.radio.rxAudio
	if rx.cancel != nil {
		rx.cancel()
		<-rx.done
		a.monitor.Stop()
		rx.cancel, rx.done = nil, nil
	}
	rx.meter.reset()
	rx.status.Store("Conecta con el servidor para escuchar la emisora")
}

const (
	remoteOutputMenu = iota + 1
	remoteInputMenu
)

func remoteAudioRect(menu int) rl.Rectangle {
	if menu == remoteInputMenu {
		return audioDeviceRect(0)
	}
	return audioDeviceRect(1)
}

func remoteAudioItem(menu, row int) rl.Rectangle {
	rect := remoteAudioRect(menu)
	return rl.NewRectangle(rect.X, 160+float32(row)*44, rect.Width, 42)
}

func (a *app) remoteAudioSelection(menu int) ([]audioio.Device, *string) {
	if menu == remoteInputMenu {
		return a.remoteDevices(true), &a.settings.RemoteTXInput
	}
	return a.remoteDevices(false), &a.settings.RemoteRXOutput
}

func (a *app) updateRemoteAudio(mouse rl.Vector2) bool {
	rx := &a.radio.rxAudio
	for _, menu := range []int{remoteOutputMenu, remoteInputMenu} {
		if clicked(mouse, remoteAudioRect(menu)) {
			devices, saved := a.remoteAudioSelection(menu)
			if rx.menu == menu || len(devices) == 0 {
				rx.menu = 0
			} else {
				rx.menu = menu
				rx.scroll = initialDeviceScroll(remoteDeviceIndex(devices, *saved), len(devices))
			}
			return true
		}
	}
	if rx.menu == 0 {
		return false
	}
	menu := rx.menu
	devices, saved := a.remoteAudioSelection(menu)
	visible := min(8, len(devices))
	if wheel := rl.GetMouseWheelMove(); wheel > 0 {
		rx.scroll = max(0, rx.scroll-1)
	} else if wheel < 0 {
		rx.scroll = min(len(devices)-visible, rx.scroll+1)
	}
	if rl.IsKeyPressed(rl.KeyEscape) {
		rx.menu = 0
		return true
	}
	if !rl.IsMouseButtonPressed(rl.MouseLeftButton) {
		return true
	}
	rx.menu = 0
	for row := range visible {
		if rl.CheckCollisionPointRec(mouse, remoteAudioItem(menu, row)) {
			*saved = devices[rx.scroll+row].Name
			a.save()
			if menu == remoteOutputMenu {
				a.startRemoteAudio()
			}
			a.refreshRemoteMonitor()
			return true
		}
	}
	return true
}

func (a *app) drawRemoteAudioSelectors(mouse rl.Vector2) {
	for _, selector := range []struct {
		menu               int
		label, placeholder string
	}{
		{remoteOutputMenu, "SALIDA DE AUDIO", "Seleccionar altavoces o auriculares"},
		{remoteInputMenu, "MICRÓFONO", "Seleccionar micrófono"},
	} {
		devices, saved := a.remoteAudioSelection(selector.menu)
		index := remoteDeviceIndex(devices, *saved)
		name := selector.placeholder
		if index >= 0 {
			name = devices[index].Name
		}
		drawSelector(remoteAudioRect(selector.menu), selector.label, name, mouse)
	}
}

func (a *app) drawRemoteAudioMenu() {
	rx := &a.radio.rxAudio
	if rx.menu == 0 {
		return
	}
	devices, saved := a.remoteAudioSelection(rx.menu)
	selected := remoteDeviceIndex(devices, *saved)
	for row := range min(8, len(devices)) {
		item := rx.scroll + row
		drawConsoleButton(remoteAudioItem(rx.menu, row), ellipsis(devices[item].Name, 60), item == selected, false)
	}
}
