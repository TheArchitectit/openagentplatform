package patches

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
)

func TestCoordinateReboots_NilConn_NoPublish(t *testing.T) {
	// When nc is nil, CoordinateReboots should still run the health check
	// and stagger logic — the publish is simply skipped.
	cfg := PatchDeployerConfig{
		RebootStagger:  1 * time.Millisecond, // near-zero for test speed
		HealthCheckFn:  func(_ context.Context, _ string) error { return nil },
		HealthCheckTimeout: 1 * time.Second,
	}
	d := NewPatchDeployer(cfg, nil) // nil conn

	reboots := []RebootRequest{
		{AgentID: "agent-1", JobID: "job-1"},
		{AgentID: "agent-2", JobID: "job-1"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := d.CoordinateReboots(ctx, reboots)
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	// With nil conn + nil health check, both should succeed.
	for _, r := range results {
		if r.Status != TargetStatusSuccess {
			t.Errorf("agent %s: got status %q, want %q", r.AgentID, r.Status, TargetStatusSuccess)
		}
	}
}

func TestCoordinateReboots_PreCheckFails(t *testing.T) {
	cfg := PatchDeployerConfig{
		RebootStagger:  1 * time.Millisecond,
		HealthCheckFn:  func(_ context.Context, _ string) error { return fmt.Errorf("unhealthy") },
		HealthCheckTimeout: 1 * time.Second,
	}
	d := NewPatchDeployer(cfg, nil)

	reboots := []RebootRequest{
		{AgentID: "agent-1", JobID: "job-1"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := d.CoordinateReboots(ctx, reboots)
	if len(results) != 1 {
		t.Fatalf("results: got %d, want 1", len(results))
	}
	if results[0].Status != TargetStatusFailed {
		t.Errorf("status: got %q, want %q", results[0].Status, TargetStatusFailed)
	}
}

func TestCoordinateReboots_Cancelled(t *testing.T) {
	cfg := PatchDeployerConfig{
		RebootStagger:  1 * time.Millisecond,
		HealthCheckFn:  func(_ context.Context, _ string) error { return nil },
		HealthCheckTimeout: 1 * time.Second,
	}
	d := NewPatchDeployer(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel

	reboots := []RebootRequest{
		{AgentID: "agent-1", JobID: "job-1"},
	}
	results := d.CoordinateReboots(ctx, reboots)
	if len(results) < 1 {
		t.Fatalf("results: got %d, want at least 1", len(results))
	}
	// The first result should be a cancellation error.
	if results[0].Status != TargetStatusFailed {
		t.Errorf("status: got %q, want %q", results[0].Status, TargetStatusFailed)
	}
}

func TestRebootSubject_Construction(t *testing.T) {
	// Verify the subject used in CoordinateReboots matches the agent-side
	// handler's expected subject.
	got := patcher.RebootSubject("agent-1")
	want := "oap.agents.agent-1.reboot"
	if got != want {
		t.Errorf("RebootSubject: got %q, want %q", got, want)
	}
}

func TestRebootQueue_EnqueueDrain(t *testing.T) {
	q := NewRebootQueue(nil)
	if q.Len() != 0 {
		t.Errorf("empty queue: got %d, want 0", q.Len())
	}
	q.Enqueue(RebootRequest{AgentID: "a1", JobID: "j1"})
	q.Enqueue(RebootRequest{AgentID: "a2", JobID: "j1"})
	if q.Len() != 2 {
		t.Errorf("after enqueue: got %d, want 2", q.Len())
	}
	pending := q.Drain()
	if len(pending) != 2 {
		t.Fatalf("drain: got %d, want 2", len(pending))
	}
	if q.Len() != 0 {
		t.Errorf("after drain: got %d, want 0", q.Len())
	}
	// Drain again returns nil.
	if q.Drain() != nil {
		t.Errorf("double drain should be nil")
	}
}
