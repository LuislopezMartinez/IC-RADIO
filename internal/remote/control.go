package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

// Radio is the control surface shared by the USB and network connections.
type Radio interface {
	Snapshot() civ.State
	TakeDisplaySnapshot() (civ.State, [civ.Samples]byte)
	EnsureRadioOn() bool
	SetRadioPower(bool) bool
	Tune(int64) bool
	SetMode(byte) bool
	SetSpan(int64) bool
	SetScopeCenter() bool
	SelectMeter(byte)
	SelectVFO(byte) bool
	EqualizeVFO() bool
	ReadBandMemory(byte, byte) bool
	RestoreBandMode(civ.BandMemory) bool
	SetPower(int) bool
	SetTuner(byte) bool
	SetRXControl(civ.RXControl, byte) bool
}

const (
	Path          = "/control/v1"
	DefaultPort   = "7300"
	PasswordError = "Contraseña incorrecta"
)

type request struct {
	Action string
	Value  int64
	Extra  byte
	Memory civ.BandMemory
}

type display struct {
	State civ.State
	Peaks [civ.Samples]byte
}

func Handler(radio Radio, authenticate func(string) bool, onActivity func(), audio http.Handler, tx *TXAudio, sweep http.Handler) http.Handler {
	mux := http.NewServeMux()
	if audio != nil {
		mux.Handle(audioPath, audio)
	}
	if tx != nil {
		mux.Handle(txPath, tx)
	}
	if sweep != nil {
		mux.Handle(sweepPath, sweep)
	}
	mux.HandleFunc(Path, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			state, peaks := radio.TakeDisplaySnapshot()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(display{state, peaks})
		case http.MethodPost:
			// A browser on another site cannot send this content type to localhost
			// without a successful CORS preflight.
			if r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			if tx != nil {
				if !tx.mu.TryLock() {
					w.WriteHeader(http.StatusConflict)
					return
				}
				defer tx.mu.Unlock()
			}
			var cmd request
			body := http.MaxBytesReader(w, r.Body, 1024)
			decoder := json.NewDecoder(body)
			if decoder.Decode(&cmd) != nil || decoder.Decode(new(any)) != io.EOF {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if !cmd.apply(radio) {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host != "127.0.0.1" && host != "localhost" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "ic-radio" || !authenticate(password) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		onActivity()
		mux.ServeHTTP(w, r)
	})
}

func (cmd request) apply(r Radio) bool {
	value := cmd.Value
	switch cmd.Action {
	case "wake":
		return r.EnsureRadioOn()
	case "shutdown":
		return r.SetRadioPower(false)
	case "tune":
		return r.Tune(value)
	case "mode":
		return value >= 0 && value <= 8 && r.SetMode(byte(value))
	case "span":
		return value >= 2500 && value <= 500000 && r.SetSpan(value)
	case "scope-center":
		return r.SetScopeCenter()
	case "meter":
		if value < 0 || value > 0x16 {
			return false
		}
		r.SelectMeter(byte(value))
		return true
	case "vfo":
		return value >= 0 && value <= 1 && r.SelectVFO(byte(value))
	case "equalize":
		return r.EqualizeVFO()
	case "read-band":
		return value >= 0 && value <= 255 && r.ReadBandMemory(byte(value), cmd.Extra)
	case "band-mode":
		return r.RestoreBandMode(cmd.Memory)
	case "power-level":
		return value >= 0 && value <= 255 && r.SetPower(int(value))
	case "tuner":
		return value >= 0 && value <= 2 && r.SetTuner(byte(value))
	case "rx-control":
		return value >= int64(civ.Preamp) && value <= int64(civ.NoiseReduction) &&
			r.SetRXControl(civ.RXControl(value), cmd.Extra)
	}
	return false
}

type Client struct {
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	endpoint string
	password string
	http     *http.Client
	commands chan request
	mu       sync.Mutex
	display  display
	meter    byte
}

var _ Radio = (*Client)(nil)
var _ Radio = (*civ.Session)(nil)

func Connect(endpoint, password string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
		endpoint: "http://" + endpoint,
		password: password,
		http: &http.Client{Timeout: time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 1, ResponseHeaderTimeout: time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		commands: make(chan request, 16),
	}
	go c.run()
	return c
}

func (c *Client) Close() {
	c.cancel()
	<-c.done
}

func (c *Client) run() {
	defer close(c.done)
	defer c.http.CloseIdleConnections()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	var sentMeter byte
	for {
		select {
		case <-c.ctx.Done():
			return
		case cmd := <-c.commands:
			if c.exchange(http.MethodPost, &cmd, nil) == http.StatusNoContent {
				c.refresh()
			} else {
				c.refresh()
				c.mu.Lock()
				if c.display.State.Error == "" {
					c.display.State.PowerError = "El servidor rechazó la orden"
				}
				c.mu.Unlock()
			}
		case <-tick.C:
			if !c.refresh() {
				continue
			}
			c.mu.Lock()
			meter := c.meter
			c.mu.Unlock()
			if meter != sentMeter && c.exchange(http.MethodPost, &request{Action: "meter", Value: int64(meter)}, nil) == http.StatusNoContent {
				sentMeter = meter
			}
		}
	}
}

func (c *Client) refresh() bool {
	var next display
	status := c.exchange(http.MethodGet, nil, &next)
	c.mu.Lock()
	defer c.mu.Unlock()
	if status != http.StatusOK {
		message := "Servidor remoto sin respuesta"
		if status == http.StatusUnauthorized {
			message = PasswordError
		}
		c.display = display{State: civ.State{Error: message}}
		return false
	}
	if next.State.Sweep.Low == c.display.State.Sweep.Low && next.State.Sweep.High == c.display.State.Sweep.High {
		for i, level := range c.display.Peaks {
			next.Peaks[i] = max(next.Peaks[i], level)
		}
	}
	c.display = next
	return true
}

func (c *Client) exchange(method string, cmd *request, result *display) int {
	var body io.Reader
	if cmd != nil {
		data, err := json.Marshal(cmd)
		if err != nil {
			return 0
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.endpoint+Path, body)
	if err != nil {
		return 0
	}
	req.SetBasicAuth("ic-radio", c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if result != nil && resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 32768)).Decode(result) != nil {
		return 0
	}
	return resp.StatusCode
}

func (c *Client) Snapshot() civ.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.display.State
}

func (c *Client) TakeDisplaySnapshot() (civ.State, [civ.Samples]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	peaks := c.display.Peaks
	c.display.Peaks = [civ.Samples]byte{}
	return c.display.State, peaks
}

func (c *Client) send(cmd request) bool {
	if c.ctx.Err() != nil {
		return false
	}
	select {
	case c.commands <- cmd:
		return true
	default:
		return false
	}
}

func (c *Client) EnsureRadioOn() bool { return c.send(request{Action: "wake"}) }
func (c *Client) SetRadioPower(on bool) bool {
	if on {
		return c.EnsureRadioOn()
	}
	return c.send(request{Action: "shutdown"})
}
func (c *Client) Tune(hz int64) bool      { return c.send(request{Action: "tune", Value: hz}) }
func (c *Client) SetMode(mode byte) bool  { return c.send(request{Action: "mode", Value: int64(mode)}) }
func (c *Client) SetSpan(span int64) bool { return c.send(request{Action: "span", Value: span}) }
func (c *Client) SetScopeCenter() bool    { return c.send(request{Action: "scope-center"}) }
func (c *Client) SelectMeter(meter byte) {
	c.mu.Lock()
	c.meter = meter
	c.mu.Unlock()
}
func (c *Client) SelectVFO(vfo byte) bool { return c.send(request{Action: "vfo", Value: int64(vfo)}) }
func (c *Client) EqualizeVFO() bool       { return c.send(request{Action: "equalize"}) }
func (c *Client) ReadBandMemory(band, register byte) bool {
	return c.send(request{Action: "read-band", Value: int64(band), Extra: register})
}
func (c *Client) RestoreBandMode(memory civ.BandMemory) bool {
	return c.send(request{Action: "band-mode", Memory: memory})
}
func (c *Client) SetPower(level int) bool {
	return c.send(request{Action: "power-level", Value: int64(level)})
}
func (c *Client) SetTuner(tuner byte) bool {
	return c.send(request{Action: "tuner", Value: int64(tuner)})
}
func (c *Client) SetRXControl(control civ.RXControl, value byte) bool {
	return c.send(request{Action: "rx-control", Value: int64(control), Extra: value})
}
