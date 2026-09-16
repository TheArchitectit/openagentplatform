package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/checks"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeOrgLookup is a minimal heartbeatOrgLookup.
type fakeOrgLookup struct {
	agents map[string]string // agent id -> org id
}

func (f *fakeOrgLookup) GetAgent(_ context.Context, _, id string) (*models.Agent, error) {
	org, ok := f.agents[id]
	if !ok {
		return nil, errors.New("agent not found")
	}
	return &models.Agent{ID: id, OrgID: org}, nil
}

func newBridgeForTest(lookup heartbeatOrgLookup) *wsEventBridge {
	server := api.NewServer(&config.Config{}, slog.Default(), nil, nil, nil)
	return newWSEventBridge(nil, server, lookup, slog.Default())
}

// TestOrgForAgent covers the fail-closed org resolution the bridge relies
// on before delivering any event.
func TestOrgForAgent(t *testing.T) {
	b := newBridgeForTest(&fakeOrgLookup{agents: map[string]string{
		"agent-1": "org-1",
	}})

	if org, ok := b.orgForAgent("agent-1"); !ok || org != "org-1" {
		t.Errorf("orgForAgent(agent-1) = %q, %v; want org-1, true", org, ok)
	}
	if _, ok := b.orgForAgent("unknown"); ok {
		t.Error("orgForAgent(unknown) resolved; want fail-closed")
	}

	// Nil lookup: nothing resolves, nothing panics.
	bNil := newBridgeForTest(nil)
	if _, ok := bNil.orgForAgent("agent-1"); ok {
		t.Error("orgForAgent with nil lookup resolved; want fail-closed")
	}
}

// TestBridgeHandlersTolerateBadInput verifies the NATS handlers never
// panic on malformed payloads or messages without orgs.
func TestBridgeHandlersTolerateBadInput(t *testing.T) {
	b := newBridgeForTest(&fakeOrgLookup{agents: map[string]string{}})

	cases := []struct {
		name string
		run  func()
	}{
		{"check-result invalid json", func() { b.onCheckResult(&nats.Msg{Data: []byte("{not json")}) }},
		{"check-result nil result", func() {
			data, _ := json.Marshal(checks.CheckResultEvent{Type: "check.result"})
			b.onCheckResult(&nats.Msg{Data: data})
		}},
		{"alert invalid json", func() { b.onAlert(&nats.Msg{Data: []byte("nope")}) }},
		{"lifecycle empty agent", func() {
			b.onAgentLifecycle(&nats.Msg{Subject: "oap.events.agent.online", Data: []byte(`{}`)})
		}},
		{"heartbeat unknown subject", func() { b.onHeartbeat(&nats.Msg{Subject: "garbage", Data: []byte(`{}`)}) }},
		{"heartbeat unknown agent", func() {
			b.onHeartbeat(&nats.Msg{Subject: "oap.agents.agent-x.heartbeat", Data: []byte(`{"agent_id":"agent-x"}`)})
		}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panic: %v", tc.name, r)
				}
			}()
			tc.run()
		}()
	}
}

// TestAgentIDFromSubjectShared verifies the exported helper matches the
// internal heartbeat subject parsing.
func TestAgentIDFromSubjectShared(t *testing.T) {
	if got := events.AgentIDFromSubject("oap.agents.abc-123.heartbeat"); got != "abc-123" {
		t.Errorf("AgentIDFromSubject = %q, want abc-123", got)
	}
	if got := events.AgentIDFromSubject("oap.events.other"); got != "" {
		t.Errorf("AgentIDFromSubject(non-agent subject) = %q, want empty", got)
	}
}
