//go:build !cgo

package main

import "errors"

func openRemoteMic(string, func([]float32), chan []byte) (func(), error) {
	return nil, errors.New("esta compilación no incluye captura de micrófono")
}
