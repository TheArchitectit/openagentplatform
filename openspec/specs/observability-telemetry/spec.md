# Observability — Telemetry

> **Phase:** 0 (Foundation) — cross-cutting, used by every subsystem
> **STATUS: COMPLETE** — OTel tracing, Prometheus metrics, build-info, HTTP/NATS
> instrumentation all implemented and wired into the server binary
> **Source:** authored 2026-08-25 from code (`internal/telemetry/`)
> **App Path:** `internal/telemetry/`
> **Source files:** `internal/telemetry/tracing.go`, `internal/telemetry/metrics.go`,
> `internal/telemetry/metrics_record.go`, `internal/telemetry/metrics_snapshot.go`,
> `internal/telemetry/http.go`, `internal/telemetry/db.go`, `internal/telemetry/version.go`

---

## Description

`internal/telemetry/` is the platform's observability substrate. It owns three
independent concerns that share a package for convenience: **OpenTelemetry
tracing** (`tracing.go`), **Prometheus metrics** (`metrics.go`), and **build
identity** (`version.go`). It also provides thin HTTP middleware (`http.go`)
and a `pgxpool`-aware DB instrumentation hook (`db.go`).

The package is intentionally **fail-open**: when no OTLP endpoint is configured,
`InitTracer` installs a no-op provider so callers can record spans without
checking for nil. When no metrics registry is configured, the Prometheus handler
is still mountable. This is a deliberate design choice — observability must not
become a startup failure.

## User Story

**As** an operator running OpenAgentPlatform,
**I want** every request, NATS message, check result, and DB query to be
observable via traces and metrics,
**so that** I can debug a slow fleet check or a dropped alert without adding
instrumentation to each subsystem.

---

## Requirements

### 1. Tracing

1.1. `InitTracer(ctx, serviceName, endpoint)` configures a `sdktrace.TracerProvider`
with an OTLP-gRPC exporter. When `endpoint` is empty it falls back to the
`OTEL_EXPORTER_OTLP_ENDPOINT` env var; when both are unset it installs a minimal
provider with no processors (spans are dropped silently).


1.2. The provider is configured with `BatchTimeout: 1s` and
`MaxExportBatchSize: 512`. `Shutdown(ctx)` flushes remaining spans with a 5s
grace period and is safe to call with a nil receiver.

1.3. W3C `TraceContext` + `Baggage` propagation is always installed via
`otel.SetTextMapPropagator`, so downstream code gets a working propagator even
when no exporter is configured.

1.4. `Tracer(name)` returns the named tracer from the global provider.

1.5. **Known limitation:** `Publish` in `internal/events` injects trace context
into NATS message headers; `Subscribe`/`SubscribeQueue` wrap handlers so each
delivered message becomes a consumer span linked to the producer span. This
NATS propagation path is implemented in `internal/events`, not here.

### 2. Metrics

2.1. All instruments are registered under the `oap` Prometheus namespace. Naming
follows Prometheus convention: counters end in `_total`, histograms in `_seconds`,
gauges are domain nouns.

2.2. **Counters:** `oap_api_requests_total`, `oap_nats_messages_total`,
`oap_agent_heartbeats_total`, `oap_check_results_total`,
`oap_alert_transitions_total`, `oap_a2a_tasks_total`, `oap_db_queries_total`,
`oap_bytes_by_adapter_total`.

2.3. **Histograms:** `oap_http_request_duration_seconds`, `oap_nats_publish_duration_seconds`,
`oap_db_query_duration_seconds`.

2.4. **Gauges:** `oap_active_agents`, `oap_open_alerts`, `oap_check_results_pending`.

2.5. `MetricsHandler()` returns a `http.Handler` serving the text exposition
format at `/metrics`.

2.6. **Known limitation:** instrument registration is global and package-level
(`var APIRequestsTotal *prometheus.CounterVec`). Adding a new metric requires
editing `metrics.go`; there is no registry pattern.

### 3. Build Identity

3.1. `BuildInfo` carries `Version`, `CommitSHA`, `BuildDate`, and `GoVersion`.
`Version`/`CommitSHA`/`BuildDate` are overridden at link time via `-ldflags`
(`-X .../internal/telemetry.Version=<semver>`); defaults are `"dev"`,
`"unknown"`, `"unknown"` so the binary builds without flags.

3.2. `GetBuildInfo()` computes the value lazily on first call (`sync.Once`) and
caches it for the process lifetime. `GoVersion` is taken from `runtime.Version()`
so it always reflects the actual toolchain.

3.3. `BuildInfo.MarshalJSON` serializes as a flat JSON object with snake_case
fields, so it can be embedded in log fields, health responses, and audit events
without allocation.

### 4. HTTP Middleware

4.1. `http.go` provides middleware that records `oap_api_requests_total` (by
method + route + status) and `oap_http_request_duration_seconds`. It is nil-safe:
a nil logger or nil handler is tolerated.

### 5. DB Instrumentation

5.1. `db.go` wraps `pgxpool` connection creation so that `oap_db_queries_total`
and `oap_db_query_duration_seconds` are recorded for every query. It is wired
into the server bootstrap in `internal/db/`.

---

## Known Limitations

- **No metric registry pattern.** Instruments are package-level vars; adding one
  requires an edit to `metrics.go` rather than registration at construction time.
- **NATS propagation lives in `internal/events`.** The trace-context injection for
  NATS messages is not in `internal/telemetry`; the two packages are coupled
  through the global `otel` propagator.
- **No sampling configuration.** `InitTracer` always uses `AlwaysSample()`.
  Production deployments that want tail-based sampling must supply their own
  `TracerProvider`.

---

## Cross-References

- `internal/events/nats.go` — trace-context injection into NATS messages
- `internal/db/` — pgxpool bootstrap that consumes `db.go`
- `internal/schema/openapi.go` — `/docs/openapi.json` surfaces `BuildInfo`
- `data-model` spec — `Alert`/`CheckResult` payloads flow through these instruments