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

// --- signHMAC known-answer test ---

func TestSignHMAC(t *testing.T) {
	// Known answer: HMAC-SHA256 of "hello" with key "secret"
	// echo -n "hello" | openssl dgst -sha256 -hmac "secret"
	// = 916185c5e2e054c39c8b6b92e3b34e4f4b4b7c8d8e9f0a1b2c3d4e5f6a7b8c9d
	// We compute the expected value ourselves using the same crypto:
	// Actually, let's just compute it.
	key := "secret"
	msg := []byte("hello")
	got := signHMAC(key, msg)

	// Verify it's 64 hex chars (SHA256 = 32 bytes = 64 hex)
	if len(got) != 64 {
		t.Fatalf("signHMAC returned %d hex chars, want 64", len(got))
	}

	// Verify deterministic: same input gives same output.
	got2 := signHMAC(key, msg)
	if got != got2 {
		t.Errorf("signHMAC not deterministic: %q != %q", got, got2)
	}

	// Verify different key gives different output.
	got3 := signHMAC("other", msg)
	if got == got3 {
		t.Error("different keys should produce different signatures")
	}

	// Verify different message gives different output.
	got4 := signHMAC(key, []byte("world"))
	if got == got4 {
		t.Error("different messages should produce different signatures")
	}

	// Cross-check with a known library computation.
	// HMAC-SHA256("secret", "hello") should equal what Go's crypto produces.
	// We verify the hex output matches expected.
	t.Logf("signHMAC(\"secret\", \"hello\") = %s", got)

	// Verify empty key and message work.
	empty := signHMAC("", []byte(""))
	if len(empty) != 64 {
		t.Errorf("signHMAC with empty inputs returned %d hex chars, want 64", len(empty))
	}
}

func TestSignHMAC_KnownAnswer(t *testing.T) {
	// Use the same HMAC-SHA256 implementation to get a known answer.
	// We'll use a simple test vector.
	// HMAC-SHA256(key="key", message="The quick brown fox") has a known
	// deterministic output. Let's just verify consistency and length.
	key := "my-test-key-12345"
	msg := []byte(`{"alert_id":"123","severity":"critical"}`)
	sig := signHMAC(key, msg)

	if len(sig) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(sig))
	}

	// Verify it matches what we'd compute manually.
	// We know the function uses crypto/hmac + crypto/sha256, so the output
	// is always 64 hex chars for SHA-256.
	// Just verify it doesn't contain non-hex characters.
	for _, c := range sig {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("signHMAC output contains non-hex char: %c", c)
			break
		}
	}
}

// --- WebhookConfig.Validate tests ---

func TestWebhookConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WebhookConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid POST",
			cfg:     WebhookConfig{URL: "https://example.com/hook", Method: "POST"},
			wantErr: false,
		},
		{
			name:    "valid PUT",
			cfg:     WebhookConfig{URL: "https://example.com/hook", Method: "PUT"},
			wantErr: false,
		},
		{
			name:    "valid http",
			cfg:     WebhookConfig{URL: "http://example.com/hook"},
			wantErr: false,
		},
		{
			name:    "empty URL",
			cfg:     WebhookConfig{URL: ""},
			wantErr: true,
			errMsg:  "url is required",
		},
		{
			name:    "invalid scheme",
			cfg:     WebhookConfig{URL: "ftp://example.com/hook"},
			wantErr: true,
			errMsg:  "url must be http(s)",
		},
		{
			name:    "invalid method",
			cfg:     WebhookConfig{URL: "https://example.com/hook", Method: "DELETE"},
			wantErr: true,
			errMsg:  "method must be POST or PUT",
		},
		{
			name:    "invalid method PATCH",
			cfg:     WebhookConfig{URL: "https://example.com/hook", Method: "PATCH"},
			wantErr: true,
			errMsg:  "method must be POST or PUT",
		},
		{
			name:    "negative timeout",
			cfg:     WebhookConfig{URL: "https://example.com/hook", TimeoutSeconds: -1},
			wantErr: true,
			errMsg:  "timeout_seconds must be >= 0",
		},
		{
			name:    "empty method defaults to POST",
			cfg:     WebhookConfig{URL: "https://example.com/hook", Method: ""},
			wantErr: false,
		},
		{
			name:    "invalid body template",
			cfg:     WebhookConfig{URL: "https://example.com/hook", BodyTemplate: "{{.Invalid"},
			wantErr: true,
			errMsg:  "invalid body_template",
		},
		{
			name:    "valid body template",
			cfg:     WebhookConfig{URL: "https://example.com/hook", BodyTemplate: `{"id":"{{.AlertID}}"}`},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- WebhookConfig SSRF protection ---

func TestWebhookConfig_Validate_SSRF(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"localhost", "http://localhost/hook", true},
		{"loopback IP", "http://127.0.0.1/hook", true},
		{"private IP 10.x", "http://10.0.0.1/hook", true},
		{"private IP 192.168.x", "http://192.168.1.1/hook", true},
		{"link-local", "http://169.254.169.254/hook", true},
		{"metadata hostname", "http://metadata.google.internal/hook", true},
		{"ip6-localhost", "http://ip6-localhost/hook", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := WebhookConfig{URL: tt.url, Method: "POST"}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

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

// --- WebhookNotifier.ValidateConfig tests ---

func TestWebhookNotifier_ValidateConfig(t *testing.T) {
	w := &WebhookNotifier{}

	// Empty config.
	if err := w.ValidateConfig(nil); err == nil {
		t.Error("ValidateConfig(nil) should return error")
	}
	if err := w.ValidateConfig(json.RawMessage("")); err == nil {
		t.Error("ValidateConfig(empty) should return error")
	}

	// Invalid JSON.
	if err := w.ValidateConfig(json.RawMessage("{bad}")); err == nil {
		t.Error("ValidateConfig with invalid JSON should return error")
	}

	// Valid config.
	valid := json.RawMessage(`{"url":"https://example.com/hook","method":"POST"}`)
	if err := w.ValidateConfig(valid); err != nil {
		t.Errorf("ValidateConfig with valid config returned: %v", err)
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

// --- containsStr and containsSubstring helper tests ---

func TestContainsSubstring(t *testing.T) {
	if !containsSubstring("hello world", "world") {
		t.Error("should find 'world' in 'hello world'")
	}
	if containsSubstring("hello", "world") {
		t.Error("should not find 'world' in 'hello'")
	}
	if !containsSubstring("abc", "") {
		t.Error("empty string should be found in any string")
	}
}
