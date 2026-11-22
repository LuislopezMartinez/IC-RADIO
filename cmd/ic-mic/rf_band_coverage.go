package main

import (
	"fmt"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

// Band edges are display references for IARU Region 1. The Icom's wider
// band-memory ranges remain in radioBands.
var coverageLimits = map[byte][2]int64{
	0x01: {1810000, 2000000},
	0x02: {3500000, 3800000},
	0x03: {7000000, 7200000},
	0x04: {10100000, 10150000},
	0x05: {14000000, 14350000},
	0x06: {18068000, 18168000},
	0x07: {21000000, 21450000},
	0x08: {24890000, 24990000},
	0x09: {28000000, 29700000},
	0x10: {50000000, 52000000},
}

func (r *radioPanel) coverageBand(frequency int64) int {
	if r.selectedBand != 0 {
		for i, band := range radioBands {
			if band.code == r.selectedBand {
				return i
			}
		}
	}
	return bandAtFrequency(frequency)
}

func rfFrequencyX(graph rl.Rectangle, sweep civ.Sweep, hz int64) float32 {
	return graph.X + float32(float64(hz-sweep.Low)/float64(sweep.High-sweep.Low))*graph.Width
}

func (a *app) drawBandCoverage(graph, waterfall rl.Rectangle, sweep civ.Sweep, frequency int64) {
	index := a.radio.coverageBand(frequency)
	if index < 0 {
		return
	}
	limits, ok := coverageLimits[radioBands[index].code]
	if !ok {
		return
	}
	low, high := limits[0], limits[1]
	left, right := graph.X, graph.X+graph.Width
	y := waterfall.Y - 5
	rl.DrawLineEx(rl.NewVector2(left, y), rl.NewVector2(right, y), 1, consoleEdge)
	if high < sweep.Low {
		drawBandArrow(left, y, -1)
		return
	}
	if low > sweep.High {
		drawBandArrow(right, y, 1)
		return
	}

	start := rfFrequencyX(graph, sweep, max(low, sweep.Low))
	end := rfFrequencyX(graph, sweep, min(high, sweep.High))
	rl.DrawRectangleRec(rl.NewRectangle(start, y-2, end-start, 4), color.cyan)
	if low < sweep.Low {
		drawBandArrow(left, y, -1)
	} else {
		drawBandEdge(graph, start, y, low, -1)
	}
	if high > sweep.High {
		drawBandArrow(right, y, 1)
	} else {
		drawBandEdge(graph, end, y, high, 1)
	}
}

func drawBandArrow(edge, y, direction float32) {
	tip := rl.NewVector2(edge, y)
	base := edge - direction*9
	rl.DrawLineEx(rl.NewVector2(base, y-4), tip, 2, color.cyan)
	rl.DrawLineEx(tip, rl.NewVector2(base, y+4), 2, color.cyan)
}

func drawBandEdge(graph rl.Rectangle, x, y float32, frequency int64, direction float32) {
	rl.DrawLineEx(rl.NewVector2(x, y-4), rl.NewVector2(x, y+4), 2, color.cyan)
	const width = 58
	labelX := max(graph.X, min(graph.X+graph.Width-width, x-29+direction*35))
	label := fmt.Sprintf("%.3f", float64(frequency)/1e6)
	drawConsoleText(semiboldFont, label, rl.NewRectangle(labelX, graph.Y+graph.Height, width, 12), 13, color.cyan)
}
