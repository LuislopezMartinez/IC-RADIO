//go:build cgo

package main

import (
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	"github.com/gen2brain/malgo"
)

func startRXCapture(audio *remote.RXAudio, choice audioChoice) (string, string, func(), error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return "", "", nil, err
	}
	closeContext := func() {
		_ = ctx.Uninit()
		ctx.Free()
	}
	inputs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		closeContext()
		return "", "", nil, err
	}
	index, err := selectAudioDevice(audioOptions(inputs), choice)
	if err != nil {
		closeContext()
		return "", "", nil, err
	}
	input := inputs[index]
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.SampleRate = remote.AudioSampleRate
	cfg.PeriodSizeInMilliseconds = 20
	cfg.Capture.Format, cfg.Capture.Channels = malgo.FormatS16, 1
	cfg.Capture.DeviceID = input.ID.Pointer()
	device, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(_, pcm []byte, _ uint32) {
		audio.Publish(pcm)
	}})
	if err != nil {
		closeContext()
		return "", "", nil, err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		closeContext()
		return "", "", nil, err
	}
	return input.Name(), input.ID.String(), func() {
		device.Uninit()
		closeContext()
	}, nil
}

func findTXPlayback(choice audioChoice) (string, string, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return "", "", err
	}
	defer ctx.Free()
	defer ctx.Uninit()
	outputs, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return "", "", err
	}
	index, err := selectAudioDevice(audioOptions(outputs), choice)
	if err != nil {
		return "", "", err
	}
	return outputs[index].Name(), outputs[index].ID.String(), nil
}

func audioOptions(devices []malgo.DeviceInfo) []audioOption {
	options := make([]audioOption, len(devices))
	for i := range devices {
		options[i] = audioOption{name: devices[i].Name(), id: devices[i].ID.String()}
	}
	return options
}
