package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- Mock notifier for Dispatch tests ---

type mockNotifier struct {
	notifyFunc   func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error
	validateFunc func(config json.RawMessage) error
	calls        int32
}

func (m *mockNotifier) Notify(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
	atomic.AddInt32(&m.calls, 1)
	if m.notifyFunc != nil {
		return m.notifyFunc(ctx, alert, ch)
	}
	return nil
}

func (m *mockNotifier) ValidateConfig(config json.RawMessage) error {
	if m.validateFunc != nil {
		return m.validateFunc(config)
	}
	return nil
}

func (m *mockNotifier) callCount() int32 {
	return atomic.LoadInt32(&m.calls)
}

// --- Dispatch tests ---

func TestDispatch_SingleNotifier_Success(t *testing.T) {
	registry := NewRegistry()
	mock := &mockNotifier{}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1", Severity: "critical", State: "open"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "sent" {
		t.Errorf("status = %q, want %q", results[0].Status, "sent")
	}
	if results[0].Err != nil {
		t.Errorf("err = %v, want nil", results[0].Err)
	}
	if mock.callCount() != 1 {
		t.Errorf("notify called %d times, want 1", mock.callCount())
	}
}

func TestDispatch_MultipleNotifiers(t *testing.T) {
	registry := NewRegistry()
	mock1 := &mockNotifier{}
	mock2 := &mockNotifier{}
	registry.Register("type-a", mock1)
	registry.Register("type-b", mock2)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "type-a", Enabled: true},
		{ID: "ch-2", Type: "type-b", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "sent" {
			t.Errorf("channel %s: status = %q, want %q", r.ChannelID, r.Status, "sent")
		}
	}
	if mock1.callCount() != 1 {
		t.Errorf("mock1 called %d times, want 1", mock1.callCount())
	}
	if mock2.callCount() != 1 {
		t.Errorf("mock2 called %d times, want 1", mock2.callCount())
	}
}

func TestDispatch_RetryOnFailure(t *testing.T) {
	registry := NewRegistry()
	callCount := int32(0)
	mock := &mockNotifier{
		notifyFunc: func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
			n := atomic.AddInt32(&callCount, 1)
			if n < 3 {
				return fmt.Errorf("transient error %d", n)
			}
			return nil // succeeds on 3rd attempt
		},
	}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "sent" {
		t.Errorf("status = %q, want %q (should succeed after retries)", results[0].Status, "sent")
	}
	if results[0].Attempt != 3 {
		t.Errorf("attempt = %d, want 3", results[0].Attempt)
	}
}

func TestDispatch_AllRetriesExhausted(t *testing.T) {
	registry := NewRegistry()
	mock := &mockNotifier{
		notifyFunc: func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
			return errors.New("always fail")
		},
	}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("status = %q, want %q", results[0].Status, "failed")
	}
	if results[0].Attempt != MaxRetryAttempts {
		t.Errorf("attempt = %d, want %d", results[0].Attempt, MaxRetryAttempts)
	}
}

func TestDispatch_ContextCancellation(t *testing.T) {
	registry := NewRegistry()
	mock := &mockNotifier{
		notifyFunc: func(ctx context.Context, alert *models.Alert, ch NotificationChannel) error {
			// Check if context is already cancelled.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return errors.New("fail")
		},
	}
	registry.Register("test", mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true},
	}

	results := Dispatch(ctx, registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("status = %q, want %q", results[0].Status, "failed")
	}
}

func TestDispatch_DisabledChannel(t *testing.T) {
	registry := NewRegistry()
	mock := &mockNotifier{}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: false},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "skipped" {
		t.Errorf("status = %q, want %q", results[0].Status, "skipped")
	}
	if mock.callCount() != 0 {
		t.Error("disabled channel should not call notifier")
	}
}

func TestDispatch_NoRegisteredNotifier(t *testing.T) {
	registry := NewRegistry()
	// Don't register any notifier for "unknown".

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "unknown", Enabled: true},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("status = %q, want %q", results[0].Status, "failed")
	}
}

func TestDispatch_InvalidConfig(t *testing.T) {
	registry := NewRegistry()
	mock := &mockNotifier{
		validateFunc: func(config json.RawMessage) error {
			return errors.New("bad config")
		},
	}
	registry.Register("test", mock)

	alert := &models.Alert{ID: "alert-1"}
	channels := []NotificationChannel{
		{ID: "ch-1", Type: "test", Enabled: true, Config: json.RawMessage(`{"bad":true}`)},
	}

	results := Dispatch(context.Background(), registry, alert, channels, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("status = %q, want %q", results[0].Status, "failed")
	}
}

func TestDispatch_EmptyChannels(t *testing.T) {
	registry := NewRegistry()
	results := Dispatch(context.Background(), registry, &models.Alert{}, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil channels, got %d", len(results))
	}
}

// --- NotifierRegistry tests ---

func TestNotifierRegistry(t *testing.T) {
	r := NewRegistry()
	if r.Get("nope") != nil {
		t.Error("Get on empty registry should return nil")
	}

	mock := &mockNotifier{}
	r.Register("email", mock)
	if r.Get("email") != mock {
		t.Error("Get should return registered notifier")
	}

	types := r.SupportedTypes()
	if len(types) != 1 || types[0] != "email" {
		t.Errorf("SupportedTypes = %v, want [email]", types)
	}

	// Overwrite.
	mock2 := &mockNotifier{}
	r.Register("email", mock2)
	if r.Get("email") != mock2 {
		t.Error("Register should overwrite existing")
	}
}

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
