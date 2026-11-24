package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	"github.com/LuislopezMartinez/IC-RADIO/internal/dxcluster"
	rl "github.com/gen2brain/raylib-go/raylib"
)

func TestClusterLimitsUseActiveBandAndVisibleSweep(t *testing.T) {
	now := time.Now()
	radio := radioPanel{selectedBand: 0x05}
	state := civ.State{Frequency: 14314000, FrequencyAt: now, Sweep: civ.Sweep{Low: 14290000, High: 14400000, At: now}}
	if low, high, ok := radio.clusterLimits(state); !ok || low != 14290000 || high != 14350000 {
		t.Fatalf("20m bounds: %d–%d, valid = %v", low, high, ok)
	}
	state.Frequency = 7150000
	state.Sweep.Low, state.Sweep.High = 6900000, 7250000
	if low, high, ok := radio.clusterLimits(state); !ok || low != 7000000 || high != 7200000 {
		t.Fatalf("40m bounds after dial change: %d–%d, valid = %v", low, high, ok)
	}
	state.Frequency = 9000000
	if _, _, ok := radio.clusterLimits(state); ok {
		t.Fatal("general coverage should not display spots")
	}
}

func TestClusterAgeScaleAndCollisions(t *testing.T) {
	wf := rfWaterfall()
	top := clusterAgeY(wf, 0)
	middle := clusterAgeY(wf, 5*time.Minute)
	bottom := clusterAgeY(wf, 10*time.Minute)
	if top != wf.Y+clusterWaterfallInset || middle != (top+bottom)/2 ||
		bottom+clusterLabelHeight != wf.Y+wf.Height-clusterWaterfallInset ||
		clusterAgeY(wf, -time.Minute) != top || clusterAgeY(wf, 11*time.Minute) != bottom {
		t.Fatalf("invalid age scale: %v, %v, %v", top, middle, bottom)
	}
	first := rl.NewRectangle(wf.X+20, top, 70, clusterLabelHeight)
	later := rl.NewRectangle(first.X, middle, first.Width, first.Height)
	overlap := rl.NewRectangle(first.X+40, top+3, first.Width, first.Height)
	labels := addClusterLabel(nil, first, dxcluster.Spot{Call: "F4ABC"})
	labels = addClusterLabel(labels, later, dxcluster.Spot{Call: "JA1ABC"})
	labels = addClusterLabel(labels, overlap, dxcluster.Spot{Call: "EA4ABC"})
	if len(labels) != 2 || len(labels[0].spots) != 2 || len(labels[1].spots) != 1 {
		t.Fatalf("incorrect grouping: %+v", labels)
	}
}

func TestClusterZoneMultiSelection(t *testing.T) {
	var zones clusterZones
	if !zones.checked(dxcluster.All) || !zones.includes(dxcluster.Asia) {
		t.Fatal("default should show all zones")
	}
	zones = zones.toggle(dxcluster.Europe).toggle(dxcluster.Asia)
	if !zones.includes(dxcluster.Europe) || !zones.includes(dxcluster.Asia) || zones.includes(dxcluster.Africa) ||
		zones.includes(dxcluster.All) || zones.summary() != "2" {
		t.Fatalf("unexpected selection: %v", zones)
	}
	zones = zones.toggle(dxcluster.Asia)
	if zones.includes(dxcluster.Asia) || zones.summary() != "EU" {
		t.Fatalf("unexpected selection after unchecking a zone: %v", zones)
	}
	if zones.toggle(dxcluster.All) != 0 || zones.toggle(dxcluster.Europe) != 0 {
		t.Fatal("ALL or clearing the last selection should restore all zones")
	}
}
