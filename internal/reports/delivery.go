// Package reports - delivery.go implements report delivery via email
// (SMTP), webhook (HTTP POST), and download (presigned URL).
package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"time"
)

// DeliveryMethod identifies how a report is delivered to the user.
type DeliveryMethod string

const (
	MethodEmail   DeliveryMethod = "email"
	MethodWebhook DeliveryMethod = "webhook"
	MethodDownload DeliveryMethod = "download"
)

// Deliverer sends a generated report to its destination.
type Deliverer interface {
	Deliver(ctx context.Context, r *Report, method, target string) (DeliveryStatus, error)
}

// DefaultDeliverer is the production Deliverer implementation.
type DefaultDeliverer struct {
	// SMTPHost, SMTPPort, Username, Password, FromAddress configure email delivery.
	SMTPHost    string
	SMTPPort    int
	Username    string
	Password    string
	FromAddress string
	// HTTPClient is used for webhook delivery.
	HTTPClient *http.Client
	// BaseURL is used to build presigned download links.
	BaseURL string
	// DownloadSecret is used to sign download tokens (HMAC).
	DownloadSecret []byte
}

// NewDefaultDeliverer constructs a DefaultDeliverer with sane defaults.
func NewDefaultDeliverer() *DefaultDeliverer {
	return &DefaultDeliverer{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Deliver dispatches the report to the given target using the
// specified method. Returns the final DeliveryStatus.
func (d *DefaultDeliverer) Deliver(ctx context.Context, r *Report, method, target string) (DeliveryStatus, error) {
	switch DeliveryMethod(method) {
	case MethodEmail:
		if target == "" {
			return DeliveryFailed, fmt.Errorf("email delivery requires a target address")
		}
		if err := d.sendEmail(r, target); err != nil {
			return DeliveryFailed, err
		}
		return DeliveryDelivered, nil
	case MethodWebhook:
		if target == "" {
			return DeliveryFailed, fmt.Errorf("webhook delivery requires a target URL")
		}
		if err := d.sendWebhook(ctx, r, target); err != nil {
			return DeliveryFailed, err
		}
		return DeliveryDelivered, nil
	case MethodDownload:
		return DeliveryDownload, nil
	case "":
		return DeliveryDownload, nil
	}
	return DeliveryFailed, fmt.Errorf("unsupported delivery method: %s", method)
}

// sendEmail sends the report as an HTML email with the JSON data
// attached inline.
func (d *DefaultDeliverer) sendEmail(r *Report, toAddress string) error {
	host := d.SMTPHost
	port := d.SMTPPort
	if host == "" {
		return fmt.Errorf("SMTP host not configured")
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var auth smtp.Auth
	if d.Username != "" {
		auth = smtp.PlainAuth("", d.Username, d.Password, host)
	}

	subject := fmt.Sprintf("Report: %s", r.Title)
	body := fmt.Sprintf(
		"<!DOCTYPE html><html><body><h2>%s</h2><p>Generated: %s</p><pre>%s</pre></body></html>",
		r.Title, r.GeneratedAt.Format(time.RFC3339), string(r.Data),
	)

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", d.FromAddress))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toAddress))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	return smtp.SendMail(addr, auth, d.FromAddress, []string{toAddress}, msg.Bytes())
}

// sendWebhook POSTs the report as JSON to the target URL.
func (d *DefaultDeliverer) sendWebhook(ctx context.Context, r *Report, target string) error {
	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Report-Id", r.ID)
	req.Header.Set("X-Report-Template", r.TemplateID)

	client := d.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// PresignedURL returns a time-limited download URL for the report.
// The token is an HMAC-SHA256 of the report ID and expiry.
func (d *DefaultDeliverer) PresignedURL(r *Report, ttl time.Duration) string {
	if d.BaseURL == "" {
		return ""
	}
	expiry := time.Now().Add(ttl).Unix()
	token := signToken(d.DownloadSecret, r.ID, expiry)
	return fmt.Sprintf("%s/api/v1/reports/runs/%s/download?token=%s&exp=%d",
		d.BaseURL, r.ID, token, expiry)
}

// signToken produces a base64url-encoded token containing the
// report ID, expiry, and an HMAC tag for verification.
func signToken(secret []byte, reportID string, expiry int64) string {
	mac := hmacSum(secret, fmt.Sprintf("%s|%d", reportID, expiry))
	return base64URLEncode(fmt.Sprintf("%s|%d|%s", reportID, expiry, mac))
}
