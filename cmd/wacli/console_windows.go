//go:build windows

package main

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ownConsole reports whether this program is alone in its console window:
// it was opened from Explorer, the Start menu or a download, and the window
// goes when the program ends. In a terminal the shell is in the list too.
func ownConsole() bool {
	var processes [2]uint32
	count, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList").
		Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	return count == 1
}

// settledDir is where a wacli that was opened from a download keeps itself:
// the signed-in user's own program data, which no cleanup empties.
func settledDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	return filepath.Join(local, "wacli")
}
