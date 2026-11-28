package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestSelectAudioDeviceWithIdenticalNames(t *testing.T) {
	devices := []audioOption{
		{name: "Default speakers", id: "other"},
		{name: "USB Audio CODEC", id: "first"},
		{name: "USB Audio CODEC", id: "second"},
	}
	choice := audioChoice{label: "Radio TX output", output: io.Discard}
	if _, err := selectAudioDevice(devices, choice); err == nil {
		t.Fatal("ambiguous devices were selected without confirmation")
	}
	choice.input = bufio.NewScanner(strings.NewReader("2\n"))
	if index, err := selectAudioDevice(devices, choice); index != 2 || err != nil {
		t.Fatalf("selection returned %d, %v", index, err)
	}
	devices[1], devices[2] = devices[2], devices[1]
	choice.input, choice.savedID = nil, "second"
	if index, err := selectAudioDevice(devices, choice); index != 1 || err != nil {
		t.Fatalf("saved device returned %d, %v after reordering", index, err)
	}
}
