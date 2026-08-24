// Package notify - email.go implements the SMTP email notifier.
// Uses only the Go standard library (net/smtp, net/textproto, html,
// text/template) -- no external dependencies.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// EmailConfig is the type-specific configuration for the email channel.
// It is decoded from the NotificationChannel.Config blob.
type EmailConfig struct {
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username,omitempty"`
	Password    string   `json:"password,omitempty"`
	FromAddress string   `json:"from_address"`
	FromName    string   `json:"from_name,omitempty"`
	ToAddresses []string `json:"to_addresses"`
	Subject     string   `json:"subject,omitempty"`
	UseTLS      bool     `json:"use_tls"`
	UseStartTLS bool     `json:"use_starttls"`
	PlatformURL string   `json:"platform_url,omitempty"` // base URL for action links
}

// ValidateConfig checks the email configuration.
func (e *EmailConfig) Validate() error {
	if e.Host == "" {
		return errors.New("email: host is required")
	}
	if e.Port <= 0 || e.Port > 65535 {
		return errors.New("email: port must be between 1 and 65535")
	}
	if e.FromAddress == "" {
		return errors.New("email: from_address is required")
	}
	if len(e.ToAddresses) == 0 {
		return errors.New("email: at least one to_address is required")
	}
	return nil
}

// EmailNotifier delivers alerts via SMTP. Supports implicit TLS, STARTTLS,
// and plain-text SMTP. Plain-text fallback is generated alongside the
// HTML body.
type EmailNotifier struct{}

// Severity-to-color mapping for the HTML header.
var emailSeverityColors = map[string]string{
	"info":      "#3b82f6",
	"warning":   "#f59e0b",
	"critical":  "#ef4444",
	"emergency": "#7f1d1d",
}

// Notify delivers an alert via SMTP.
func (n *EmailNotifier) Notify(ctx context.Context, alert *models.Alert, channel NotificationChannel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cfg EmailConfig
	if err := json.Unmarshal(channel.Config, &cfg); err != nil {
		return fmt.Errorf("email: decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	subject := cfg.Subject
	if subject == "" {
		subject = fmt.Sprintf("[%s] Alert: %s", strings.ToUpper(alert.Severity), alert.CheckID)
	}

	// Lookup color; default to gray for unknown severities.
	color, ok := emailSeverityColors[alert.Severity]
	if !ok {
		color = "#6b7280"
	}

	platformURL := cfg.PlatformURL
	if platformURL == "" {
		platformURL = "https://localhost:8443"
	}
	alertURL := fmt.Sprintf("%s/alerts/%s", strings.TrimRight(platformURL, "/"), alert.ID)

	data := struct {
		Title     string
		Severity  string
		CheckID   string
		AgentID   string
		State     string
		Timestamp string
		Message   string
		Color     string
		AlertURL  string
		SentAt    string
	}{
		Title:     fmt.Sprintf("Check %s is failing", alert.CheckID),
		Severity:  alert.Severity,
		CheckID:   alert.CheckID,
		AgentID:   alert.AgentID,
		State:     alert.State,
		Timestamp: alert.CreatedAt.UTC().Format(time.RFC3339),
		Message:   alert.Message,
		Color:     color,
		AlertURL:  alertURL,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
	}

	var htmlBuf, textBuf bytes.Buffer
	if err := htmlTpl.Execute(&htmlBuf, data); err != nil {
		return fmt.Errorf("email: render html: %w", err)
	}
	if err := textTpl.Execute(&textBuf, data); err != nil {
		return fmt.Errorf("email: render text: %w", err)
	}

	msg := buildMIMEMessage(cfg.FromAddress, cfg.FromName, cfg.ToAddresses, subject, textBuf.String(), htmlBuf.String())

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return sendMail(ctx, cfg, addr, msg)
}

// ValidateConfig verifies the email channel configuration.
func (n *EmailNotifier) ValidateConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("email: empty config")
	}
	var cfg EmailConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("email: invalid json: %w", err)
	}
	return cfg.Validate()
}

// buildMIMEMessage constructs a minimal multipart/alternative MIME message.
func buildMIMEMessage(fromAddr, fromName string, toAddrs []string, subject, textBody, htmlBody string) []byte {
	var b bytes.Buffer
	if fromName != "" {
		fmt.Fprintf(&b, "From: %s <%s>\r\n", fromName, fromAddr)
	} else {
		fmt.Fprintf(&b, "From: %s\r\n", fromAddr)
	}
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(toAddrs, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"oap-boundary\"\r\n")
	b.WriteString("\r\n")
	b.WriteString("--oap-boundary\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	b.WriteString(textBody)
	b.WriteString("\r\n")
	b.WriteString("--oap-boundary\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")
	b.WriteString("--oap-boundary--\r\n")
	return b.Bytes()
}
