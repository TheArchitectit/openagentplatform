package executor

import (
	"context"
	"os/exec"
	"runtime"
)

// CmdExecutor runs Windows batch scripts via cmd.exe. It is registered so
// the "cmd" runtime (aliases "batch"/"bat" after normalisation) executes
// instead of failing with "unsupported runtime" — previously the runtime
// was recognised but never had an executor registered.
type CmdExecutor struct {
	interpreter string
}

// NewCmdExecutor probes for cmd.exe. It always returns a non-nil
// executor; Available() reports false on non-Windows hosts.
func NewCmdExecutor() *CmdExecutor {
	candidates := []string{"cmd.exe", "cmd"}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return &CmdExecutor{interpreter: p}
		}
	}
	return &CmdExecutor{interpreter: "cmd.exe"}
}

func (c *CmdExecutor) Runtime() Runtime    { return RuntimeCmd }
func (c *CmdExecutor) Extension() string   { return "bat" }
func (c *CmdExecutor) Interpreter() string { return c.interpreter }
func (c *CmdExecutor) Available() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	_, err := exec.LookPath(c.interpreter)
	return err == nil
}

func (c *CmdExecutor) BuildArgs(scriptPath string) []string {
	return []string{"/c", scriptPath}
}

func (c *CmdExecutor) Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd {
	return exec.CommandContext(ctx, c.interpreter, "/c", scriptPath)
}
