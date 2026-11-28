package main

import (
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	editRemoteHost = iota + 1
	editRemotePort
	editRemotePassword
)

func connectionModeRect() rl.Rectangle { return configControl(1330, 113) }

func (a *app) remoteTarget() (string, string) {
	host, port := a.settings.RemoteHost, a.settings.RemotePort
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = remote.DefaultPort
	}
	return host, port
}

func (a *app) connectRemote() bool {
	host, port := a.remoteTarget()
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 || !validRemoteHost(host) {
		a.message = "Revisa la dirección y el puerto del servidor en CFG"
		a.radio.page = pageConfig
		return false
	}
	if a.radio.remotePassword == "" {
		a.message = "Introduce la contraseña del servidor y pulsa Enter"
		a.radio.page = pageConfig
		a.radio.remoteEdit, a.radio.input = editRemotePassword, ""
		return false
	}
	a.radio.remote = remote.Connect(net.JoinHostPort(host, strconv.FormatUint(number, 10)), a.radio.remotePassword)
	a.radio.session = a.radio.remote
	a.radio.requestInitialVFO()
	a.message = "Servidor remoto · " + net.JoinHostPort(host, port)
	a.startRemoteAudio()
	return true
}

func validRemoteHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	return host != "" && strings.Trim(host, ".-") != "" && strings.IndexFunc(host, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-')
	}) == -1
}

func (a *app) updateConnectionMode(mouse rl.Vector2) bool {
	r := &a.radio
	if clicked(mouse, connectionModeRect()) {
		if a.settings.RemoteMode {
			a.closeRemoteMonitor()
		}
		a.disconnectRadio()
		a.settings.RemoteMode = !a.settings.RemoteMode
		if a.settings.RemoteMode {
			a.stopRXMonitor()
			a.txView.engine.Stop()
			if a.monitor != nil {
				a.monitor.Stop()
			}
			a.signalSequencer.Stop()
			a.refreshRemoteMonitor()
		} else {
			a.switchMode("tx")
			a.reconnectAudio()
			if a.settings.RXInput != "" && a.settings.RXOutput != "" {
				if err := a.startRXMonitor(); err != nil {
					a.message = "RX monitor: " + err.Error()
				}
			}
			if a.settings.MonitorEnabled {
				if err := a.startOutputMonitor(); err != nil {
					a.message = "TX monitor: " + err.Error()
				}
			}
		}
		a.save()
		return true
	}
	if !a.settings.RemoteMode {
		return false
	}
	if r.remoteEdit != 0 {
		if rl.IsKeyPressed(rl.KeyEscape) {
			r.input = ""
			r.remoteEdit = 0
			return true
		}
		switch r.remoteEdit {
		case editRemoteHost:
			r.collectInput("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.:-", 253)
		case editRemotePort:
			r.collectInput("0123456789", 5)
		case editRemotePassword:
			for key := rl.GetCharPressed(); key > 0; key = rl.GetCharPressed() {
				char := rune(key)
				if unicode.IsPrint(char) && len(r.input)+utf8.RuneLen(char) <= 128 {
					r.input += string(char)
				}
			}
			if rl.IsKeyPressed(rl.KeyBackspace) && len(r.input) > 0 {
				_, size := utf8.DecodeLastRuneInString(r.input)
				r.input = r.input[:len(r.input)-size]
			}
		}
		if rl.IsKeyPressed(rl.KeyEnter) {
			if r.remoteEdit == editRemotePassword {
				if len(r.input) < 8 || strings.TrimSpace(r.input) != r.input {
					a.message = "Usa al menos 8 caracteres, sin espacios al principio ni al final"
					return true
				}
				r.remotePassword, r.input, r.remoteEdit = r.input, "", 0
				a.message = "Contraseña lista. Pulsa Encendido o Conectar."
				return true
			}
			if r.remoteEdit == editRemoteHost {
				if !validRemoteHost(r.input) {
					a.message = "Introduce una dirección IP o un nombre de servidor"
					return true
				}
				a.settings.RemoteHost = r.input
			} else {
				number, err := strconv.ParseUint(r.input, 10, 16)
				if err != nil || number == 0 {
					a.message = "El puerto debe estar entre 1 y 65535"
					return true
				}
				a.settings.RemotePort = strconv.FormatUint(number, 10)
			}
			r.remoteEdit = 0
			a.message = "Destino remoto guardado"
			a.save()
		}
		return true
	}
	if clicked(mouse, configControl(710, 270)) {
		if r.remote != nil {
			a.disconnectRadio()
		} else {
			a.connectRemote()
		}
	} else if clicked(mouse, remotePasswordRect()) {
		if r.remote != nil {
			a.disconnectRadio()
		}
		r.remoteEdit, r.input = editRemotePassword, ""
	} else if r.remote == nil {
		host, port := a.remoteTarget()
		if clicked(mouse, remoteHostRect()) {
			r.remoteEdit, r.input = editRemoteHost, host
		} else if clicked(mouse, configControl(490, 183)) {
			r.remoteEdit, r.input = editRemotePort, port
		}
	}
	return true
}

func remoteHostRect() rl.Rectangle     { return rl.NewRectangle(50, 183, 425, 44) }
func remotePasswordRect() rl.Rectangle { return rl.NewRectangle(710, 183, 360, 44) }

func (a *app) drawConnectionMode() {
	label := "MODO LOCAL"
	if a.settings.RemoteMode {
		label = "MODO REMOTO"
	}
	drawButton(connectionModeRect(), label, a.settings.RemoteMode)
}

func (a *app) drawRemoteConfig() {
	drawDarkDisplay(rl.NewRectangle(30, 96, 1540, 282))
	drawText("CONEXIÓN REMOTA", 50, 114, 28, color.text)
	a.drawConnectionMode()
	host, port := a.remoteTarget()
	switch a.radio.remoteEdit {
	case editRemoteHost:
		host = a.radio.input + "_"
	case editRemotePort:
		port = a.radio.input + "_"
	}
	drawButton(remoteHostRect(), "SERVIDOR  "+ellipsis(host, 39), a.radio.remoteEdit == editRemoteHost)
	drawButton(configControl(490, 183), "PUERTO  "+port, a.radio.remoteEdit == editRemotePort)
	password := "CONTRASEÑA"
	if a.radio.remoteEdit == editRemotePassword {
		password += "  " + strings.Repeat("*", min(20, utf8.RuneCountInString(a.radio.input))) + "_"
	} else if a.radio.remotePassword != "" {
		password += "  ********"
	}
	drawButton(remotePasswordRect(), password, a.radio.remoteEdit == editRemotePassword)
	label := "CONECTAR"
	if a.radio.remote != nil {
		label = "DESCONECTAR"
	}
	drawButton(configControl(710, 270), label, a.radio.remote != nil)
	var devices remote.AudioDevices
	if a.radio.remote != nil {
		devices = a.radio.remote.AudioDevices()
	}
	if devices.RXInput == "" {
		devices.RXInput = "Unavailable"
	}
	if devices.TXOutput == "" {
		devices.TXOutput = "Unavailable"
	}
	drawText("Radio RX input   "+ellipsis(devices.RXInput, 56), 50, 242, 17, color.text)
	drawText("Radio TX output   "+ellipsis(devices.TXOutput, 56), 50, 277, 17, color.text)
	drawText(ellipsis(a.message, 100), 50, 336, 17, color.muted)
}
