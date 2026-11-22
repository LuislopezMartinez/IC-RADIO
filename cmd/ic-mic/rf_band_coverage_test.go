package main

import (
	"testing"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
)

func TestSelectedBandCoverageFollowsSpectrumScale(t *testing.T) {
	radio := radioPanel{selectedBand: 0x06}
	index := radio.coverageBand(21050000)
	if index < 0 || radioBands[index].code != 0x06 {
		t.Fatal("tuning away changed the selected band")
	}
	lowHigh := coverageLimits[radioBands[index].code]
	if lowHigh != [2]int64{18068000, 18168000} {
		t.Fatalf("unexpected 17 m limits: %v", lowHigh)
	}
	graph := rfGraph()
	sweep := civ.Sweep{Low: 18040000, High: 18220000}
	start, end := rfFrequencyX(graph, sweep, lowHigh[0]), rfFrequencyX(graph, sweep, lowHigh[1])
	cursor := rfFrequencyX(graph, sweep, 18200000)
	if !(graph.X < start && start < end && end < cursor && cursor < graph.X+graph.Width) {
		t.Fatal("band and cursor do not share the spectrum scale")
	}
}
