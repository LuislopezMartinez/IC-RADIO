package main

import "testing"

func TestSWRFrequenciesAndCalibration(t *testing.T) {
	points := swrFrequencies(14100000, 10000, 13)
	if len(points) != 13 || points[0].Hz != 14040000 || points[6].Hz != 14100000 || points[12].Hz != 14160000 {
		t.Fatal("incorrect symmetric sweep")
	}
	if !validSWRRange(points[0].Hz, points[12].Hz) || validSWRRange(13990000, 14160000) {
		t.Fatal("incorrect range guard")
	}
	for raw, want := range map[int]float64{0: 1, 48: 1.5, 80: 2, 120: 3} {
		if got := swrRatio(raw); got != want {
			t.Fatalf("raw %d: got %g, want %g", raw, got, want)
		}
	}
}

func TestSWRZones(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		want  int
	}{
		{1, 0}, {1.5, 0}, {1.51, 1}, {2, 1}, {2.01, 2}, {3, 2},
	} {
		if got := swrZone(tc.ratio); got != tc.want {
			t.Errorf("ROE %g: got zone %d, want %d", tc.ratio, got, tc.want)
		}
	}
}
