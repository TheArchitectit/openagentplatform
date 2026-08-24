# Adapter Service

> **Phase:** 2 (A2A + Agents)
> **STATUS: PARTIAL** (implemented as a standalone service but not integrated with gateway/API routing)
> **Source:** authored 2026-08-23 from code (audit `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §4 #9)
> **App Path:** `py/oap/`

---

## Description

The Adapter Service is the standalone Python/FastAPI deployable unit that hosts
the OAP framework-adapter subsystem. It owns three things an operator cares about
as a single process: a FastAPI application assembled by `oap/app.py`, a lifespan
that boots an `OrchestrationService` plus a `CostManager`, and a REST surface that
exposes adapter discovery, invocation, streaming, cancellation, and cost
reporting over HTTP on port 8001.

It is *not* the gateway. It speaks no JSON-RPC and no gRPC, it carries no
authentication, and it persists nothing to Postgres at request time. It is the
Python side of the bridge the Go gateway calls into: every A2A task that resolves
to a framework agent ultimately lands here as an `InvokeRequest`, runs inside a
pooled subprocess worker, and returns as an `InvokeResponse` or an SSE
`StreamEvent` stream. The adapter protocol and the process pool are specified in
their own capabilities (`a2a-framework-adapters`, `process-pool`); this spec
covers the service that wraps them — app assembly, the route table, settings,
the dormant DB layer, and the deployment posture.

## User Story

**As** a platform operator bringing up the agent layer,
**I want** a single Python process I can `uvicorn`-launch that registers every
installed framework adapter, warms a pool of subprocess workers, and answers
adapter discovery and invocation over a documented REST surface,
**so that** the Go gateway (or any authenticated client of the platform) has one
HTTP address for agent work, without me wiring each framework into the Go binary.

---

## Requirements

### 1. Application Assembly

1.1. The service MUST be assembled by `create_app()` in `py/oap/app.py`, which
constructs `FastAPI(title="OAP Adapter Service", version="0.1.0", lifespan=...)`
and returns it. A module-level `app = create_app()` is also exported so the
service runs as `uvicorn oap.app:app --port 8001` (the default documented in
`app.py`).

1.2. Application state MUST be owned by the lifespan context, not module globals:
`app.state.orchestrator` is set to a fresh `OrchestrationService`, and
`app.state.cost_manager` to a fresh `CostManager`, before the app starts handling
requests. The lifespan MUST `await orchestrator.start()` before `yield` and
`await orchestrator.stop()` on shutdown.

1.3. The app MUST register the adapter REST router — `oap/adapters/api.py`'s
`router` (prefix `/api/v1`, tag `adapters`) — via `app.include_router`, plus three
non-versioned legacy aliases (`GET /adapters`, `GET /adapters/{name}/health`,
`GET /health`) defined directly on the app for backwards compatibility.

1.4. CORS MUST be restricted to the Vite dev origin
(`allow_origins=["http://localhost:5173"]`) with credentials, all methods, and
all headers. No auth middleware is applied inside the service; authentication is
the caller's responsibility (see Known Limitations).

### 2. REST Route Table

2.1. The service MUST expose the following versioned routes from
`py/oap/adapters/api.py` (all under the `/api/v1` prefix):

| Method | Path | Response model | Source |
|--------|------|----------------|--------|
| POST | `/api/v1/adapters/invoke` | `InvokeResponse` | `invoke_adapter` |
| POST | `/api/v1/adapters/stream` | `text/event-stream` | `stream_adapter` |
| POST | `/api/v1/adapters/{task_id}/cancel` | `CancelResponse` | `cancel_task` |
| GET | `/api/v1/adapters` | `AdapterListResponse` | `list_adapters` |
| GET | `/api/v1/adapters/{name}/card` | `AdapterCardResponse` | `get_adapter_card` |
| GET | `/api/v1/adapters/{name}/health` | `AdapterHealthResponse` | `get_adapter_health` |
| GET | `/api/v1/adapters/{name}/models` | `ModelsResponse` | `get_adapter_models` |
| GET | `/api/v1/cost/usage` | `CostUsageResponse` | `get_cost_usage` |
| GET | `/api/v1/cost/budgets` | `BudgetResponse` | `get_cost_budgets` |

2.2. The app MUST additionally serve three non-versioned routes from `app.py`:
`GET /adapters` (returns `list[AdapterInfo]`), `GET /adapters/{name}/health`
(returns `HealthStatus`), and `GET /health` (returns a pool + adapter summary
with a `status` of `"ok"` when every adapter is healthy and `"degraded"`
otherwise). The summary echoes `pool.total_processes`, `pool.max_processes`,
`pool.warm_adapter_count`, and the per-adapter `{name, healthy}` list.

2.3. `GET /api/v1/adapters/{name}/card` MUST return `404` with
`Adapter '{name}' not found` when the name is not in the registry, and
`GET /api/v1/adapters/{name}/health` MUST return `404` under the same condition
(detected by the literal substring `"not found"` in `HealthStatus.last_error`).

### 3. Invocation and Streaming Semantics

3.1. `POST /api/v1/adapters/invoke` MUST accept an `InvokeRequestModel`
(`adapter_name`, `task_id`, `messages`, `metadata`, `timeout`). When
`adapter_name` is non-empty it MUST be injected as `metadata["preferred_agent"]`
before the request is handed to `OrchestrationService.handle_task`. A missing
`task_id` MUST be filled with `str(uuid.uuid4())`.

3.2. `POST /api/v1/adapters/stream` MUST return a `StreamingResponse` with
`media_type="text/event-stream"` and headers `Cache-Control: no-cache` and
`X-Accel-Buffering: no`. Each `StreamEvent` MUST be emitted as
`data: {json}\n\n`, the stream MUST terminate with `data: [DONE]\n\n`, and an
exception during streaming MUST be converted into a final `error` `StreamEvent`
before the `[DONE]` line.

3.3. `POST /api/v1/adapters/{task_id}/cancel` MUST delegate to
`OrchestrationService.cancel_task` and return `CancelResponse(task_id, cancelled)`
where `cancelled` is `True` only when an in-flight task was found and signalled.

3.4. The two dependency providers in `api.py` MUST enforce service readiness:
`get_orchestrator` raises `HTTPException(503, "Orchestrator not initialised")`
when `app.state.orchestrator` is absent, and `get_cost_manager` falls back to a
new `CostManager()` (with `DEFAULT_COST_MODELS`) when `app.state.cost_manager`
is absent, so the cost routes stay functional even if cost wiring is skipped.

### 4. Cost Reporting

4.1. `GET /api/v1/cost/usage` MUST accept `org_id` (str, default `""`), `from`
(alias for `from_ts`, float, default `0.0`), and `to` (alias for `to_ts`,
optional float, defaulting to `time.time()` at request time) and return a
`CostUsageResponse` wrapping the `CostManager.get_usage` report. **Note:** the
parameters are Unix epoch floats, not RFC 3339 strings — a contract mismatch
with the Go callers documented in Known Limitations.

4.2. `GET /api/v1/cost/budgets` MUST return `BudgetResponse` listing every org
configured in `BudgetTracker._limits` with `org_id`, `monthly_limit`,
`current_spend` (from `BudgetTracker.get_spend`), and `currency`.

4.3. `GET /api/v1/adapters/{name}/models` MUST return `ModelsResponse` echoing
the path `{name}` and listing every entry in `CostManager._models` as a
`ModelEntry`. Cost models are global, not per-adapter; the `{name}` parameter is
accepted for API consistency only.

4.4. `ModelEntry` (`api_models.py`) MUST emit both the backend field names
(`input_per_1k`, `output_per_1k`) and the frontend-compatible aliases
(`input_cost_per_1k`, `output_cost_per_1k`) via a `model_validator(mode="after")`
so either contract reads the pricing correctly (P2-5 remediation in commit
`6c473cb`).

### 5. Settings and Environment

5.1. Configuration MUST be loaded by `oap/settings.py`'s `Settings` class, a
`pydantic_settings.BaseSettings` subclass with `env_file=".env"`,
`env_file_encoding="utf-8"`, `case_sensitive=False`, and `extra="ignore"`.

5.2. The service MUST read these settings (defaults shown):

| Field | Default | Consumed by |
|-------|---------|-------------|
| `app_env` | `"development"` | — |
| `log_level` | `"info"` | — |
| `postgres_dsn` | `postgresql+asyncpg://oap:oap@localhost:5432/oap` | `oap/db.py` |
| `nats_url` | `nats://localhost:4222` | — |
| `nats_cert_file` / `nats_key_file` / `nats_ca_file` | `None` | — |
| `oidc_issuer_url` | `http://localhost:5556/dex` | — |
| `oidc_client_id` / `oidc_client_secret` | `oap-web` / `oap-web-secret` | — |
| `jwt_secret` | `dev-secret-change-me` | — |
| `jwt_audience` / `jwt_algorithm` | `oap` / `HS256` | — |
| `sentry_dsn` | `None` | — |

5.3. `get_settings()` MUST return a new `Settings()` instance on each call (no
LRU cache). As of this spec, only `postgres_dsn` is actually consumed at import
time (by `oap/db.py`); the NATS, OIDC, JWT, and Sentry fields are declared but
not read by the service.

### 6. Database Layer

6.1. `oap/db.py` MUST create a module-level async SQLAlchemy engine from
`get_settings().postgres_dsn` with `pool_size=10`, `max_overflow=20`,
`pool_pre_ping=True`, `echo=False`, and an `async_sessionmaker` with
`expire_on_commit=False` and `autoflush=False`.

6.2. `oap/db.py` MUST expose `Base` (a `DeclarativeBase`), a `session_scope()`
async context manager that commits on success and rolls back on exception, and a
`get_session()` async generator suitable as a FastAPI dependency.

6.3. As of this spec, no ORM model in the codebase subclasses `Base`; the engine
is created at import time but no request path opens a session. The DB layer is
wired for Alembic migrations (`py/alembic/env.py` imports `oap.db.Base`) but is
otherwise dormant in the running adapter service. Cost and budget state lives
in memory on the `CostManager` instance and is lost on restart.

### 7. Packaging and Deployment Posture

7.1. The service MUST be packaged as the `oap` Python project
(`py/pyproject.toml`): name `oap`, version `0.1.0`, `requires-python = ">=3.12"`,
hatchling build backend, wheel target `packages=["oap"]`. Runtime dependencies
are `fastapi`, `uvicorn[standard]`, `sqlalchemy[asyncio]`, `alembic`, `asyncpg`,
`pydantic-settings`, `pyjwt[crypto]`, and `httpx`; dev extras are `pytest`,
`pytest-asyncio`, `ruff`, and `mypy`.

7.2. The service MUST be launchable as `uvicorn oap.app:app --port 8001` from the
`py/` directory (the module-level `app` in `app.py`). The Go side expects this
exact port: `cmd/server/server_init_a2a.go` hard-codes the bridge adapter client
to `BaseURL: "http://localhost:8001"`, and `internal/api/a2a_proxy.go` hard-codes
`adapterBaseURL = "http://localhost:8001"` (a `var` so tests can override).

7.3. The service MUST NOT be started by the platform's deployment artifacts as of
this spec: there is no `adapter` service in `deploy/docker-compose.yml` (only
`postgres`, `nats`, `dex`, `server`, `web`), no `Dockerfile` for the Python
service (`deploy/` ships only `Dockerfile.server` and `Dockerfile.web`), no
`make` target that starts it, and `deploy.sh` does not launch it (its Python gate
is `ruff check` plus `scripts/regression_check.py`). Local bring-up is manual.

7.4. The service MUST be exercised in CI by `.github/workflows/python.yml`
(`uv sync --all-extras`, `ruff check`, `ruff format --check`, `mypy oap`,
`uv run pytest`) on an `ubuntu-latest`/`macos-latest` × Python `3.11`/`3.12`
matrix — note that the matrix includes 3.11 while `pyproject.toml` requires
`>=3.12`, a minor inconsistency. The Makefile `test` target also runs
`cd py && uv run pytest`, and `migrate` runs `cd py && uv run alembic upgrade head`.

### 8. Health Endpoints

8.1. `GET /health` (defined in `app.py`) MUST return a JSON object with
`status` (`"ok"` or `"degraded"`), a `pool` object (`total_processes`,
`max_processes`, `warm_adapter_count`), and an `adapters` array of
`{name, healthy}`. With zero adapters registered the status is `"ok"`
(`all()` over an empty list).

8.2. `GET /api/v1/adapters/{name}/health` and its non-versioned alias
`GET /adapters/{name}/health` MUST delegate to
`OrchestrationService.adapter_health`, which returns a `HealthStatus`
(`healthy`, `last_error`, `uptime_seconds`, `active_tasks`, `memory_mb`).

---

## Known Limitations

**No live integration with the gateway/API routing.** Although commit `6c473cb`
added a Go translation proxy (`internal/api/a2a_proxy.go`, `a2a_proxy_sse.go`)
mounted at `/api/v1/a2a/*` in `internal/api/routes_sub.go` (lines 266–287), and
wired the Python `bridge.AdapterClient` (BaseURL `http://localhost:8001`) into the
A2A gateway via `bridge.RPCBridge` in `cmd/server/server_init_a2a.go`, the
`docs/QA_REVIEW_PHASE2_v2.md` finding P2-1 ("3-way contract divergence ... every
A2A call 404s") was resolved only at the route/handler/mock level. That same
document's own remediation note (line 117) records that **"verified only at
route/handler/mock level; live end-to-end against a running Python adapter
service is a future runtime check."** No reverse-proxy integration against a
running adapter service has been verified end-to-end. Cross-reference:
`docs/QA_REVIEW_PHASE2_v2.md` P2-1, P2-2, P2-4 (findings) and the `6c473cb`
remediation block.

**Proxy↔Python contract (W7, resolved).** The former route-prefix mismatch,
cost-param format mismatch, and empty-registry limitation described in
QA_REVIEW_PHASE2_v2 ("orphaned" claim) are fixed as of the W7 commit:

- All `internal/api/a2a_proxy.go` / `a2a_proxy_sse.go` upstream paths target the
  versioned router — `/api/v1/adapters/*` and `/api/v1/cost/usage` — matching
  the FastAPI mount in `py/oap/adapters/api.py`; the legacy un-prefixed aliases
  in `app.py` are no longer relied on.
- `handleA2ACostSummary` converts the frontend's RFC 3339 `start`/`end` params
  to Unix epoch floats before forwarding (`strconv.FormatFloat` over
  `UnixNano()/1e9`), satisfying FastAPI's `float` Query parser.
- `py/oap/app.py` imports all seven adapter modules at assembly time so every
  `@register_adapter` decorator fires; `ADAPTER_REGISTRY` is populated with
  anthropic, autogen, crewai, langgraph, openai_agents, ozore,
  semantic_kernel without requiring the framework packages to be installed
  (their imports are lazy inside each wrapper's `start()`).
- Task-events SSE ownership: `handleA2ATaskEvents` keeps proxying to the
  adapter service with the keep-alive fallback as the designed degradation.
  The Python service intentionally has no global task-event feed; a real feed
  would require NATS-backed fan-out and is tracked separately.

The remaining gap for this subsystem is purely the live end-to-end runtime
check above; unit-level contracts are pinned by `a2a_proxy_test.go` (mock
upstreams now assert the versioned paths).

**No authentication on the service.** The FastAPI app applies only CORS; there
is no JWT/OIDC middleware despite `settings.py` declaring `oidc_*` and `jwt_*`
fields. Any client that can reach port 8001 can invoke adapters. The service is
meant to sit behind the Go API, which enforces `auth.RequireRole` on the
mutating `/a2a` sub-routes (`routes_sub.go:281-286`), but nothing prevents
direct access to the Python port.

**Dormant DB and settings.** Only `postgres_dsn` is consumed (by `db.py`); the
NATS, OIDC, JWT, and Sentry settings are declared but unread, and `db.py` itself
is imported only by `py/alembic/env.py` for migrations. Cost and budget state is
in-process and non-persistent (see §6.3). The hard-coded CORS origin
(`http://localhost:5173`) and the `jwt_secret` default of `dev-secret-change-me`
are development-only values that are not environment-overridable for CORS.

**No process supervisor.** The service has no systemd unit, no Docker image, no
compose entry, and no health-check loop of its own (the pool's
`health_check_interval` checks subprocess workers, not the FastAPI process).
Bringing it up is a manual `uvicorn` invocation; the Go server will fail its
adapter calls with connection refused if the operator forgets to start it.
