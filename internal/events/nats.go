package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

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
//
// When a TracerProvider is configured, Publish creates a producer span and
// injects the trace context into the NATS message headers so subscribers
// can continue the trace.
func (c *Client) Publish(ctx context.Context, subject string, payload []byte) error {
	if c == nil || c.conn == nil {
		return errors.New("nats: client not connected")
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "nats.publish "+subject,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination", subject),
			attribute.String("messaging.operation", "publish"),
			attribute.Int("messaging.message.body.size", len(payload)),
		),
	)
	defer span.End()

	msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, NewHeaderCarrier(msg.Header))

	if err := c.conn.PublishMsg(msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// Subscribe registers a handler for the literal subject. The subscription is
// tracked so Close() can drain it. The returned *nats.Subscription is the
// same one returned by the underlying client.
//
// The handler is wrapped so that each delivered message becomes a
// consumer span linked to the producer span via the trace context
// embedded in the NATS message headers.
func (c *Client) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("nats: client not connected")
	}
	wrapped := c.wrapHandler(subject, handler)
	sub, err := c.conn.Subscribe(subject, wrapped)
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
	wrapped := c.wrapHandler(subject, handler)
	sub, err := c.conn.QueueSubscribe(subject, queue, wrapped)
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
	subs := c.subs
	c.subs = nil
	c.subsMu.Unlock()

	for _, s := range subs {
		if s == nil {
			continue
		}
		if err := s.Drain(); err != nil {
			c.log.Warn("nats sub drain failed", "subject", s.Subject, "err", err)
		}
	}

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
