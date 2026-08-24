# Platform Foundation

> **Phase:** 0 (Foundation)
> **STATUS: COMPLETE**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/config/`, `internal/db/`, `internal/schema/`

---

## Description

Platform Foundation is the process-startup substrate of the OpenAgentPlatform
monorepo server (`cmd/server`). It is three small glue packages that every
other capability depends on but that contain no business logic of their own:

- **`internal/config`** (config.go, 133 lines) — environment-variable-only
  configuration loading into a single `Config` struct, with defaults,
  required-var enforcement, and environment-conditional production hardening.
- **`internal/db`** (postgres.go, 33 lines) — Postgres connection-pool
  bootstrap over `pgx/v5/pgxpool` with fixed pool sizing and a startup
  connectivity probe.
- **`internal/schema`** (openapi.go, 73 lines + embedded `openapi.yaml`) —
  compile-time embedding of the OpenAPI 3.1 document and its export as
  Swagger UI, raw YAML, and JSON.

These packages were delivered as Phase 0 Sprint 0.1 infrastructure
(docs/architecture/ROADMAP_AND_SPRINTS.md): Story 0.1.1 (monorepo scaffold,
of which the Go server config/db substrate is part), Story 0.1.3 (PostgreSQL
schema), and Story 0.1.6 (OpenAPI 3.1 spec generation).

## User Story

**As** a platform operator booting the server for the first time,
**I want** the process to configure itself entirely from environment
variables, fail fast and loudly when a required variable is missing or an
insecure combination is attempted outside development, verify database
connectivity before serving traffic, and publish interactive API
documentation without any extra tooling,
**so that** a misconfigured deployment never starts silently and a
`docker compose up` is sufficient to get a working, self-documenting server.

---

## Requirements

### 1. Environment-Variable Configuration Loading

1.1. `config.Load()` (`internal/config/config.go`) MUST be the single entry
point for server configuration and MUST read exclusively from process
environment variables. There is no config file, no `.env` parsing, and no
viper/godotenv dependency anywhere in the package.

1.2. The `Config` struct MUST carry, at minimum: HTTP/gRPC ports (`HTTPPort`,
`GRPCPort`), environment name (`Env`), `LogLevel`, `PostgresDSN`, NATS
connection (`NATSURL`, `NATSCertFile`, `NATSKeyFile`, `NATSCAFile`), OIDC
client settings (`OIDCIssuerURL`, `OIDCClientID`, `OIDCClientSecret`,
`OIDCRedirectURL`), session minting (`SessionIssuer`, `SessionAudience`,
`SessionKeyPath`), cookie policy (`CookieDomain`, `CookieSecure`),
`SentryDSN`, `DebugMode`, `PolicyEvalInterval`, and the hosted-LLM provider
settings (`OzoreModel`, `OzoreBaseURL`).

1.3. Defaults MUST be applied via `getEnv(key, fallback)` with these values:
`HTTP_PORT`=8080, `GRPC_PORT`=9090, `APP_ENV`=development, `LOG_LEVEL`=info,
`NATS_URL`=nats://localhost:4222,
`OIDC_REDIRECT_URL`=http://localhost:8080/auth/callback,
`SESSION_ISSUER`=openagentplatform, `SESSION_AUDIENCE`=oap-web,
`COOKIE_DOMAIN`=localhost, `COOKIE_SECURE`=false, `DEBUG_MODE`=false,
`OZORE_MODEL`=ozore/custom, `OZORE_BASE_URL`=https://ozore.com/v1.
`POSTGRES_DSN`, the NATS TLS file paths, all OIDC settings,
`SESSION_KEY_PATH`, and `SENTRY_DSN` have no default and are empty when unset.

1.4. `POLICY_EVAL_INTERVAL` MUST be parsed by `getDurationEnv` accepting first
a `time.ParseDuration` string, then a bare integer interpreted as seconds, and
falling back to 5 minutes when missing or unparseable. The value is consumed
as the policy engine sweep interval in `NewServer`
(`cmd/server/server_init.go`, `policy.Config.Interval`).

1.5. The LLM provider API key (`OZORE_API_KEY`) MUST NOT be stored in
`Config`; the Python adapter reads it directly from the environment
(`py/oap/adapters/ozore_adapter.py`), and `config.go` documents this
separation explicitly.

### 2. Configuration Validation and Production Hardening

2.1. `Config.validate()` MUST reject a missing `POSTGRES_DSN` with an error
naming the absent variable ("missing required env vars: POSTGRES_DSN").
`POSTGRES_DSN` is the only required variable.

2.2. `APP_ENV` MUST be restricted to exactly `development`, `staging`, or
`production`; any other value MUST fail loading.

2.3. Outside `development`, loading MUST fail when `DEBUG_MODE` is true
(rationale: it mounts unauthenticated pprof and a config-dump endpoint) and
when `COOKIE_SECURE` is false (rationale: the session cookie carries the
auth JWT). Both errors are explicit and descriptive.

2.4. A failed `Load()` MUST prevent process start: `cmd/server/main.go`
logs the error and calls `os.Exit(1)` before any listener is opened.

2.5. The `DebugMode` flag MUST gate the diagnostic surface at route-build
time: `internal/api/routes_routes.go` mounts `GET /debug/config` (a redacted
config dump via `handleDebugConfig`) and the `/debug/pprof/*` routes only
when `cfg.DebugMode` is true.

### 3. Postgres Pool Bootstrap

3.1. `db.NewPool(ctx, dsn)` (`internal/db/postgres.go`) MUST build the pool
from `pgxpool.ParseConfig(dsn)` and MUST fix the pool sizing parameters:
`MaxConns`=25, `MinConns`=2, `MaxConnLifetime`=1 hour,
`MaxConnIdleTime`=30 minutes.

3.2. After pool construction, `NewPool` MUST probe connectivity with
`pool.Ping` under a 5-second context timeout. On ping failure it MUST close
the pool and return the error wrapped as `db: ping: ...`; parse and
construction failures are wrapped as `db: parse dsn:` / `db: create pool:`.

3.3. `cmd/server/main.go` MUST create the pool with a 10-second timeout
context before any other subsystem (check-library seeding, NATS, HTTP) is
initialized, and MUST exit with code 1 if bootstrap fails. The pool is closed
via `defer pool.Close()` at shutdown.

3.4. `internal/db` MUST remain migration-free: it performs no DDL. Table
creation is the responsibility of individual stores (see §6).

### 4. OpenAPI Specification and Docs Export

4.1. The OpenAPI document MUST live at `internal/schema/openapi.yaml`
(OpenAPI 3.1.0, `info.version` 1.1.0, ~2300 lines) and MUST be compiled into
the binary via `//go:embed openapi.yaml`. There is no build tag and no
runtime generation step — export is a startup-time mount of an embedded
artifact.

4.2. `schema.MountSwagger(r chi.Router)` MUST register exactly three routes:

| Route | Content-Type | Source |
|-------|--------------|--------|
| `GET /docs` | `text/html; charset=utf-8` | Static Swagger UI HTML page (`swaggerHTML` const) |
| `GET /docs/swagger` | `application/yaml; charset=utf-8` | Raw embedded `openapi.yaml` bytes |
| `GET /docs/openapi.json` | `application/json; charset=utf-8` | YAML converted to JSON |

4.3. The YAML→JSON conversion MUST be performed once process-wide via
`sync.Once` using `sigs.k8s.io/yaml.YAMLToJSON` and cached (`toJSON()`).
If conversion fails, the endpoint MUST return HTTP 500 with body
`{"error":"spec_conversion_failed"}`.

4.4. The Swagger UI page MUST load its assets from the unpkg CDN
(`swagger-ui-dist@5`) and MUST point SwaggerUIBundle at the local
`/docs/openapi.json` endpoint with deep linking enabled.

4.5. `MountSwagger` MUST be mounted unconditionally (no environment gate) in
`internal/api/handler.go` `buildRouter()`, after `registerRoutes`, so the
docs are served in every environment. The docs paths are excluded from the
audit middleware (`/docs` filtered) and are reachable without authentication.

### 5. Server Startup Wiring Order

5.1. `cmd/server/main.go` MUST initialize subsystems in this order:
(1) `config.Load()`, (2) `logger.New(cfg.LogLevel)` installed as the
`slog` default, (3) `signal.NotifyContext` for SIGINT/SIGTERM,
(4) `db.NewPool`, (5) `checklib.Seed`, (6) NATS `events.NewClient`
(consuming `NATSURL` + the three NATS TLS file fields), (7) `NewServer`,
(8) policy default seeding, (9) `srv.Start`.

5.2. Session minting MUST consume `SessionIssuer`, `SessionAudience`, and
`SessionKeyPath` via `auth.NewSessionMinterFromFile`
(`internal/api/handler.go`); when key loading fails, the server MUST log the
error and fall back to an ephemeral in-memory key so startup still succeeds
(sessions then do not survive restarts).

### 6. Database Schema Location

6.1. The only shipped SQL is `deploy/postgres/init.sql` (6 lines), which
creates extensions only — `uuid-ossp`, `pgcrypto`, `timescaledb` — and is
mounted read-only into the Postgres container's
`docker-entrypoint-initdb.d` by `deploy/docker-compose.yml` (image
`timescale/timescaledb:2.17.2-pg16`).

6.2. Application tables MUST NOT be expected from `init.sql`. Table DDL is
bootstrapped in-code by store constructors using idempotent
`CREATE TABLE IF NOT EXISTS` (e.g. `internal/tenancy/rls_migration.go`,
`internal/reports/store.go`, `internal/remote/recording_store.go`), which
run against the pool established in §3.

6.3. A legacy data migration script exists at
`scripts/migrations/v1_to_v2.py`; it is not executed by the server process.

---

## Known Limitations

- **No unit tests.** None of the three packages contains a `_test.go` file;
  behavior is exercised only indirectly by the running server.
- **No migration tool.** The roadmap (Story 0.1.3) records "PostgreSQL
  schema with migrations 01-09" as complete, but no numbered migration files
  exist in the repo for the platform server (the only migration directories
  belong to the separate `mcp-server` module). Schema evolution relies on
  per-store `CREATE TABLE IF NOT EXISTS` bootstraps, which cannot express
  column changes or downgrades.
- **Hand-maintained OpenAPI spec.** `openapi.yaml` is authored by hand and
  embedded; there is no generation or validation step that reconciles it
  against the chi routes, so it can drift from actual endpoints.
- **CDN dependency for docs UI.** `GET /docs` renders only when the unpkg
  CDN is reachable; the YAML/JSON spec endpoints are self-contained.
- **No secret redaction policy in `Config` itself.** Redaction of the debug
  config dump is implemented in `internal/api/health.go`, not in the config
  package; a new sensitive field added to `Config` must be handled there
  separately.
- **Fixed pool sizing.** `MaxConns`/`MinConns` and lifetime settings are
  hard-coded in `db.NewPool`; there is no environment override.
