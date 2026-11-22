package civ

import (
	"bytes"
	"testing"
	"time"
)

func TestSSBFilterReadbackAndWrites(t *testing.T) {
	now := time.Now()
	s := &Session{address: 0x94, requests: make(chan []byte, 2), stop: make(chan struct{})}
	var scope assembler
	receive := func(data ...byte) { s.receive(append([]byte{0xe0, 0x94}, data...), &scope, now) }
	receive(0x26, 0, 1, 1, 2)
	receive(0x1a, 3, 0x28)
	receive(0x14, 7, 0x01, 0x55)
	f := s.Snapshot().IF
	if f.WidthHz != 2400 || f.PBT[0] != 155 || f.Data != 1 {
		t.Fatalf("unexpected decoded filter %+v", f)
	}
	if !s.SetFilterSlot(3) || !s.SetFilterWidth(1800) || s.SetFilterWidth(550) {
		t.Fatal("unexpected filter validation")
	}
	for _, want := range [][]byte{{0x26, 0, 1, 1, 3}, {0x1a, 3, 0x22}} {
		command := <-s.requests
		if !bytes.Equal(command, want) || !bytes.Equal(commandReadback(command), want[:2]) {
			t.Fatalf("command % X, expected % X", command, want)
		}
	}
	receive(0x26, 0, 1, 1, 3)
	if f := s.Snapshot().IF; !f.WidthAt.IsZero() || !f.PBTAt[0].IsZero() {
		t.Fatal("previous slot width and PBT remain valid")
	}
}
