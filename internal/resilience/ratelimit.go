// Package resilience – ratelimit.go.
//
// Token-bucket rate limiter middleware that throttles requests per
// client IP and per authenticated user.  The bucket is sharded by a
// periodic janitor that evicts idle entries so the limiter does not
// grow without bound under sustained traffic.
package resilience

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// RateLimitConfig holds tuning knobs for the token-bucket rate limiter.
type RateLimitConfig struct {
	// Rate is the sustained request rate permitted per key (req/s).
	Rate float64

	// Burst is the maximum number of requests allowed in a short
	// burst before throttling kicks in.
	Burst float64

	// Enabled toggles the middleware.  When false, the returned
	// middleware is a no-op pass-through.
	Enabled bool

	// IdleTTL controls when an unused bucket is garbage-collected.
	// Defaults to 5 minutes.
	IdleTTL time.Duration

	// CleanupInterval is how often the janitor scans for expired
	// buckets.  Defaults to 1 minute.
	CleanupInterval time.Duration

	// SkipPaths are request paths that bypass rate limiting
	// (e.g. /healthz, /readyz, /metrics).
	SkipPaths []string

	// KeyFunc extracts the per-request rate-limit key.  When nil
	// the default key is the client IP.
	KeyFunc func(*http.Request) string
}

// DefaultRateLimitConfig returns a sane production default: 100 req/s
// sustained, 200 burst, enabled.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Rate:            100,
		Burst:           200,
		Enabled:         true,
		IdleTTL:         5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// bucket is a single token-bucket instance for one key.
type bucket struct {
	tokens   float64
	lastFill time.Time
	lastSeen time.Time
}

// RateLimiter manages per-key token buckets.
type RateLimiter struct {
	cfg RateLimitConfig

	mu      sync.Mutex
	buckets map[string]*bucket

	stopCh chan struct{}
}

// NewRateLimiter constructs a RateLimiter and starts its background
// janitor goroutine.  Call Stop to terminate the janitor.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*bucket),
		stopCh:  make(chan struct{}),
	}
	if cfg.CleanupInterval > 0 {
		go rl.janitor()
	}
	return rl
}

// Stop halts the background janitor.
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// Allow reports whether a request identified by key is permitted right
// now.  When the key has no bucket yet one is lazily created.
func (rl *RateLimiter) Allow(key string) bool {
	if !rl.cfg.Enabled {
		return true
	}

	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{
			tokens:   rl.cfg.Burst,
			lastFill: now,
			lastSeen: now,
		}
		rl.buckets[key] = b
		return true
	}

	// Refill tokens based on elapsed time.  rate is tokens per second.
	elapsed := now.Sub(b.lastFill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * rl.cfg.Rate
		if b.tokens > rl.cfg.Burst {
			b.tokens = rl.cfg.Burst
		}
		b.lastFill = now
	}
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// janitor periodically removes buckets that have not been seen for
// longer than the idle TTL.
func (rl *RateLimiter) janitor() {
	ticker := time.NewTicker(rl.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case now := <-ticker.C:
			rl.mu.Lock()
			for k, b := range rl.buckets {
				if now.Sub(b.lastSeen) > rl.cfg.IdleTTL {
					delete(rl.buckets, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Middleware returns a chi-compatible HTTP middleware that enforces
// the rate limit.  Requests that are throttled receive a 429 with a
// Retry-After header.  Health and metrics endpoints are exempt.
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	skip := make(map[string]bool, len(rl.cfg.SkipPaths))
	for _, p := range rl.cfg.SkipPaths {
		skip[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			if skip[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			key := rl.key(r)
			if rl.Allow(key) {
				next.ServeHTTP(w, r)
				return
			}

			// Compute Retry-After: the time until one full token is
			// refilled.  For a sustained rate of R req/s this is
			// approximately 1/R seconds; clamp to a minimum of 1s.
			retryAfter := 1.0 / rl.cfg.Rate
			if retryAfter < 1.0 {
				retryAfter = 1.0
			}
			seconds := int(retryAfter + 0.5)
			w.Header().Set("Retry-After", itoa(seconds))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
		})
	}
}

// ChiMiddleware is a convenience wrapper that returns a chi.Middlewares
// slice containing the rate-limit middleware.  Useful with
// router.Use(rl.ChiMiddleware()...).
func (rl *RateLimiter) ChiMiddleware() chi.Middlewares {
	return chi.Middlewares{rl.Middleware()}
}

// key returns the rate-limit key for a request.  By default it is the
// remote IP.  A custom KeyFunc overrides this.
func (rl *RateLimiter) key(r *http.Request) string {
	if rl.cfg.KeyFunc != nil {
		if k := rl.cfg.KeyFunc(r); k != "" {
			return k
		}
	}
	// Trim the optional port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		addr = addr[:idx]
	}
	return addr
}

// itoa is a small, allocation-free integer-to-ASCII converter used
// for the Retry-After header.  It avoids pulling in strconv just for
// one call site.
func itoa(n int) string {
	if n < 0 {
		return "0"
	}
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
