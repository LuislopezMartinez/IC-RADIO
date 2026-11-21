package civ

import (
	"bytes"
	"testing"
	"time"
)

func TestDecoderAndRadioState(t *testing.T) {
	s := &Session{address: 0x94}
	var d decoder
	var a assembler
	now := time.Now()
	receive := func(frame []byte) { s.receive(frame, &a, now) }

	// The first frame is the local echo of a tuning request. Split the
	// subsequent response across reads, including its two-byte preamble.
	d.feed([]byte{0xfe, 0xfe, 0x94, 0xe0, 0x05, 0xfd, 0xfe}, receive)
	d.feed([]byte{0xfe, 0xe0, 0x94, 0x03, 0x00, 0x50}, receive)
	d.feed([]byte{0x10, 0x07, 0x00, 0xfd, 0xfe, 0xfe, 0, 0x94, 1, 0, 2, 0xfd}, receive)
	d.feed([]byte{0xfe, 0xfe, 0xe0, 0x94, 0x15, 2, 0x01, 0x20, 0xfd}, receive)
	d.feed([]byte{0xfe, 0xfe, 0xe0, 0x94, 0x1c, 0, 1, 0xfd}, receive)

	state := s.Snapshot()
	if state.Frequency != 7105000 || !state.ModeKnown || state.Mode != 0 || state.Filter != 2 || state.Meters[2] != 120 || !state.TXKnown || !state.TX {
		t.Fatalf("unexpected state: freq=%d mode=%d filter=%d meter=%d tx=%v", state.Frequency, state.Mode, state.Filter, state.Meters[2], state.TX)
	}
}

func TestIC7300ScopeReassembly(t *testing.T) {
	var a assembler
	now := time.Now()
	// Centered on 7.105 MHz with a half-span of 25 kHz.
	header := []byte{0, 1, 0x11, 0, 0x00, 0x50, 0x10, 0x07, 0, 0, 0x50, 0x02, 0, 0, 0}
	piece := func(part byte, length int) []byte {
		return append([]byte{0, part, 0x11}, bytes.Repeat([]byte{part}, length)...)
	}
	a.add(header, now)
	a.add(piece(3, 50), now)
	if _, ok := a.add(piece(2, 50), now); ok || a.next != 0 {
		t.Fatal("accepted an out-of-order sweep")
	}

	a.add(header, now)
	for _, part := range []byte{2, 3, 4, 5, 6, 7, 8, 9, 0x10} {
		if _, ok := a.add(piece(part, 50), now); ok {
			t.Fatal("published a partial sweep")
		}
	}
	sweep, ok := a.add(piece(0x11, 25), now)
	if !ok || sweep.Low != 7080000 || sweep.High != 7130000 || sweep.Levels[0] != 2 || sweep.Levels[474] != 0x11 {
		t.Fatalf("invalid assembled sweep: valid=%v bounds=%d-%d", ok, sweep.Low, sweep.High)
	}

	header[14] = 1
	sweep, ok = a.add(header, now)
	if !ok || sweep.Low != 0 || sweep.High != 0 {
		t.Fatal("out-of-range header did not clear the live sweep")
	}
}

func TestTuneEncodesFrequency(t *testing.T) {
	s := &Session{requests: make(chan []byte, 1)}
	if !s.Tune(7105000) || !bytes.Equal(<-s.requests, []byte{5, 0, 0x50, 0x10, 7, 0}) {
		t.Fatal("incorrect frequency command")
	}
	if s.Tune(-1) || s.Tune(100000000) {
		t.Fatal("accepted an out-of-range frequency")
	}
}

func TestFrequencyReplyFreshness(t *testing.T) {
	s := &Session{address: 0x94}
	var scope assembler
	now := time.Now()
	s.receive([]byte{0xe0, 0x94, 0x03, 0x00, 0x50, 0x10, 0x07}, &scope, now)
	s.receive([]byte{0xe0, 0x94, 0x03, 0xff, 0, 0, 0, 0}, &scope, now.Add(time.Second))
	got := s.Snapshot()
	if got.Frequency != 7105000 || got.FrequencyAt != now {
		t.Fatalf("invalid VFO state: %d at %v", got.Frequency, got.FrequencyAt)
	}
}

func TestScopeSpanCommand(t *testing.T) {
	s := &Session{requests: make(chan []byte, 2)}
	if !s.SetScopeCenter() || !bytes.Equal(<-s.requests, []byte{0x27, 0x14, 0, 0}) {
		t.Fatal("wrong CENTER command")
	}
	if !s.SetSpan(25000) || !bytes.Equal(<-s.requests, []byte{0x27, 0x15, 0, 0, 0x50, 0x02, 0, 0}) {
		t.Fatal("wrong span command")
	}
}

func TestRXControlsAndVFOCommands(t *testing.T) {
	s := &Session{address: 0x94, requests: make(chan []byte, 3)}
	if !s.SetRXControl(NoiseBlanker, 1) || !bytes.Equal(<-s.requests, []byte{0x16, 0x22, 1}) {
		t.Fatal("invalid NB command")
	}
	if !s.SelectVFO(1) || !bytes.Equal(<-s.requests, []byte{0x07, 0x01}) {
		t.Fatal("invalid VFO B command")
	}
	if !s.EqualizeVFO() || !bytes.Equal(<-s.requests, []byte{0x07, 0xa0}) {
		t.Fatal("invalid VFO equalization command")
	}
	now := time.Now()
	var scope assembler
	s.receive([]byte{0xe0, 0x94, 0x16, 0x22, 1}, &scope, now)
	s.receive([]byte{0xe0, 0x94, 0x11, 0x20}, &scope, now)
	got := s.Snapshot()
	if got.RX[NoiseBlanker] != (RXValue{1, now}) || got.RX[Attenuator] != (RXValue{0x20, now}) {
		t.Fatal("wrong state after RX replies")
	}
}

func TestCommandPacingAndReadback(t *testing.T) {
	s := &Session{address: 0x94}
	request := []byte{0x16, 0x22}
	if !s.isReply([]byte{0xe0, 0x94, 0x16, 0x22, 1}, request) {
		t.Fatal("NB reply did not advance the scheduler")
	}
	for _, frame := range [][]byte{
		{0x94, 0xe0, 0x16, 0x22},
		{0xe0, 0x94, 0x27, 0, 0},
		{0, 0x94, 0, 0x50, 0x10, 0x07, 0, 0},
	} {
		if s.isReply(frame, request) {
			t.Fatalf("unrelated data advanced the scheduler: %x", frame)
		}
	}
	if readyForCommand(40*time.Millisecond, true) || !readyForCommand(45*time.Millisecond, true) ||
		readyForCommand(45*time.Millisecond, false) || !readyForCommand(90*time.Millisecond, false) {
		t.Fatal("incorrect serial pacing")
	}
	if !bytes.Equal(commandReadback([]byte{0x16, 0x22, 1}), request) {
		t.Fatal("RX changes must request confirmation")
	}
}

func TestBandRegisterReadAndRestore(t *testing.T) {
	s := &Session{address: 0x94, requests: make(chan []byte, 2)}
	if !s.ReadBandMemory(0x10, 3) || !bytes.Equal(<-s.requests, []byte{0x1a, 1, 0x10, 3}) {
		t.Fatal("wrong band register read command")
	}
	now := time.Now()
	var scope assembler
	s.receive([]byte{0xe0, 0x94, 0x1a, 1, 0x10, 3, 0, 0, 0x10, 0x50, 0, 1, 2, 0x10}, &scope, now)
	got := s.Snapshot().Band
	if got != (BandMemory{Band: 0x10, Register: 3, Frequency: 50100000, Mode: 1, Filter: 2, Data: 1, At: now}) {
		t.Fatalf("wrong band register: %+v", got)
	}
	if !s.RestoreBandMode(got) || !bytes.Equal(<-s.requests, []byte{0x26, 0, 1, 1, 2}) {
		t.Fatal("wrong selected VFO mode command")
	}
}

func TestConfirmedVFOFeedback(t *testing.T) {
	s := &Session{address: 0x94}
	now := time.Now()
	command := []byte{0x07, 1}
	if s.Snapshot().VFOKnown || !s.isReply([]byte{0xe0, 0x94, 0xfb}, command) {
		t.Fatal("VFO must start unknown and accept a matching acknowledgement")
	}
	s.confirmVFOCommand(command, now)
	s.confirmVFOCommand([]byte{0x07, 0xa0}, now)
	got := s.Snapshot()
	if !got.VFOKnown || got.VFO != 1 || got.VFOEqualizedAt != now {
		t.Fatalf("unexpected VFO feedback: known=%v VFO=%d equalized=%v", got.VFOKnown, got.VFO, got.VFOEqualizedAt)
	}
}

func TestSWRPowerControl(t *testing.T) {
	s := &Session{address: 0x94, requests: make(chan []byte, 1)}
	if !s.SetPower(77) || !bytes.Equal(<-s.requests, []byte{0x14, 0x0a, 0, 0x77}) {
		t.Fatal("wrong power command")
	}
	var scope assembler
	now := time.Now()
	s.receive([]byte{0xe0, 0x94, 0x14, 0x0a, 0, 0x77}, &scope, now)
	s.receive([]byte{0xe0, 0x94, 0x0f, 1}, &scope, now)
	if got := s.Snapshot(); got.PowerSetting != 77 || got.PowerAt != now || !got.Split {
		t.Fatal("wrong power or split feedback")
	}
}

func TestTunerStateAndCommand(t *testing.T) {
	s := &Session{address: 0x94, requests: make(chan []byte, 1)}
	if !s.SetTuner(2) || !bytes.Equal(<-s.requests, []byte{0x1c, 1, 2}) {
		t.Fatal("wrong tuner start command")
	}
	if !bytes.Equal(commandReadback([]byte{0x1c, 1, 2}), []byte{0x1c, 1}) {
		t.Fatal("tuner command must confirm state")
	}
	var scope assembler
	now := time.Now()
	s.receive([]byte{0xe0, 0x94, 0x1c, 0, 0}, &scope, now)
	s.receive([]byte{0xe0, 0x94, 0x1c, 1, 2}, &scope, now)
	got := s.Snapshot()
	if got.Tuner != 2 || got.TunerAt != now || got.TX || !got.TXKnown {
		t.Fatal("tuner status corrupted the transmit state")
	}
}
