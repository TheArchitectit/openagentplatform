package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Handler is the agent-side subscriber for RMM-09 mesh bring-up. It listens
// on MeshConfigSubject(agentID) for the server-issued WireGuard config,
// validates it (org-scoping + key/address integrity), applies it through a
// WireGuardDriver, and publishes the result on MeshConfigResultSubject.
//
// The handler never touches the wire-protocol or device details itself; all
// fabric state changes go through the driver, which is what keeps the unit
// tests hermetic (a fake driver, no TUN device, no root).
type Handler struct {
	agentID string
	nc      *nats.Conn
	driver  WireGuardDriver
	log     *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewHandler builds a mesh handler. A nil driver resolves to the build's
// default (wireguard-go behind the `mesh` build tag when the host supports a
// TUN device; a no-op logger otherwise), so callers can always pass nil and
// get a safe default.
func NewHandler(agentID string, nc *nats.Conn, driver WireGuardDriver, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	if driver == nil {
		driver = defaultDriver(log)
	}
	return &Handler{
		agentID: agentID,
		nc:      nc,
		driver:  driver,
		log:     log,
	}
}

// SetDriver overrides the driver. Useful in tests and for hosts that want a
// driver chosen at runtime rather than via build tag.
func (h *Handler) SetDriver(d WireGuardDriver) {
	if d == nil {
		return
	}
	h.driver = d
}

// Close marks the handler closed and tears down the mesh interface.
func (h *Handler) Close() {
	h.mu.Lock()
	already := h.closed
	h.closed = true
	h.mu.Unlock()
	if already {
		return
	}
	if err := h.driver.Close(); err != nil {
		h.log.Warn("mesh: driver close failed", "err", err)
	}
}

// Run subscribes to the agent's mesh.config subject and dispatches each
// message to the driver. The returned subscription is owned by the caller;
// call .Unsubscribe() to stop consuming. Run blocks only long enough to
// subscribe — message handling runs in the NATS callback goroutine.
func (h *Handler) Run(ctx context.Context) (*nats.Subscription, error) {
	subject := MeshConfigSubject(h.agentID)
	sub, err := h.nc.Subscribe(subject, func(msg *nats.Msg) {
		h.handle(ctx, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("mesh subscribe %s: %w", subject, err)
	}
	h.log.Info("mesh handler subscribed", "subject", subject, "driver", h.driver.Name())
	return sub, nil
}

// handle validates and applies a single mesh config message.
func (h *Handler) handle(parent context.Context, msg *nats.Msg) {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	cfg, err := ParseMeshConfig(msg.Data)
	if err != nil {
		h.log.Warn("mesh: invalid config payload", "err", err)
		h.publishResult(ctx, "", false, h.agentID, err)
		return
	}
	// Defense in depth: the subject is already agent-scoped, but reject a
	// payload that claims a different owner so a cross-agent config can never
	// bring up an interface bound to this agent's identity.
	if cfg.AgentID != h.agentID {
		h.log.Warn("mesh: config agent_id mismatch",
			"subject_agent", h.agentID, "payload_agent", cfg.AgentID)
		h.publishResult(ctx, cfg.OrgID, false, cfg.AgentID, fmt.Errorf("mesh: config agent mismatch"))
		return
	}

	h.log.Info("mesh: applying config",
		"agent_id", cfg.AgentID,
		"org_id", cfg.OrgID,
		"address", cfg.Address,
		"interface", cfg.interfaceName())

	if err := h.driver.Apply(ctx, cfg); err != nil {
		h.log.Warn("mesh: driver apply failed", "err", err)
		h.publishResult(ctx, cfg.OrgID, false, cfg.AgentID, err)
		return
	}
	h.publishResult(ctx, cfg.OrgID, true, cfg.AgentID, nil)
}

// publishResult sends a MeshConfigResult envelope on the result subject.
func (h *Handler) publishResult(ctx context.Context, orgID string, ok bool, agentID string, applyErr error) {
	if h.nc == nil {
		return
	}
	res := MeshConfigResult{
		AgentID:    agentID,
		OrgID:      orgID,
		OK:         ok,
		Driver:     h.driver.Name(),
		ReceivedAt: time.Now(),
	}
	if applyErr != nil {
		res.Error = applyErr.Error()
	}
	payload, err := json.Marshal(res)
	if err != nil {
		h.log.Warn("mesh: marshal result failed", "err", err)
		return
	}
	if err := h.nc.Publish(MeshConfigResultSubject(h.agentID), payload); err != nil {
		h.log.Warn("mesh: publish result failed", "err", err)
	}
}
