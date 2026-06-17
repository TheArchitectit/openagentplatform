// Package gateway - middleware.go implements HTTP middleware for the
// A2A gateway: request ID injection, structured logging, panic recovery,
// and CORS. These are transport-agnostic helpers usable with any
// net/http-compatible router.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

// ============================================================
// Context keys
// ============================================================

type contextKey string

const (
	// CtxKeyRequestID is the context key for the per-request ID.
	CtxKeyRequestID contextKey = "requestID"

	// CtxKeyIdentity is the context key for the authenticated identity.
	CtxKeyIdentity contextKey = "identity"

	// CtxKeyStartTime is the context key for the request start time.
	CtxKeyStartTime contextKey = "startTime"
)

// ============================================================
// RequestID middleware
// ============================================================

// RequestIDMiddleware injects a unique request ID into both the
// request context and the X-Request-ID response header. If the
// incoming request already has an X-Request-ID header, it is reused.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", rid)

		ctx := context.WithValue(r.Context(), CtxKeyRequestID, rid)
		ctx = context.WithValue(ctx, CtxKeyStartTime, time.Now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extracts the request ID from a context.
// Returns empty string if not set.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// IdentityFromContext extracts the authenticated identity from a context.
// Returns nil if not set.
func IdentityFromContext(ctx context.Context) *Identity {
	if v, ok := ctx.Value(CtxKeyIdentity).(*Identity); ok {
		return v
	}
	return nil
}

// ============================================================
// Logging middleware
// ============================================================

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// LoggingMiddleware logs each request with method, path, status code,
// duration, and request ID using the provided slog logger.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rec, r)

			rid := RequestIDFromContext(r.Context())
			startTime, _ := r.Context().Value(CtxKeyStartTime).(time.Time)
			duration := time.Since(startTime)

			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.statusCode),
				slog.Duration("duration", duration),
				slog.String("request_id", rid),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// ============================================================
// Recovery middleware
// ============================================================

// RecoveryMiddleware catches panics in downstream handlers, logs them
// with the request ID and stack trace, and returns a 500 JSON response.
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					rid := RequestIDFromContext(r.Context())
					logger.Error("panic recovered",
						slog.Any("error", err),
						slog.String("request_id", rid),
						slog.String("stack", string(debug.Stack())),
					)
					writeJSONError(w, http.StatusInternalServerError, "internal server error", rid)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================
// CORS middleware
// ============================================================

// CORSConfig configures the CORS middleware.
type CORSConfig struct {
	// AllowedOrigins is the list of allowed Origin header values.
	// Use ["*"] to allow all origins.
	AllowedOrigins []string

	// AllowedMethods lists the permitted HTTP methods.
	// Default: GET, POST, PUT, DELETE, OPTIONS
	AllowedMethods []string

	// AllowedHeaders lists the permitted request headers.
	// Default: Content-Type, Authorization, X-Request-ID
	AllowedHeaders []string

	// MaxAge is the Access-Control-Max-Age value in seconds.
	MaxAge int
}

// Default CORS configuration.
var defaultCORS = CORSConfig{
	AllowedOrigins: []string{"*"},
	AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
	AllowedHeaders: []string{"Content-Type", "Authorization", "X-Request-ID"},
	MaxAge:         86400,
}

// CORSMiddleware returns a middleware that sets CORS headers and
// handles preflight OPTIONS requests.
func CORSMiddleware(cfg CORSConfig) func(http.Handler) http.Handler {
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = defaultCORS.AllowedOrigins
	}
	if len(cfg.AllowedMethods) == 0 {
		cfg.AllowedMethods = defaultCORS.AllowedMethods
	}
	if len(cfg.AllowedHeaders) == 0 {
		cfg.AllowedHeaders = defaultCORS.AllowedHeaders
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = defaultCORS.MaxAge
	}

	origins := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		origins[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (origins["*"] || origins[origin]) {
				if origins["*"] && origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", joinStrings(cfg.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", joinStrings(cfg.AllowedHeaders, ", "))
				if cfg.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", itoa(cfg.MaxAge))
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================
// Authentication middleware
// ============================================================

// AuthMiddleware extracts credentials from the request and populates
// the context with an Identity. If the gateway requires auth and no
// valid credentials are present, the request is rejected with 401.
func AuthMiddleware(auth *Authenticator, requireAuth bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identity, err := auth.Authenticate(r)
			if err != nil {
				if requireAuth {
					rid := RequestIDFromContext(r.Context())
					writeJSONError(w, http.StatusUnauthorized, err.Error(), rid)
					return
				}
				// Auth not required; proceed without identity
				identity = nil
			}

			ctx := context.WithValue(r.Context(), CtxKeyIdentity, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ============================================================
// JSON response helpers
// ============================================================

// writeJSONError writes a standard JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]any{
		"error": map[string]any{
			"code":       status,
			"message":    message,
			"request_id": requestID,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// joinStrings is a small helper to avoid importing strings just for Join.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

// itoa converts a non-negative int to its decimal string representation
// without importing strconv for a single use.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
