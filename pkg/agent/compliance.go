package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/policy/collectors"
)

// ComplianceCommand is the payload the server sends to the agent on
// oap.agents.<agentID>.compliance. The agent runs the named collector
// and publishes the result on oap.agents.<agentID>.compliance.results.
type ComplianceCommand struct {
	RequestID  string `json:"request_id"`
	Collector  string `json:"collector"`
	PolicyID   string `json:"policy_id,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// ComplianceResultEnvelope is the payload the agent publishes back
// after running a collector. It mirrors collectors.ComplianceResult
// but lives in the agent package to avoid a circular import.
type ComplianceResultEnvelope struct {
	RequestID  string                    `json:"request_id"`
	AgentID    string                    `json:"agent_id"`
	Collector  string                    `json:"collector"`
	PolicyID   string                    `json:"policy_id,omitempty"`
	Data       *collectors.ComplianceData `json:"data"`
	Error      string                    `json:"error,omitempty"`
	ReceivedAt time.Time                 `json:"received_at"`
}

// ComplianceResultSubject returns the NATS subject the agent publishes
// compliance results on. The server's ComplianceDispatcher
// subscribes to oap.agents.*.compliance.results.
func ComplianceResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.compliance.results", agentID)
}

// ComplianceHandler owns the collector registry and dispatches
// incoming collection requests to the appropriate collector. It is
// created once per agent process.
type ComplianceHandler struct {
	agentID  string
	nc       *NATSClient
	registry *collectors.CollectorRegistry
	log      *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewComplianceHandler creates a handler with the standard set of
// collectors pre-registered. Callers may add additional collectors
// via Registry() before calling Run.
func NewComplianceHandler(agentID string, nc *NATSClient, log *slog.Logger) *ComplianceHandler {
	if log == nil {
		log = slog.Default()
	}
	reg := collectors.NewCollectorRegistry()
	for _, c := range defaultCollectors() {
		_ = reg.Register(c)
	}
	return &ComplianceHandler{
		agentID:  agentID,
		nc:       nc,
		registry: reg,
		log:      log,
	}
}

// Registry returns the underlying collector registry so callers can
// register custom collectors before Run is called.
func (h *ComplianceHandler) Registry() *collectors.CollectorRegistry {
	return h.registry
}

// Run subscribes to the per-agent compliance subject and processes
// requests until ctx is cancelled or the subscription returns an
// error.
func (h *ComplianceHandler) Run(ctx context.Context) (*nats.Subscription, error) {
	subject := collectors.ComplianceRequestSubject(h.agentID)
	sub, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		h.handle(ctx, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("compliance subscribe %s: %w", subject, err)
	}
	h.log.Info("compliance handler subscribed", "subject", subject)
	return sub, nil
}

// handle processes a single compliance request message. It runs the
// named collector, publishes the result on the result subject, and
// does not block other request handling.
func (h *ComplianceHandler) handle(parent context.Context, msg *nats.Msg) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	var cmd ComplianceCommand
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Warn("compliance: bad payload", "err", err)
		return
	}
	if cmd.Collector == "" {
		h.log.Warn("compliance: payload missing collector")
		return
	}
	if cmd.RequestID == "" {
		cmd.RequestID = uuid.NewString()
	}

	timeout := 30 * time.Second
	if cmd.TimeoutSec > 0 {
		timeout = time.Duration(cmd.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	h.log.Info("compliance: running collector",
		"request_id", cmd.RequestID,
		"collector", cmd.Collector,
		"policy_id", cmd.PolicyID)

	data, err := h.registry.Collect(ctx, cmd.Collector, h.agentID)
	env := ComplianceResultEnvelope{
		RequestID:  cmd.RequestID,
		AgentID:    h.agentID,
		Collector:  cmd.Collector,
		PolicyID:   cmd.PolicyID,
		Data:       data,
		ReceivedAt: time.Now(),
	}
	if err != nil {
		env.Error = err.Error()
		h.log.Warn("compliance: collector failed",
			"request_id", cmd.RequestID,
			"collector", cmd.Collector,
			"err", err)
	}

	payload, err := json.Marshal(env)
	if err != nil {
		h.log.Warn("compliance: marshal result failed", "err", err)
		return
	}
	if err := h.nc.Publish(ctx, ComplianceResultSubject(h.agentID), payload); err != nil {
		h.log.Warn("compliance: publish result failed", "err", err)
	}
}

// Close marks the handler as closed. Subsequent incoming messages
// are dropped. The subscription itself is owned by the caller.
func (h *ComplianceHandler) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
}

// defaultCollectors returns the built-in collector set. These
// cover the common compliance domains described in the
// collectors/ package.
func defaultCollectors() []collectors.Collector {
	return []collectors.Collector{
		&collectors.AntivirusCollector{},
		&collectors.FirewallCollector{},
		&collectors.EncryptionCollector{},
		&collectors.PatchingCollector{},
		&collectors.PasswordPolicyCollector{},
		&collectors.ScreenLockCollector{},
		&collectors.USBStorageCollector{},
		&collectors.BrowserExtensionsCollector{},
		&collectors.RemoteAccessCollector{},
	}
}
