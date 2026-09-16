package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// heartbeatOrgLookup resolves an agent's owning org so events can be
// scoped to a tenant before delivery. The default implementation is the
// eventStoreAdapter in this package.
type heartbeatOrgLookup interface {
	GetAgent(ctx context.Context, orgID, id string) (*models.Agent, error)
}

// wsEventBridge subscribes to the platform's event subjects and forwards
// them to the WebSocket hub, so dashboards receive live updates instead
// of polling. Every delivery is scoped by org: events whose org cannot be
// determined are dropped (fail closed) rather than broadcast to all
// tenants.
//
// Plain (non-queue) subscriptions are used for every subject: each
// server replica serves its own WebSocket clients, so every replica
// needs a copy of each event. Queue groups would deliver each message
// to only one replica, silently starving the browsers connected to the
// others.
type wsEventBridge struct {
	client *events.Client
	server *api.Server
	agents heartbeatOrgLookup
	log    *slog.Logger
	subs   []*nats.Subscription
}

func newWSEventBridge(client *events.Client, server *api.Server, agents heartbeatOrgLookup, log *slog.Logger) *wsEventBridge {
	if log == nil {
		log = slog.Default()
	}
	return &wsEventBridge{
		client: client,
		server: server,
		agents: agents,
		log:    log,
	}
}

// Start subscribes to all forwarded subjects.
func (b *wsEventBridge) Start() error {
	if b.client == nil || b.client.Conn() == nil {
		return errors.New("ws event bridge: nats client not connected")
	}
	pairs := []struct {
		name    string
		subject string
		handler nats.MsgHandler
	}{
		{"check-results", events.SubjectCheckResultEvent, b.onCheckResult},
		{"alerts", events.SubjectAlertEvents, b.onAlert},
		{"agent-lifecycle", events.SubjectAgentEvents + ".*", b.onAgentLifecycle},
		{"heartbeats", events.SubjectHeartbeatPrefix, b.onHeartbeat},
	}
	for _, p := range pairs {
		sub, err := b.client.Subscribe(p.subject, p.handler)
		if err != nil {
			return errors.New("ws event bridge: subscribe " + p.name + ": " + err.Error())
		}
		b.subs = append(b.subs, sub)
	}
	b.log.Info("ws event bridge started",
		"subjects", []string{
			events.SubjectCheckResultEvent,
			events.SubjectAlertEvents,
			events.SubjectAgentEvents + ".*",
			events.SubjectHeartbeatPrefix,
		})
	return nil
}

// Stop unsubscribes from all subjects.
func (b *wsEventBridge) Stop() {
	for _, sub := range b.subs {
		if err := sub.Unsubscribe(); err != nil {
			b.log.Warn("ws event bridge unsubscribe failed", "err", err)
		}
	}
	b.subs = nil
}

// onCheckResult forwards ingested check results to the "checks" channel.
func (b *wsEventBridge) onCheckResult(msg *nats.Msg) {
	var evt checks.CheckResultEvent
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		b.log.Warn("ws bridge: check result decode failed", "err", err)
		return
	}
	if evt.Result == nil {
		return
	}
	// Fail closed: the result payload carries the owning org derived
	// from the check definition; without it there is no safe tenant to
	// deliver to.
	if evt.Result.OrgID == "" {
		b.log.Warn("ws bridge: check result without org dropped", "agent_id", evt.Result.AgentID)
		return
	}
	// The embedded alert is delivered via the alerts subject (onAlert);
	// forwarding it here too would double-deliver.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.server.PublishCheckResult(ctx, evt.Result.OrgID, evt.Result)
}

// onAlert forwards alert lifecycle events to the "alerts" channel.
func (b *wsEventBridge) onAlert(msg *nats.Msg) {
	var payload checks.AlertPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		b.log.Warn("ws bridge: alert decode failed", "err", err)
		return
	}
	// Fail closed on unknown org (see onCheckResult).
	if payload.OrgID == "" {
		b.log.Warn("ws bridge: alert without org dropped", "agent_id", payload.AgentID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.server.PublishAlert(ctx, payload.OrgID, payload)
}

// onAgentLifecycle forwards AgentOnline/AgentOffline transitions to the
// "agents" channel. The payload carries no org, so it is resolved from
// the agent record; unknown agents are dropped.
func (b *wsEventBridge) onAgentLifecycle(msg *nats.Msg) {
	var evt struct {
		Type    string `json:"type"`
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		b.log.Warn("ws bridge: agent lifecycle decode failed", "err", err)
		return
	}
	if evt.AgentID == "" {
		return
	}
	orgID, ok := b.orgForAgent(evt.AgentID)
	if !ok {
		return
	}
	event := strings.TrimPrefix(evt.Type, "Agent")
	event = strings.ToLower(event) // "online" / "offline"
	if event == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.server.PublishAgentEvent(ctx, orgID, event, evt)
}

// onHeartbeat forwards agent heartbeats to the "agents" channel, scoped
// to the agent's org.
func (b *wsEventBridge) onHeartbeat(msg *nats.Msg) {
	agentID := events.AgentIDFromSubject(msg.Subject)
	if agentID == "" {
		return
	}
	var hb models.Heartbeat
	if err := json.Unmarshal(msg.Data, &hb); err != nil {
		b.log.Warn("ws bridge: heartbeat decode failed", "agent_id", agentID, "err", err)
		return
	}
	if hb.AgentID != "" {
		agentID = hb.AgentID
	}
	orgID, ok := b.orgForAgent(agentID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.server.PublishHeartbeat(ctx, orgID, hb)
}

// orgForAgent resolves the owning org of an agent. It fails closed: an
// unknown agent yields no delivery.
func (b *wsEventBridge) orgForAgent(agentID string) (string, bool) {
	if b.agents == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := b.agents.GetAgent(ctx, "", agentID)
	if err != nil || a == nil || a.OrgID == "" {
		return "", false
	}
	return a.OrgID, true
}
