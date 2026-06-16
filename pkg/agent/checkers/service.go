package checkers

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ServiceChecker reports the status of an OS service.
type ServiceChecker struct{}

func (s *ServiceChecker) Name() string { return "service" }

func (s *ServiceChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "service check requires target (service name)"}
	}
	timeout := 10 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	start := time.Now()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// sc query returns "RUNNING" when active.
		cmd = exec.CommandContext(ctx, "sc", "query", req.Target)
	default:
		// systemctl is-active <name> exits 0 when active.
		cmd = exec.CommandContext(ctx, "systemctl", "is-active", req.Target)
	}
	timeoutCh := time.After(timeout)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case <-ctx.Done():
		return &Result{OK: false, Error: "service check cancelled", Duration: time.Since(start).Milliseconds()}
	case <-timeoutCh:
		_ = cmd.Process.Kill()
		return &Result{OK: false, Error: "service check timeout", Duration: time.Since(start).Milliseconds()}
	case err := <-done:
		_ = err
		ok := cmd.ProcessState != nil && cmd.ProcessState.Success()
		status := "unknown"
		if ok {
			status = "active"
		} else {
			status = "inactive"
		}
		_ = strings.TrimSpace
		return &Result{
			OK:       ok,
			Status:   status,
			Message:  "service " + req.Target,
			Duration: time.Since(start).Milliseconds(),
		}
	}
}
