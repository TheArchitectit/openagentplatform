//go:build windows

package executor

import (
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP = 0x00000200
const createNewProcessGroup = 0x00000200

func setProcessGroupPlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup,
	}
}
