// Package executor provides a cross-platform, multi-runtime script executor
// for the OAP agent. It abstracts over Bash, PowerShell, Python, and Node.js,
// handling temp-file lifecycle, environment isolation, output streaming,
// timeout enforcement, and cancellation.
//
// The core entry point is ScriptExecutor.Execute, which builds a context with
// a per-run timeout, prepares the command (via a temp file when needed),
// captures stdout/stderr line-by-line, and reports the final result with
// exit code and truncated output.
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

// Runtime identifies a supported scripting runtime.
type Runtime string

const (
	RuntimeBash       Runtime = "bash"
	RuntimeShell      Runtime = "sh"
	RuntimePowerShell Runtime = "powershell"
	RuntimePwsh       Runtime = "pwsh"
	RuntimePython     Runtime = "python"
	RuntimePython3    Runtime = "python3"
	RuntimeNode       Runtime = "node"
	RuntimeCmd        Runtime = "cmd"
)

// MaxOutputBytes caps the total output returned in a Result.
const MaxOutputBytes = 64 * 1024

// MaxLineBytes caps the size of a single line scanned from process output.
const MaxLineBytes = 1024 * 1024

// EnvSandbox controls the environment isolation applied before every run.
// When enabled, the process is started with a minimal, predictable environment
// (HOME, PATH, TEMP/TMP) plus any caller-supplied overrides. When disabled,
// the process inherits the agent's full environment (minus a denylist of
// secrets when AllowInherit is false).
type EnvSandbox struct {
	// Enabled toggles sandboxing on. If false, the agent's environment is
	// inherited verbatim.
	Enabled bool
	// Home, Temp, Path override the corresponding env vars inside the
	// sandbox. Empty means "use a sensible default derived from the OS".
	Home string
	Temp string
	Path string
}

// Options control a single Execute call. All fields are optional; zero
// values are replaced with safe defaults.
type Options struct {
	// Runtime is the scripting runtime to use. If empty, it is inferred
	// from the script's shebang or content.
	Runtime Runtime
	// Script is the inline source to execute. Either Script or ScriptFile
	// must be set (ScriptFile takes precedence when both are provided).
	Script string
	// ScriptFile, if non-empty, is the path to a file already on disk that
	// should be executed. The file is copied into a temp dir and deleted
	// after the run.
	ScriptFile string
	// Args are appended after the interpreter's flags.
	Args []string
	// Env is merged into the sandboxed environment.
	Env map[string]string
	// Dependencies lists pip/npm packages that should be installed before
	// running. Currently best-effort: logged and best-effort, but not
	// enforced as a hard failure.
	Dependencies []string
	// Timeout is the maximum wall-clock duration for the run. Zero means
	// no timeout (not recommended in production).
	Timeout time.Duration
	// Sandbox controls environment isolation.
	Sandbox EnvSandbox
	// OutputCallback is invoked for each captured line of stdout/stderr.
	// It may be nil. The first argument is the stream name ("stdout" or
	// "stderr"), the second is the line text (newline-trimmed).
	OutputCallback func(stream, line string)
}

// Result is the final outcome of a single Execute call.
type Result struct {
	Runtime   Runtime
	ExitCode  int
	Stdout    string // truncated to MaxOutputBytes
	Stderr    string // truncated to MaxOutputBytes
	Duration  time.Duration
	TimedOut  bool
	Cancelled bool
	Error     string // non-empty if the run could not be started
}

// ScriptExecutor is the contract implemented by each per-runtime executor.
type ScriptExecutor interface {
	// Runtime returns the runtime name (e.g. "bash", "python").
	Runtime() Runtime
	// Available reports whether the runtime's interpreter is present on
	// the current host.
	Available() bool
	// BuildArgs returns the per-runtime flag sequence (excluding the
	// interpreter path itself) used to invoke a script at scriptPath.
	// Callers append any user-supplied args after the returned slice.
	BuildArgs(scriptPath string) []string
	// Command constructs the *exec.Cmd that will run the script. The
	// caller is responsible for starting, waiting, and signalling it.
	// The returned Cmd uses ctx for cancellation.
	Command(ctx context.Context, scriptPath string, opts Options) *exec.Cmd
	// Extension is the file extension (without the dot) for temp scripts.
	Extension() string
	// Interpreter is the absolute path or binary name of the interpreter.
	Interpreter() string
}

// Registry maps runtime names to their executor implementations. The
// DefaultRegistry is populated with every supported runtime at startup.
type Registry struct {
	mu        sync.RWMutex
	executors map[Runtime]ScriptExecutor
}

// NewRegistry returns an empty registry. Use DefaultRegistry for a ready-to-go
// instance with all built-in runtimes.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[Runtime]ScriptExecutor)}
}

// DefaultRegistry is initialized lazily on first call to Default.
var (
	defaultOnce  sync.Once
	defaultReg   *Registry
	defaultRegMu sync.Mutex
)

// Default returns the process-wide default registry, building it on first
// call. The registry contains one executor per supported runtime; runtimes
// that are not present on the host are still registered but will report
// Available() == false.
func Default() *Registry {
	defaultOnce.Do(func() {
		r := NewRegistry()
		r.Register(NewBashExecutor())
		r.Register(NewPowerShellExecutor())
		r.Register(NewPythonExecutor())
		r.Register(NewNodeExecutor())
		defaultReg = r
	})
	return defaultReg
}

// Register adds or replaces an executor for a runtime.
func (r *Registry) Register(e ScriptExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[e.Runtime()] = e
}

// Get returns the executor for the given runtime, or nil if not registered.
func (r *Registry) Get(rt Runtime) ScriptExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executors[rt]
}

// Available returns the set of runtimes whose interpreter is present on
// this host. Useful for startup logging.
func (r *Registry) Available() []Runtime {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Runtime, 0, len(r.executors))
	for rt, e := range r.executors {
		if e.Available() {
			out = append(out, rt)
		}
	}
	return out
}

// DetectRuntime picks a runtime by inspecting the script's content.
// Falls back to bash. Mirrors the heuristic used in the original agent.
func DetectRuntime(script, url string) Runtime {
	lower := strings.ToLower(script + " " + url)
	switch {
	case strings.HasPrefix(lower, "#!/usr/bin/env python"), strings.HasPrefix(lower, "#!/usr/bin/python"), strings.Contains(lower, "import "):
		return RuntimePython3
	case strings.HasPrefix(lower, "#!/bin/bash"), strings.HasPrefix(lower, "#!/usr/bin/env bash"):
		return RuntimeBash
	case strings.HasPrefix(lower, "#!/usr/bin/env node"):
		return RuntimeNode
	case strings.HasPrefix(lower, "$"), strings.Contains(lower, "get-"):
		return RuntimePowerShell
	}
	return RuntimeBash
}

// Execute runs a script using the appropriate runtime executor from the
// default registry. It is a convenience wrapper around ExecuteWith.
func Execute(ctx context.Context, opts Options) (*Result, error) {
	return ExecuteWith(ctx, Default(), opts)
}

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
// cancel/timeout. The platform-specific bits live in sysproc_unix.go
// and sysproc_windows.go.
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
	off int // write offset within buf once capped
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
