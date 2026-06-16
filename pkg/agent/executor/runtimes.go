package executor

import (
	"context"
	"os/exec"
	"runtime"
)

// BashExecutor runs scripts via /bin/bash. The temp script is always
// invoked with -c, which keeps the call site uniform across inline and
// file-based payloads.
type BashExecutor struct {
	interpreter string
}

// NewBashExecutor probes the host for a usable bash interpreter. It
// always returns a non-nil executor; Available() reports whether the
// interpreter was found.
func NewBashExecutor() *BashExecutor {
	candidates := []string{"bash"}
	if runtime.GOOS == "windows" {
		candidates = []string{"bash", "C:\\Program Files\\Git\\bin\\bash.exe", "C:\\Windows\\System32\\bash.exe"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return &BashExecutor{interpreter: p}
		}
	}
	return &BashExecutor{interpreter: "bash"}
}

func (b *BashExecutor) Runtime() Runtime   { return RuntimeBash }
func (b *BashExecutor) Extension() string  { return "sh" }
func (b *BashExecutor) Interpreter() string { return b.interpreter }
func (b *BashExecutor) Available() bool {
	_, err := exec.LookPath(b.interpreter)
	return err == nil
}

func (b *BashExecutor) BuildArgs(scriptPath string) []string {
	return []string{scriptPath}
}

func (b *BashExecutor) Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd {
	return exec.CommandContext(ctx, b.interpreter, scriptPath)
}

// PowerShellExecutor runs scripts via powershell.exe (Windows) or pwsh
// (Linux/macOS). It always passes -NoProfile and -ExecutionPolicy Bypass
// so the agent can run scripts in locked-down environments.
type PowerShellExecutor struct {
	interpreter string
	rt          Runtime
}

// NewPowerShellExecutor probes for the platform-appropriate PowerShell.
func NewPowerShellExecutor() *PowerShellExecutor {
	if runtime.GOOS == "windows" {
		for _, c := range []string{"powershell.exe", "powershell"} {
			if p, err := exec.LookPath(c); err == nil {
				return &PowerShellExecutor{interpreter: p, rt: RuntimePowerShell}
			}
		}
		return &PowerShellExecutor{interpreter: "powershell.exe", rt: RuntimePowerShell}
	}
	for _, c := range []string{"pwsh", "pwsh-preview"} {
		if p, err := exec.LookPath(c); err == nil {
			return &PowerShellExecutor{interpreter: p, rt: RuntimePwsh}
		}
	}
	return &PowerShellExecutor{interpreter: "pwsh", rt: RuntimePwsh}
}

func (p *PowerShellExecutor) Runtime() Runtime   { return p.rt }
func (p *PowerShellExecutor) Extension() string  { return "ps1" }
func (p *PowerShellExecutor) Interpreter() string { return p.interpreter }
func (p *PowerShellExecutor) Available() bool {
	_, err := exec.LookPath(p.interpreter)
	return err == nil
}

func (p *PowerShellExecutor) BuildArgs(scriptPath string) []string {
	return []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-NonInteractive", "-File", scriptPath}
}

func (p *PowerShellExecutor) Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd {
	return exec.CommandContext(ctx, p.interpreter,
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-NonInteractive",
		"-File", scriptPath,
	)
}

// PythonExecutor runs scripts via python3 (or python on Windows).
type PythonExecutor struct {
	interpreter string
}

// NewPythonExecutor probes for python3 / python.
func NewPythonExecutor() *PythonExecutor {
	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "python3", "py"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return &PythonExecutor{interpreter: p}
		}
	}
	return &PythonExecutor{interpreter: "python3"}
}

func (p *PythonExecutor) Runtime() Runtime   { return RuntimePython3 }
func (p *PythonExecutor) Extension() string  { return "py" }
func (p *PythonExecutor) Interpreter() string { return p.interpreter }
func (p *PythonExecutor) Available() bool {
	_, err := exec.LookPath(p.interpreter)
	return err == nil
}

func (p *PythonExecutor) BuildArgs(scriptPath string) []string {
	return []string{scriptPath}
}

func (p *PythonExecutor) Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd {
	return exec.CommandContext(ctx, p.interpreter, scriptPath)
}

// NodeExecutor runs scripts via node.
type NodeExecutor struct {
	interpreter string
}

// NewNodeExecutor probes for node / nodejs.
func NewNodeExecutor() *NodeExecutor {
	for _, c := range []string{"node", "nodejs"} {
		if p, err := exec.LookPath(c); err == nil {
			return &NodeExecutor{interpreter: p}
		}
	}
	return &NodeExecutor{interpreter: "node"}
}

func (n *NodeExecutor) Runtime() Runtime   { return RuntimeNode }
func (n *NodeExecutor) Extension() string  { return "js" }
func (n *NodeExecutor) Interpreter() string { return n.interpreter }
func (n *NodeExecutor) Available() bool {
	_, err := exec.LookPath(n.interpreter)
	return err == nil
}

func (n *NodeExecutor) BuildArgs(scriptPath string) []string {
	return []string{scriptPath}
}

func (n *NodeExecutor) Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd {
	return exec.CommandContext(ctx, n.interpreter, scriptPath)
}
