package notify

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// TestSlackConfigValidateBlocksInternalURLs verifies that Slack channel
// validation shares the webhook SSRF blocklists: loopback, RFC1918,
// link-local/metadata, and internal hostnames are all rejected.
func TestSlackConfigValidateBlocksInternalURLs(t *testing.T) {
	cases := map[string]string{
		"loopback":        "http://127.0.0.1:8080/hook",
		"loopback-alt":    "http://[::1]:8080/hook",
		"rfc1918":         "http://10.1.2.3/hook",
		"rfc1918-b":       "https://192.168.1.10/hook",
		"link-local":      "http://169.254.169.254/latest/meta-data",
		"metadata-name":   "http://metadata.google.internal/computeMetadata/v1",
		"localhost-name":  "http://localhost:9000/hook",
		"resolving-local": "http://localhost.localdomain/hook",
		"non-http-scheme": "ftp://hooks.example.com/hook",
		"missing-host":    "http://",
		"empty":           "",
	}
	for name, url := range cases {
		cfg := &SlackConfig{WebhookURL: url}
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate(%q) = nil, want error", name, url)
		}
	}
}

// TestSlackConfigValidateAcceptsPublicURL verifies a public https webhook
// still passes validation.
func TestSlackConfigValidateAcceptsPublicURL(t *testing.T) {
	// example.com is IANA-reserved and resolves publicly; it is not in
	// any blocklist.
	cfg := &SlackConfig{WebhookURL: "https://example.com/hook"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate(public url) = %v, want nil", err)
	}
}

// TestSlackNotifierNotifyRejectsInternalTarget is defense in depth: even
// if validation is bypassed (config edited directly in the DB), Notify
// must fail because the hardened transport refuses to dial internal
// addresses.
func TestSlackNotifierNotifyRejectsInternalTarget(t *testing.T) {
	n := &SlackNotifier{}
	cfg := SlackConfig{WebhookURL: "http://127.0.0.1:1/hook"} // skips Validate on purpose
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	channel := NotificationChannel{Type: "slack", Config: raw}
	alert := &models.Alert{ID: "a-1", CheckID: "c-1", AgentID: "agent-1", Severity: "critical", State: "open"}
	if err := n.Notify(context.Background(), alert, channel); err == nil {
		t.Fatal("Notify to loopback target = nil, want dial error")
	}
}
