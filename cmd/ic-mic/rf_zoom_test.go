package main

import (
	"testing"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestRFVerticalZoomAnchorsNoiseFloor(t *testing.T) {
	now := time.Now()
	sweep := civ.Sweep{Low: 7000000, High: 7050000, At: now}
	for i := range sweep.Levels {
		sweep.Levels[i] = 20
	}
	sweep.Levels[0] = 160
	var zoom rfVerticalZoom
	zoom.observe(sweep)
	if zoom.floor != 20 {
		t.Fatalf("strong peak moved the floor to %v", zoom.floor)
	}
	zoom.index = 2
	if zoom.displayLevel(20) != 20 || zoom.displayLevel(27) != 34 {
		t.Fatal("zoom moved the floor or did not magnify the signal")
	}
	bottom, top := zoom.limitsDB()
	if bottom != -75 || top != -35 || zoom.displayLevel(90) != 160 {
		t.Fatalf("unexpected axis limits: %v to %v", bottom, top)
	}

	sweep.At = now.Add(time.Second)
	for i := range sweep.Levels {
		sweep.Levels[i] = 40
	}
	zoom.observe(sweep)
	if zoom.floor != 20 {
		t.Fatal("zoom anchor drifted while zoom was active")
	}
	zoom.index = 0
	if zoom.displayLevel(27) != 27 {
		t.Fatal("reset did not restore the original scale")
	}
}

func TestRFWaterfallUsesSpectrumFrequencyAxis(t *testing.T) {
	graph, waterfall := rfGraph(), rfWaterfall()
	if waterfall.X != graph.X || waterfall.Width != graph.Width {
		t.Fatal("waterfall and spectrum have different horizontal frequency scales")
	}
}
