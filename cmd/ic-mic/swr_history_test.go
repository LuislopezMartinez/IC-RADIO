package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSavedSWRResultsRoundTrip(t *testing.T) {
	points := swrFrequencies(14178000, 10000, 13)
	for i := range points {
		points[i].Valid, points[i].Raw, points[i].Power = true, i+12, 60
	}
	band := swrBandCode(14178000)
	settingsBefore := settings{SWRHistory: map[byte]swrSavedSweep{
		band: {Step: 10000, MeasuredAt: time.Now(), Points: points},
	}, SWROverlay: true}
	data, err := json.Marshal(settingsBefore)
	if err != nil {
		t.Fatal(err)
	}
	var settingsAfter settings
	if err := json.Unmarshal(data, &settingsAfter); err != nil {
		t.Fatal(err)
	}
	panel := swrPanel{history: settingsAfter.SWRHistory}
	panel.openSavedSweep(14178000)
	if len(panel.points) != 13 || panel.points[6] != points[6] || !settingsAfter.SWROverlay {
		t.Fatal("saved SWR result was not restored")
	}
	panel.openSavedSweep(7100000)
	if len(panel.points) != 0 {
		t.Fatal("showed a sweep from a different band")
	}
	points[2].Valid = false
	if completeSWRSweep(points) {
		t.Fatal("treated an incomplete scan as complete")
	}
}
