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
	PowerPending           bool
	PoweredOff             bool
	PowerError             string
	TXSince                time.Time
	Tuner                  byte
	TunerAt                time.Time
	PowerSetting           int
	PowerAt, SplitAt, TXAt time.Time
	Split                  bool
	VFO                    byte
	VFOKnown               bool
	VFOAt                  time.Time
	VFOEqualizedAt         time.Time
	Band                   BandMemory
	RX                     [rxControlCount]RXValue
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
	RFGain
	AFGain
	Squelch
	AGC
	rxControlCount
)

type RXValue struct {
	Value byte
	At    time.Time
}

var rxCommands = [rxControlCount][]byte{
	{0x16, 2}, {0x11}, {0x16, 0x41}, {0x16, 0x22}, {0x16, 0x40},
	{0x14, 2}, {0x14, 1}, {0x14, 3}, {0x16, 0x12},
}

type powerRequest byte

const (
	powerOff powerRequest = iota
	powerOn
	powerProbe
)

type Session struct {
	address    byte
	stop, done chan struct{}
	requests   chan []byte
	power      chan powerRequest
	meter      atomic.Uint32
	swrMeter   atomic.Bool
	mu         sync.RWMutex
	state      State
	peaks      [Samples]byte
}

func Start(port Port, address byte) (*Session, error) {
	if err := port.SetReadTimeout(10 * time.Millisecond); err != nil {
		return nil, err
	}
	s := &Session{address: address, stop: make(chan struct{}), done: make(chan struct{}), requests: make(chan []byte, 8), power: make(chan powerRequest, 1)}
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

func (s *Session) SetRadioPower(on bool) bool {
	if on {
		return s.requestPower(powerOn)
	}
	return s.requestPower(powerOff)
}

// EnsureRadioOn probes the radio before attempting to wake it.
func (s *Session) EnsureRadioOn() bool {
	return s.requestPower(powerProbe)
}

func (s *Session) requestPower(request powerRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := &s.state
	if state.PowerPending || state.Error != "" || s.swrMeter.Load() {
		return false
	}
	live := time.Since(state.LastReply) < 2*time.Second
	if request == powerOn && live || request == powerOff && (!live || !state.TXKnown || state.TX) {
		return false
	}
	select {
	case <-s.done:
		return false
	case s.power <- request:
		state.PowerPending, state.PowerError = true, ""
		return true
	}
}

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

func CanTune(hz int64) bool {
	return hz >= 30000 && hz <= 74800000
}

func (s *Session) Tune(hz int64) bool {
	if !CanTune(hz) {
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
	if control < Preamp || control >= rxControlCount || control == AGC && (value < 1 || value > 3) {
		return false
	}
	command := append([]byte(nil), rxCommands[control]...)
	if command[0] == 0x14 {
		return s.command(append(command, value/100, (value/10%10)<<4|value%10)...)
	}
	return s.command(append(command, value)...)
}

func (s *Session) SelectVFO(vfo byte) bool {
	return s.command(0x07, vfo)
}

func (s *Session) EqualizeVFO() bool { return s.command(0x07, 0xa0) }

func (s *Session) ReadPower() bool { return s.command(0x14, 0x0a) }
func (s *Session) SetTuner(value byte) bool {
	return value <= 2 && s.command(0x1c, 1, value)
}

func (s *Session) ReadSplit() bool { return s.command(0x0f) }

func (s *Session) SetPower(level int) bool {
	if level < 0 || level > 255 {
		return false
	}
	return s.command(0x14, 0x0a, byte(level/100), byte((level/10%10)<<4|level%10))
}

func (s *Session) MeasureSWR(active bool) { s.swrMeter.Store(active) }

func (s *Session) SetMode(mode byte) bool { return s.command(0x06, mode) }

func (s *Session) write(port Port, data ...byte) bool {
	frame := append([]byte{0xfe, 0xfe, s.address, 0xe0}, data...)
	frame = append(frame, 0xfd)
	if bytes.Equal(data, []byte{0x18, 1}) {
		// The USB serial port runs at 115200 baud.
		frame = append(bytes.Repeat([]byte{0xfe}, 150), frame...)
	}
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
	s.state.PowerPending, s.state.PoweredOff = false, false
	s.mu.Unlock()
}

func (s *Session) finishPower(off bool, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if off {
		s.state = State{PoweredOff: true}
		s.peaks = [Samples]byte{}
		return
	}
	s.state.PowerPending, s.state.PowerError = false, message
}

func (s *Session) run(port Port) {
	defer close(s.done)
	var parser decoder
	var scope assembler
	var sentAt, enableAt time.Time
	var outstanding, readback []byte
	var answered bool
	var enableOutput bool
	var poll, rxPoll, swrPoll, txPoll int
	var powerSent, offAck, probeUntil time.Time
	var poweringOn bool
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
		if !powerSent.IsZero() {
			state := s.Snapshot()
			switch {
			case poweringOn && state.FrequencyAt.After(powerSent):
				s.finishPower(false, "")
				powerSent = time.Time{}
			case !offAck.IsZero() && now.Sub(offAck) >= time.Second && now.Sub(state.LastReply) >= time.Second:
				s.finishPower(true, "")
				scope = assembler{}
				enableAt, enableOutput, readback = time.Time{}, false, nil
				powerSent, offAck = time.Time{}, time.Time{}
			case now.Sub(powerSent) >= 5*time.Second:
				s.finishPower(false, "La emisora no confirmó la orden")
				powerSent, offAck = time.Time{}, time.Time{}
			}
		}
		waitingForPowerOff := !powerSent.IsZero() && !poweringOn
		if !waitingForPowerOff && readyForCommand(now.Sub(sentAt), answered) {
			state := s.Snapshot()
			live := now.Sub(state.LastReply) < 2*time.Second
			var cmd []byte
			select {
			case request := <-s.power:
				if request == powerProbe {
					probeUntil = now.Add(time.Second)
					break
				}
				on := request == powerOn
				if on && live || !on && (!live || state.TX || !state.TXKnown) || s.swrMeter.Load() {
					s.finishPower(false, "La orden ya no se puede ejecutar")
					break
				}
				cmd = []byte{0x18, 0}
				if on {
					cmd[1] = 1
				}
				poweringOn, powerSent = on, now
				readback = nil
			case request := <-s.requests:
				if live && !state.PowerPending && !state.TX && (state.TXKnown || request[0] == 0x27) {
					cmd = request
					readback = commandReadback(request)
				}
			default:
			}
			if !probeUntil.IsZero() {
				switch {
				case live:
					s.finishPower(false, "")
					probeUntil = time.Time{}
				case now.Before(probeUntil):
					cmd = []byte{0x03}
				default:
					cmd = []byte{0x18, 1}
					poweringOn, powerSent = true, now
					probeUntil, readback = time.Time{}, nil
				}
			}
			if cmd == nil {
				switch {
				case !powerSent.IsZero() || state.PoweredOff:
					cmd = []byte{0x03}
				case readback != nil:
					cmd, readback = readback, nil
				case s.swrMeter.Load():
					cmd = swrMeasurementPoll[swrPoll]
					swrPoll = (swrPoll + 1) % len(swrMeasurementPoll)
				case state.TXKnown && state.TX:
					cmd = txTelemetryPoll[txPoll]
					txPoll = (txPoll + 1) % len(txTelemetryPoll)
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
				if len(frame) == 3 && len(outstanding) == 2 && outstanding[0] == 0x18 {
					switch {
					case frame[2] == 0xfa:
						s.finishPower(false, "La emisora rechazó la orden")
						powerSent = time.Time{}
					case !poweringOn:
						offAck = now
					}
					return
				}
				if len(frame) == 3 && frame[2] == 0xfb {
					s.confirmVFOCommand(outstanding, now)
				}
			}
			s.receive(frame, &scope, now)
		})
	}
}

var txTelemetryPoll = [...][]byte{
	{0x15, 0x11}, {0x15, 0x12}, {0x15, 0x13}, {0x1c, 0},
	{0x15, 0x15}, {0x15, 0x16}, {0x15, 0x14}, {0x1c, 0},
	{0x15, 0x11}, {0x1c, 1},
}

var swrMeasurementPoll = [...][]byte{{0x15, 0x11}, {0x15, 0x12}, {0x1c, 0}, {0x15, 0x11}, {0x15, 0x12}, {0x03}, {0x0f}, {0x04}}

type pollKind int

const (
	pollFrequency pollKind = iota
	pollMode
	pollTX
	pollMeter
	pollRX
	pollPower
	pollTuner
)

// Interleave receiver controls with frequency, TX status and meter reads.
var pollPlan = [...]pollKind{
	pollFrequency, pollRX, pollMode, pollMeter,
	pollFrequency, pollRX, pollTX, pollRX,
	pollFrequency, pollMeter, pollRX, pollMode,
	pollFrequency, pollRX, pollTX, pollMeter,
	pollFrequency, pollPower, pollTuner, pollMeter,
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
	case 0x1c:
		if len(command) == 3 && command[1] == 1 {
			return command[:2]
		}
	case 0x14:
		if len(command) == 4 {
			return command[:2]
		}
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
		s.state.VFO, s.state.VFOKnown, s.state.VFOAt = command[1], true, now
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
		case 5, 6, 7, 0x18, 0x26:
			return true
		case 0x11:
			return len(command) == 2
		case 0x14, 0x16, 0x1c, 0x27:
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
	case pollPower:
		return []byte{0x14, 0x0a}
	case pollTuner:
		return []byte{0x1c, 1}
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
		state.PoweredOff = false
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
		if len(data) != 3 {
			return
		}
		switch {
		case data[1] == 0 && data[2] <= 1:
			transmitting := data[2] == 1
			if transmitting && !state.TX {
				state.TXSince = now
			}
			state.TX, state.TXKnown, state.TXAt = transmitting, true, now
		case data[1] == 1 && data[2] <= 2:
			state.Tuner, state.TunerAt = data[2], now
		default:
			return
		}
	case 0x0f:
		if len(data) != 2 {
			return
		}
		state.Split, state.SplitAt = data[1] != 0, now
	case 0x14:
		if len(data) != 4 {
			return
		}
		level, ok := decimal([]byte{data[3], data[2]})
		if !ok || level > 255 {
			return
		}
		switch data[1] {
		case 1:
			state.RX[AFGain] = RXValue{byte(level), now}
		case 2:
			state.RX[RFGain] = RXValue{byte(level), now}
		case 3:
			state.RX[Squelch] = RXValue{byte(level), now}
		case 0x0a:
			state.PowerSetting, state.PowerAt = int(level), now
		default:
			return
		}
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
