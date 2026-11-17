//go:build !windows

package main

// ownConsole is a Windows matter: elsewhere a program opened without a
// terminal has no window of its own to keep open.
func ownConsole() bool { return false }

func settledDir() string { return "" }
