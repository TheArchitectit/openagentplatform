package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// streamTo reads from r line-by-line, writing into buf and invoking cb
// (when non-nil) for each line. Partial lines at EOF are still emitted.
func streamTo(r io.Reader, stream string, buf *cappedBuffer, cb func(string, string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')
		if cb != nil {
			cb(stream, line)
		}
	}
}

// installDependency is a best-effort pre-run hook for pip/npm packages.
// It returns a non-empty message on failure (caller logs it but does not
// abort the run).
func installDependency(ctx context.Context, rt Runtime, pkg string) string {
	switch rt {
	case RuntimePython, RuntimePython3:
		cmd := exec.CommandContext(ctx, "python3", "-m", "pip", "install", "--quiet", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("pip install %s failed: %v: %s", pkg, err, strings.TrimSpace(string(out)))
		}
	case RuntimeNode:
		cmd := exec.CommandContext(ctx, "npm", "install", "--silent", "--no-save", pkg)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("npm install %s failed: %v: %s", pkg, err, strings.TrimSpace(string(out)))
		}
	}
	return ""
}

// applySandbox sets cmd.Env to a minimal, predictable set, optionally
// inheriting from the agent's environment where useful (PATH).
func applySandbox(cmd *exec.Cmd, s EnvSandbox) {
	if !s.Enabled {
		// Inherit the agent's environment untouched.
		cmd.Env = os.Environ()
		return
	}
	home := s.Home
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = h
		} else {
			home = os.TempDir()
		}
	}
	temp := s.Temp
	if temp == "" {
		temp = os.TempDir()
	}
	path := s.Path
	if path == "" {
		// Reasonable default per platform.
		if runtime.GOOS == "windows" {
			path = `C:\Windows\System32;C:\Windows`
		} else {
			path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		}
	}
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + path,
		"TEMP=" + temp,
		"TMP=" + temp,
	}
}

// setProcessGroup arranges for the child to be the leader of a new
// process group so that we can signal the entire process group on
// cancel/timeout.
func setProcessGroup(cmd *exec.Cmd) {
	setProcessGroupPlatform(cmd)
}

// killProcessGroup signals the entire process group of cmd. Safe to call
// after the process has already exited.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid)).Run()
		return
	}
	// Negative pid signals the process group.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

// normaliseRuntime maps common aliases to canonical runtime names.
func normaliseRuntime(rt Runtime) Runtime {
	switch strings.ToLower(strings.TrimSpace(string(rt))) {
	case "bash", "sh", "zsh":
		return RuntimeBash
	case "powershell", "pwsh", "ps1", "ps":
		return runtimePowerShell()
	case "python", "python3", "py":
		return RuntimePython3
	case "node", "nodejs", "javascript", "js":
		return RuntimeNode
	case "cmd", "batch", "bat":
		return RuntimeCmd
	}
	return rt
}

// runtimePowerShell picks powershell.exe on Windows and pwsh elsewhere.
func runtimePowerShell() Runtime {
	if runtime.GOOS == "windows" {
		return RuntimePowerShell
	}
	return RuntimePwsh
}

// cappedBuffer is a thread-safe bytes.Buffer that stops growing once it
// reaches MaxOutputBytes, so a misbehaving process can't exhaust memory.
type cappedBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf)+len(p) <= MaxOutputBytes {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	// Fill remaining capacity, then start dropping into a fixed-size ring
	// of the most recent MaxOutputBytes bytes.
	remaining := MaxOutputBytes - len(b.buf)
	if remaining > 0 {
		b.buf = append(b.buf, p[:remaining]...)
		p = p[remaining:]
	}
	// Shift and append for the overflow tail.
	if len(b.buf) == MaxOutputBytes {
		copy(b.buf, b.buf[len(p):])
		b.buf = append(b.buf[:0], b.buf...)
		b.buf = append(b.buf, p...)
	}
	return len(p), nil
}

func (b *cappedBuffer) WriteString(s string) {
	_, _ = b.Write([]byte(s))
}

func (b *cappedBuffer) WriteByte(c byte) error {
	_, _ = b.Write([]byte{c})
	return nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
