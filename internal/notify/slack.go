// Package notify - slack.go implements the Slack webhook notifier.
// Uses Block Kit with a color-coded attachment to display alert
// metadata and a link button to the platform's alert detail page.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// SlackConfig is the type-specific configuration for the Slack channel.
type SlackConfig struct {
	WebhookURL string `json:"webhook_url"`
	Channel    string `json:"channel,omitempty"`    // optional override (e.g. "#alerts")
	Username   string `json:"username,omitempty"`   // bot display name
	IconEmoji  string `json:"icon_emoji,omitempty"` // e.g. ":rotating_light:"
	PlatformURL string `json:"platform_url,omitempty"`
}

// Validate verifies the slack channel configuration.
func (s *SlackConfig) Validate() error {
	if s.WebhookURL == "" {
		return errors.New("slack: webhook_url is required")
	}
	if !strings.HasPrefix(s.WebhookURL, "https://") && !strings.HasPrefix(s.WebhookURL, "http://") {
		return errors.New("slack: webhook_url must be http(s)")
	}
	return nil
}

// SlackNotifier delivers alerts via Slack incoming webhooks.
type SlackNotifier struct {
	HTTPClient *http.Client
}

// severityToColor returns the Slack attachment color for a given severity.
func slackSeverityColor(severity string) string {
	switch severity {
	case "info":
		return "#3b82f6" // blue
	case "warning":
		return "#f59e0b" // yellow
	case "critical":
		return "#ef4444" // red
	case "emergency":
		return "#ff0000" // bright red
	default:
		return "#6b7280" // gray
	}
}

// slackSeverityEmoji returns a leading emoji for the message.
func slackSeverityEmoji(severity string) string {
	switch severity {
	case "info":
		return ":information_source:"
	case "warning":
		return ":warning:"
	case "critical":
		return ":rotating_light:"
	case "emergency":
		return ":sos:"
	default:
		return ":bell:"
	}
}

// slackMessage is the JSON payload posted to a Slack webhook.
type slackMessage struct {
	Channel   string            `json:"channel,omitempty"`
	Username  string            `json:"username,omitempty"`
	IconEmoji string            `json:"icon_emoji,omitempty"`
	Text      string            `json:"text"`
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Title  string       `json:"title"`
	Text   string       `json:"text"`
	Fields []slackField `json:"fields"`
	Actions []slackAction `json:"actions,omitempty"`
	Footer string       `json:"footer,omitempty"`
	Ts     int64        `json:"ts,omitempty"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

type slackAction struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
	Style string `json:"style,omitempty"`
}

// Notify delivers an alert as a Block-Kit message to a Slack webhook.
func (n *SlackNotifier) Notify(ctx context.Context, alert *models.Alert, channel NotificationChannel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cfg SlackConfig
	if err := json.Unmarshal(channel.Config, &cfg); err != nil {
		return fmt.Errorf("slack: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	platformURL := cfg.PlatformURL
	if platformURL == "" {
		platformURL = "https://localhost:8443"
	}
	alertURL := fmt.Sprintf("%s/alerts/%s", strings.TrimRight(platformURL, "/"), alert.ID)

	outputSummary := alert.Message
	if len(outputSummary) > 500 {
		outputSummary = outputSummary[:497] + "..."
	}

	hostname := alert.AgentID
	if alert.Metadata != nil {
		if h, ok := alert.Metadata["hostname"].(string); ok && h != "" {
			hostname = h
		}
	}

	msg := slackMessage{
		Channel:   cfg.Channel,
		Username:  cfg.Username,
		IconEmoji: cfg.IconEmoji,
		Text:      fmt.Sprintf("%s [%s] Alert: %s", slackSeverityEmoji(alert.Severity), strings.ToUpper(alert.Severity), alert.CheckID),
		Attachments: []slackAttachment{{
			Color: slackSeverityColor(alert.Severity),
			Title: fmt.Sprintf("Check %s is failing", alert.CheckID),
			Text:  outputSummary,
			Fields: []slackField{
				{Title: "Check", Value: alert.CheckID, Short: true},
				{Title: "Agent", Value: hostname, Short: true},
				{Title: "Severity", Value: strings.ToUpper(alert.Severity), Short: true},
				{Title: "State", Value: alert.State, Short: true},
				{Title: "Timestamp", Value: alert.CreatedAt.UTC().Format(time.RFC3339), Short: true},
				{Title: "Output", Value: outputSummary, Short: false},
			},
			Actions: []slackAction{{
				Type:  "button",
				Text:  "View Alert",
				URL:   alertURL,
				Style: "primary",
			}},
			Footer: "OpenAgentPlatform",
			Ts:     alert.CreatedAt.Unix(),
		}},
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("slack: marshal: %w", err)
	}

	client := n.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("slack: webhook returned status %d: %s", resp.StatusCode, string(body))
}

// ValidateConfig verifies the slack channel configuration.
func (n *SlackNotifier) ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("slack: empty config")
	}
	var cfg SlackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("slack: invalid json: %w", err)
	}
	return cfg.Validate()
}
