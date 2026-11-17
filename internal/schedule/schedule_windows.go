package schedule

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
	"unicode/utf16"
)

// Install creates the job as a task in Task Scheduler and starts it. A task
// with the same name is replaced.
//
// The task is made from XML because the command line form cannot say that it
// may run on battery, which is where a laptop spends its day.
func Install(job Job) error {
	account, err := user.Current()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "wacli-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(utf16File(taskXML(job, account.Username)))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if out, err := exec.Command("schtasks", "/Create", "/F", "/TN", job.Label, "/XML", file.Name()).CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Create: %s", firstLine(out, err))
	}
	if out, err := exec.Command("schtasks", "/Run", "/TN", job.Label).CombinedOutput(); err != nil {
		_ = Uninstall(job.Label)
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

// taskXML is the task: every Every from a start in the past, also on battery,
// caught up after the computer was off or asleep, one run at a time, with the
// rights of the signed-in user.
func taskXML(job Job, account string) string {
	text := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	args := make([]string, len(job.Args))
	for i, arg := range job.Args {
		args[i] = syscall.EscapeArg(arg)
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <TimeTrigger>
      <Repetition><Interval>` + fmt.Sprintf("PT%dM", int(job.Every.Minutes())) + `</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition>
      <StartBoundary>2026-01-01T00:00:00</StartBoundary>
      <Enabled>true</Enabled>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + text(account) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + text(job.Program) + `</Command>
      <Arguments>` + text(strings.Join(args, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// utf16File is the text as schtasks reads a task file: UTF-16, little endian,
// with a byte order mark.
func utf16File(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, unit := range units {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return out
}

func firstLine(out []byte, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n"); line != "" {
		return strings.TrimSpace(line)
	}
	return err.Error()
}
