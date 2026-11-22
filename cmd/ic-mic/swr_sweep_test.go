package main

import (
	"github.com/LuislopezMartinez/IC-RADIO/internal/swr"
	"testing"
)

func TestSWRFrequenciesAndCalibration(t *testing.T) {
	points := swr.Frequencies(14100000, 10000, 13)
	if len(points) != 13 || points[0].Hz != 14040000 || points[6].Hz != 14100000 || points[12].Hz != 14160000 {
		t.Fatal("incorrect symmetric sweep")
	}
	wide := (swr.Request{Center: 18145000, Step: 10000, Count: 13}).ValidatedPoints()
	if len(wide) != 13 || wide[0].Hz != 18085000 || wide[12].Hz != 18205000 {
		t.Fatal("sweep crossing the previous band limit was not accepted")
	}
	if (swr.Request{Center: 74800000, Step: 10000, Count: 3}).ValidatedPoints() != nil {
		t.Fatal("sweep exceeded the tuning limits")
	}
	for raw, want := range map[int]float64{0: 1, 48: 1.5, 80: 2, 120: 3} {
		if got := swr.Ratio(raw); got != want {
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

func TestSWRRadioOutputNames(t *testing.T) {
	const mk2 = "Altavoces (USB Audio Device)"
	for _, tc := range []struct {
		active, configured string
		want               bool
	}{
		{"Altavoces (USB Audio CODEC)", "", true},
		{mk2, mk2, true},
		{mk2, "", false},
		{mk2, "Salida anterior", false},
		{"Auriculares USB", "Auriculares USB", false},
	} {
		if got := isConfiguredRadioOutput(tc.active, tc.configured); got != tc.want {
			t.Errorf("active %q, configured %q: got %v, want %v", tc.active, tc.configured, got, tc.want)
		}
	}
}
