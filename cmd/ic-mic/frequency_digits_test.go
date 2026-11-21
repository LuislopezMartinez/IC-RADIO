package main

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func TestFrequencyClickSelectsEveryDigit(t *testing.T) {
	for _, tc := range []struct {
		hz    int64
		steps []int64
	}{
		{7105267, []int64{1000000, 0, 100000, 10000, 1000, 0, 100, 10, 1}},
		{14280267, []int64{10000000, 1000000, 0, 100000, 10000, 1000, 0, 100, 10, 1}},
	} {
		for i, glyph := range frequencyGlyphs(tc.hz) {
			click := rl.NewVector2(glyph.cell.X+glyph.cell.Width/2, glyph.cell.Y+glyph.cell.Height/2)
			step := frequencyDigitAt(tc.hz, click)
			if step != tc.steps[i] {
				t.Fatalf("frequency %d, character %d: got step %d, want %d", tc.hz, i, step, tc.steps[i])
			}
			if step > 0 && operationWheelStep(step, 100, false) != step {
				t.Fatalf("the wheel lost step %d after the pointer moved", step)
			}
		}
	}
	if operationWheelStep(0, 100, false) != 0 || operationWheelStep(0, 100, true) != 100 {
		t.Fatal("unexpected fallback wheel behavior")
	}
}
