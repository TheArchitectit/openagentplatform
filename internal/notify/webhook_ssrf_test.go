package notify

import (
	"net"
	"testing"
)

// TestValidateWebhookURLBlocksSSRF verifies the webhook config validator
// rejects destinations that would enable Server-Side Request Forgery:
// loopback, link-local, private, unspecified, and the cloud metadata
// endpoint, both as literal IPs and as hostnames that resolve to them.
func TestValidateWebhookURLBlocksSSRF(t *testing.T) {
	cfg := func(url string) *WebhookConfig {
		return &WebhookConfig{URL: url, Method: "POST"}
	}

	blocked := []string{
		"http://127.0.0.1:8080/",
		"http://localhost:8080/",
		"http://localhost.localdomain/",
		"http://[::1]:8080/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.5/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
		"http://0.0.0.0/",
		"http://metadata.google.internal/computeMetadata/",
	}
	for _, u := range blocked {
		t.Run("blocked "+u, func(t *testing.T) {
			if err := cfg(u).Validate(); err == nil {
				t.Errorf("expected %q to be blocked, got nil error", u)
			}
		})
	}
}

// TestValidateWebhookURLAllowsPublic verifies a public hostname/IP and the
// standard https scheme are accepted (regression guard against over-blocking).
func TestValidateWebhookURLAllowsPublic(t *testing.T) {
	allowed := []string{
		"https://example.com/hooks",
		"https://1.1.1.1/hook",
		"http://8.8.8.8/",
	}
	for _, u := range allowed {
		t.Run("allowed "+u, func(t *testing.T) {
			if err := (&WebhookConfig{URL: u, Method: "POST"}).Validate(); err != nil {
				t.Errorf("expected %q to be allowed, got %v", u, err)
			}
		})
	}
}

// TestIsBlockedIP covers the IP classifier directly.
func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"169.254.169.254", true},
		{"10.1.2.3", true},
		{"172.16.5.4", true},
		{"192.168.0.1", true},
		{"0.0.0.0", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tc := range cases {
		got := isBlockedIP(net.ParseIP(tc.ip))
		if got != tc.want {
			t.Errorf("isBlockedIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}
