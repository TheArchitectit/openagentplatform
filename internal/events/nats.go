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
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation name used for all NATS-related spans.
const tracerName = "openagentplatform/nats"

// natsHeaderCarrier adapts a nats.Header to the otel TextMapCarrier interface
// so the trace context can be serialised into NATS message headers.
type natsHeaderCarrier struct{ hdr nats.Header }

// NewHeaderCarrier returns a TextMapCarrier backed by a nats.Header.
// Used internally by Publish/Subscribe to inject/extract trace context.
func NewHeaderCarrier(hdr nats.Header) propagation.TextMapCarrier {
	if hdr == nil {
		hdr = nats.Header{}
	}
	return &natsHeaderCarrier{hdr: hdr}
}

func (c *natsHeaderCarrier) Get(key string) string {
	return c.hdr.Get(key)
}

func (c *natsHeaderCarrier) Set(key, value string) {
	c.hdr.Set(key, value)
}

func (c *natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.hdr))
	for k := range c.hdr {
		keys = append(keys, k)
	}
	return keys
}

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

	// SubjectPatchEvents is the wildcard subject the patch management
	// subsystem publishes to whenever a patch is approved, deployed,
	// rolled back, or its status changes. The WebSocket hub subscribes
	// here to broadcast live patch updates to connected dashboards.
	SubjectPatchEvents = "oap.events.patches"

	// SubjectScriptEvents is the wildcard subject the script execution
	// subsystem publishes to whenever a script is run, completes, or
	// its status changes. The WebSocket hub subscribes here to broadcast
	// live script updates to connected dashboards.
	SubjectScriptEvents = "oap.events.scripts"
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

// wrapHandler returns a nats.MsgHandler that extracts the producer's trace
// context from the message headers, starts a consumer span, and invokes
// the user-provided handler.  The consumer span ends when the handler
// returns.
func (c *Client) wrapHandler(subject string, handler nats.MsgHandler) nats.MsgHandler {
	tracer := otel.Tracer(tracerName)
	propagator := otel.GetTextMapPropagator()

	return func(msg *nats.Msg) {
		parentCtx := context.Background()
		if msg.Header != nil {
			parentCtx = propagator.Extract(parentCtx, NewHeaderCarrier(msg.Header))
		}
		ctx, span := tracer.Start(parentCtx, "nats.subscribe "+msg.Subject,
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "nats"),
				attribute.String("messaging.source", msg.Subject),
				attribute.String("messaging.destination", subject),
				attribute.String("messaging.operation", "subscribe"),
				attribute.Int("messaging.message.body.size", len(msg.Data)),
			),
		)
		defer span.End()

		// Replace the message's context with our traced one so the
		// downstream handler can call telemetry.StartSpan and join the
		// same trace.
		msg.Header.Set("traceparent", span.SpanContext().TraceID().String())
		handler(msg)

		// If the handler called span.RecordError via the context, the
		// span status is already set. We only mark the span as failed
		// when the handler itself returns an error (signalled via
		// context.Value sentinel) -- but nats.MsgHandler has no error
		// return, so we leave status at Unset for handler-level errors.
		_ = ctx
	}
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
