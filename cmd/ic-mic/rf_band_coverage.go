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

func (a *app) drawBandCoverage(graph, waterfall rl.Rectangle, sweep civ.Sweep, frequency, tuned int64, exact bool) {
	const height = 32
	gap := waterfall.Y - graph.Y - graph.Height
	bar := rl.NewRectangle(graph.X, graph.Y+graph.Height+(gap-height)/2, graph.Width, height)
	outside := rl.NewColor(32, 32, 36, 255)
	inside := rl.NewColor(204, 205, 208, 255)
	rl.DrawRectangleRec(bar, outside)
	index := a.radio.coverageBand(frequency)
	var limits [2]int64
	if index >= 0 {
		limits = coverageLimits[radioBands[index].code]
	}
	low, high := limits[0], limits[1]
	left, right := bar.X, bar.X+bar.Width
	y := bar.Y + bar.Height/2
	start, end := right, right
	if low != 0 && high >= sweep.Low && low <= sweep.High {
		start = rfFrequencyX(graph, sweep, max(low, sweep.Low))
		end = rfFrequencyX(graph, sweep, min(high, sweep.High))
		rl.DrawRectangleRec(rl.NewRectangle(start, bar.Y, end-start, bar.Height), inside)
	}
	if low != 0 {
		drawOutOfBandHatch(bar, left, start)
		drawOutOfBandHatch(bar, end, right)
	}
	rl.DrawRectangleLinesEx(bar, 1, consoleEdge)
	if low != 0 {
		if low < sweep.Low {
			drawBandArrow(left+4, y, -1, high < sweep.Low)
		} else if low <= sweep.High {
			drawBandEdge(bar, rfFrequencyX(graph, sweep, low), low, -1)
		}
		if high > sweep.High {
			drawBandArrow(right-4, y, 1, low > sweep.High)
		} else if high >= sweep.Low {
			drawBandEdge(bar, rfFrequencyX(graph, sweep, high), high, 1)
		}
	}
	hasTune := tuned > 0 && tuned >= sweep.Low && tuned <= sweep.High
	tuneX := rfFrequencyX(graph, sweep, tuned)
	for i := 0; i <= 5; i++ {
		x := left + float32(i)*bar.Width/5
		hz := rfFrequency(graph, x, sweep)
		if hasTune && x > tuneX-96 && x < tuneX+96 {
			continue
		}
		fill, shade := outside, color.text
		if low != 0 && hz >= low && hz <= high {
			fill, shade = inside, color.bg
		}
		rect := rfScaleLabelRect(bar, x)
		if rect.X < start && rect.X+rect.Width > start || rect.X < end && rect.X+rect.Width > end {
			rl.DrawRectangleRec(rect, fill)
		}
		drawConsoleText(semiboldFont, fmt.Sprintf("%.3f", float64(hz)/1e6), rect, 18, shade)
	}
	if hasTune {
		shade := color.muted
		if exact {
			shade = color.green
		}
		rl.DrawLineEx(rl.NewVector2(tuneX, graph.Y), rl.NewVector2(tuneX, bar.Y+bar.Height), 2, shade)
		rect := rfScaleLabelRect(bar, tuneX)
		rl.DrawRectangleRounded(rect, .2, 6, color.bg)
		drawConsoleText(semiboldFont, fmt.Sprintf("%.3f", float64(tuned)/1e6), rect, 19, shade)
	}
}

func rfScaleLabelRect(bar rl.Rectangle, x float32) rl.Rectangle {
	const width = 78
	return rl.NewRectangle(max(bar.X+14, min(bar.X+bar.Width-width-14, x-width/2)), bar.Y+8, width, bar.Height-11)
}

func drawOutOfBandHatch(bar rl.Rectangle, left, right float32) {
	if right <= left {
		return
	}
	shade := rl.NewColor(58, 58, 63, 255)
	for x := left - bar.Height; x < right; x += 11 {
		from := max(0, left-x)
		to := min(bar.Height, right-x)
		if to > from {
			rl.DrawLineEx(
				rl.NewVector2(x+from, bar.Y+bar.Height-from),
				rl.NewVector2(x+to, bar.Y+bar.Height-to), 1, shade,
			)
		}
	}
}

func drawBandArrow(edge, y, direction float32, outside bool) {
	shade := color.bg
	if outside {
		shade = color.text
	}
	tip := rl.NewVector2(edge, y)
	base := edge - direction*9
	rl.DrawLineEx(rl.NewVector2(base, y-5), tip, 2, shade)
	rl.DrawLineEx(tip, rl.NewVector2(base, y+5), 2, shade)
}

func drawBandEdge(bar rl.Rectangle, x float32, frequency int64, direction float32) {
	rl.DrawLineEx(rl.NewVector2(x, bar.Y), rl.NewVector2(x, bar.Y+9), 2, rl.White)
	rl.DrawLineEx(rl.NewVector2(x, bar.Y+bar.Height-7), rl.NewVector2(x, bar.Y+bar.Height), 2, rl.White)
	const width = 58
	labelX := max(bar.X, min(bar.X+bar.Width-width, x-29+direction*35))
	label := fmt.Sprintf("%.3f", float64(frequency)/1e6)
	rect := rl.NewRectangle(labelX, bar.Y, width, 12)
	rl.DrawRectangleRec(rect, color.panel2)
	drawConsoleText(semiboldFont, label, rect, 13, color.text)
}
