package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallCopyReplacesAnOlderProgram(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "download", "wacli.exe")
	target := filepath.Join(dir, "home", "wacli", "wacli.exe")
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	write(source, "build 1")
	if err := installCopy(source, target); err != nil {
		t.Fatalf("first copy: %v", err)
	}
	write(source, "build 2")
	if err := installCopy(source, target); err != nil {
		t.Fatalf("copy over an older program: %v", err)
	}
	if got := read(target); got != "build 2" {
		t.Fatalf("the program in place is %q, want the newer one", got)
	}
	if left, _ := filepath.Glob(target + ".*"); len(left) != 0 {
		t.Fatalf("left beside the program: %v", left)
	}
	if !sameFile(target, target) || sameFile(source, target) {
		t.Fatal("sameFile does not tell the download from the copy")
	}

	// A source that is gone leaves the program that was there.
	if err := installCopy(filepath.Join(dir, "missing.exe"), target); err == nil {
		t.Fatal("copying a missing file gave no error")
	}
	if got := read(target); got != "build 2" {
		t.Fatalf("after a failed copy the program in place is %q", got)
	}
}
