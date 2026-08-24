package events

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
		// same trace. The traceparent header follows the W3C format:
		// version-traceID-parentID-traceFlags.
		sc := span.SpanContext()
		traceparent := fmt.Sprintf("00-%s-%s-%s",
			sc.TraceID().String(),
			sc.SpanID().String(),
			fmt.Sprintf("%02x", sc.TraceFlags()))
		msg.Header.Set("traceparent", traceparent)
		handler(msg)

		// If the handler called span.RecordError via the context, the
		// span status is already set. We only mark the span as failed
		// when the handler itself returns an error (signalled via
		// context.Value sentinel) -- but nats.MsgHandler has no error
		// return, so we leave status at Unset for handler-level errors.
		_ = ctx // reserved for future handler error propagation
	}
}
