package telemetry

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const requestIDHeader = "X-Request-ID"

// healthCheckPaths are skipped from tracing to avoid noise on load-balancer
// probes.
var healthCheckPaths = map[string]struct{}{
	"/healthz":     {},
	"/health":      {},
	"/ready":       {},
	"/readyz":      {},
	"/live":        {},
	"/livez":       {},
	"/ping":        {},
	"/favicon.ico": {},
}

// ctxKey is a private type to prevent collisions in context.Value lookups.
type ctxKey int

const requestIDKey ctxKey = iota

// HTTPMiddleware returns a chi-compatible middleware that creates a span
// for every incoming request.  Health-check and probe endpoints are skipped
// to keep traces focused on real traffic.
//
// Each span is tagged with:
//   - http.method
//   - http.route  (the chi route pattern, when available)
//   - http.status_code
//   - http.url
//
// Incoming trace context is extracted from the request headers using the
// globally registered propagator so downstream calls in the handler can
// participate in the same trace.  The X-Request-ID header is additionally
// recorded as a span attribute and stored in the request context so
// downstream handlers can correlate logs.
func HTTPMiddleware() func(http.Handler) http.Handler {
	tracer := otel.Tracer("openagentplatform/http")
	propagator := otel.GetTextMapPropagator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isHealthCheckPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract any incoming trace context so this request joins
			// an existing trace if the caller already started one.
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := r.Method + " " + routePattern(r)
			ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			// Propagate the request id so downstream handlers can
			// correlate their logs with this HTTP call.  The id is
			// attached both to the request context (via a typed key)
			// and as a span attribute for easy filtering.
			if reqID := r.Header.Get(requestIDHeader); reqID != "" {
				ctx = context.WithValue(ctx, requestIDKey, reqID)
				span.SetAttributes(attribute.String(requestIDHeader, reqID))
			}

			span.SetAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPath(r.URL.Path),
				semconv.URLQuery(r.URL.RawQuery),
				attribute.String("http.url", r.URL.String()),
			)
			if route := routePattern(r); route != "" {
				span.SetAttributes(semconv.HTTPRoute(route))
			}

			// Wrap the ResponseWriter so we can capture the status code.
			ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r.WithContext(ctx))

			span.SetAttributes(semconv.HTTPResponseStatusCode(ww.status))
			if ww.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(ww.status))
			}
		})
	}
}

// statusRecorder captures the HTTP status code written by downstream handlers
// so the middleware can record it on the span after the handler returns.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// routePattern returns the chi route pattern for the request (e.g.
// "/api/v1/agents/{id}"), or the raw path if no pattern matched.  The chi
// router stores the pattern under RouteContext().
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return ""
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return r.URL.Path
}

// isHealthCheckPath reports whether the given path is a known health/probe
// endpoint.
func isHealthCheckPath(path string) bool {
	if _, ok := healthCheckPaths[path]; ok {
		return true
	}
	p := "/" + strings.TrimPrefix(path, "/")
	if _, ok := healthCheckPaths[p]; ok {
		return true
	}
	return false
}

// RequestIDFromContext returns the request ID stored in ctx by the tracing
// middleware, or an empty string if none was set.
func RequestIDFromContext(ctx context.Context) string {
	v := ctx.Value(requestIDKey)
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
