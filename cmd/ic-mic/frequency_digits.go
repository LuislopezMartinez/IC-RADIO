package main

import (
	"fmt"

	rl "github.com/gen2brain/raylib-go/raylib"
)

type frequencyGlyph struct {
	cell rl.Rectangle
	text string
	step int64
}

func frequencyGlyphs(hz int64) []frequencyGlyph {
	if hz <= 0 {
		return nil
	}
	text := fmt.Sprintf("%d.%03d.%03d", hz/1000000, hz/1000%1000, hz%1000)
	const digitWidth, dotWidth = 46, 15
	width := float32((len(text)-2)*digitWidth + 2*dotWidth)
	x := 542 + (402-width)/2
	step := int64(1000000)
	if hz >= 10000000 {
		step *= 10
	}
	glyphs := make([]frequencyGlyph, 0, len(text))
	for _, char := range text {
		w, place := float32(dotWidth), int64(0)
		if char != '.' {
			w, place = digitWidth, step
			step /= 10
		}
		glyphs = append(glyphs, frequencyGlyph{rl.NewRectangle(x, 103, w, 96), string(char), place})
		x += w
	}
	return glyphs
}

func drawDialGlyph(glyph frequencyGlyph, shade rl.Color, size float32) {
	const spacing = 0
	width := rl.MeasureTextEx(semiboldFont, glyph.text, size, spacing).X
	if width > glyph.cell.Width-1 {
		size *= (glyph.cell.Width - 1) / width
	}
	dim := rl.MeasureTextEx(semiboldFont, glyph.text, size, spacing)
	center := rl.NewVector2(
		glyph.cell.X+(glyph.cell.Width-dim.X)/2,
		glyph.cell.Y+(glyph.cell.Height-dim.Y)/2,
	)
	// A narrow stroke gives the white numerals more weight at small scales.
	for _, offset := range [...]rl.Vector2{{X: -0.8}, {X: 0.8}, {Y: -0.6}, {Y: 0.6}, {}} {
		rl.DrawTextEx(semiboldFont, glyph.text, rl.NewVector2(center.X+offset.X, center.Y+offset.Y), size, spacing, shade)
	}
}

func frequencyDigitAt(hz int64, mouse rl.Vector2) int64 {
	for _, glyph := range frequencyGlyphs(hz) {
		if rl.CheckCollisionPointRec(mouse, glyph.cell) {
			return glyph.step
		}
	}
	return 0
}

func tuneDigit(hz, step, delta int64) int64 {
	base := hz / step
	if delta < 0 && hz%step != 0 {
		base++
	}
	return (base + delta) * step
}

func operationTuningStep(selected, fallback int64, overTuningArea bool) int64 {
	if selected > 0 {
		return selected
	}
	if overTuningArea {
		return fallback
	}
	return 0
}
