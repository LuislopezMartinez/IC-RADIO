package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LuislopezMartinez/IC-DSP/audio-processing-core/audioio"
	"github.com/LuislopezMartinez/IC-RADIO/internal/ptt"
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := loadConfiguration()
	flag.StringVar(&cfg.COM, "com", cfg.COM, "Puerto USB CI-V de la emisora")
	flag.StringVar(&cfg.CIV, "civ", cfg.CIV, "Dirección CI-V hexadecimal, sin el sufijo h")
	flag.StringVar(&cfg.Port, "puerto", cfg.Port, "Puerto TCP de escucha en 127.0.0.1")
	rxName := flag.String("rx-audio", "", "Nombre exacto de la entrada USB de audio de la emisora (detección automática por defecto)")
	txName := flag.String("tx-audio", "", "Nombre exacto de la salida USB de audio de la emisora (detección automática por defecto)")
	pttLine := flag.String("ptt-line", "RTS", "Línea USB SEND de la emisora (RTS o DTR)")
	flag.Parse()
	line := ptt.Line(strings.ToUpper(*pttLine))
	if line != ptt.RTS && line != ptt.DTR {
		return errors.New("la línea PTT debe ser RTS o DTR")
	}

	fmt.Println("Bienvenido al servidor de IC-RADIO")
	fmt.Println("Software libre creado por EA3IWW.")
	fmt.Println()
	interactive := len(os.Args) == 1
	input := bufio.NewScanner(os.Stdin)
	if interactive {
		var err error
		cfg, err = promptConfiguration(input, os.Stdout, ptt.Ports(), cfg)
		if err != nil {
			return err
		}
	}
	if interactive || passwordCheck(cfg.Password) == nil {
		var err error
		cfg.Password, err = promptPassword(input, os.Stdout, cfg.Password)
		if err != nil {
			return err
		}
	}
	if cfg.COM == "" {
		return errors.New("indica el puerto USB de la emisora con -com COMx")
	}
	if !validCIV(cfg.CIV) {
		return errors.New("la dirección CI-V debe estar entre 01 y DF")
	}
	if !validPort(cfg.Port) {
		return errors.New("el puerto TCP debe estar entre 1 y 65535")
	}
	civAddress, _ := strconv.ParseUint(cfg.CIV, 16, 8)
	portNumber, _ := strconv.ParseUint(cfg.Port, 10, 16)
	cfg.CIV, cfg.Port = fmt.Sprintf("%02X", civAddress), strconv.FormatUint(portNumber, 10)
	endpoint := net.JoinHostPort("127.0.0.1", cfg.Port)
	listener, err := net.Listen("tcp4", endpoint)
	if err != nil {
		return err
	}
	defer listener.Close()

	var controller ptt.Controller
	if err := controller.Connect(cfg.COM, line); err != nil {
		return err
	}
	defer controller.Close()
	radio, err := controller.StartCIV(byte(civAddress))
	if err != nil {
		return err
	}
	var audioHandler http.Handler
	audio := &remote.RXAudio{}
	audioDevice, stopAudio, audioErr := startRXCapture(audio, *rxName)
	if audioErr == nil {
		defer stopAudio()
		audioHandler = audio
	}
	var tx *remote.TXAudio
	var sweepHandler http.Handler
	txDevice, txErr := findTXPlayback(*txName)
	if txErr == nil {
		var playback *audioio.PlaybackMonitor
		playback, txErr = audioio.NewPlaybackMonitor()
		if txErr == nil {
			defer playback.Close()
			tx = &remote.TXAudio{
				Ready: func() bool {
					state := radio.Snapshot()
					return state.Error == "" && !state.PowerPending && !state.PoweredOff && state.Tuner != 2 &&
						state.TXKnown && !state.TX && time.Since(state.TXAt) < time.Second
				},
				Keyed: func() bool {
					state := radio.Snapshot()
					return state.Error == "" && state.TXKnown && state.TX && time.Since(state.TXAt) < time.Second
				},
				Key:           controller.SetTX,
				StartPlayback: func() error { return playback.StartNamed(txDevice) },
				StopPlayback:  playback.Stop,
				Write:         playback.Write,
			}
			defer tx.Close()
			sweepHandler = tx.SweepHandler(radio, &controller)
		}
	}
	var lastActivity atomic.Int64
	server := &http.Server{
		Handler: remote.Handler(radio, passwordCheck(cfg.Password), func() {
			lastActivity.Store(time.Now().UnixMilli())
		}, audioHandler, tx, sweepHandler), ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 5 * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	useColor, restoreColor := consoleColor()
	defer restoreColor()
	showConnection(cfg, useColor)
	if audioErr != nil {
		fmt.Printf("Audio de recepción no disponible — %v\n", audioErr)
	} else {
		fmt.Printf("Audio de recepción — %s\n", audioDevice)
	}
	if txErr != nil {
		fmt.Printf("Audio de transmisión no disponible — %v\n", txErr)
	} else {
		fmt.Printf("Audio de transmisión — %s · PTT %s\n", txDevice, line)
	}
	if interactive {
		fmt.Println("En IC-RADIO, abre CFG y selecciona Modo Remoto.")
		fmt.Println("Introduce la dirección, el puerto y la contraseña. Luego pulsa el botón de encendido.")
		fmt.Println("Deja esta ventana abierta. Para detener el servidor, pulsa Ctrl+C aquí.")
	}
	fmt.Println("\nEstado del cliente — esperando conexión.")
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		watchClient(ctx, &lastActivity)
	}()
	saved := make(chan struct{})
	go func() {
		defer close(saved)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				state := radio.Snapshot()
				if state.FrequencyAt.IsZero() || state.Error != "" {
					continue
				}
				if err := saveConfiguration(cfg); err != nil {
					log.Printf("No se pudo guardar la configuración: %v", err)
				} else {
					fmt.Println("Emisora detectada. Configuración guardada.")
				}
				return
			}
		}
	}()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	cancel()
	<-saved
	<-watching
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func showConnection(cfg configuration, useColor bool) {
	lines := []string{
		"DATOS PARA CONECTAR IC-RADIO",
		"Direccion del servidor  127.0.0.1",
		"Puerto de conexion      " + cfg.Port,
		"Emisora por USB         " + cfg.COM + " / CI-V " + cfg.CIV + "h",
		"Contrasena              La que has configurado",
	}
	width := 0
	for _, line := range lines {
		width = max(width, len(line))
	}
	border := "+" + strings.Repeat("-", width+2) + "+"
	var box strings.Builder
	if useColor {
		box.WriteString("\x1b[96m")
	}
	fmt.Fprintln(&box, "\n"+border)
	for _, line := range lines {
		fmt.Fprintf(&box, "| %-*s |\n", width, line)
	}
	fmt.Fprint(&box, border)
	if useColor {
		box.WriteString("\x1b[0m")
	}
	fmt.Fprintln(&box)
	fmt.Print(box.String())
}

func watchClient(ctx context.Context, lastActivity *atomic.Int64) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	connected := false
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			active := now.UnixMilli()-lastActivity.Load() < 2000
			if active == connected {
				continue
			}
			connected = active
			if connected {
				fmt.Println("Estado del cliente — conectado.")
			} else {
				fmt.Println("Estado del cliente — desconectado. Esperando conexión.")
			}
		}
	}
}
