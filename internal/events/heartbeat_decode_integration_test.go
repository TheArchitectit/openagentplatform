package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeHeartbeatStore captures UpdateAgentHeartbeat calls.
type fakeHeartbeatStore struct {
	gotID     string
	gotStatus string
	gotTime   time.Time
	calls     int
}

func (f *fakeHeartbeatStore) UpdateAgentHeartbeat(_ context.Context, agentID string, status string, lastSeen any, _, _, _ float64) error {
	f.calls++
	f.gotID = agentID
	f.gotStatus = status
	if ts, ok := lastSeen.(time.Time); ok {
		f.gotTime = ts
	}
	return nil
}
func (f *fakeHeartbeatStore) GetAgent(_ context.Context, _, _ string) (*models.Agent, error) {
	return nil, nil
}
func (f *fakeHeartbeatStore) MarkStaleAgentsOffline(_ context.Context, _ any) ([]string, error) {
	return nil, nil
}

// Regression for wiring plan W1: the exact payload pkg/agent publishes
// (Timestamp as int64 unix seconds) must decode and reach the store.
// Before the tolerant UnmarshalJSON on models.Heartbeat this decoded to
// "heartbeat decode failed" and the heartbeat was dropped.
func TestOnHeartbeatAcceptsAgentUnixSecondsPayload(t *testing.T) {
	wantSec := time.Now().Truncate(time.Second).Unix()

	agentPayload := map[string]any{
		"agent_id":     "agent-42",
		"timestamp":    wantSec,
		"cpu_percent":  11.5,
		"mem_percent":  62.0,
		"disk_percent": 48.25,
		"uptime_secs":  7200,
		"version":      "0.9.1",
	}
	data, _ := json.Marshal(agentPayload)

	msg := nats.NewMsg("oap.agents.agent-42.heartbeat")
	msg.Data = data

	store := &fakeHeartbeatStore{}
	h := NewHeartbeatHandler(nil, store, nil)
	h.onHeartbeat(msg)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1 (heartbeat was dropped)", store.calls)
	}
	if store.gotID != "agent-42" || store.gotStatus != "online" {
		t.Errorf("persisted id=%q status=%q, want agent-42/online", store.gotID, store.gotStatus)
	}
	if store.gotTime.Unix() != wantSec {
		t.Errorf("persisted timestamp = %d, want %d", store.gotTime.Unix(), wantSec)
	}
}

// RFC3339 producers must keep working too.
func TestOnHeartbeatAcceptsRFC3339Payload(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	payload := `{"agent_id":"agent-7","timestamp":"` + now.Format(time.RFC3339) + `","cpu_percent":5}`

	store := &fakeHeartbeatStore{}
	h := NewHeartbeatHandler(nil, store, nil)
	msg := nats.NewMsg("oap.agents.agent-7.heartbeat")
	msg.Data = []byte(payload)
	h.onHeartbeat(msg)

	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if !store.gotTime.Equal(now) {
		t.Errorf("persisted timestamp = %v, want %v", store.gotTime, now)
	}
}
