package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// NATSClient wraps a NATS connection with mTLS, reconnect handling, and a
// small helper surface used by the rest of the agent.
type NATSClient struct {
	conn *nats.Conn
	log  *slog.Logger
}

// ConnectNATS dials the NATS server using mTLS if cert/key files are provided.
// The connection uses sensible reconnect defaults; callers can wait on
// Connected() to ensure the initial dial succeeded.
func ConnectNATS(ctx context.Context, url, caFile, certFile, keyFile string, log *slog.Logger) (*NATSClient, error) {
	opts := []nats.Option{
		nats.Name("oap-agent"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.Timeout(10 * time.Second),
		nats.PingInterval(30 * time.Second),
		nats.MaxPingsOutstanding(2),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if sub != nil {
				log.Warn("nats subscription error", "subject", sub.Subject, "err", err)
			} else {
				log.Warn("nats connection error", "err", err)
			}
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Warn("nats connection closed")
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn("nats disconnected", "err", err)
		}),
	}

	if caFile != "" || certFile != "" || keyFile != "" {
		opts = append(opts, nats.ClientCert(certFile, keyFile))
		opts = append(opts, nats.RootCAs(caFile))
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	c := &NATSClient{conn: conn, log: log}
	if !conn.IsConnected() {
		conn.Close()
		return nil, fmt.Errorf("nats not connected after dial")
	}
	return c, nil
}

// Conn returns the underlying *nats.Conn for advanced operations.
func (c *NATSClient) Conn() *nats.Conn { return c.conn }

// Close drains pending messages and closes the connection.
func (c *NATSClient) Close() {
	if c == nil || c.conn == nil {
		return
	}
	_ = c.conn.Drain()
}

// Publish sends a message on the given subject.
func (c *NATSClient) Publish(ctx context.Context, subject string, data []byte) error {
	if err := c.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("nats publish %s: %w", subject, err)
	}
	return c.conn.Flush()
}

// Subscribe registers a handler on subject. The returned subscription should
// be drained on shutdown.
func (c *NATSClient) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	sub, err := c.conn.Subscribe(subject, handler)
	if err != nil {
		return nil, fmt.Errorf("nats subscribe %s: %w", subject, err)
	}
	return sub, nil
}

// QueueSubscribe registers a queue-grouped subscription.
func (c *NATSClient) QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	sub, err := c.conn.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, fmt.Errorf("nats queue subscribe %s: %w", subject, err)
	}
	return sub, nil
}

// Request performs a request/reply with the given timeout.
func (c *NATSClient) Request(_ context.Context, subject string, data []byte, timeout time.Duration) (*nats.Msg, error) {
	return c.conn.Request(subject, data, timeout)
}
