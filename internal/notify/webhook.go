// Package notify - webhook.go implements a generic HTTP webhook notifier.
// The URL, method, headers, and body template are all configurable. Each
// request is signed with HMAC-SHA256 in the X-OAP-Signature header so
// the receiving end can verify the payload came from the platform.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// WebhookConfig is the type-specific configuration for the generic webhook.
type WebhookConfig struct {
	URL          string            `json:"url"`
	Method       string            `json:"method"`          // "POST" or "PUT"
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"body_template,omitempty"` // Go text/template; "" => default JSON
	Secret       string            `json:"secret,omitempty"`        // HMAC signing key
	TimeoutSeconds int             `json:"timeout_seconds,omitempty"`
	MaxRetries   int               `json:"max_retries,omitempty"`   // per-call; capped by Dispatch
}

// Validate verifies the webhook channel configuration.
func (w *WebhookConfig) Validate() error {
	if w.URL == "" {
		return errors.New("webhook: url is required")
	}
	if !strings.HasPrefix(w.URL, "https://") && !strings.HasPrefix(w.URL, "http://") {
		return errors.New("webhook: url must be http(s)")
	}
	if w.Method == "" {
		w.Method = http.MethodPost
	}
	method := strings.ToUpper(w.Method)
	if method != http.MethodPost && method != http.MethodPut {
		return errors.New("webhook: method must be POST or PUT")
	}
	w.Method = method
	if w.TimeoutSeconds < 0 {
		return errors.New("webhook: timeout_seconds must be >= 0")
	}
	if w.BodyTemplate != "" {
		if _, err := template.New("body").Parse(w.BodyTemplate); err != nil {
			return fmt.Errorf("webhook: invalid body_template: %w", err)
		}
	}
	return nil
}

// defaultWebhookBody is the default JSON payload sent to the webhook.
const defaultWebhookBody = `{"alert_id":"{{.AlertID}}","severity":"{{.Severity}}","state":"{{.State}}","check_id":"{{.CheckID}}","agent_id":"{{.AgentID}}","message":{{quote .Message}},"timestamp":"{{.Timestamp}}","platform_url":"{{.PlatformURL}}","alert_url":"{{.AlertURL}}"}`

// WebhookNotifier delivers alerts via generic HTTP webhooks with
// optional HMAC-SHA256 body signing.
type WebhookNotifier struct {
	HTTPClient *http.Client
}

// Notify delivers an alert via HTTP POST/PUT.
func (w *WebhookNotifier) Notify(ctx context.Context, alert *models.Alert, channel NotificationChannel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cfg WebhookConfig
	if err := json.Unmarshal(channel.Config, &cfg); err != nil {
		return fmt.Errorf("webhook: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	platformURL := ""
	if alert.Metadata != nil {
		if p, ok := alert.Metadata["platform_url"].(string); ok {
			platformURL = p
		}
	}
	alertURL := ""
	if platformURL != "" {
		alertURL = fmt.Sprintf("%s/alerts/%s", strings.TrimRight(platformURL, "/"), alert.ID)
	}

	data := struct {
		AlertID    string
		Severity   string
		State      string
		CheckID    string
		AgentID    string
		Message    string
		Timestamp  string
		PlatformURL string
		AlertURL   string
	}{
		AlertID:    alert.ID,
		Severity:   alert.Severity,
		State:      alert.State,
		CheckID:    alert.CheckID,
		AgentID:    alert.AgentID,
		Message:    alert.Message,
		Timestamp:  alert.CreatedAt.UTC().Format(time.RFC3339),
		PlatformURL: platformURL,
		AlertURL:   alertURL,
	}

	body, err := renderBody(cfg.BodyTemplate, data)
	if err != nil {
		return fmt.Errorf("webhook: render body: %w", err)
	}

	client := w.HTTPClient
	if client == nil {
		timeout := 10 * time.Second
		if cfg.TimeoutSeconds > 0 {
			timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}

	req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: new request: %w", err)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if cfg.Secret != "" {
		sig := signHMAC(cfg.Secret, body)
		req.Header.Set("X-OAP-Signature", "sha256="+sig)
	}
	req.Header.Set("User-Agent", "OpenAgentPlatform-Webhook/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("webhook: returned status %d: %s", resp.StatusCode, string(respBody))
}

// ValidateConfig verifies the webhook channel configuration.
func (w *WebhookNotifier) ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("webhook: empty config")
	}
	var cfg WebhookConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("webhook: invalid json: %w", err)
	}
	return cfg.Validate()
}

// renderBody renders the body template against data, or returns the
// default JSON payload when no template is configured.
func renderBody(tplText string, data any) ([]byte, error) {
	if tplText == "" {
		tplText = defaultWebhookBody
	}
	funcs := template.FuncMap{
		"quote": func(s string) (string, error) {
			b, err := json.Marshal(s)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
	tpl, err := template.New("body").Funcs(funcs).Parse(tplText)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// signHMAC returns the lowercase hex HMAC-SHA256 of msg using key.
func signHMAC(key string, msg []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(msg)
	return hex.EncodeToString(mac.Sum(nil))
}
