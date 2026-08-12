package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ExecuteWith runs a script using the provided registry. It handles
// runtime selection, temp-file management, environment isolation, line
// streaming, timeout enforcement, and process-group cancellation.
func ExecuteWith(ctx context.Context, reg *Registry, opts Options) (*Result, error) {
	res := &Result{}

	// 1. Resolve runtime.
	rt := opts.Runtime
	if rt == "" {
		rt = DetectRuntime(opts.Script, opts.ScriptFile)
	}
	rt = normaliseRuntime(rt)
	rtExec := reg.Get(rt)
	if rtExec == nil {
		res.Runtime = rt
		res.Error = fmt.Sprintf("unsupported runtime: %q", rt)
		res.ExitCode = -1
		return res, fmt.Errorf("unsupported runtime: %q", rt)
	}
	if !rtExec.Available() {
		res.Runtime = rt
		res.Error = fmt.Sprintf("runtime %q not available on this host", rt)
		res.ExitCode = -1
		return res, fmt.Errorf("%s", res.Error)
	}
	res.Runtime = rt

	// 2. Apply timeout.
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// 3. Stage the script to a temp file (we always use a temp file so
	//    that long scripts and binary content are handled uniformly and
	//    we never leave scripts on disk after the run).
	tmpDir, err := os.MkdirTemp("", "oap-script-*")
	if err != nil {
		res.Error = "tempdir: " + err.Error()
		return res, err
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "script."+rtExec.Extension())
	scriptBody := opts.Script
	if scriptBody == "" && opts.ScriptFile != "" {
		data, err := os.ReadFile(opts.ScriptFile)
		if err != nil {
			res.Error = "read script: " + err.Error()
			return res, err
		}
		scriptBody = string(data)
	}
	if scriptBody == "" {
		res.Error = "empty script"
		return res, errors.New(res.Error)
	}
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o600); err != nil {
		res.Error = "write temp script: " + err.Error()
		return res, err
	}

	// 4. Best-effort dependency install (non-fatal; logged via error field).
	for _, dep := range opts.Dependencies {
		if msg := installDependency(runCtx, rt, dep); msg != "" {
			res.Stderr += msg + "\n"
		}
	}

	// 5. Build the command. We delegate flag shaping to the executor and
	//    append any caller-supplied args after the script path.
	cmd := exec.CommandContext(runCtx, rtExec.Interpreter())
	cmd.Args = append([]string{cmd.Path}, rtExec.BuildArgs(scriptPath)...)
	cmd.Args = append(cmd.Args, opts.Args...)
	applySandbox(cmd, opts.Sandbox)
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Force a new process group so we can kill children on timeout/cancel.
	setProcessGroup(cmd)

	// 6. Pipe stdout/stderr.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		res.Error = "stdout pipe: " + err.Error()
		return res, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		res.Error = "stderr pipe: " + err.Error()
		return res, err
	}

	// 7. Start and stream output.
	start := time.Now()
	if err := cmd.Start(); err != nil {
		res.Error = "start: " + err.Error()
		res.Duration = time.Since(start)
		return res, err
	}

	var (
		stdoutBuf cappedBuffer
		stderrBuf cappedBuffer
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		streamTo(stdout, "stdout", &stdoutBuf, opts.OutputCallback)
	}()
	go func() {
		defer wg.Done()
		streamTo(stderr, "stderr", &stderrBuf, opts.OutputCallback)
	}()

	// 8. Wait, applying timeout/cancel semantics.
	waitErr := cmd.Wait()
	wg.Wait()
	res.Duration = time.Since(start)
	res.Stdout = stdoutBuf.String()
	res.Stderr = stderrBuf.String()

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.ExitCode = -1
		killProcessGroup(cmd)
	case errors.Is(runCtx.Err(), context.Canceled):
		res.Cancelled = true
		res.ExitCode = -1
		killProcessGroup(cmd)
	case waitErr != nil:
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Error = waitErr.Error()
		}
	default:
		res.ExitCode = 0
	}
	return res, nil
}

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
