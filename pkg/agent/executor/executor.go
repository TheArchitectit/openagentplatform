package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
