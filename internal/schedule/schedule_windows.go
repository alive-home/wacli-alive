package schedule

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Install creates the job as a task in Task Scheduler and starts it. A task
// with the same name is replaced.
func Install(job Job) error {
	out, err := exec.Command("schtasks", "/Create", "/F", "/TN", job.Label,
		"/SC", "MINUTE", "/MO", strconv.Itoa(int(job.Every.Minutes())), "/TR", commandLine(job)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create: %s", firstLine(out, err))
	}
	if out, err := exec.Command("schtasks", "/Run", "/TN", job.Label).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Run: %s", firstLine(out, err))
	}
	return nil
}

// Uninstall removes the task. A task that is not there is not an error.
func Uninstall(label string) error {
	if ok, _ := Installed(label); !ok {
		return nil
	}
	if out, err := exec.Command("schtasks", "/Delete", "/TN", label, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Delete: %s", firstLine(out, err))
	}
	return nil
}

// Installed reports whether Task Scheduler holds the task.
func Installed(label string) (bool, error) {
	return exec.Command("schtasks", "/Query", "/TN", label).Run() == nil, nil
}

// commandLine is the program and its arguments as one line, each part that
// holds a space in quotes.
func commandLine(job Job) string {
	parts := make([]string, 0, len(job.Args)+1)
	for _, part := range append([]string{job.Program}, job.Args...) {
		if strings.ContainsAny(part, " \t") {
			part = `"` + part + `"`
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func firstLine(out []byte, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n"); line != "" {
		return strings.TrimSpace(line)
	}
	return err.Error()
}
