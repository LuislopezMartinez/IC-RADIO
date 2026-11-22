//go:build cgo

package main

import (
	"fmt"
	"strings"

	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	"github.com/gen2brain/malgo"
)

func startRXCapture(audio *remote.RXAudio, name string) (string, func(), error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return "", nil, err
	}
	closeContext := func() {
		_ = ctx.Uninit()
		ctx.Free()
	}
	inputs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		closeContext()
		return "", nil, err
	}
	var matches []malgo.DeviceInfo
	for _, input := range inputs {
		if name == "" && strings.Contains(strings.ToLower(input.Name()), "usb audio codec") ||
			name != "" && strings.EqualFold(input.Name(), name) {
			matches = append(matches, input)
		}
	}
	if len(matches) != 1 {
		closeContext()
		names := make([]string, 0, len(inputs))
		for _, input := range inputs {
			names = append(names, input.Name())
		}
		return "", nil, fmt.Errorf("no se pudo identificar una única entrada USB. Usa -rx-audio con el nombre exacto. Entradas: %s", strings.Join(names, " | "))
	}
	input := matches[0]
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
		return "", nil, err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		closeContext()
		return "", nil, err
	}
	return input.Name(), func() {
		device.Uninit()
		closeContext()
	}, nil
}

func findTXPlayback(name string) (string, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return "", err
	}
	defer ctx.Free()
	defer ctx.Uninit()
	outputs, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return "", err
	}
	var selected string
	for _, output := range outputs {
		if name == "" && strings.Contains(strings.ToLower(output.Name()), "usb audio codec") ||
			name != "" && strings.EqualFold(output.Name(), name) {
			if selected != "" {
				return "", fmt.Errorf("varias salidas de la emisora detectadas. Usa -tx-audio con el nombre exacto")
			}
			selected = output.Name()
		}
	}
	if selected == "" {
		return "", fmt.Errorf("no se encontró la salida USB de la emisora. Usa -tx-audio con el nombre exacto")
	}
	return selected, nil
}
