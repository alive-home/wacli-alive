package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// runSettled is for a wacli that was opened by itself from wherever it was
// downloaded: it puts a copy in dir, a place that stays, and runs the app
// page from there. The scheduled push names the program by its path, and a
// download folder gets cleared. It reports whether the copy ran, and how that
// ended; when the copy cannot be made, wacli runs from where it is.
func runSettled(dir string) (ran bool, err error) {
	exe, exeErr := os.Executable()
	if exeErr != nil || dir == "" {
		return false, nil
	}
	target := filepath.Join(dir, filepath.Base(exe))
	if sameFile(exe, target) {
		return false, nil
	}
	if err := installCopy(exe, target); err != nil {
		fmt.Fprintf(os.Stderr, "wacli runs from %s: no copy could be put in %s (%v).\nKeep this file where it is while a push is turned on.\n", exe, dir, err)
		return false, nil
	}
	cmd := exec.Command(target, "app")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return true, cmd.Run()
}

func sameFile(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ai, bi)
}

// installCopy puts a copy of source at target. A program that is running
// there (a scheduled round) cannot be overwritten on Windows but can be
// renamed, so the old one steps aside first.
func installCopy(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	fresh, old := target+".new", target+".old"
	if err := copyFile(source, fresh); err != nil {
		_ = os.Remove(fresh)
		return err
	}
	_ = os.Remove(old)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			_ = os.Remove(fresh)
			return err
		}
	}
	if err := os.Rename(fresh, target); err != nil {
		_ = os.Rename(old, target)
		_ = os.Remove(fresh)
		return err
	}
	_ = os.Remove(old)
	return nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
