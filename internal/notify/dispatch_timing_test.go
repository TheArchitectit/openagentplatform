package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- Dispatch timing: verify retry uses backoff ---

func TestDispatch_RetryBackoffTiming(t *testing.T) {
	registry := NewRegistry()
	var times []time.Time
	mock := &mockNotifier{
		notifyFunc: func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
			times = append(times, time.Now())
			return errors.New("always fail")
		},
	}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true},
	}

	start := time.Now()
	results := Dispatch(context.Background(), registry, alert, channels, nil)
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Attempt != MaxRetryAttempts {
		t.Errorf("attempt = %d, want %d", results[0].Attempt, MaxRetryAttempts)
	}

	// With BaseBackoff=1s, retries at 1s, 2s -> at least 3s total.
	// We check it took at least 2.5s (allowing some tolerance).
	if elapsed < 2500*time.Millisecond {
		t.Errorf("elapsed %v, expected at least ~3s for 3 retries with backoff", elapsed)
	}

	// Verify there were exactly MaxRetryAttempts calls.
	if len(times) != MaxRetryAttempts {
		t.Errorf("notify called %d times, want %d", len(times), MaxRetryAttempts)
	}
}

// --- Dispatch: mixed success and failure ---

func TestDispatch_MixedResults(t *testing.T) {
	registry := NewRegistry()
	mockA := &mockNotifier{} // succeeds
	mockB := &mockNotifier{
		notifyFunc: func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
			return errors.New("fail")
		},
	}
	registry.Register("type-a", mockA)
	registry.Register("type-b", mockB)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-ok", Type: "type-a", Enabled: true},
		{ID: "ch-fail", Type: "type-b", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		switch r.ChannelID {
		case "ch-ok":
			if r.Status != "sent" {
				t.Errorf("ch-ok status = %q, want %q", r.Status, "sent")
			}
		case "ch-fail":
			if r.Status != "failed" {
				t.Errorf("ch-fail status = %q, want %q", r.Status, "failed")
			}
		}
	}
}
