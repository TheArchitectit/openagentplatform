package executor

import (
	"context"
	"os/exec"
	"strings"
	"sync"
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
