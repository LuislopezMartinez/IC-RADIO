package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/remote"
)

type initialVFORadio struct {
	remote.Radio
	state civ.State
	vfos  []byte
}

func (r *initialVFORadio) Snapshot() civ.State { return r.state }

func (r *initialVFORadio) SelectVFO(vfo byte) bool {
	r.vfos = append(r.vfos, vfo)
	return true
}

func TestInitialVFOWaitsForNewConfirmationAndFrequency(t *testing.T) {
	now := time.Now()
	radio := &initialVFORadio{state: civ.State{
		LastReply: now, TX: true, TXKnown: true, VFOKnown: true, VFOAt: now,
	}}
	a := &app{}
	a.radio.session = radio
	a.radio.requestInitialVFO()
	a.applyInitialVFO()
	if len(radio.vfos) != 0 {
		t.Fatal("requested VFO A during TX")
	}
	radio.state.TX = false
	a.applyInitialVFO()
	a.applyInitialVFO()
	if len(radio.vfos) != 1 || radio.vfos[0] != 0 || !a.radio.initialVFOPending {
		t.Fatal("stale VFO A confirmation was accepted")
	}
	radio.state.VFOAt = now.Add(time.Millisecond)
	a.applyInitialVFO()
	if !a.radio.initialVFOPending {
		t.Fatal("accepted VFO A before reading its frequency")
	}
	radio.state.FrequencyAt = now.Add(2 * time.Millisecond)
	a.applyInitialVFO()
	if a.radio.initialVFOPending {
		t.Fatal("did not accept confirmed VFO A and frequency")
	}
}
