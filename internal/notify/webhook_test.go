package notify

import (
	"encoding/json"
	"testing"
)

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
