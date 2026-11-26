//go:build !windows

package checkrun

import (
	"os/exec"
	"syscall"
)

// isolate starts the command in its own process group and kills the whole
// group on cancellation, so a timed-out check leaves no children behind.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
