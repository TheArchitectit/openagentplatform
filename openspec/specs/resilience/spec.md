# Resilience

> **Phase:** 5 (Production Hardening — Sprint 5.3 "Resilience + Documentation")
> **STATUS: PARTIAL**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** internal/resilience/

---

## Description

The resilience capability provides the platform's cross-cutting failure
protection primitives as a standalone Go package with no platform
dependencies beyond `go-chi` (for middleware typing) and `slog`. It
contains five source files:

| File | Primitive |
|------|-----------|
| `ratelimit.go` | Per-key token-bucket rate limiter with an idle-bucket janitor, exposed as chi/net-http middleware |
| `breaker.go` | Three-state (closed → open → half-open) circuit breaker with an `OnStateChange` metrics hook |
| `graceful.go` | `GracefulShutdown` coordinator: HTTP drain, in-flight tracking, LIFO dependency teardown with per-closer timeouts |
| `retry.go` | Context-aware exponential-backoff retry with jitter and an HTTP-specific retryable predicate |

Production wiring lives in `cmd/server`: the rate limiter wraps the entire
HTTP handler stack (`server_init_a2a.go` `buildHTTPServer`) and is also
applied inside the API router (`internal/api/handler.go` `buildRouter`);
the graceful-shutdown coordinator owns the ordered teardown of 16 named
dependencies (`server_start.go`); a circuit breaker named `"adapter"` is
constructed in `wireSupportServices` for the Python adapter service.

## User Story

**As** a platform operator,
**I want** the API server to throttle abusive clients, fail fast when a
downstream dependency is unhealthy, and shut down in an orderly way that
drains in-flight requests before closing databases and message buses,
**so that** a single misbehaving client or a failing dependency cannot
take down the whole platform, and restarts never corrupt in-flight work.

---

## Requirements

### 1. Token-Bucket Rate Limiter

1.1. `RateLimiter` (`ratelimit.go`) MUST maintain one token bucket per key
in a mutex-guarded map. Buckets are created lazily on first request with
`Burst` tokens; tokens MUST refill continuously at `Rate` tokens/second
(fractional, capped at `Burst`), and a request MUST be allowed iff at
least one whole token is available, consuming exactly one token.

1.2. `DefaultRateLimitConfig()` MUST return Rate 100 req/s, Burst 200,
enabled, `IdleTTL` 5 minutes, `CleanupInterval` 1 minute. Production
wiring in `cmd/server/server_init_a2a.go` MUST use these same values and
additionally set `SkipPaths: ["/healthz", "/readyz", "/metrics"]`.

1.3. A background janitor goroutine MUST evict buckets idle for longer
than `IdleTTL`, scanning every `CleanupInterval`; `Stop()` MUST terminate
it. The janitor MUST NOT be started when `CleanupInterval <= 0`. When
`Enabled` is false, both `Allow` and the middleware MUST pass every
request through.

1.4. The middleware (`Middleware()` / `ChiMiddleware()`) MUST return HTTP
429 with JSON body `{"error":"rate limit exceeded"}` and a `Retry-After`
header when a request is throttled. `Retry-After` MUST be
`ceil(1 / Rate)` seconds clamped to a minimum of 1 second. Paths listed
in `SkipPaths` MUST bypass limiting entirely (exact `r.URL.Path` match).

1.5. The default rate-limit key MUST be the client IP (`r.RemoteAddr`
with the port stripped); a custom `KeyFunc` MAY override it and MUST fall
back to the IP when it returns an empty string. In the API router the
limiter runs after `middleware.RealIP`, so the key reflects the
forwarded client address.

1.6. The middleware MUST be installed server-wide before authentication
(it precedes the audit middleware in `api.Server.buildRouter`) so login
and mutation endpoints are protected from brute force and exhaustion.

### 2. Circuit Breaker

2.1. `CircuitBreaker` (`breaker.go`) MUST implement the state machine
closed → open → half-open → closed:

- **closed**: calls pass through; each error increments a consecutive
  failure counter, each success resets it. When the counter reaches
  `MaxFailures`, the breaker MUST trip to open.
- **open**: calls MUST be rejected immediately with `ErrOpen` without
  invoking the wrapped function. After `OpenDuration` elapses the next
  call MUST transition the breaker to half-open and be admitted as a
  probe.
- **half-open**: at most `HalfOpenMax` concurrent probe calls are
  admitted; the first success MUST close the breaker, any failure MUST
  re-open it for a fresh `OpenDuration`.

2.2. Defaults applied by `NewCircuitBreaker` for non-positive values MUST
be `MaxFailures = 5`, `OpenDuration = 30s`, `HalfOpenMax = 1`, and
`slog.Default()` when no logger is given. `BreakerConfig.Validate()` MUST
require a non-empty `Name` (`ErrBreakerConfig`).

2.3. Every state transition MUST be logged at Info level with `name`,
`from`, `to`, and MUST invoke the optional synchronous `OnStateChange`
hook (intended for Prometheus metrics). `IsBreakerError(err)` MUST
distinguish breaker short-circuits (`errors.Is(err, ErrOpen)`) from
errors returned by the wrapped operation.

2.4. The production server MUST construct a breaker named `"adapter"`
(`MaxFailures: 5`, `OpenDuration: 30s`, `HalfOpenMax: 1`) in
`wireSupportServices` (`cmd/server/server_init_a2a.go`) and retain it on
the server struct as `adapterBreaker`, designated to protect calls to the
Python adapter service.

### 3. Graceful Shutdown

3.1. `GracefulShutdown` (`graceful.go`) MUST orchestrate a three-step
teardown via `ShutdownAll(srv)`: (1) `srv.Shutdown(ctx)` stops accepting
new requests; (2) wait for tracked in-flight work to drain; (3) close
registered dependencies in LIFO (reverse registration) order. Steps MUST
continue after individual failures; all errors MUST be aggregated with
`errors.Join`.

3.2. The overall drain budget is `ShutdownConfig.Timeout` (default 30s,
production value 30s). Each dependency closer MUST receive its own
context capped at 5 seconds or the remaining budget, whichever is
smaller; when the deadline is already exhausted the remaining closers
MUST be skipped with an error naming each one.

3.3. Dependencies MUST register via `Register(name, Closer)` (fluent,
returns the coordinator). Anything closeable MAY implement
`Closer = interface{ Close(ctx context.Context) error }`; `CloserFunc`
adapts plain functions. Production registration order in
`cmd/server/server_start.go` MUST be: heartbeat, dispatcher, ingestor,
alert-engine, policy-engine, event-bridge, rpc-bridge, grpc-server
(conditional), patch-scheduler, metering-flush, secrets-sweeper,
retention-purger, rate-limiter, nats-client, db-pool, tracer-provider —
so the rate-limiter janitor, NATS client, DB pool, and tracer are closed
last (LIFO).

3.4. `TrackInFlight()` MUST return a completion callback that middleware
can defer to count in-flight requests; `InFlightCount()` MUST expose the
live count. The HTTP drain step MUST treat timeout as an error reporting
the number of requests still in flight.

3.5. `ShutdownAll` MUST be safe to invoke as the final step of the
server's signal-handled `Shutdown` path; in production it is the single
teardown entry point (`server_start.go` returns
`s.graceful.ShutdownAll(s.httpServer)`).

### 4. Retry Helper

4.1. `RetryConfig.Do` (`retry.go`) MUST run a `RetryableFunc` up to
`MaxAttempts` times (default 3), sleeping between attempts with
exponential backoff `BaseBackoff × 2^(attempt−1)` (default base 100ms)
capped at `MaxBackoff` (default 10s), plus symmetric multiplicative
jitter of ±`JitterFraction` (default 10%). No sleep MUST occur after the
final attempt.

4.2. Retry MUST be context-aware: cancellation before an attempt or
during a backoff sleep MUST abort immediately, returning `ctx.Err()`
joined (`errors.Join`) with the last operation error when one exists.
When `IsRetryable` is set and rejects an error, `Do` MUST stop retrying
and return that error at once.

4.3. `IsRetryableHTTP` MUST treat `*RetryableHTTPError` values with a
status ≥ 500 as retryable (4xx non-retryable), and any other error type
as retryable by default. `RetryableHTTPError` MUST carry `StatusCode`
and `Body` for predicate inspection.

### 5. Middleware Integration

5.1. The rate limiter MUST be applied at two layers in production:
outermost around the combined API+A2A handler in `buildHTTPServer`
(after the OpenTelemetry tracing wrapper, so throttled 429 responses
still get a server span), and again inside `api.Server.buildRouter` on
the API router itself.

5.2. Health-check endpoints MUST be exempt from rate limiting via
`SkipPaths` on the outer limiter and via `middleware.Heartbeat("/healthz")`
which answers `/healthz` ahead of the inner limiter in the API router.

## Known Limitations

- **The circuit breaker is constructed but never applied.** The
  `"adapter"` breaker is built in `wireSupportServices` and stored on the
  server struct, yet no code path ever calls its `Execute` — adapter
  traffic instead flows through `a2a/bridge.AdapterClient`, which ships
  its own separate, independent breaker implementation
  (`a2a/bridge/client_types.go`). The `resilience.CircuitBreaker` is
  therefore dead code in production today (tests cover it; wiring does
  not exist).
- **Requests are rate-limited twice.** `api.Server` builds its own
  limiter from `DefaultRateLimitConfig()` (no skip paths) and applies it
  in `buildRouter`, while `buildHTTPServer` wraps the same router with a
  second limiter from `wireSupportServices`. Both run at 100 req/s / 200
  burst per IP, so each request consumes a token from each limiter;
  `/readyz` and `/metrics` are skipped by the outer limiter but counted
  by the inner one.
- `GracefulShutdown.TrackInFlight` / `InFlightCount` are implemented and
  unit-tested but no middleware calls `TrackInFlight`, so the in-flight
  drain step always observes zero tracked requests (it relies entirely on
  `http.Server.Shutdown`'s own connection draining).
- `retry.go` has no production consumers; nothing outside the package
  (and its tests) calls `RetryConfig.Do` or `IsRetryableHTTP`.
- The limiter's key derivation strips the port with
  `strings.LastIndex(addr, ":")`, which mishandles bracketless IPv6
  literals (e.g. `::1` truncates to `:`).
- `breaker.allow()` admits the open→half-open probe and increments
  `halfOpenCount` under the mutex but releases it before `fn()` runs, so
  with `HalfOpenMax > 1` admission counting is racy across goroutines
  (production uses `HalfOpenMax: 1`).
