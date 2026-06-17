package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SpanAttr is a convenience alias so callers can pass attributes without
// importing the otel/attribute package directly.
type SpanAttr = attribute.KeyValue

// StringAttr creates a string attribute key/value pair.
func StringAttr(key, value string) SpanAttr {
	return attribute.String(key, value)
}

// IntAttr creates an integer attribute key/value pair.
func IntAttr(key string, value int) SpanAttr {
	return attribute.Int(key, value)
}

// BoolAttr creates a boolean attribute key/value pair.
func BoolAttr(key string, value bool) SpanAttr {
	return attribute.Bool(key, value)
}

// StartSpan begins a new span using the global tracer with the given name
// and attributes.  The returned context carries the span so child spans and
// log fields can be correlated.
func StartSpan(ctx context.Context, name string, attrs ...SpanAttr) (context.Context, trace.Span) {
	return Tracer("openagentplatform").Start(ctx, name, trace.WithAttributes(attrs...))
}

// AddSpanEvents adds a named event with the given attributes to the span.
// Events are useful for recording discrete occurrences within a span
// (e.g. "cache_miss", "retry_attempt").
func AddSpanEvents(span trace.Span, name string, attrs ...SpanAttr) {
	if span == nil {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// RecordError marks the span as failed and attaches the error as a span
// event. The span status is set to Error.
func RecordError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanStatus sets the span's status code and optional description.
func SetSpanStatus(span trace.Span, code codes.Code, description string) {
	if span == nil {
		return
	}
	span.SetStatus(code, description)
}
