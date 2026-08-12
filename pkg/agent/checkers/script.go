package checkers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/agent/executor"
)

// ScriptChecker runs an arbitrary script via the executor package and
// returns a Result based on the exit code and optional expected output.
type ScriptChecker struct{}

func (s *ScriptChecker) Name() string { return "script" }

// Metadata describes the script checker.
func (s *ScriptChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:        "script",
		Version:     "1.0.0",
		Description: "Runs an inline script via the agent executor (bash, python, powershell, node) and checks the exit code and optional expected output.",
		SupportedPlatforms: []string{
			"linux", "darwin", "freebsd", "windows",
		},
	}
}

func (s *ScriptChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	start := time.Now()

	if req.Script == "" && req.Command == "" {
		return &Result{OK: false, Error: "script check requires script or command", Duration: time.Since(start).Milliseconds()}
	}

	timeout := 60 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	opts := executor.Options{
		Timeout: timeout,
		Args:    req.Args,
		Sandbox: executor.EnvSandbox{Enabled: true},
	}

	// Inline script takes precedence; if only a command is given, run it
	// as a one-liner in the detected/default shell.
	if req.Script != "" {
		opts.Script = req.Script
	} else {
		opts.Script = req.Command
		opts.Runtime = executor.RuntimeBash
	}

	if rt, ok := req.Options["runtime"].(string); ok && rt != "" {
		opts.Runtime = executor.Runtime(rt)
	}

	res, err := executor.Execute(ctx, opts)
	if err != nil && res == nil {
		return &Result{
			OK:       false,
			Error:    fmt.Sprintf("script execute: %v", err),
			Duration: time.Since(start).Milliseconds(),
		}
	}

	if res.TimedOut {
		return &Result{
			OK:       false,
			Error:    "script timed out",
			Status:   "timeout",
			Duration: time.Since(start).Milliseconds(),
		}
	}

	if res.Cancelled {
		return &Result{
			OK:       false,
			Error:    "script cancelled",
			Status:   "cancelled",
			Duration: time.Since(start).Milliseconds(),
		}
	}

	ok := res.ExitCode == 0

	message := strings.TrimSpace(res.Stdout)
	if len(message) > 512 {
		message = message[:512] + "..."
	}

	if req.Expected != "" && !strings.Contains(res.Stdout, req.Expected) {
		ok = false
		if message == "" {
			message = "expected output not found"
		}
	}

	return &Result{
		OK:      ok,
		Status:  fmt.Sprintf("exit_code=%d", res.ExitCode),
		Message: message,
		Value: map[string]interface{}{
			"exit_code": res.ExitCode,
			"runtime":   string(res.Runtime),
		},
		Duration: time.Since(start).Milliseconds(),
	}
}
