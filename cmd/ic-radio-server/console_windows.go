package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func consoleColor() (bool, func()) {
	return consoleMode(os.Stdout, windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING, 0)
}

func hideConsoleInput() (func(), bool) {
	ok, restore := consoleMode(os.Stdin, 0, windows.ENABLE_ECHO_INPUT)
	return restore, ok
}

func consoleMode(file *os.File, set, clear uint32) (bool, func()) {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil || windows.SetConsoleMode(handle, (mode|set)&^clear) != nil {
		return false, func() {}
	}
	return true, func() { _ = windows.SetConsoleMode(handle, mode) }
}
