package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// HeartbeatStore is the persistence seam for heartbeat handling. The default
// implementation is *pgAgentStore in internal/api; events is a separate
// package so it cannot import api without an import cycle, hence the
// interface.
type HeartbeatStore interface {
	UpdateAgentHeartbeat(ctx context.Context, agentID string, status string, lastSeen any, cpu, mem, disk float64) error
	GetAgent(ctx context.Context, id string) (*models.Agent, error)
	MarkStaleAgentsOffline(ctx context.Context, threshold any) ([]string, error)
}

// HeartbeatHandler owns the NATS subscription for the heartbeat wildcard
// subject and the background sweep that flips stale agents offline.
type HeartbeatHandler struct {
	client *Client
	store  HeartbeatStore
	log    *slog.Logger

	sub *nats.Subscription

	// onlineTracks agent IDs that we have already seen as online during this
	// process's lifetime. We use it to avoid emitting a redundant
	// AgentOnline event for every heartbeat.
	onlineMu  sync.Mutex
	online    map[string]struct{}

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewHeartbeatHandler constructs a handler; call Start to begin consuming.
func NewHeartbeatHandler(client *Client, store HeartbeatStore, log *slog.Logger) *HeartbeatHandler {
	if log == nil {
		log = slog.Default()
	}
	return &HeartbeatHandler{
		client: client,
		store:  store,
		log:    log,
		online: make(map[string]struct{}),
		stopCh: make(chan struct{}),
	}
}

// Start subscribes to the heartbeat wildcard subject and starts the
// stale-agent sweeper goroutine. Returns an error if subscription fails.
func (h *HeartbeatHandler) Start(ctx context.Context) error {
	if h.client == nil || h.client.conn == nil {
		return errors.New("heartbeat: nats client not connected")
	}
	sub, err := h.client.Subscribe(SubjectHeartbeatPrefix, h.onHeartbeat)
	if err != nil {
		return fmt.Errorf("heartbeat: subscribe: %w", err)
	}
	h.sub = sub
	h.log.Info("heartbeat handler started", "subject", SubjectHeartbeatPrefix)

	h.wg.Add(1)
	go h.sweepStale(ctx)

	return nil
}

// Stop unsubscribes and signals background goroutines to exit. Blocks until
// the sweeper has exited.
func (h *HeartbeatHandler) Stop() {
	if h.sub != nil {
		if err := h.sub.Unsubscribe(); err != nil {
			h.log.Warn("heartbeat unsubscribe failed", "err", err)
		}
	}
	close(h.stopCh)
	h.wg.Wait()
}

// onHeartbeat is the nats.MsgHandler invoked for every message on the
// wildcard subject. It extracts the agent_id from the subject, parses the
// payload, updates the DB, and emits an AgentOnline event the first time we
// see this agent in the current process.
func (h *HeartbeatHandler) onHeartbeat(msg *nats.Msg) {
	agentID := agentIDFromSubject(msg.Subject)
	if agentID == "" {
		h.log.Warn("heartbeat on unknown subject", "subject", msg.Subject)
		return
	}

	var payload models.Heartbeat
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		h.log.Warn("heartbeat decode failed", "agent_id", agentID, "err", err)
		return
	}
	if payload.AgentID != "" && payload.AgentID != agentID {
		// Body agent_id takes precedence over subject-derived id; if they
		// disagree, trust the body (the agent wrote both sides).
		agentID = payload.AgentID
	}
	if payload.Timestamp.IsZero() {
		payload.Timestamp = time.Now().UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	previousStatus := h.previousStatus(ctx, agentID)
	if err := h.store.UpdateAgentHeartbeat(ctx, agentID, "online", payload.Timestamp, payload.CPUPercent, payload.MemPercent, payload.DiskPercent); err != nil {
		h.log.Warn("heartbeat persist failed", "agent_id", agentID, "err", err)
		return
	}

	if previousStatus != "online" {
		h.markOnline(agentID)
		h.emitLifecycle(ctx, "AgentOnline", agentID, &payload)
	}
}

// sweepStale runs every 30s and flips any agent whose last_seen is older
// than 120s to status='offline'. When an agent transitions from online to
// offline, an AgentOffline event is emitted.
func (h *HeartbeatHandler) sweepStale(ctx context.Context) {
	defer h.wg.Done()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	threshold := time.Now().Add(-2 * time.Minute)
	for {
		select {
		case <-h.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			threshold = time.Now().Add(-2 * time.Minute)
			ids, err := h.store.MarkStaleAgentsOffline(ctx, threshold)
			if err != nil {
				h.log.Warn("mark stale offline failed", "err", err)
				continue
			}
			for _, id := range ids {
				h.markOffline(id)
				h.emitLifecycle(ctx, "AgentOffline", id, nil)
			}
		}
	}
}

// previousStatus returns the current status of the agent from the DB, or
// empty string on error. We treat any error as "unknown" so we still emit an
// AgentOnline event (idempotent downstream, not catastrophic).
func (h *HeartbeatHandler) previousStatus(ctx context.Context, agentID string) string {
	if h.store == nil {
		return ""
	}
	a, err := h.store.GetAgent(ctx, agentID)
	if err != nil || a == nil {
		return ""
	}
	return a.Status
}

func (h *HeartbeatHandler) markOnline(agentID string) {
	h.onlineMu.Lock()
	defer h.onlineMu.Unlock()
	h.online[agentID] = struct{}{}
}

func (h *HeartbeatHandler) markOffline(agentID string) {
	h.onlineMu.Lock()
	defer h.onlineMu.Unlock()
	delete(h.online, agentID)
}

func (h *HeartbeatHandler) emitLifecycle(ctx context.Context, eventType, agentID string, payload *models.Heartbeat) {
	evt := map[string]any{
		"type":      eventType,
		"agent_id":  agentID,
		"timestamp": time.Now().UTC().Unix(),
	}
	if payload != nil {
		evt["version"] = payload.Version
		evt["cpu_percent"] = payload.CPUPercent
		evt["mem_percent"] = payload.MemPercent
		evt["disk_percent"] = payload.DiskPercent
	}
	data, err := json.Marshal(evt)
	if err != nil {
		h.log.Warn("lifecycle marshal failed", "type", eventType, "err", err)
		return
	}
	subject := SubjectAgentEvents + "." + strings.ToLower(strings.TrimPrefix(eventType, "Agent"))
	if h.client != nil {
		if err := h.client.Publish(ctx, subject, data); err != nil {
			h.log.Warn("lifecycle publish failed", "type", eventType, "err", err)
		}
	}
}

// agentIDFromSubject extracts the agent_id segment from
// "oap.agents.<id>.heartbeat". Returns "" for malformed subjects.
func agentIDFromSubject(subject string) string {
	// Expected: oap.agents.<id>.heartbeat
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != "oap" || parts[1] != "agents" {
		return ""
	}
	// id may itself contain dots (UUIDs do not, but be permissive and take
	// everything between "agents" and the last segment).
	return strings.Join(parts[2:len(parts)-1], ".")
}
