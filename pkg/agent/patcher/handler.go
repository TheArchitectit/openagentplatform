package patcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// PatchScanCommand is the payload the server sends to the agent on
// oap.agents.<agentID>.patch_scan. The agent runs the scanner and
// publishes the result on oap.agents.<agentID>.patch_scan.results.
type PatchScanCommand struct {
	RequestID  string `json:"request_id"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
	// Force forces a fresh scan even if a recent result is cached.
	Force bool `json:"force,omitempty"`
}

// PatchScanResultEnvelope is the payload the agent publishes back to
// the server after running a scan.
type PatchScanResultEnvelope struct {
	RequestID  string      `json:"request_id"`
	AgentID    string      `json:"agent_id"`
	OS         string      `json:"os"`
	Scanner    string      `json:"scanner"`
	Patches    []PatchInfo `json:"patches"`
	Error      string      `json:"error,omitempty"`
	ReceivedAt time.Time   `json:"received_at"`
	DurationMs int64       `json:"duration_ms"`
}

// PatchInstallCommand is the payload the server sends to the agent
// on oap.agents.<agentID>.patch_install. The agent installs the
// named patch and publishes the result on the install result subject.
type PatchInstallCommand struct {
	RequestID  string     `json:"request_id"`
	Patch      *PatchInfo `json:"patch"`
	TimeoutSec int        `json:"timeout_sec,omitempty"`
}

// PatchInstallResultEnvelope is the result of a patch install.
type PatchInstallResultEnvelope struct {
	RequestID string        `json:"request_id"`
	AgentID   string        `json:"agent_id"`
	Result    *InstallResult `json:"result"`
	Error     string        `json:"error,omitempty"`
	DurationMs int64        `json:"duration_ms"`
	ReceivedAt time.Time    `json:"received_at"`
}

// PatchScanSubject returns the NATS subject the server uses to
// request a scan from this agent.
func PatchScanSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.patch_scan", agentID)
}

// PatchScanResultSubject returns the NATS subject the agent publishes
// scan results on.
func PatchScanResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.patch_scan.results", agentID)
}

// PatchInstallSubject returns the NATS subject the server uses to
// request a patch install on this agent.
func PatchInstallSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.patch_install", agentID)
}

// PatchInstallResultSubject returns the NATS subject the agent
// publishes install results on.
func PatchInstallResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.patch_install.results", agentID)
}

// Handler is the agent-side dispatcher for patch scan and install
// commands. It owns the scanner + installer and the NATS
// subscriptions for both subjects.
type Handler struct {
	agentID   string
	scanner   PatchScanner
	installer PatchInstaller
	nc        *nats.Conn
	log       *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewHandler creates a handler with the default AutoScanner and
// AutoInstaller. Callers can override the scanner or installer via
// the Set methods before Run is called.
func NewHandler(agentID string, nc *nats.Conn, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		agentID:   agentID,
		scanner:   NewAutoScanner(),
		installer: NewAutoInstaller(nil),
		nc:        nc,
		log:       log,
	}
}

// SetScanner overrides the default scanner.
func (h *Handler) SetScanner(s PatchScanner) {
	if s == nil {
		return
	}
	h.scanner = s
}

// SetInstaller overrides the default installer.
func (h *Handler) SetInstaller(i PatchInstaller) {
	if i == nil {
		return
	}
	h.installer = i
}

// Close marks the handler closed. The subscriptions themselves are
// managed by the caller (the agent's main loop) and should be
// unsubscribed by that caller.
func (h *Handler) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

// RunScanHandler subscribes to the per-agent patch_scan subject and
// dispatches each message to the scanner. The subscription is owned
// by the caller; call .Unsubscribe() to tear it down.
func (h *Handler) RunScanHandler(ctx context.Context) (*nats.Subscription, error) {
	subject := PatchScanSubject(h.agentID)
	sub, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		h.handleScan(ctx, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("patch scan subscribe %s: %w", subject, err)
	}
	h.log.Info("patch scan handler subscribed", "subject", subject)
	return sub, nil
}

// RunInstallHandler subscribes to the per-agent patch_install
// subject and dispatches each message to the installer.
func (h *Handler) RunInstallHandler(ctx context.Context) (*nats.Subscription, error) {
	subject := PatchInstallSubject(h.agentID)
	sub, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		h.handleInstall(ctx, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("patch install subscribe %s: %w", subject, err)
	}
	h.log.Info("patch install handler subscribed", "subject", subject)
	return sub, nil
}

func (h *Handler) handleScan(parent context.Context, msg *nats.Msg) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	var cmd PatchScanCommand
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Warn("patch scan: bad payload", "err", err)
		return
	}
	if cmd.RequestID == "" {
		cmd.RequestID = uuid.NewString()
	}

	timeout := 60 * time.Second
	if cmd.TimeoutSec > 0 {
		timeout = time.Duration(cmd.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()
	h.log.Info("patch scan: starting", "request_id", cmd.RequestID, "scanner", h.scanner.Name())

	patches, err := h.scanner.Scan(ctx)
	env := PatchScanResultEnvelope{
		RequestID:  cmd.RequestID,
		AgentID:    h.agentID,
		OS:         runtime.GOOS,
		Scanner:    h.scanner.Name(),
		Patches:    patches,
		ReceivedAt: time.Now(),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		env.Error = err.Error()
		h.log.Warn("patch scan: scanner failed",
			"request_id", cmd.RequestID, "err", err)
	} else {
		h.log.Info("patch scan: complete",
			"request_id", cmd.RequestID, "count", len(patches))
	}

	payload, err := json.Marshal(env)
	if err != nil {
		h.log.Warn("patch scan: marshal result failed", "err", err)
		return
	}
	if err := h.nc.Publish(PatchScanResultSubject(h.agentID), payload); err != nil {
		h.log.Warn("patch scan: publish result failed", "err", err)
	}
}

func (h *Handler) handleInstall(parent context.Context, msg *nats.Msg) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	var cmd PatchInstallCommand
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Warn("patch install: bad payload", "err", err)
		return
	}
	if cmd.RequestID == "" {
		cmd.RequestID = uuid.NewString()
	}
	if cmd.Patch == nil {
		h.log.Warn("patch install: missing patch", "request_id", cmd.RequestID)
		return
	}

	timeout := 5 * time.Minute
	if cmd.TimeoutSec > 0 {
		timeout = time.Duration(cmd.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	start := time.Now()
	h.log.Info("patch install: starting",
		"request_id", cmd.RequestID,
		"package", cmd.Patch.Name,
		"manager", cmd.Patch.PackageManager)

	result, err := h.installer.Install(ctx, cmd.Patch)
	env := PatchInstallResultEnvelope{
		RequestID:  cmd.RequestID,
		AgentID:    h.agentID,
		Result:     result,
		ReceivedAt: time.Now(),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		env.Error = err.Error()
		h.log.Warn("patch install: failed",
			"request_id", cmd.RequestID, "err", err)
	} else {
		h.log.Info("patch install: complete",
			"request_id", cmd.RequestID,
			"success", result.Success,
			"reboot_required", result.RebootRequired)
	}

	payload, jerr := json.Marshal(env)
	if jerr != nil {
		h.log.Warn("patch install: marshal result failed", "err", jerr)
		return
	}
	if perr := h.nc.Publish(PatchInstallResultSubject(h.agentID), payload); perr != nil {
		h.log.Warn("patch install: publish result failed", "err", perr)
	}
}
