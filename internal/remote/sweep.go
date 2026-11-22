package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/swr"
)

const sweepPath = "/swr/v1"
const sweepTimeout = time.Minute

func (a *TXAudio) SweepHandler(radio swr.Radio, keyer swr.Keyer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		var request swr.Request
		input := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
		if input.Decode(&request) != nil || input.Decode(new(any)) != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		points := request.ValidatedPoints()
		if points == nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !a.mu.TryLock() {
			w.WriteHeader(http.StatusConflict)
			return
		}
		defer a.mu.Unlock()
		state := radio.Snapshot()
		if a.closed || !a.Ready() || state.Frequency != request.Center || !state.ModeKnown || state.Mode > 1 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if a.StartPlayback() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer a.StopPlayback()
		ctx, cancel := context.WithTimeout(r.Context(), sweepTimeout)
		defer cancel()
		control := http.NewResponseController(w)
		_ = control.SetReadDeadline(time.Time{})
		_ = control.SetWriteDeadline(time.Now().Add(2 * time.Second))
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		if control.Flush() != nil {
			return
		}

		var toneUntil atomic.Int64
		toneCtx, stopTone := context.WithCancel(ctx)
		toneDone := make(chan struct{})
		go func() {
			defer close(toneDone)
			playSweepTone(toneCtx, &toneUntil, a.Write)
		}()
		defer func() { stopTone(); <-toneDone }()
		reports := json.NewEncoder(w)
		swr.Run(ctx, radio, keyer, func(duration time.Duration) {
			var until int64
			if duration > 0 {
				until = time.Now().Add(duration).UnixNano()
			}
			toneUntil.Store(until)
		}, state, points, func(report swr.Report) {
			if r.Context().Err() != nil {
				return
			}
			_ = control.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if reports.Encode(report) != nil || control.Flush() != nil {
				cancel()
			}
		})
	})
}

func playSweepTone(ctx context.Context, until *atomic.Int64, write func([]float32)) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var block [AudioSampleRate / 50]float32
	for i := range block {
		block[i] = float32(.15 * math.Sin(2*math.Pi*swr.ToneHz*float64(i)/AudioSampleRate))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if ctx.Err() == nil && time.Now().UnixNano() < until.Load() {
				write(block[:])
			}
		}
	}
}

func (c *Client) Sweep(ctx context.Context, parameters swr.Request, report func(swr.Report)) error {
	data, err := json.Marshal(parameters)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+sweepPath, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("ic-radio", c.password)
	transport := c.http.Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 3 * time.Second
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: c.http.CheckRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return errors.New(PasswordError)
	case http.StatusBadRequest:
		return errors.New("El servidor rechazó el rango del barrido")
	case http.StatusConflict:
		return errors.New("La emisora está ocupada o cambió de frecuencia o modo")
	default:
		return errors.New("Barrido de ROE no disponible en el servidor")
	}
	reports := json.NewDecoder(io.LimitReader(resp.Body, 65536))
	for {
		var update swr.Report
		if err := reports.Decode(&update); err != nil {
			return err
		}
		report(update)
		if update.Final {
			return nil
		}
	}
}
