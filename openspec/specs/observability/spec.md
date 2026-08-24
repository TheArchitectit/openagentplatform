# Observability

> **Phase:** 5 (Production Readiness)
> **STATUS: COMPLETE** (telemetry, readiness, and metrics wiring closed by W8)
> **Source:** authored 2026-08-23 from code (audit `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §4)
> **App Path:** `internal/telemetry/`, `internal/monitoring/`
>
> Remediation W8 closed the three wiring gaps this spec flagged: DB tracing is
> attached at pool creation via `db.WithTracing()` when OTEL export is
> configured, `monitoring.HealthChecker` is wired into `/readyz` via
> `SetHealthChecker`, and the metrics summary writers (`RecordCounterRollup`)
> now have call sites in `metricsMiddleware` so `/api/v1/metrics/summary`
> reports live counters.

---

## Description

The observability capability gives operators three production primitives:
distributed tracing (OpenTelemetry over OTLP/gRPC), Prometheus metrics
(scraped in text exposition format), and HTTP health probes for Kubernetes
liveness/readiness. Two Go packages implement it:

- `internal/telemetry/` (6 files) — OTel `TracerProvider` initialisation,
  span helpers, chi HTTP tracing middleware, NATS trace propagation, an
  `otelpgx` database tracer, the full Prometheus instrument registry
  (namespace `oap`), snapshot roll-ups for a JSON summary endpoint, and
  build-identity metadata (`version.go`).
- `internal/monitoring/` (3 files + tests) — in-memory aggregates: a
  pluggable component `HealthChecker`, an `AlertManager` lifecycle state
  machine, and a compliance `Scorecard`.

The HTTP endpoints themselves (`/metrics`, `/healthz`, `/readyz`, `/status`,
`/version`, `/api/v1/metrics/summary`) are mounted by `internal/api` and
`cmd/server`, which consume these packages.

## User Story

**As** a platform operator running OpenAgentPlatform in Kubernetes,
**I want** standard scrape targets (Prometheus `/metrics`, OTLP spans) and
dependency-aware readiness probes, plus per-request metrics and traces,
**so that** my existing monitoring stack can detect, attribute, and page on
failures without custom instrumentation per service.

---

## Requirements

### 1. Tracer Initialisation and Lifecycle

1.1. `telemetry.InitTracer(ctx, serviceName, endpoint)` (`internal/telemetry/tracing.go`)
MUST create an `*sdktrace.TracerProvider` with an OTLP **gRPC** exporter
(`otlptracegrpc`, insecure transport) and MUST install it globally via
`otel.SetTracerProvider`.

1.2. Endpoint resolution MUST fall back from the explicit argument to the
`OTEL_EXPORTER_OTLP_ENDPOINT` environment variable. When neither is set, a
processor-less SDK provider with `AlwaysSample` MUST be installed so span
call sites never nil-check; `Shutdown` MUST remain safe to call.

1.3. The W3C `TraceContext` + `Baggage` composite propagator MUST be
installed globally even when no exporter is configured.

1.4. The batch span processor MUST use `BatchTimeout` 1s and
`MaxExportBatchSize` 512. The OTel resource MUST carry `service.name`
(semconv v1.26.0) plus process, host, and OS detectors.

1.5. `telemetry.Shutdown(ctx, tp)` MUST flush remaining spans under a 5s
timeout context and MUST be nil-receiver safe. `cmd/server/server_start.go`
registers it as the tracer-provider step of graceful shutdown.

1.6. Services opt in at boot: `cmd/server/server_init.go` calls
`InitTracer(ctx, "openagentplatform", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))`;
init failure MUST be logged as a warning and MUST NOT abort startup.

### 2. HTTP Tracing Middleware

2.1. `telemetry.HTTPMiddleware()` (`internal/telemetry/http.go`) MUST create
one server-kind span per request named `<METHOD> <chi route pattern>` and
MUST extract inbound trace context with the global propagator so the request
joins an existing trace.

2.2. Probe noise suppression: the middleware MUST skip span creation for
`/healthz`, `/health`, `/ready`, `/readyz`, `/live`, `/livez`, `/ping`, and
`/favicon.ico` (normalised with or without a leading slash).

2.3. Each span MUST record `http.request.method`, `url.path`, `url.query`,
`http.url`, `http.response.status_code` (via a `statusRecorder`
ResponseWriter wrapper), and `http.route` when a chi pattern matched. Status
codes ≥ 500 MUST set span status to Error.

2.4. An inbound `X-Request-ID` header MUST be recorded as a span attribute
and stored in the request context, retrievable via
`telemetry.RequestIDFromContext`.

2.5. The API server MUST wrap its top-level handler with this middleware
(`withTracing` in `cmd/server/a2a_routes.go`), covering `/a2a/*` and all
API routes.

### 3. NATS Trace Propagation

3.1. `internal/events/nats.go` MUST start a producer span on `Publish`
(`messaging.operation=publish`, body-size attribute) and MUST inject the
trace context into NATS message headers via the global propagator.

3.2. `Subscribe`/`SubscribeQueue` MUST wrap handlers to extract that context
and start a consumer span (`nats.subscribe <subject>`,
`messaging.system=nats`), then rewrite the `traceparent` header (W3C
`00-<trace>-<span>-<flags>` format) so downstream handlers continue the
trace.

### 4. Span Helper API

4.1. `internal/telemetry/spans.go` MUST provide `StartSpan` (tracer name
`openagentplatform`), `StringAttr`/`IntAttr`/`BoolAttr` constructors,
`AddSpanEvents`, `RecordError` (records the error event and sets Error
status), and `SetSpanStatus`. All helpers MUST be nil-span safe.

### 5. PostgreSQL Tracing (otelpgx)

5.1. The otelpgx tracer MUST be attached to `cfg.ConnConfig.Tracer`
(`otelpgx.NewTracer(otelpgx.WithIncludeQueryParameters())`) **before** pool
creation. Production wiring (W8) does this in `cmd/server/main.go` via
`db.WithTracing()` — an `Option` passed to `db.NewPool` — only when
`OTEL_EXPORTER_OTLP_ENDPOINT` is set. `pgxpool.Config` is immutable
post-creation, which is why attaching the tracer later can never work.

5.2. `telemetry.TraceDBFromDSN` and `telemetry.TraceDB`
(`internal/telemetry/db.go`) remain defined — `TraceDB` a documented
deprecated no-op — but both have zero callers in production; the
`db.WithTracing()` option path is the sole wiring mechanism.

### 6. Prometheus Registry and Instruments

6.1. `telemetry.InitMeter(ctx, serviceName)` (`internal/telemetry/metrics.go`)
MUST register all instruments on a **private** `prometheus.Registry`
(isolated from `prometheus.DefaultRegisterer`), MUST be idempotent
(subsequent calls return a handler over the same registry), and MUST return
a `promhttp` handler for the text exposition format.

6.2. All metrics MUST use namespace `oap` and Prometheus naming conventions
(counters `_total`, histograms `_seconds`). The instrument set MUST be:

| Kind | Metric | Labels |
|------|--------|--------|
| Counter | `oap_api_requests_total` | method, path, status |
| Counter | `oap_nats_messages_total` | subject, direction |
| Counter | `oap_agent_heartbeats_total` | agent_id, status |
| Counter | `oap_check_results_total` | check_type, status |
| Counter | `oap_alert_transitions_total` | from_state, to_state |
| Counter | `oap_a2a_tasks_total` | adapter, status |
| Counter | `oap_db_queries_total` | operation |
| Counter | `oap_bytes_by_adapter_total` | adapter, direction |
| Histogram | `oap_http_request_duration_seconds` | method, path |
| Histogram | `oap_db_query_duration_seconds` | operation |
| Histogram | `oap_check_execution_duration_seconds` | check_type |
| Histogram | `oap_adapter_invoke_duration_seconds` | adapter |
| Gauge | `oap_agents_online` | agent_id |
| Gauge | `oap_active_alerts` | severity |
| Gauge | `oap_active_shell_sessions` | — |
| Gauge | `oap_adapter_pool_processes` | state |
| Gauge | `oap_cost_total_by_adapter` | adapter |

All histograms MUST use `prometheus.DefBuckets`.

6.3. Every instrument MUST have a nil-guarded convenience recorder
(`RecordAPIRequest`, `ObserveHTTPRequestDuration`, `SetAgentsOnline`, etc.)
so call sites made before `InitMeter` are safe no-ops.

6.4. Per-request recording MUST be applied server-wide by `metricsMiddleware`
(`internal/api/routes_helpers_extra.go`), which increments
`oap_api_requests_total` and observes
`oap_http_request_duration_seconds` labelled with the chi route pattern and
numeric status, and MUST exclude `/metrics` and `/api/v1/metrics*` so
scrapes do not pollute request rates.

### 7. Metrics Endpoints

7.1. `GET /metrics` MUST serve the Prometheus text exposition format and MUST
be mounted **before authentication** (`metricsRouter`,
`internal/api/metrics.go`, mounted in `registerRoutes`). Before the handler
is installed via `api.SetPrometheusHandler`, the endpoint MUST respond `503`
with a plain-text "not initialised" body.

7.2. `GET /api/v1/metrics/summary` MUST return JSON
(`MetricsResponse`: `generated_at`, `uptime_seconds`, `counters`, `gauges`)
fed by `telemetry.SnapshotCounters()` / `telemetry.SnapshotGauges()`.

7.3. Probe and scrape paths (`/healthz`, `/readyz`, `/metrics`) MUST be
excluded from the platform rate limiter (`resilience.RateLimitConfig.SkipPaths`
in `cmd/server/server_init_a2a.go`).

### 8. Health, Readiness, and Status Endpoints

8.1. `GET /healthz` MUST be a dependency-free liveness probe returning
`200 {"status":"ok"}`; chi `middleware.Heartbeat("/healthz")` MUST short-
circuit LB probes before the middleware stack runs.

8.2. `GET /readyz` (`internal/api/health.go`) MUST be a readiness probe that
does real work under a 3s timeout: `db.Ping` for Postgres and
`Conn().IsConnected()` for NATS. It MUST respond `503` with
`{"status":"degraded"}` when any configured dependency fails, and MUST report
`not_configured` (not failure) for absent dependencies. NATS readiness MUST
check connection state only — no publish round-trip — to avoid flapping
during reconnects.

8.3. `GET /status` MUST return build info (`telemetry.GetBuildInfo()`),
uptime, goroutine count, memory stats, GC count, environment, and component
states. `GET /version` MUST serve `BuildInfo` alone.

8.4. All four endpoints MUST be mounted before authentication so Kubernetes
and CI smoke tests need no credentials. `/debug/config` and `/debug/pprof/*`
MUST be mounted only when `cfg.DebugMode` is set.

### 9. Component Health Aggregation (`internal/monitoring/health.go`)

9.1. `HealthChecker` MUST accept named `HealthCheck` implementations
(`Register(name, check)`; `HealthCheckFunc` adapts bare functions) and MUST
reject empty names and nil checks.

9.2. `Check(ctx)` MUST evaluate checks in sorted-name order, normalise any
unrecognised status to `unknown` (vocabulary: `healthy`, `degraded`,
`unhealthy`, `unknown`), and set report status to the **worst** component by
rank `unhealthy(3) > degraded(2) > unknown(1) > healthy(0)`. A checker with
no registered checks MUST report `unknown`. The report MUST include per-
status `Counts` and backfill missing `Name`/`CheckedAt` on component results.

### 10. Alert Lifecycle (`internal/monitoring/alerts.go`)

10.1. `AlertManager` MUST manage states `open` → `acknowledged`, `snoozed`,
`resolved` with these transition rules: `Acknowledge` only from `open` or
`snoozed` (records `AcknowledgedBy`, clears snooze); `Snooze` from any state
except `resolved` with a strictly positive duration; `Resolve` from any state
except `resolved` (stamps `ResolvedAt`). Violations MUST return
`ErrInvalidAlertTransition`; unknown IDs MUST return `ErrAlertNotFound`.

10.2. `Add` MUST require an ID, reject duplicates, default state to `open`,
and backfill zero timestamps. `List(AlertFilter)` MUST filter by
state/severity/source and sort newest-first (`CreatedAt` desc, ID asc on
ties). All returned alerts MUST be value-cloned (pointer time fields copied)
so callers cannot mutate manager state.

### 11. Compliance Scorecard (`internal/monitoring/scorecard.go`)

11.1. `Scorecard.Compute([]ComplianceResult)` MUST return per-agent
pass-percentages and an overall percentage (`passed × 100 / total`, 0 when
empty), with agents sorted by `AgentID` and an always-non-nil `Agents` slice.

### 12. Build Identity (`internal/telemetry/version.go`)

12.1. `Version`, `CommitSHA`, and `BuildDate` MUST be overridable at link
time via `-ldflags -X`, with dev defaults (`dev`/`unknown`/`unknown`) so
flag-less builds run. `GetBuildInfo()` MUST add `runtime.Version()`, cache
via `sync.Once`, and be JSON-marshalable for `/version`, `/status`, and
diagnostics responses.

---

## Known Limitations

- **`AlertManager` and `Scorecard` still have no production consumers.** W8
  wired only `monitoring.HealthChecker` into `/readyz` (`SetHealthChecker` +
  a `database` component check; unhealthy checks degrade readiness). The
  in-memory `AlertManager` and compliance `Scorecard` remain unit-tested but
  unwired, and alert state is in-memory only, lost on restart.
- **Metrics summary roll-ups cover only request counters.** W8 gave the
  summary writers a call site: `metricsMiddleware` feeds
  `RecordCounterRollup("api_requests_total", 1)` on every request, so
  `GET /api/v1/metrics/summary` now reports live request counts. Gauges and
  other counters still have no writers.
- **Service name is not a metric label.** `InitMeter` stores `serviceName`
  (and its comment claims a constant label), but no collector carries it.
- `/metrics`, `/healthz`, `/readyz`, `/status`, `/version` are
  unauthenticated by design; access control is delegated to the network
  layer.
- Metrics use `prometheus/client_golang` directly; there is no OTel Metrics
  SDK or OTLP metric exporter — traces go to OTLP, metrics are scrape-only.
- `internal/telemetry/` has no test files; only `internal/monitoring/`
  (`health_test.go`) is tested.
