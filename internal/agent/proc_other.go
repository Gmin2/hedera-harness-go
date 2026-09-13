//go:build !unix

package agent

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}
