package checkers

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ServiceChecker reports the status of an OS service.
//
// It detects the platform's init system at runtime and uses the appropriate
// query command:
//   - Linux (systemd):  systemctl is-active <name>
//   - Linux (sysvinit/upstart):  service <name> status  (or `initctl status`)
//   - Linux (OpenRC):   rc-service <name> status
//   - Darwin (launchd): launchctl list <name>
//   - FreeBSD (rc):     service <name> status
//   - Windows:          sc query <name>
type ServiceChecker struct{}

func (s *ServiceChecker) Name() string { return "service" }

// Metadata describes the service checker.
func (s *ServiceChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:        "service",
		Version:     "1.1.0",
		Description: "Reports the status of an OS-managed service (systemd, init.d, launchd, sc).",
		SupportedPlatforms: []string{
			"linux", "darwin", "freebsd", "windows",
		},
	}
}

// detectInit picks the appropriate init system query command for the host.
// Returns the command args to run and a human-readable label.
func detectInit(target string) (args []string, label string) {
	switch runtime.GOOS {
	case "windows":
		return []string{"sc", "query", target}, "sc"
	case "darwin":
		return []string{"launchctl", "list", target}, "launchd"
	case "freebsd", "netbsd", "openbsd":
		return []string{"service", target, "status"}, "rc"
	default:
		// Linux — pick the first init system whose binary is on PATH.
		for _, cand := range []struct {
			bin   string
			args  []string
			label string
		}{
			{"systemctl", []string{"systemctl", "is-active", target}, "systemd"},
			{"rc-service", []string{"rc-service", target, "status"}, "openrc"},
			{"initctl", []string{"initctl", "status", target}, "upstart"},
			{"service", []string{"service", target, "status"}, "sysvinit"},
		} {
			if _, err := exec.LookPath(cand.bin); err == nil {
				return cand.args, cand.label
			}
		}
		// No init tool found — fall back to systemctl and let it fail.
		return []string{"systemctl", "is-active", target}, "systemd(unavailable)"
	}
}

func (s *ServiceChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "service check requires target (service name)"}
	}
	timeout := 10 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	start := time.Now()

	args, initLabel := detectInit(req.Target)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	timeoutCh := time.After(timeout)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case <-ctx.Done():
		return &Result{OK: false, Error: "service check cancelled", Duration: time.Since(start).Milliseconds()}
	case <-timeoutCh:
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return &Result{OK: false, Error: "service check timeout", Duration: time.Since(start).Milliseconds()}
	case err := <-done:
		_ = err
		ok := cmd.ProcessState != nil && cmd.ProcessState.Success()
		status := "inactive"
		if ok {
			status = "active"
		}
		_ = strings.TrimSpace
		return &Result{
			OK:       ok,
			Status:   status,
			Message:  "service " + req.Target + " (via " + initLabel + ")",
			Duration: time.Since(start).Milliseconds(),
		}
	}
}
