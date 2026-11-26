//go:build windows

package checkrun

import (
	"os/exec"
	"strconv"
	"syscall"
)

// isolate starts the command in a new process group and kills its process
// tree on cancellation, so a timed-out check leaves no children behind.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// taskkill /T terminates the tree; Kill covers a missing taskkill.
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return cmd.Process.Kill()
	}
}
