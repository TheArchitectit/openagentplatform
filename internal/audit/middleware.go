package audit

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// Recorder is the subset of AuditService used by Middleware.
type Recorder interface {
	Record(ctx context.Context, in EventInput) (*Event, error)
}

// statusRecorder captures the response status code and byte count for a
// downstream handler so the middleware can record them in the audit event
// after the handler returns.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// skippedPathPrefixes are paths the middleware will not audit. They are
// matched by prefix against the request URL path.
var skippedPathPrefixes = []string{
	"/health",
	"/docs",
	"/ws",
}

// Middleware returns a chi-compatible middleware that records an
// api_call audit event for every request that is not in the skip list.
// The event is recorded asynchronously after the response has been written
// so a slow or failed audit insert does not block the request.
func Middleware(svc Recorder, log *slog.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil {
				next.ServeHTTP(w, r)
				return
			}
			if shouldSkip(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			duration := time.Since(start)

			actorID := ""
			orgID := ""
			siteID := ""
			if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
				actorID = claims.Subject
				orgID = claims.OrgID
				siteID = claims.SiteID
			}

			action := eventActionFromMethod(r.Method, r.URL.Path)
			// The "resource" recorded for an api_call is the API path
			// itself; the resource_id is a stable id (the matched route
			// pattern), falling back to the raw path.
			resourceID := routePattern(r)
			outcome := outcomeFromStatus(rec.status)

			details := map[string]any{
				"method":   r.Method,
				"path":     r.URL.Path,
				"status":   rec.status,
				"bytes":    rec.bytes,
				"duration_ms": duration.Milliseconds(),
			}

			// Use a detached context so audit writes survive request
			// cancellation; cap to a short timeout so a stuck DB cannot
			// accumulate goroutines.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			go func() {
				defer cancel()
				_, err := svc.Record(ctx, EventInput{
					ActorType:    ActorUser,
					ActorID:      actorID,
					Action:       action,
					ResourceType: "http",
					ResourceID:   resourceID,
					Details:      details,
					Outcome:      outcome,
					IP:           clientIP(r),
					UserAgent:    r.UserAgent(),
					OrgID:        orgID,
					SiteID:       siteID,
				})
				if err != nil {
					log.Error("audit: api_call record failed",
						"path", r.URL.Path,
						"err", err)
				}
			}()
		})
	}
}

func shouldSkip(path string) bool {
	for _, p := range skippedPathPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func eventActionFromMethod(method, path string) string {
	// E.g. GET /api/v1/agents -> "GET /api/v1/agents"
	// Keep it simple; consumers can parse the verb out if needed.
	if method == "" {
		method = "UNKNOWN"
	}
	return method + " " + path
}

// routePattern returns a stable identifier for the matched route when chi's
// RouteContext is available, otherwise the raw path.
func routePattern(r *http.Request) string {
	// chi stores the route pattern in a context value; we import the type
	// lazily to avoid adding a hard dependency in this file's package
	// (audit already depends on chi's siblings elsewhere). Fall back to
	// the raw path if the pattern is not present.
	type ctxKey struct{}
	if rc := r.Context().Value("chi.routeContext"); rc != nil {
		if getter, ok := rc.(interface{ RoutePattern() string }); ok {
			if p := getter.RoutePattern(); p != "" {
				return p
			}
		}
	}
	return r.URL.Path
}

// clientIP returns the best-effort client IP, preferring the X-Forwarded-For
// header if present (set by chi's RealIP middleware).
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		// Take the first entry (original client).
		if comma := strings.Index(h, ","); comma >= 0 {
			return strings.TrimSpace(h[:comma])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return strings.TrimSpace(h)
	}
	return r.RemoteAddr
}

func outcomeFromStatus(status int) Outcome {
	switch {
	case status == 0:
		return OutcomeSuccess
	case status >= 200 && status < 300:
		return OutcomeSuccess
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return OutcomeDenied
	case status >= 400 && status < 500:
		return OutcomeFailure
	default:
		return OutcomeError
	}
}
