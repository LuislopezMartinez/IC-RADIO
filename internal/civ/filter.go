package civ

import "time"

type FilterState struct {
	Mode, Data, Slot byte
	ModeAt           time.Time
	WidthHz          int
	WidthAt          time.Time
	PBT              [2]int
	PBTAt            [2]time.Time
}

func (f FilterState) SSB() bool { return !f.ModeAt.IsZero() && f.Mode <= 1 }

func SSBFilterWidth(hz int) int {
	hz = max(50, min(3600, hz))
	step := 50
	if hz > 500 {
		step = 100
	}
	return (hz + step/2) / step * step
}

func (s *Session) SetFilterSlot(slot byte) bool {
	f := s.Snapshot().IF
	return slot >= 1 && slot <= 3 && freshFilter(f.ModeAt) &&
		s.command(0x26, 0, f.Mode, f.Data, slot)
}

func (s *Session) SetFilterWidth(hz int) bool {
	f := s.Snapshot().IF
	if !f.SSB() || !freshFilter(f.ModeAt) || !freshFilter(f.WidthAt) || hz != SSBFilterWidth(hz) {
		return false
	}
	code := hz/50 - 1
	if hz > 500 {
		code = hz/100 + 4
	}
	return s.command(0x1a, 3, byte(code/10<<4|code%10))
}

func freshFilter(at time.Time) bool {
	return !at.IsZero() && time.Since(at) < 3*time.Second
}

func (f *FilterState) syncMode(mode, data, slot byte, now time.Time) {
	if f.Mode != mode || f.Data != data || f.Slot != slot {
		f.WidthAt = time.Time{}
		f.PBTAt = [2]time.Time{}
	}
	f.Mode, f.Data, f.Slot, f.ModeAt = mode, data, slot, now
}

func (f *FilterState) receiveWidth(b byte, now time.Time) bool {
	code, ok := decimal([]byte{b})
	if !ok || !f.SSB() || code > 40 {
		return false
	}
	hz := (int(code) + 1) * 50
	if code >= 10 {
		hz = (int(code) - 4) * 100
	}
	f.WidthHz, f.WidthAt = hz, now
	return true
}
