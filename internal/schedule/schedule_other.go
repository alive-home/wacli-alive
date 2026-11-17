//go:build !darwin && !windows

package schedule

func Install(Job) error              { return ErrUnsupported }
func Uninstall(string) error         { return ErrUnsupported }
func Installed(string) (bool, error) { return false, ErrUnsupported }
