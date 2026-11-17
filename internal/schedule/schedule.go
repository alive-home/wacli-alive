// Package schedule has the operating system start a program every few minutes
// for the signed-in user: a launchd agent on macOS, a task in Task Scheduler
// on Windows. The job belongs to the user; nothing here needs an administrator.
package schedule

import (
	"errors"
	"time"
)

// ErrUnsupported is returned where this package has no scheduler to use.
var ErrUnsupported = errors.New("no scheduler for this operating system; start the command from cron or a systemd timer")

// Job is one program the system starts at once and then every Every, for as
// long as the user is signed in. A run that is still going is not started a
// second time.
type Job struct {
	Label   string // the job's name in the system's scheduler
	Program string // full path of the program
	Args    []string
	Every   time.Duration
}
