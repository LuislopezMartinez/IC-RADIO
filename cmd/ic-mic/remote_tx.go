package main

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
)

type remoteTX struct {
	cancel      context.CancelFunc
	done        chan error
	stopMic     func()
	blocks      chan []byte
	active      atomic.Bool
	monitor     atomic.Pointer[audioio.PlaybackMonitor]
	stopPreview func()
	ending      atomic.Bool
	meter       remoteAudioMeter
}

func (a *app) transmitting() bool {
	return a.ptt.TX() || a.radio.txAudio.active.Load()
}

func (a *app) monitorEnabled() bool {
	if a.settings.RemoteMode {
		return a.radio.txAudio.monitor.Load() != nil
	}
	return a.settings.MonitorEnabled
}

func (a *app) setRemoteTX(enabled bool) bool {
	tx := &a.radio.txAudio
	if !enabled {
		a.releaseRemoteTX()
		return true
	}
	if tx.active.Load() {
		return true
	}
	client := a.radio.remote
	state, online := a.radio.snapshot()
	if client == nil || !online || state.PowerPending || !state.TXKnown || state.TX || state.Tuner == 2 {
		a.message = "La emisora remota debe estar conectada y en recepción"
		return false
	}
	inputs := a.remoteDevices(true)
	index := remoteDeviceIndex(inputs, a.settings.RemoteTXInput)
	if index < 0 {
		a.message = "Selecciona un micrófono en la pestaña Audio"
		return false
	}
	blocks := make(chan []byte, 3)
	processor := a.txSignal
	a.stopRemoteMicPreview()
	start := a.settings.Signaling.Start
	sequencer := a.signalSequencer
	sequencer.Stop()
	if a.settings.Signaling.StartEnabled && len(start) > 0 &&
		sequencer.Start(append([]signalBlock{{Type: signalPause, DurationMS: 80}}, start...)) {
		processor.cancelFade()
	} else if a.settings.TXFadeEnabled {
		processor.startFade(a.settings.TXFadeMS)
	} else {
		processor.cancelFade()
	}
	stopMic, err := openRemoteMic(inputs[index].Name, func(samples []float32) {
		if tx.ending.Load() {
			clear(samples)
		}
		var monitor sampleMonitor
		if output := tx.monitor.Load(); output != nil {
			monitor = output
		}
		tx.meter.process(samples, func(block []float32) { processor.process(block, monitor) })
	}, blocks)
	if err != nil {
		sequencer.Stop()
		processor.cancelFade()
		a.resumeRemoteMicPreview()
		a.message = "No se pudo abrir el micrófono: " + err.Error()
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxTXDuration)
	tx.cancel, tx.done, tx.stopMic = cancel, make(chan error, 1), stopMic
	tx.blocks = blocks
	a.txSince = time.Now()
	tx.active.Store(true)
	go func(done chan error) { done <- client.Transmit(ctx, blocks) }(tx.done)
	a.message = "PTT remoto solicitado"
	return true
}

func (a *app) releaseRemoteTX() {
	tx := &a.radio.txAudio
	if !tx.active.Load() || tx.ending.Load() {
		return
	}
	end := a.settings.Signaling.End
	_, online := a.radio.snapshot()
	if online && a.settings.Signaling.EndEnabled && len(end) > 0 {
		// Keep the radio keyed long enough for the server's playback buffer
		// to finish the final note before the upload reaches EOF.
		sequence := append(append([]signalBlock(nil), end...), signalBlock{Type: signalPause, DurationMS: 140})
		tx.ending.Store(true)
		a.txSignal.cancelFade()
		if a.signalSequencer.Start(sequence) {
			a.message = "Reproduciendo el tono de fin de transmisión"
			return
		}
	}
	a.stopRemoteTX()
}

func (a *app) finishRemoteSignal() {
	tx := &a.radio.txAudio
	if tx.ending.Load() {
		if tx.stopMic != nil {
			tx.stopMic()
			tx.stopMic = nil
			close(tx.blocks)
			tx.blocks = nil
		}
	} else if a.settings.TXFadeEnabled {
		a.txSignal.startFade(a.settings.TXFadeMS)
	}
}

func (a *app) toggleRemoteMonitor() {
	enabled := !a.monitorEnabled()
	if enabled {
		if err := a.startRemoteMonitor(); err != nil {
			a.message = "MON: " + err.Error()
			return
		}
		a.message = "MON activo · escuchando el micrófono local"
	} else {
		a.closeRemoteMonitor()
		a.message = "MON desactivado"
	}
	a.settings.RemoteMonitor = enabled
	a.save()
}

func (a *app) startRemoteMonitor() error {
	outputs := a.remoteDevices(false)
	index := remoteDeviceIndex(outputs, a.settings.RemoteRXOutput)
	if index < 0 {
		return errors.New("selecciona una salida disponible en Audio")
	}
	output, err := audioio.NewPlaybackMonitor()
	if err != nil {
		return err
	}
	if err := output.StartNamed(outputs[index].Name); err != nil {
		output.Close()
		return err
	}
	a.radio.txAudio.monitor.Store(output)
	if err := a.startRemoteMicPreview(); err != nil {
		a.closeRemoteMonitor()
		return err
	}
	return nil
}

func (a *app) startRemoteMicPreview() error {
	tx := &a.radio.txAudio
	output := tx.monitor.Load()
	if output == nil || tx.active.Load() || tx.stopPreview != nil {
		return nil
	}
	inputs := a.remoteDevices(true)
	index := remoteDeviceIndex(inputs, a.settings.RemoteTXInput)
	if index < 0 {
		return errors.New("selecciona un micrófono disponible en Audio")
	}
	processor := a.txSignal
	stop, err := openRemoteMic(inputs[index].Name, func(samples []float32) {
		tx.meter.process(samples, func(block []float32) { processor.process(block, output) })
	}, nil)
	if err == nil {
		tx.stopPreview = stop
	}
	return err
}

func (a *app) stopRemoteMicPreview() {
	tx := &a.radio.txAudio
	if tx.stopPreview != nil {
		tx.stopPreview()
		tx.stopPreview = nil
		tx.meter.reset()
	}
}

func (a *app) closeRemoteMonitor() {
	a.stopRemoteMicPreview()
	if output := a.radio.txAudio.monitor.Swap(nil); output != nil {
		output.Close()
	}
}

func (a *app) refreshRemoteMonitor() {
	a.closeRemoteMonitor()
	if a.settings.RemoteMonitor {
		if err := a.startRemoteMonitor(); err != nil {
			a.message = "MON: " + err.Error()
		}
	}
}

func (a *app) resumeRemoteMicPreview() {
	if err := a.startRemoteMicPreview(); err != nil {
		a.message = "MON: " + err.Error()
	}
}

func (a *app) stopRemoteTX() {
	tx := &a.radio.txAudio
	if tx.cancel == nil {
		return
	}
	tx.cancel()
	if tx.stopMic != nil {
		tx.stopMic()
	}
	tx.cancel, tx.done, tx.stopMic = nil, nil, nil
	tx.blocks = nil
	tx.active.Store(false)
	tx.ending.Store(false)
	tx.meter.reset()
	a.signalSequencer.Stop()
	a.txSignal.cancelFade()
	a.txSignal.enabled.Store(false)
	a.message = "PTT remoto liberado"
	a.resumeRemoteMicPreview()
}

func (a *app) updateRemoteTX() {
	tx := &a.radio.txAudio
	select {
	case err := <-tx.done:
		a.stopRemoteTX()
		if err != nil {
			a.message = "PTT remoto finalizado: " + err.Error()
		}
	default:
	}
}
