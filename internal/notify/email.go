// Package notify - email.go implements the SMTP email notifier.
// Uses only the Go standard library (net/smtp, net/textproto, html,
// text/template) -- no external dependencies.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/smtp"
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

// emailHTMLTemplate is the HTML body. Uses {{ }} delimiters to avoid
// conflicts with Go template syntax elsewhere.
const emailHTMLTemplate = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; margin: 0; padding: 0; background: #f3f4f6; }
    .container { max-width: 600px; margin: 0 auto; background: #ffffff; }
    .header { padding: 20px 24px; color: #ffffff; font-size: 20px; font-weight: 600; }
    .body { padding: 24px; color: #1f2937; font-size: 14px; line-height: 1.5; }
    .body h2 { margin: 0 0 12px 0; font-size: 16px; }
    .body table { width: 100%; border-collapse: collapse; margin: 12px 0; }
    .body table td { padding: 8px 0; border-bottom: 1px solid #e5e7eb; font-size: 14px; }
    .body table td:first-child { font-weight: 600; width: 140px; color: #6b7280; }
    .message { background: #f9fafb; border-left: 3px solid #d1d5db; padding: 12px 16px; margin: 16px 0; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 13px; white-space: pre-wrap; }
    .footer { padding: 16px 24px; background: #f9fafb; font-size: 12px; color: #6b7280; text-align: center; }
    .btn { display: inline-block; background: #1f2937; color: #ffffff !important; text-decoration: none; padding: 10px 20px; border-radius: 4px; font-size: 14px; font-weight: 500; margin-top: 8px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header" style="background: {{.Color}};">[{{.Severity | upper}}] {{.Title}}</div>
    <div class="body">
      <h2>Alert Details</h2>
      <table>
        <tr><td>Severity</td><td>{{.Severity}}</td></tr>
        <tr><td>Check</td><td>{{.CheckID}}</td></tr>
        <tr><td>Agent</td><td>{{.AgentID}}</td></tr>
        <tr><td>State</td><td>{{.State}}</td></tr>
        <tr><td>Triggered</td><td>{{.Timestamp}}</td></tr>
      </table>
      <div class="message">{{.Message}}</div>
      <a class="btn" href="{{.AlertURL}}">View Alert in OpenAgentPlatform</a>
    </div>
    <div class="footer">OpenAgentPlatform &middot; sent {{.SentAt}}</div>
  </div>
</body>
</html>`

// emailTextTemplate is the plain-text fallback.
const emailTextTemplate = `[{{.Severity | upper}}] {{.Title}}

Alert Details
-------------
Severity:   {{.Severity}}
Check:      {{.CheckID}}
Agent:      {{.AgentID}}
State:      {{.State}}
Triggered:  {{.Timestamp}}

{{.Message}}

View alert: {{.AlertURL}}
---

OpenAgentPlatform · sent {{.SentAt}}
`

var (
	htmlTpl = template.Must(template.New("email").Funcs(template.FuncMap{
		"upper": strings.ToUpper,
	}).Parse(emailHTMLTemplate))
	textTpl = template.Must(template.New("email_text").Funcs(template.FuncMap{
		"upper": strings.ToUpper,
	}).Parse(emailTextTemplate))
)

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

// sendMail delivers the message via SMTP, supporting implicit TLS, STARTTLS,
// and plaintext. It is blocking but respects ctx cancellation between
// connection attempts.
func sendMail(ctx context.Context, cfg EmailConfig, addr string, msg []byte) error {
	done := make(chan error, 1)
	go func() {
		done <- doSendMail(cfg, addr, msg)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func doSendMail(cfg EmailConfig, addr string, msg []byte) error {
	host, _, _ := net.SplitHostPort(addr)
	if host == "" {
		host = cfg.Host
	}

	if cfg.UseTLS {
		// Implicit TLS (SMTPS, typically port 465).
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return fmt.Errorf("email: tls dial: %w", err)
		}
		c, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return fmt.Errorf("email: smtp client: %w", err)
		}
		defer c.Quit()
		return smtpAuthAndSend(c, cfg, msg)
	}

	// Plaintext connection with optional STARTTLS upgrade.
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("email: dial: %w", err)
	}
	defer c.Quit()
	if cfg.UseStartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}
	return smtpAuthAndSend(c, cfg, msg)
}

func smtpAuthAndSend(c *smtp.Client, cfg EmailConfig, msg []byte) error {
	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := c.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	for _, to := range cfg.ToAddresses {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("email: RCPT TO %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return nil
}
