//go:build unix

package agent

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the agent in its own process group so cancelling
// also stops the shells and tools it spawned.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
