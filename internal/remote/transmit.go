package remote

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	txPath           = "/audio/tx/v1"
	TXBlockBytes     = AudioSampleRate / 50 * 2
	txSilenceTimeout = 500 * time.Millisecond
	txLimit          = 3 * time.Minute
)

type TXAudio struct {
	mu            sync.Mutex
	closed        bool
	Ready         func() bool
	Keyed         func() bool
	Key           func(bool) error
	StartPlayback func() error
	StopPlayback  func()
	Write         func([]float32)
}

func (a *TXAudio) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
}

func (a *TXAudio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Content-Type") != audioFormat {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}
	connection := http.NewResponseController(w)
	if connection.EnableFullDuplex() != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_ = connection.SetWriteDeadline(time.Now().Add(txLimit + 3*time.Second))
	if !a.mu.TryLock() {
		w.WriteHeader(http.StatusConflict)
		return
	}
	defer a.mu.Unlock()
	if a.closed || !a.Ready() {
		w.WriteHeader(http.StatusConflict)
		return
	}
	var pcm [TXBlockBytes]byte
	_ = connection.SetReadDeadline(time.Now().Add(txSilenceTimeout))
	if _, err := io.ReadFull(r.Body, pcm[:]); err != nil {
		return
	}
	if err := a.StartPlayback(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer a.StopPlayback()
	if !a.Ready() || r.Context().Err() != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	defer func() {
		// Retry once if the serial driver reports a failed release.
		if a.Key(false) != nil {
			_ = a.Key(false)
		}
	}()
	if a.Key(true) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	start := time.Now()
	var samples [TXBlockBytes / 2]float32
	for {
		now := time.Now()
		if r.Context().Err() != nil || now.Sub(start) >= txLimit {
			return
		}
		if now.Sub(start) >= time.Second && !a.Keyed() {
			w.WriteHeader(http.StatusConflict)
			return
		}
		for i := range samples {
			samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
		}
		a.Write(samples[:])
		deadline := now.Add(txSilenceTimeout)
		if limit := start.Add(txLimit); deadline.After(limit) {
			deadline = limit
		}
		_ = connection.SetReadDeadline(deadline)
		if _, err := io.ReadFull(r.Body, pcm[:]); err != nil {
			return
		}
	}
}

// Transmit streams microphone blocks. Closing ctx stops the HTTP request and
// releases the server's PTT, independently of the control connection.
func (c *Client) Transmit(ctx context.Context, blocks <-chan []byte) error {
	bodyContext, closeBody := context.WithCancel(ctx)
	defer closeBody()
	body := &txBody{ctx: bodyContext, cancel: closeBody, blocks: blocks}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+txPath, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", audioFormat)
	req.SetBasicAuth("ic-radio", c.password)
	// A transmission is a streaming upload, so a response is only expected
	// when the server rejects or ends it.
	transport := c.http.Transport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 0
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: c.http.CheckRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		if body.ended.Load() {
			return nil
		}
		return errors.New("El servidor terminó la transmisión")
	case http.StatusUnauthorized:
		return errors.New(PasswordError)
	case http.StatusConflict:
		return errors.New("La emisora no confirmó el PTT. Comprueba que está en RX y revisa RTS/DTR")
	case http.StatusServiceUnavailable, http.StatusNotFound:
		return errors.New("El servidor no tiene disponible el audio o el PTT de transmisión")
	default:
		return errors.New("El servidor terminó la transmisión")
	}
}

type txBody struct {
	ctx     context.Context
	cancel  context.CancelFunc
	blocks  <-chan []byte
	pending []byte
	ended   atomic.Bool
}

func (b *txBody) Close() error {
	b.cancel()
	return nil
}

func (b *txBody) Read(p []byte) (int, error) {
	if len(b.pending) == 0 {
		select {
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		case next, ok := <-b.blocks:
			if !ok {
				b.ended.Store(true)
				return 0, io.EOF
			}
			b.pending = next
		}
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}
