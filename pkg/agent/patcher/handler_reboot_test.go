package patcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

func TestRebootSubject(t *testing.T) {
	got := RebootSubject("agent-1")
	want := "oap.agents.agent-1.reboot"
	if got != want {
		t.Errorf("RebootSubject: got %q, want %q", got, want)
	}
}

func TestRebootResultSubject(t *testing.T) {
	got := RebootResultSubject("agent-1")
	want := "oap.agents.agent-1.reboot.results"
	if got != want {
		t.Errorf("RebootResultSubject: got %q, want %q", got, want)
	}
}

func TestRebootCommandJSON(t *testing.T) {
	cmd := RebootCommand{
		RequestID:  "req-1",
		JobID:      "job-1",
		Reason:     "patch deployment",
		KBs:        []string{"KB123", "KB456"},
		TimeoutSec: 300,
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded RebootCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.RequestID != "req-1" {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, "req-1")
	}
	if decoded.JobID != "job-1" {
		t.Errorf("JobID: got %q, want %q", decoded.JobID, "job-1")
	}
	if decoded.Reason != "patch deployment" {
		t.Errorf("Reason: got %q, want %q", decoded.Reason, "patch deployment")
	}
	if len(decoded.KBs) != 2 || decoded.KBs[0] != "KB123" || decoded.KBs[1] != "KB456" {
		t.Errorf("KBs: got %v, want [KB123 KB456]", decoded.KBs)
	}
	if decoded.TimeoutSec != 300 {
		t.Errorf("TimeoutSec: got %d, want 300", decoded.TimeoutSec)
	}
}

func TestRebootResultEnvelopeJSON(t *testing.T) {
	env := RebootResultEnvelope{
		RequestID:  "req-1",
		AgentID:    "agent-1",
		Accepted:   true,
		ReceivedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded RebootResultEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.RequestID != "req-1" {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, "req-1")
	}
	if !decoded.Accepted {
		t.Errorf("Accepted: got false, want true")
	}
}

func TestRebootCommand_OmitEmpty(t *testing.T) {
	cmd := RebootCommand{RequestID: "req-2"}
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded RebootCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.JobID != "" {
		t.Errorf("JobID should be empty, got %q", decoded.JobID)
	}
	if decoded.Reason != "" {
		t.Errorf("Reason should be empty, got %q", decoded.Reason)
	}
	if len(decoded.KBs) != 0 {
		t.Errorf("KBs should be nil/empty, got %v", decoded.KBs)
	}
	if decoded.TimeoutSec != 0 {
		t.Errorf("TimeoutSec should be 0, got %d", decoded.TimeoutSec)
	}
}

func TestNoopRebooter(t *testing.T) {
	r := &noopRebooter{log: slog.Default()}
	if err := r.Reboot(nil); err != nil {
		t.Errorf("noopRebooter.Reboot returned error: %v", err)
	}
}

// errRebooter is a test Rebooter that always returns an error.
type errRebooter struct {
	err error
}

func (e *errRebooter) Reboot(_ context.Context) error { return e.err }

func TestSetRebooter(t *testing.T) {
	h := NewHandler("test-agent", nil, nil)
	// Default is noopRebooter; setting nil should be a no-op.
	h.SetRebooter(nil)
	// Setting a real rebooter should stick.
	custom := &errRebooter{err: fmt.Errorf("boom")}
	h.SetRebooter(custom)
}
