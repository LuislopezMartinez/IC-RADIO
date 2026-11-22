package civ

import "time"

const MaxTuningOffset = 9999

func isTuningChange(command byte) bool {
	return command == 0x0f || command == 0x21 || command == 0x25
}

// EffectiveTuning returns zero for any frequency that is not confirmed.
func (s State) EffectiveTuning(now time.Time) (rx, tx int64) {
	t := s.Tuning
	fresh := func(at time.Time) bool { return !at.IsZero() && now.Sub(at) < 3*time.Second }
	if !fresh(s.FrequencyAt) || s.TX {
		return 0, 0
	}
	if fresh(t.RITAt) && (!t.RIT || fresh(t.OffsetAt)) {
		rx = s.Frequency
		if t.RIT {
			rx += int64(t.Offset)
		}
	}
	if fresh(s.SplitAt) && fresh(t.DeltaTXAt) && (!t.DeltaTX || fresh(t.OffsetAt)) {
		tx = s.Frequency
		if s.Split {
			if !fresh(t.OtherVFOAt) {
				return rx, 0
			}
			tx = t.OtherVFO
		}
		if t.DeltaTX {
			tx += int64(t.Offset)
		}
	}
	return rx, tx
}

type TuningState struct {
	Offset                     int
	RIT, DeltaTX               bool
	OffsetAt, RITAt, DeltaTXAt time.Time
	OtherVFO                   int64
	OtherVFOAt                 time.Time
}

func (s *Session) SetSplit(on bool) bool {
	return s.command(0x0f, switchByte(on))
}

func (s *Session) SetIncrementalTuning(transmit, on bool) bool {
	sub := byte(1)
	if transmit {
		sub = 2
	}
	return s.command(0x21, sub, switchByte(on))
}

func (s *Session) SetTuningOffset(hz int) bool {
	if hz < -MaxTuningOffset || hz > MaxTuningOffset {
		return false
	}
	sign := switchByte(hz < 0)
	if hz < 0 {
		hz = -hz
	}
	encoded := encodeFrequency(int64(hz))
	return s.command(0x21, 0, encoded[0], encoded[1], sign)
}

func (s *Session) TuneOtherVFO(hz int64) bool {
	return CanTune(hz) && s.command(append([]byte{0x25, 1}, encodeFrequency(hz)...)...)
}

func switchByte(on bool) byte {
	if on {
		return 1
	}
	return 0
}

func (t *TuningState) receive(data []byte, now time.Time) bool {
	if len(data) < 2 {
		return false
	}
	switch {
	case data[0] == 0x25 && data[1] == 1 && len(data) == 7:
		hz, ok := decimal(data[2:])
		if !ok || !CanTune(hz) {
			return false
		}
		t.OtherVFO, t.OtherVFOAt = hz, now
	case data[0] == 0x21 && data[1] == 0 && len(data) == 5 && data[4] <= 1:
		hz, ok := decimal(data[2:4])
		if !ok {
			return false
		}
		if data[4] == 1 {
			hz = -hz
		}
		t.Offset, t.OffsetAt = int(hz), now
	case data[0] == 0x21 && data[1] == 1 && len(data) == 3 && data[2] <= 1:
		t.RIT, t.RITAt = data[2] == 1, now
	case data[0] == 0x21 && data[1] == 2 && len(data) == 3 && data[2] <= 1:
		t.DeltaTX, t.DeltaTXAt = data[2] == 1, now
	default:
		return false
	}
	return true
}
