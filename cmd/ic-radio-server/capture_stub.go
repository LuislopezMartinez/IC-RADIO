//go:build !cgo

package main

import (
	"errors"

	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
)

func startRXCapture(*remote.RXAudio, audioChoice) (string, string, func(), error) {
	return "", "", nil, errors.New("esta compilación no incluye captura de audio")
}

func findTXPlayback(audioChoice) (string, string, error) {
	return "", "", errors.New("esta compilación no incluye reproducción de audio")
}
