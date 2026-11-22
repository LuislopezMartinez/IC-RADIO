package main

import (
	"fmt"
	"math"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

var rfZoomFactors = [...]float32{1, 1.5, 2, 3, 4}

type rfVerticalZoom struct {
	index     int
	floor     float32
	lastSweep civ.Sweep
}

func (z *rfVerticalZoom) factor() float32 { return rfZoomFactors[z.index] }

func (z *rfVerticalZoom) observe(sweep civ.Sweep) {
	prev := z.lastSweep
	if prev.At == sweep.At {
		return
	}
	z.lastSweep = sweep
	if prev.At.IsZero() || sweep.High-sweep.Low != prev.High-prev.Low || sweep.Mode != prev.Mode {
		z.floor = noiseFloor(sweep.Levels)
	} else if z.index == 0 {
		weight := float32(-math.Expm1(-sweep.At.Sub(prev.At).Seconds() / 1.5))
		z.floor += (noiseFloor(sweep.Levels) - z.floor) * weight
	}
}

func noiseFloor(levels [civ.Samples]byte) float32 {
	var counts [256]int
	for _, level := range levels {
		counts[level]++
	}
	count := 0
	for level, n := range counts {
		count += n
		if count >= civ.Samples*35/100 {
			return float32(level)
		}
	}
	return 0
}

func (z *rfVerticalZoom) displayLevel(level float32) float32 {
	return z.floor + (level-z.floor)*z.factor()
}

func (z *rfVerticalZoom) limitsDB() (bottom, top float32) {
	factor := z.factor()
	return (z.floor - z.floor/factor - 160) / 2, (z.floor + (160-z.floor)/factor - 160) / 2
}

var zoomControls = [...]rl.Rectangle{
	{X: 1064, Y: 240, Width: 36, Height: 36},
	{X: 1105, Y: 240, Width: 71, Height: 36},
	{X: 1181, Y: 240, Width: 36, Height: 36},
}

func (z *rfVerticalZoom) update(mouse rl.Vector2) bool {
	for i, rect := range zoomControls {
		if clicked(mouse, rect) {
			switch i {
			case 0:
				z.index = max(0, z.index-1)
			case 1:
				z.index = 0
			case 2:
				z.index = min(len(rfZoomFactors)-1, z.index+1)
			}
			return true
		}
	}
	return false
}

func (z *rfVerticalZoom) draw() {
	drawConsoleText(semiboldFont, "ZOOM Y", rl.NewRectangle(983, 240, 78, 36), 17, color.muted)
	drawConsoleButton(zoomControls[0], "-", false, false)
	drawRFOption(zoomControls[1], fmt.Sprintf("x%.1f", z.factor()), z.index != 0)
	drawConsoleButton(zoomControls[2], "+", false, false)
}

func (z *rfVerticalZoom) drawGrid(graph rl.Rectangle) {
	bottom, top := z.limitsDB()
	step := float32(10)
	if top-bottom <= 30 {
		step = 5
	}
	for db := float32(math.Ceil(float64(bottom/step))) * step; db <= top; db += step {
		y := graph.Y + graph.Height*(top-db)/(top-bottom)
		rl.DrawLineEx(rl.NewVector2(graph.X, y), rl.NewVector2(graph.X+graph.Width, y), 1, rl.NewColor(44, 44, 49, 255))
		drawConsoleText(semiboldFont, fmt.Sprintf("%.0f dB", db), rl.NewRectangle(21, y-11, 74, 22), 18, color.text)
	}
}
