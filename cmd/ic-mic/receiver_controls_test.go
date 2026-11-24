package main

import (
	"testing"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func TestReceiverControlStripDoesNotOverlap(t *testing.T) {
	rects := []rl.Rectangle{bandControl(), clusterButton(), clusterZoneButton(), agcRect(), peakRect(), spanRect(), swrOverlayButton(), swrTestButton()}
	for i := range receiverKnobs {
		rects = append(rects, receiverKnobRect(i))
	}
	for _, control := range rfControls {
		rects = append(rects, control.rect)
	}
	rects = append(rects, zoomControls[:]...)
	for i, rect := range rects {
		if rect.Y+rect.Height > rfGraph().Y {
			t.Fatalf("control %d extends into FFT: %+v", i, rect)
		}
		for j, other := range rects[:i] {
			if rect.X < other.X+other.Width && other.X < rect.X+rect.Width &&
				rect.Y < other.Y+other.Height && other.Y < rect.Y+rect.Height {
				t.Fatalf("controls %d and %d overlap: %+v and %+v", i, j, rect, other)
			}
		}
	}
}
