package schedule

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Install writes the job as a launchd agent and starts it. A job with the
// same label is replaced. When launchd does not take it, nothing is left that
// would start it at the next sign-in.
func Install(job Job) error {
	if err := check(job); err != nil {
		return err
	}
	path, err := plistPath(job.Label)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, plist(job), 0o644); err != nil {
		return err
	}
	bootout(job.Label)
	if out, err := exec.Command("launchctl", "bootstrap", domain(), path).CombinedOutput(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("launchctl bootstrap: %s", firstLine(out, err))
	}
	return nil
}

// check refuses a program launchd could not start later. An app opened
// straight from its disk image or its download runs from a path that is gone
// after a restart.
func check(job Job) error {
	if strings.Contains(job.Program, "/AppTranslocation/") {
		return errors.New("this app runs from its download; move it to Applications, open it there and try again")
	}
	return nil
}

// bootout takes the job out of launchd and waits until it is gone: launchd
// answers before a running job has ended, and refuses to load a label it
// still holds.
func bootout(label string) {
	if exec.Command("launchctl", "bootout", target(label)).Run() != nil {
		return
	}
	for range 50 {
		if loaded, _ := Installed(label); !loaded {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Uninstall stops the job and removes it. A job that is not there is not an error.
func Uninstall(label string) error {
	path, err := plistPath(label)
	if err != nil {
		return err
	}
	bootout(label)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Installed reports whether launchd holds the job.
func Installed(label string) (bool, error) {
	return exec.Command("launchctl", "print", target(label)).Run() == nil, nil
}

func domain() string             { return fmt.Sprintf("gui/%d", os.Getuid()) }
func target(label string) string { return domain() + "/" + label }

func plistPath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func plist(job Job) []byte {
	var b bytes.Buffer
	str := func(s string) {
		b.WriteString("<string>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</string>")
	}
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0"><dict>` + "\n<key>Label</key>")
	str(job.Label)
	b.WriteString("\n<key>ProgramArguments</key><array>")
	for _, arg := range append([]string{job.Program}, job.Args...) {
		str(arg)
	}
	b.WriteString("</array>\n<key>RunAtLoad</key><true/>\n")
	fmt.Fprintf(&b, "<key>StartInterval</key><integer>%d</integer>\n", int(job.Every.Seconds()))
	b.WriteString("<key>ProcessType</key><string>Background</string>\n</dict></plist>\n")
	return b.Bytes()
}

func firstLine(out []byte, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n"); line != "" {
		return line
	}
	return err.Error()
}
