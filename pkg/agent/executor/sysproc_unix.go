//go:build !windows

package executor

import (
	"os/exec"
	"syscall"
)

func setProcessGroupPlatform(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
