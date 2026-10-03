//go:build darwin || linux

package archive

import (
	"os"
	"os/exec"
	"syscall"
)

func configureNativeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}

// WaitDelay can finish after the leader exits while a descendant still owns a
// pipe. Terminate that inherited group before releasing the shared helper slot.
func finishNativeProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
