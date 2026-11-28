package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type audioOption struct {
	name, id string
}

type audioChoice struct {
	label, name, savedID string
	input                *bufio.Scanner
	output               io.Writer
}

func selectAudioDevice(devices []audioOption, choice audioChoice) (int, error) {
	var matches []int
	for i, device := range devices {
		name := strings.ToLower(device.name)
		if choice.name != "" {
			if strings.EqualFold(device.name, choice.name) {
				matches = append(matches, i)
			}
		} else if strings.Contains(name, "usb audio codec") || strings.Contains(name, "usb audio device") {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("no matching %s device", choice.label)
	case 1:
		return matches[0], nil
	}
	for _, index := range matches {
		if devices[index].id == choice.savedID {
			return index, nil
		}
	}
	if choice.input == nil {
		return -1, fmt.Errorf("multiple %s devices found; start the server interactively to select one", choice.label)
	}
	fmt.Fprintf(choice.output, "\n%s devices:\n", choice.label)
	for i, index := range matches {
		device := devices[index]
		fmt.Fprintf(choice.output, "  %d. %s (ID …%s)\n", i+1, device.name, device.id[max(0, len(device.id)-24):])
	}
	for {
		fmt.Fprint(choice.output, "Select a device, or enter 0 to skip: ")
		if !choice.input.Scan() {
			break
		}
		number, err := strconv.Atoi(strings.TrimSpace(choice.input.Text()))
		if err == nil && number >= 0 && number <= len(matches) {
			if number == 0 {
				break
			}
			return matches[number-1], nil
		}
	}
	return -1, fmt.Errorf("no %s device selected", choice.label)
}
