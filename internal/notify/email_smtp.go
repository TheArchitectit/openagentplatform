package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
)

// sendMail delivers the message via SMTP, supporting implicit TLS, STARTTLS,
// and plaintext. It is blocking but respects ctx cancellation between
// connection attempts. When the context is cancelled, the underlying SMTP
// goroutine may still be running; the connection will time out naturally.
func sendMail(ctx context.Context, cfg EmailConfig, addr string, msg []byte) error {
	done := make(chan error, 1)
	go func() {
		done <- doSendMail(cfg, addr, msg)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Context cancelled; the goroutine will finish when the SMTP
		// connection times out. We do not block waiting for it.
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
