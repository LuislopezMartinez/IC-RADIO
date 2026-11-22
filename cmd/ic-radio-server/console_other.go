//go:build !windows

package main

func consoleColor() (bool, func())     { return false, func() {} }
func hideConsoleInput() (func(), bool) { return func() {}, false }
