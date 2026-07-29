package billing

import (
	"log/slog"
	"testing"
)

// TestMeteringServiceRecordUsage verifies the metering service queues usage
// records for known metrics and rejects unknown ones. This guards the
// in-memory aggregation that the server wires up in NewServer (previously the
// MeteringService was never constructed, so usage was silently lost).
func TestMeteringServiceRecordUsage(t *testing.T) {
	m := NewMeteringService(NewStripeClientWithKey("sk_test_dummy"), slog.Default())

	if err := m.RecordUsage("org-1", MetricAgentCountDays, 5); err != nil {
		t.Fatalf("RecordUsage known metric: %v", err)
	}
	if err := m.RecordUsage("org-1", MetricA2ATaskCount, 3); err != nil {
		t.Fatalf("RecordUsage known metric: %v", err)
	}
	if err := m.RecordUsage("org-2", MetricAPICallCount, 10); err != nil {
		t.Fatalf("RecordUsage known metric: %v", err)
	}

	// Unknown metric must be rejected.
	if err := m.RecordUsage("org-1", "bogus_metric", 1); err == nil {
		t.Error("expected error for unknown metric, got nil")
	}

	m.mu.Lock()
	if len(m.pending["org-1"]) != 2 {
		t.Errorf("org-1 pending: got %d, want 2", len(m.pending["org-1"]))
	}
	if len(m.pending["org-2"]) != 1 {
		t.Errorf("org-2 pending: got %d, want 1", len(m.pending["org-2"]))
	}
	m.mu.Unlock()
}

// TestMeteringServiceFlushEmpty verifies Flush on an empty queue is a no-op
// (no Stripe call attempted). The pending map is reset on each flush.
func TestMeteringServiceFlushEmpty(t *testing.T) {
	m := NewMeteringService(NewStripeClientWithKey("sk_test_dummy"), slog.Default())
	// Flush with nothing queued — must not error and must not call Stripe.
	// (meterevent.New would fail against the dummy key, so an empty queue
	// that short-circuits returns nil.)
	if err := m.Flush(t.Context()); err != nil {
		t.Errorf("Flush empty queue: got %v, want nil", err)
	}
}

// TestNewStripeClientRequiresKey verifies the constructor fails fast when
// STRIPE_SECRET_KEY is absent — the server wiring relies on this to keep
// billing disabled rather than constructing a half-broken client.
func TestNewStripeClientRequiresKey(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "")
	if _, err := NewStripeClient(); err == nil {
		t.Error("expected ErrSecretKeyMissing when STRIPE_SECRET_KEY unset, got nil")
	}
}
