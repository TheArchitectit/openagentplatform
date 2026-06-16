package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"
)

const (
	// SubjectHeartbeatPrefix is the wildcard subject every agent publishes
	// heartbeats on. Each agent's full subject is
	// oap.agents.<agent_id>.heartbeat.
	SubjectHeartbeatPrefix = "oap.agents.*.heartbeat"

	// SubjectCheckResultsPrefix is the wildcard subject agents publish
	// check results on.
	SubjectCheckResultsPrefix = "oap.agents.*.results"

	// SubjectAgentEvents is where the server publishes lifecycle events
	// (AgentOnline, AgentOffline, etc.) for downstream consumers.
	SubjectAgentEvents = "oap.events.agent"

	// SubjectCheckAssignmentPrefix is the per-agent subject check
	// assignments are published on.
	SubjectCheckAssignmentPrefix = "oap.agents"

	// SubjectCheckResultPrefix is the wildcard subject the check result
	// ingest pipeline subscribes to. It mirrors SubjectCheckResultsPrefix
	// but is named with a "Result" suffix to disambiguate from the existing
	// result subject consumed by CheckDispatcher.
	SubjectCheckResultPrefix = "oap.agents.*.results"

	// SubjectAlertEvents is the wildcard subject the threshold evaluator
	// publishes alert lifecycle events on. Consumers (WebSocket hub, pager
	// integrations) subscribe to this subject to receive AlertFired /
	// AlertResolved notifications.
	SubjectAlertEvents = "oap.events.alerts"

	// SubjectCheckResultEvent is the wildcard subject the ingest pipeline
	// publishes to whenever a new check result is persisted. The WebSocket
	// hub subscribes here to broadcast live result updates to connected
	// dashboards.
	SubjectCheckResultEvent = "oap.events.checks.result"
)

// HeartbeatStaleThreshold is the duration after which a silent agent is
// considered offline.
const HeartbeatStaleThreshold = 120 * 1_000_000_000 // 120s in ns; kept as a hint for callers using time.Duration elsewhere

type Client struct {
	conn   *nats.Conn
	log    *slog.Logger
	subsMu sync.Mutex
	subs   []*nats.Subscription
}

// NewClient dials NATS, applies optional TLS material, and returns a Client
// that owns the underlying connection and tracks every subscription created
// through Subscribe* so they can be drained on shutdown.
func NewClient(url, certFile, keyFile, caFile string, log *slog.Logger) (*Client, error) {
	if log == nil {
		log = slog.Default()
	}
	opts := []nats.Option{
		nats.Name("openagentplatform-server"),
		nats.MaxReconnects(-1),
		nats.ReconnectHandler(func(c *nats.Conn) {
			log.Warn("nats reconnected", "url", c.ConnectedUrl())
		}),
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", "err", err)
			}
		}),
		nats.ClosedHandler(func(c *nats.Conn) {
			log.Warn("nats connection closed", "err", c.LastError())
		}),
		nats.ErrorHandler(func(c *nats.Conn, sub *nats.Subscription, err error) {
			log.Error("nats async error", "subject", sub.Subject, "err", err)
		}),
	}

	if certFile != "" && keyFile != "" {
		opts = append(opts, nats.ClientCert(certFile, keyFile))
	}
	if caFile != "" {
		opts = append(opts, nats.RootCAs(caFile))
	}

	conn, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect: %w", err)
	}
	return &Client{conn: conn, log: log}, nil
}

func (c *Client) Conn() *nats.Conn { return c.conn }

// Publish sends payload on subject. The ctx is reserved for future use; the
// underlying nats-go API is synchronous and does not respect context
// cancellation in this client version.
func (c *Client) Publish(ctx context.Context, subject string, payload []byte) error {
	_ = ctx
	if c == nil || c.conn == nil {
		return errors.New("nats: client not connected")
	}
	return c.conn.Publish(subject, payload)
}

// Subscribe registers a handler for the literal subject. The subscription is
// tracked so Close() can drain it. The returned *nats.Subscription is the
// same one returned by the underlying client.
func (c *Client) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("nats: client not connected")
	}
	sub, err := c.conn.Subscribe(subject, handler)
	if err != nil {
		return nil, fmt.Errorf("nats: subscribe %q: %w", subject, err)
	}
	c.subsMu.Lock()
	c.subs = append(c.subs, sub)
	c.subsMu.Unlock()
	return sub, nil
}

// SubscribeQueue joins a queue group for load-balanced work distribution.
func (c *Client) SubscribeQueue(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("nats: client not connected")
	}
	sub, err := c.conn.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, fmt.Errorf("nats: queue subscribe %q/%q: %w", subject, queue, err)
	}
	c.subsMu.Lock()
	c.subs = append(c.subs, sub)
	c.subsMu.Unlock()
	return sub, nil
}

// Close drains every tracked subscription and the underlying connection.
// Safe to call multiple times.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.subsMu.Lock()
	for _, s := range c.subs {
		if s == nil {
			continue
		}
		if err := s.Drain(); err != nil {
			c.log.Warn("nats sub drain failed", "subject", s.Subject, "err", err)
		}
	}
	c.subs = nil
	c.subsMu.Unlock()

	if c.conn != nil {
		c.conn.Drain()
	}
}

// IsConnected reports whether the underlying NATS connection is currently
// connected. Used by readiness probes.
func (c *Client) IsConnected() bool {
	if c == nil || c.conn == nil {
		return false
	}
	return c.conn.Status() == nats.CONNECTED
}
