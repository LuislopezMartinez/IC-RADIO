package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	audioPath       = "/audio/rx/v1"
	audioFormat     = "application/x-ic-radio-pcm-s16le"
	AudioSampleRate = 48000
)

// RXAudio sends mono 48 kHz PCM to each connected listener. A slow listener
// loses its oldest block so it cannot stall capture or accumulate latency.
type RXAudio struct {
	mu        sync.Mutex
	listeners map[chan []byte]struct{}
}

func (a *RXAudio) Publish(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.listeners) == 0 {
		return
	}
	block := append([]byte(nil), pcm...)
	for listener := range a.listeners {
		if len(listener) == cap(listener) {
			select {
			case <-listener:
			default:
			}
		}
		listener <- block
	}
}

func (a *RXAudio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	listener := make(chan []byte, 4)
	a.mu.Lock()
	if a.listeners == nil {
		a.listeners = make(map[chan []byte]struct{})
	}
	a.listeners[listener] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.listeners, listener)
		a.mu.Unlock()
	}()
	w.Header().Set("Content-Type", audioFormat)
	w.Header().Set("Cache-Control", "no-store")
	controller := http.NewResponseController(w)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			return
		case block := <-listener:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(2 * time.Second)
			_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := w.Write(block); err != nil || controller.Flush() != nil {
				return
			}
		}
	}
}

// PlayRX reads PCM until the stream ends or ctx is cancelled.
func (c *Client) PlayRX(ctx context.Context, write func([]float32)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+audioPath, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("ic-radio", c.password)
	client := &http.Client{Transport: c.http.Transport, CheckRedirect: c.http.CheckRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New(PasswordError)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != audioFormat {
		return errors.New("El servidor no tiene audio de recepción disponible")
	}
	var pcm [AudioSampleRate / 50 * 2]byte
	var samples [AudioSampleRate / 50]float32
	for {
		if _, err := io.ReadFull(resp.Body, pcm[:]); err != nil {
			return err
		}
		for i := range samples {
			samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		}
		write(samples[:])
	}
}
