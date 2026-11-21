package civ

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const Samples = 475

type Port interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	SetReadTimeout(time.Duration) error
}

type Sweep struct {
	Low, High int64
	Center    int64
	Mode      byte
	Levels    [Samples]byte
	At        time.Time
}

type State struct {
	VFO                    byte
	VFOKnown               bool
	VFOEqualizedAt         time.Time
	Band                   BandMemory
	RX                     [5]RXValue
	Frequency              int64
	FrequencyAt            time.Time
	Mode, Filter           byte
	ModeKnown, TX, TXKnown bool
	Meters                 [23]int
	MeterAt                [23]time.Time
	Sweep                  Sweep
	LastReply              time.Time
	Error                  string
}

type BandMemory struct {
	Band, Register, Mode, Filter, Data byte
	Frequency                          int64
	At                                 time.Time
}

func (s *Session) ReadBandMemory(band, register byte) bool {
	return s.command(0x1a, 1, band, register)
}

func (s *Session) RestoreBandMode(memory BandMemory) bool {
	return s.command(0x26, 0, memory.Mode, memory.Data, memory.Filter)
}

type RXControl int

const (
	Preamp RXControl = iota
	Attenuator
	AutoNotch
	NoiseBlanker
	NoiseReduction
)

type RXValue struct {
	Value byte
	At    time.Time
}

var rxCommands = [...][]byte{{0x16, 2}, {0x11}, {0x16, 0x41}, {0x16, 0x22}, {0x16, 0x40}}

type Session struct {
	address    byte
	stop, done chan struct{}
	requests   chan []byte
	meter      atomic.Uint32
	mu         sync.RWMutex
	state      State
	peaks      [Samples]byte
}

func Start(port Port, address byte) (*Session, error) {
	if err := port.SetReadTimeout(10 * time.Millisecond); err != nil {
		return nil, err
	}
	s := &Session{address: address, stop: make(chan struct{}), done: make(chan struct{}), requests: make(chan []byte, 8)}
	go s.run(port)
	return s, nil
}

func (s *Session) Close() {
	close(s.stop)
	<-s.done
}

func (s *Session) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// TakeDisplaySnapshot consumes the per-bin maximum accumulated since the last
// render. Polling and input handling use Snapshot and leave the peaks intact.
func (s *Session) TakeDisplaySnapshot() (State, [Samples]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	peaks := s.peaks
	s.peaks = [Samples]byte{}
	return s.state, peaks
}

func (s *Session) SelectMeter(code byte) { s.meter.Store(uint32(code)) }

func (s *Session) command(data ...byte) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.requests <- data:
		return true
	default:
		return false
	}
}

func (s *Session) Tune(hz int64) bool {
	if hz < 30000 || hz > 74800000 {
		return false
	}
	return s.command(append([]byte{0x05}, encodeFrequency(hz)...)...)
}

func encodeFrequency(hz int64) []byte {
	data := make([]byte, 5)
	for i := range data {
		data[i] = byte(hz%10) | byte(hz/10%10)<<4
		hz /= 100
	}
	return data
}

func (s *Session) SetSpan(halfWidth int64) bool {
	return s.command(append([]byte{0x27, 0x15, 0}, encodeFrequency(halfWidth)...)...)
}

func (s *Session) SetScopeCenter() bool {
	return s.command(0x27, 0x14, 0, 0)
}

func (s *Session) SetRXControl(control RXControl, value byte) bool {
	return s.command(append(append([]byte{}, rxCommands[control]...), value)...)
}

func (s *Session) SelectVFO(vfo byte) bool {
	return s.command(0x07, vfo)
}

func (s *Session) EqualizeVFO() bool { return s.command(0x07, 0xa0) }

func (s *Session) SetMode(mode byte) bool { return s.command(0x06, mode) }

func (s *Session) write(port Port, data ...byte) bool {
	frame := append([]byte{0xfe, 0xfe, s.address, 0xe0}, data...)
	frame = append(frame, 0xfd)
	n, err := port.Write(frame)
	if err == nil && n == len(frame) {
		return true
	}
	s.fail(fmt.Sprintf("CI-V: escritura incompleta (%d/%d): %v", n, len(frame), err))
	return false
}

func (s *Session) fail(message string) {
	s.mu.Lock()
	s.state.Error = message
	s.mu.Unlock()
}

func (s *Session) run(port Port) {
	defer close(s.done)
	var parser decoder
	var scope assembler
	var sentAt, enableAt time.Time
	var outstanding, readback []byte
	var answered bool
	var enableOutput bool
	var poll, rxPoll int
	var buf [1024]byte
	for {
		select {
		case <-s.stop:
			if !enableAt.IsZero() {
				s.write(port, 0x27, 0x11, 0)
			}
			return
		default:
		}
		now := time.Now()
		if readyForCommand(now.Sub(sentAt), answered) {
			state := s.Snapshot()
			live := now.Sub(state.LastReply) < 2*time.Second
			var cmd []byte
			select {
			case request := <-s.requests:
				if live && !state.TX && (state.TXKnown || request[0] == 0x27) {
					cmd = request
					readback = commandReadback(request)
				}
			default:
			}
			if cmd == nil {
				switch {
				case readback != nil:
					cmd, readback = readback, nil
				case enableOutput:
					cmd = []byte{0x27, 0x11, 1}
					enableOutput = false
				case live && now.Sub(state.Sweep.At) > 2*time.Second && now.Sub(enableAt) > 3*time.Second:
					cmd = []byte{0x27, 0x10, 1}
					enableOutput, enableAt = true, now
				default:
					if pollPlan[poll] == pollRX && live && !state.TX {
						cmd = rxCommands[rxPoll]
						rxPoll = (rxPoll + 1) % len(rxCommands)
					} else {
						cmd = s.pollCommand(pollPlan[poll], state.TX)
					}
					poll = (poll + 1) % len(pollPlan)
				}
			}
			if !s.write(port, cmd...) {
				return
			}
			sentAt, outstanding, answered = now, cmd, false
		}
		n, err := port.Read(buf[:])
		if err != nil {
			s.fail("CI-V: " + err.Error())
			return
		}
		parser.feed(buf[:n], func(frame []byte) {
			now := time.Now()
			if !answered && s.isReply(frame, outstanding) {
				answered = true
				if len(frame) == 3 && frame[2] == 0xfb {
					s.confirmVFOCommand(outstanding, now)
				}
			}
			s.receive(frame, &scope, now)
		})
	}
}

type pollKind int

const (
	pollFrequency pollKind = iota
	pollMode
	pollTX
	pollMeter
	pollRX
)

// Interleave receiver controls with frequency, TX status and meter reads.
var pollPlan = [...]pollKind{
	pollFrequency, pollRX, pollMode, pollMeter,
	pollFrequency, pollRX, pollTX, pollRX,
	pollFrequency, pollMeter, pollRX, pollMode,
	pollFrequency, pollRX, pollTX, pollMeter,
}

func readyForCommand(elapsed time.Duration, answered bool) bool {
	return elapsed >= 90*time.Millisecond || answered && elapsed >= 42*time.Millisecond
}

func commandReadback(command []byte) []byte {
	switch command[0] {
	case 5, 7:
		return []byte{3}
	case 6, 0x26:
		return []byte{4}
	case 0x11:
		return command[:1]
	case 0x16:
		return command[:2]
	}
	return nil
}

func (s *Session) confirmVFOCommand(command []byte, now time.Time) {
	if len(command) != 2 || command[0] != 0x07 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch command[1] {
	case 0, 1:
		s.state.VFO, s.state.VFOKnown = command[1], true
	case 0xa0:
		s.state.VFOEqualizedAt = now
	}
}

func (s *Session) isReply(frame, command []byte) bool {
	if len(frame) < 3 || len(command) == 0 || frame[0] != 0xe0 || frame[1] != s.address {
		return false
	}
	data := frame[2:]
	if len(data) == 1 && (data[0] == 0xfb || data[0] == 0xfa) {
		switch command[0] {
		case 5, 6, 7, 0x26:
			return true
		case 0x11:
			return len(command) == 2
		case 0x16, 0x27:
			return len(command) > 2
		}
		return false
	}
	return len(data) > len(command) && bytes.HasPrefix(data, command)
}

func (s *Session) pollCommand(kind pollKind, transmitting bool) []byte {
	switch kind {
	case pollFrequency:
		return []byte{0x03}
	case pollMode:
		return []byte{0x04}
	case pollTX:
		return []byte{0x1c, 0}
	default:
		code := byte(s.meter.Load())
		if code == 0 {
			code = 2
			if transmitting {
				code = 0x11
			}
		}
		return []byte{0x15, code}
	}
}

func (s *Session) receive(frame []byte, scope *assembler, now time.Time) {
	if len(frame) < 3 || frame[1] != s.address || (frame[0] != 0xe0 && frame[0] != 0) {
		return
	}
	data := frame[2:]
	s.mu.Lock()
	defer s.mu.Unlock()
	state := &s.state
	switch data[0] {
	case 0, 3, 5:
		if len(data) != 6 && len(data) != 5 {
			return
		}
		hz, ok := decimal(data[1:])
		if !ok || hz < 30000 || hz > 74800000 {
			return
		}
		state.Frequency, state.FrequencyAt = hz, now
	case 1, 4, 6:
		if len(data) < 2 {
			return
		}
		state.Mode, state.ModeKnown = data[1], true
		if len(data) >= 3 {
			state.Filter = data[2]
		}
	case 0x1a:
		if len(data) < 12 || data[1] != 1 || data[3] < 1 || data[3] > 3 {
			return
		}
		hz, ok := decimal(data[4:9])
		if !ok || hz < 30000 || hz > 74800000 || data[9] > 8 || data[10] < 1 || data[10] > 3 {
			return
		}
		state.Band = BandMemory{
			Band: data[2], Register: data[3], Frequency: hz,
			Mode: data[9], Filter: data[10], Data: (data[11] >> 4) & 1, At: now,
		}
	case 0x1c:
		if len(data) != 3 || data[1] != 0 || data[2] > 1 {
			return
		}
		state.TX, state.TXKnown = data[2] == 1, true
	case 0x11, 0x16:
		for i, command := range rxCommands {
			if len(data) == len(command)+1 && bytes.HasPrefix(data, command) {
				state.RX[i] = RXValue{Value: data[len(command)], At: now}
				break
			}
		}
	case 0x15:
		if len(data) != 4 || int(data[1]) >= len(state.Meters) {
			return
		}
		level, ok := decimal([]byte{data[3], data[2]})
		if !ok || level > 255 {
			return
		}
		state.Meters[data[1]], state.MeterAt[data[1]] = int(level), now
	case 0x27:
		if len(data) < 2 || data[1] != 0 {
			return
		}
		if sweep, ok := scope.add(data[2:], now); ok {
			if sweep.Low != state.Sweep.Low || sweep.High != state.Sweep.High {
				s.peaks = [Samples]byte{}
			}
			for i, level := range sweep.Levels {
				s.peaks[i] = max(s.peaks[i], level)
			}
			state.Sweep = sweep
		}
	case 0xfa:
		return
	default:
		return
	}
	state.LastReply = now
}

type decoder struct{ pending []byte }

func (d *decoder) feed(chunk []byte, accept func([]byte)) {
	d.pending = append(d.pending, chunk...)
	for {
		start := bytes.Index(d.pending, []byte{0xfe, 0xfe})
		if start < 0 {
			if len(d.pending) != 0 && d.pending[len(d.pending)-1] == 0xfe {
				d.pending = d.pending[len(d.pending)-1:]
			} else {
				d.pending = d.pending[:0]
			}
			return
		}
		d.pending = d.pending[start+2:]
		for len(d.pending) > 0 && d.pending[0] == 0xfe {
			d.pending = d.pending[1:]
		}
		end := bytes.IndexByte(d.pending, 0xfd)
		next := bytes.Index(d.pending, []byte{0xfe, 0xfe})
		if next >= 0 && (end < 0 || next < end) {
			d.pending = d.pending[next:]
			continue
		}
		if end < 0 {
			if len(d.pending) > 256 {
				d.pending = d.pending[:0]
			} else {
				d.pending = append([]byte{0xfe, 0xfe}, d.pending...)
			}
			return
		}
		accept(d.pending[:end])
		d.pending = d.pending[end+1:]
	}
}

func decimal(data []byte) (int64, bool) {
	var value int64
	for i := len(data) - 1; i >= 0; i-- {
		hi, lo := data[i]>>4, data[i]&15
		if hi > 9 || lo > 9 {
			return 0, false
		}
		value = value*100 + int64(hi)*10 + int64(lo)
	}
	return value, true
}

type assembler struct {
	sweep   Sweep
	next    byte
	count   int
	started time.Time
}

func (a *assembler) add(data []byte, now time.Time) (Sweep, bool) {
	if len(data) < 3 || data[0] != 0 || data[2] != 0x11 {
		a.next = 0
		return Sweep{}, false
	}
	part := data[1]
	if part == 1 {
		a.next, a.count = 0, 0
		if len(data) != 15 || data[3] > 3 {
			return Sweep{}, false
		}
		left := [5]byte(data[4:9])
		negative := left[4]>>4 == 15
		if negative {
			left[4] &= 15
		}
		low, okLow := decimal(left[:])
		high, okHigh := decimal(data[9:14])
		if !okLow || !okHigh {
			return Sweep{}, false
		}
		if negative {
			low = -low
		}
		var center int64
		if data[3] == 0 {
			center = low
			low, high = center-high, center+high
		}
		if low >= high {
			return Sweep{}, false
		}
		if data[14] == 1 {
			return Sweep{At: now}, true
		}
		if data[14] != 0 {
			return Sweep{}, false
		}
		a.sweep, a.next, a.started = Sweep{Low: low, High: high, Mode: data[3], Center: center}, 2, now
		return Sweep{}, false
	}
	if a.next == 0 || part != a.next || now.Sub(a.started) > time.Second || len(data) == 3 || a.count+len(data)-3 > Samples {
		a.next = 0
		return Sweep{}, false
	}
	for _, level := range data[3:] {
		if level > 160 {
			a.next = 0
			return Sweep{}, false
		}
	}
	a.count += copy(a.sweep.Levels[a.count:], data[3:])
	if part == 0x11 {
		a.next = 0
		a.sweep.At = now
		return a.sweep, a.count == Samples
	}
	a.next++
	if a.next == 0x0a {
		a.next = 0x10
	}
	return Sweep{}, false
}
