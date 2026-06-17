// Package telemetry provides OpenTelemetry tracing primitives for the
// openagentplatform server: a TracerProvider initialiser, span helpers,
// HTTP middleware, NATS propagation, and a pgxpool wrapper.
package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	batchTimeout    = 1 * time.Second
	maxBatchSize    = 512
	shutdownGrace   = 5 * time.Second
	envOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

// InitTracer creates a *sdktrace.TracerProvider configured with an OTLP gRPC
// exporter pointing at endpoint. If endpoint is empty the function falls back
// to the value of the OTEL_EXPORTER_OTLP_ENDPOINT environment variable; when
// neither is set a no-op provider is installed so callers can safely record
// spans without checking for nil.
//
// The provider is configured with:
//   - BatchTimeout:        1s
//   - MaxExportBatchSize: 512
//
// Shutdown should be called with a context that has at least 5s to allow the
// remaining spans to flush.
func InitTracer(ctx context.Context, serviceName, endpoint string) (*sdktrace.TracerProvider, error) {
	if endpoint == "" {
		endpoint = os.Getenv(envOTLPEndpoint)
	}

	// Always set up the W3C trace-context + baggage propagators so
	// downstream code that calls otel.GetTextMapPropagator gets a
	// working propagator even when the exporter is absent.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if endpoint == "" {
		// No collector configured -- install a minimal SDK provider with
		// no processors.  Spans created against it are dropped silently
		// so callers do not need to check for nil.  Shutdown is still
		// safe to call.
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		otel.SetTracerProvider(tp)
		return tp, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithOS(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	exporter, err := otlptrace.New(ctx,
		otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create otlp exporter: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithBatchTimeout(batchTimeout),
		sdktrace.WithMaxExportBatchSize(maxBatchSize),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tp)

	return tp, nil
}

// Shutdown gracefully shuts down the TracerProvider, flushing any
// remaining spans. It is safe to call with a nil receiver.
func Shutdown(ctx context.Context, tp *sdktrace.TracerProvider) error {
	if tp == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	return tp.Shutdown(shutdownCtx)
}

// Tracer returns the named tracer from the global provider. It is a thin
// convenience wrapper around otel.Tracer so callers do not need to import
// the otel package directly.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
