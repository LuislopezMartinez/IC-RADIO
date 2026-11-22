package main

import (
	"fmt"
	"github.com/LuislopezMartinez/IC-RADIO/internal/swr"
	"math"
	"time"

	"github.com/LuislopezMartinez/IC-RADIO/internal/civ"
	rl "github.com/gen2brain/raylib-go/raylib"
)

type txReading struct {
	label, unit  string
	code         byte
	value, limit float64
	available    bool
}

func interpolateMeter(raw int, points ...[2]float64) float64 {
	for i := 1; i < len(points); i++ {
		lo, hi := points[i-1], points[i]
		if float64(raw) <= hi[0] {
			return lo[1] + (hi[1]-lo[1])*(float64(raw)-lo[0])/(hi[0]-lo[0])
		}
	}
	return points[len(points)-1][1]
}

func txReadings(state civ.State, online bool, now time.Time) [6]txReading {
	raw := state.Meters
	po := interpolateMeter(raw[0x11], [2]float64{0, 0}, [2]float64{143, 50}, [2]float64{213, 100})
	volts := interpolateMeter(raw[0x15], [2]float64{0, 0}, [2]float64{13, 10}, [2]float64{241, 16})
	amps := interpolateMeter(raw[0x16], [2]float64{0, 0}, [2]float64{97, 10}, [2]float64{146, 15}, [2]float64{241, 25})
	comp := interpolateMeter(raw[0x14], [2]float64{0, 0}, [2]float64{130, 15}, [2]float64{241, 30})
	readings := [6]txReading{
		{label: "Po", unit: "%", code: 0x11, value: po, limit: 100},
		{label: "ROE", unit: ":1", code: 0x12, value: swr.Ratio(raw[0x12]), limit: 3},
		{label: "ALC", unit: "%", code: 0x13, value: min(100, 100*float64(raw[0x13])/120), limit: 100},
		{label: "Vd", unit: "V", code: 0x15, value: volts, limit: 16},
		{label: "Id", unit: "A", code: 0x16, value: amps, limit: 25},
		{label: "COMP", unit: "dB", code: 0x14, value: comp, limit: 30},
	}
	for i := range readings {
		r := &readings[i]
		at := state.MeterAt[r.code]
		r.available = online && state.TX && !state.TXSince.IsZero() && !at.Before(state.TXSince) && now.Sub(at) < 1500*time.Millisecond
		if r.code == 0x12 {
			r.available = r.available && !state.MeterAt[0x11].Before(state.TXSince) &&
				now.Sub(state.MeterAt[0x11]) < 1500*time.Millisecond && raw[0x11] > 0
			if raw[r.code] > 120 {
				r.unit = "+:1"
			}
		}
	}
	return readings
}

func (r txReading) text() string {
	if !r.available {
		return "--"
	}
	switch r.code {
	case 0x12:
		return fmt.Sprintf("%.1f%s", r.value, r.unit)
	case 0x11, 0x13:
		return fmt.Sprintf("%.0f %s", r.value, r.unit)
	default:
		return fmt.Sprintf("%.1f %s", r.value, r.unit)
	}
}

func (r txReading) fraction() float64 {
	if r.code == 0x12 {
		return (r.value - 1) / 2
	}
	return r.value / r.limit
}

type txNeedles struct {
	position       [3]float64
	since, drawnAt time.Time
}

func (n *txNeedles) update(readings [6]txReading, since, now time.Time) {
	if n.since != since || now.Sub(n.drawnAt) > time.Second {
		*n = txNeedles{since: since, drawnAt: now}
	}
	gain := -math.Expm1(-now.Sub(n.drawnAt).Seconds() / .12)
	for i := range n.position {
		if readings[i].available {
			n.position[i] += (readings[i].fraction() - n.position[i]) * gain
		}
	}
	n.drawnAt = now
}

func txScalePoint(center rl.Vector2, radius float32, fraction float64) rl.Vector2 {
	theta := (213 + 114*fraction) * math.Pi / 180
	return rl.NewVector2(center.X+radius*float32(math.Cos(theta)), center.Y+radius*float32(math.Sin(theta)))
}

func drawTXGauge(rect rl.Rectangle, reading txReading, position float64) {
	drawConsolePanel(rect, consoleEdge)
	drawConsoleText(semiboldFont, reading.label, rl.NewRectangle(rect.X+10, rect.Y+3, rect.Width-20, 30), 32, color.text)
	center := rl.NewVector2(rect.X+rect.Width/2, rect.Y+217)
	const radius = 151
	rl.DrawRing(center, radius-9, radius, 213, 327, 64, rl.NewColor(31, 56, 66, 255))
	if reading.code == 0x12 {
		rl.DrawRing(center, radius-9, radius, 213, 241.5, 16, swrZoneColors[0])
		rl.DrawRing(center, radius-9, radius, 241.5, 270, 16, swrZoneColors[1])
		rl.DrawRing(center, radius-9, radius, 270, 327, 28, swrZoneColors[2])
	} else if reading.available && position > .001 {
		shade := color.cyan
		if reading.code == 0x13 {
			shade = color.amber
		}
		rl.DrawRing(center, radius-9, radius, 213, 213+float32(114*position), 48, shade)
	}
	ticks := [...]string{"0", "25", "50", "75", "100 %"}
	if reading.code == 0x12 {
		ticks = [...]string{"1.0", "1.5", "2.0", "2.5", "3.0+"}
	}
	for i := 0; i <= 20; i++ {
		position := float64(i) / 20
		major := i%5 == 0
		in, out := txScalePoint(center, radius-10, position), txScalePoint(center, radius+1, position)
		if major {
			out = txScalePoint(center, radius+8, position)
		}
		rl.DrawLineEx(in, out, 1.1, color.text)
		if major {
			label := txScalePoint(center, radius+21, position)
			drawConsoleText(semiboldFont, ticks[i/5], rl.NewRectangle(label.X-38, label.Y-10, 76, 20), 17, color.text)
		}
	}
	if reading.available {
		tip := txScalePoint(center, radius-2, position)
		rl.DrawLineEx(center, tip, 3, rl.NewColor(247, 234, 197, 255))
		rl.DrawCircleV(center, 8, rl.NewColor(174, 190, 194, 255))
	}
	rl.DrawLineEx(rl.NewVector2(rect.X+36, center.Y+17), rl.NewVector2(rect.X+rect.Width-36, center.Y+17), 1, consoleEdge)
	drawConsoleText(displayFont, reading.text(), rl.NewRectangle(rect.X+15, center.Y+20, rect.Width-30, 49), 48, color.text)
}

func drawTXReadout(rect rl.Rectangle, reading txReading, ticks []string) {
	drawText(reading.label, int32(rect.X+19), int32(rect.Y+12), 25, color.text)
	drawConsoleText(displayFont, reading.text(), rl.NewRectangle(rect.X+148, rect.Y+4, rect.Width-165, 51), 42, color.text)
	const segments = 25
	x, y, w := rect.X+20, rect.Y+65, rect.Width-40
	for i := 0; i < segments; i++ {
		shade := rl.NewColor(37, 57, 66, 255)
		if reading.available && float64(i+1)/segments <= reading.fraction() {
			shade = color.cyan
		}
		rl.DrawRectangleRec(rl.NewRectangle(x+float32(i)*w/segments, y, w/segments-2, 15), shade)
	}
	rl.DrawLineEx(rl.NewVector2(x, y+21), rl.NewVector2(x+w, y+21), 1, color.muted)
	for i, label := range ticks {
		p := x + float32(i)*w/float32(len(ticks)-1)
		rl.DrawLineEx(rl.NewVector2(p, y+21), rl.NewVector2(p, y+27), 1, color.muted)
		drawConsoleText(semiboldFont, label, rl.NewRectangle(p-20, y+29, 40, 20), 15, color.muted)
	}
}

func (a *app) drawTXTelemetry(readings [6]txReading, state civ.State, now time.Time) {
	drawConsolePanel(rl.NewRectangle(7, 225, 1586, 533), consoleEdge)
	drawText("TRANSMISIÓN", 28, 243, 30, color.text)
	n := &a.radio.txNeedles
	n.update(readings, state.TXSince, now)
	for i := range 3 {
		r := rl.NewRectangle(25+float32(i)*520, 291, 509, 292)
		drawTXGauge(r, readings[i], n.position[i])
	}
	drawConsolePanel(rl.NewRectangle(25, 596, 1549, 145), consoleEdge)
	for i, ticks := range [][]string{{"0", "4", "8", "12", "16 V"}, {"0", "5", "10", "15", "20", "25 A"}, {"0", "5", "10", "15", "20", "25", "30 dB"}} {
		r := rl.NewRectangle(31+float32(i)*516, 603, 506, 133)
		if i > 0 {
			rl.DrawLine(int32(r.X-6), 610, int32(r.X-6), 727, consoleEdge)
		}
		drawTXReadout(r, readings[i+3], ticks)
	}
}

func drawTXHeaderPower(reading txReading) {
	drawConsoleText(semiboldFont, "Po", rl.NewRectangle(1040, 127, 83, 46), 29, color.text)
	const x, y, width, segments = 1140, 143, 417, 32
	for i := range segments {
		shade := rl.NewColor(37, 57, 66, 255)
		if reading.available && float64(i+1)/segments <= reading.fraction() {
			shade = color.cyan
		}
		rl.DrawRectangle(x+int32(i)*width/segments, y, width/segments-2, 22, shade)
	}
	for i, label := range []string{"0", "25", "50", "75", "100 %"} {
		p := x + int32(i)*width/4
		rl.DrawLine(p, 131, p, 140, color.muted)
		drawConsoleText(semiboldFont, label, rl.NewRectangle(float32(p-31), 106, 62, 24), 16, color.text)
	}
}
