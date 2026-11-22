package civ

import (
	"bytes"
	"testing"
	"time"
)

func TestSplitAndIncrementalTuning(t *testing.T) {
	now := time.Now()
	session := &Session{address: 0x94}
	var scope assembler
	for _, reply := range [][]byte{
		{0x03, 0, 0x50, 0x19, 0x14, 0},
		{0x0f, 1},
		{0x21, 0, 0x20, 0x01, 1},
		{0x21, 1, 1},
		{0x21, 2, 1},
		{0x25, 1, 0, 0, 0x20, 0x14, 0},
	} {
		session.receive(append([]byte{0xe0, 0x94}, reply...), &scope, now)
	}
	state := session.Snapshot()
	if rx, tx := state.EffectiveTuning(now); rx != 14194880 || tx != 14199880 {
		t.Fatalf("effective tuning: got RX %d TX %d", rx, tx)
	}
	state.Split = false
	state.Tuning.DeltaTX = false
	if rx, tx := state.EffectiveTuning(now); rx != 14194880 || tx != 14195000 {
		t.Fatalf("RIT only: got RX %d TX %d", rx, tx)
	}
	state.Split = true
	state.Tuning.OtherVFOAt = time.Time{}
	if _, tx := state.EffectiveTuning(now); tx != 0 {
		t.Fatalf("unconfirmed TX shown as %d", tx)
	}
}

func TestTuningWriteReadbacks(t *testing.T) {
	session := &Session{address: 0x94}
	for _, tc := range []struct {
		command, readback []byte
	}{
		{[]byte{0x0f, 1}, []byte{0x0f}},
		{[]byte{0x21, 0, 0x20, 0x01, 1}, []byte{0x21, 0}},
		{[]byte{0x21, 2, 1}, []byte{0x21, 2}},
		{[]byte{0x25, 1, 0, 0, 0x20, 0x14, 0}, []byte{0x25, 1}},
	} {
		if !session.isReply([]byte{0xe0, 0x94, 0xfb}, tc.command) ||
			!bytes.Equal(commandReadback(tc.command), tc.readback) ||
			commandReadback(tc.readback) != nil {
			t.Fatalf("wrong acknowledgement or readback for % X", tc.command)
		}
	}
}
