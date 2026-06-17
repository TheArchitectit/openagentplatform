# QA Review Report

> **Date:** 2026-06-17 | **Sprints Reviewed:** 0.1 - 1.5 | **Total Findings:** 34

---

## Executive Summary

### Findings by Severity

| Severity | Count | Description |
|----------|-------|-------------|
| **Critical** | 2 | Blocks core functionality; will cause runtime failures |
| **High** | 7 | API calls will 404/405; integration breakage |
| **Medium** | 8 | Runtime failures possible; significant gaps |
| **Low** | 9 | Code cleanup; minor mismatches |
| **Info** | 8 | Noted but not actionable |
| **Total** | **34** | |

### Overall Health Assessment: **YELLOW**

The platform has solid architectural separation (Go backend, React frontend, separate mcp-server module, Python services) and consistent patterns across most subsystems. However, **critical NATS and DB schema mismatches** mean the check pipeline cannot function end-to-end, and **seven frontend API path mismatches** will produce silent UI failures. These are addressable but must be resolved before Phase 2.

### Top 3 Highest-Priority Issues

1. **NATS subject mismatch** -- Agent publishes check results on `oap.agents.<id>.checks.result` but the server subscribes on `oap.agents.*.results`. No check results will ever be received by the server.
2. **`description` column missing from DB schema** -- `pgCheckStore.InsertCheck` inserts into a `description` column that does not exist in the `check_definitions` table. INSERTs will fail with a SQL error.
3. **Frontend `/patches/jobs/{id}` vs server `/patches/{id}`** -- All frontend patch operations (approve, reject, status, cancel) use the wrong path prefix. The entire patch management UI is non-functional.

---

## Findings by Domain

### Backend (Go API)

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| B-1 | **Critical** | DB Schema | `py/alembic/versions/0003_checks.py` | `description` column is absent from the `check_definitions` table but `pgCheckStore.InsertCheck` (`internal/api/check_store.go:48`) inserts into it. | Add `description TEXT` column in a new Alembic migration, or remove the column from the INSERT statement and Go model. |
| B-2 | **Critical** | Messaging | `pkg/agent/checks.go:50` vs `internal/events/nats.go:21` | Agent publishes check results on `oap.agents.<id>.checks.result`; server subscribes on `oap.agents.*.results`. These subjects never match. | Standardize on a single subject pattern. Recommend changing agent publish to `oap.agents.<id>.results` to match the server subscription. |
| B-3 | **High** | API Routing | `internal/api/routes.go:82` | Check update endpoint registered as `r.Put("/")` but frontend sends `PATCH`. | Change to `r.Patch("/")` or update the frontend to use `PUT`. |
| B-4 | **High** | API Routing | `internal/api/routes.go:85-86` | Check assignment endpoints use `/assign` and `/assign/{agent_id}` but frontend calls `/agents` and `/agents/{agentId}`. | Align path conventions. Recommend updating frontend to `/assign` or adding `/agents` aliases on the server. |
| B-5 | **High** | API Routing | `internal/api/routes.go:~265` | Compliance summary is registered at `/compliance/summary` under the policies group, but frontend calls `/policies/compliance/summary`. | Move route to `/policies/compliance/summary` or update frontend path. |
| B-6 | **High** | API Routing | `internal/api/routes.go:~240` | Policy agent assignment registered as `/policies/{id}/assign` but frontend calls `/policies/{id}/agents`. | Align with check assignment pattern. |
| B-7 | **High** | API Routing | `internal/api/routes.go:195` | Script run cancel registered as `/scripts/runs/{run_id}/cancel` but frontend calls `/script-runs/{runId}/cancel` (note singular `run`). | Add a redirect/alias or fix frontend path. |
| B-8 | **High** | API Routing | `internal/api/routes.go:214-225` | Patch job endpoints are at `/patches/{id}` but frontend uses `/patches/jobs/{id}` for all operations (approve, reject, status, cancel). | Either add a `/jobs` prefix in routes or update frontend to drop `/jobs`. |
| B-9 | **High** | API Routing | `internal/api/routes.go:235` | No `/patches/scans` route exists. Scan triggers live at `/agents/{id}/patches/scan`. | Add a top-level `/patches/scans` endpoint or fix frontend to use the agent-scoped path. |
| B-10 | **Medium** | Data Model | `pkg/models/models.go:67-79` | `CheckDefinition` model missing 6 DB columns: `fail_threshold`, `warning_threshold`, `error_threshold`, `alert_severity`, `is_template`, `last_status`. | Extend the Go model struct and update `pgCheckStore` to insert and query these columns. |
| B-11 | **Medium** | Data Model | `pkg/models/models.go:28` | `Agent.OS` field but DB column is `operating_system`. Also missing `agent_id` string, `tags`, `metadata`, `total_memory_mb`, `total_disk_gb`. | Rename struct field to `OperatingSystem` or add a DB alias. Add missing fields to model and store layer. |
| B-12 | **Medium** | WebSocket | `internal/api/websocket.go:33-35,335-339` | Server only supports `agents`, `checks`, `alerts` channels. Frontend requests `patches` and `scripts` channels which are rejected by `validChannel()`. | Add `wsChannelPatches` and `wsChannelScripts` with corresponding NATS subscriptions. |
| B-13 | **Medium** | Check Execution | `internal/api/checks.go:42-53` vs `pkg/agent/checkers/registry.go:76-85` | API validates `script` and `custom` check types but agent registry only has 8 types (`ping, http, tcp, dns, cpu, memory, disk, service`). `custom` has no handler anywhere. | Remove `custom` from validation or implement a generic executor. Clarify `script` handling. |
| B-14 | **Medium** | Code Structure | `cmd/server/main.go` | Entry point is 15KB monolithic. | Split into `main.go` (bootstrap), `server.go` (HTTP server), `routes.go` (route registration). |
| B-15 | **Low** | Dead Code | `pkg/models/models.go:85-95` | `CheckAssignment` struct has zero references outside `models.go`. A separate `events.CheckAssignment` is used by the dispatcher. | Delete the model or wire it into the API/store layer. |
| B-16 | **Low** | Build Hygiene | `bin/oap-agent` (12 MB) | Compiled binary is checked into the repository. | Add `bin/` to `.gitignore`. Remove the tracked binary. Rebuild in CI. |
| B-17 | **Low** | Module Boundary | `mcp-server/go.mod` | Separate Go module with different module path (`github.com/thearchitectit/guardrail-mcp`) and Go version (1.23.2 vs root 1.25.0). | Document the boundary. Consider unifying module path if they will share types. |
| B-18 | **Info** | Test Coverage | `internal/api/`, `internal/alerts/`, `internal/events/`, `internal/notify/`, `internal/auth/`, `internal/checks/`, `internal/checklib/` | Zero `*_test.go` files across 7 core server packages. | Add unit tests for handlers, stores, and middleware. Prioritize `api/` and `events/` (highest blast radius). |
| B-19 | **Info** | DevOps | Root level | No root-level `Dockerfile` or `docker-compose.yml`. Dockerfiles exist in `deploy/` subdirectories. | Create a top-level Docker Compose for local development that orchestrates server, agent, frontend, NATS, and Postgres. |

### Agent (CLI Binary)

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| A-1 | **Critical** | Messaging | `pkg/agent/checks.go:50` | Publishes check results on `oap.agents.<id>.checks.result` -- does not match server subscription `oap.agents.*.results`. | Change to `oap.agents.<id>.results`. See B-2 for the matching server fix. |
| A-2 | **Medium** | Check Registry | `pkg/agent/checkers/registry.go:76-85` | Only 8 checker types registered (`ping, http, tcp, dns, cpu, memory, disk, service`). API accepts `script` and `custom` but agent has no `custom` handler. | Implement a `CustomChecker` that delegates to a configured plugin/script, or reject `custom` type assignments at the server. |
| A-3 | **Info** | Library | `pkg/agent/checkers/` | 11 checkers defined; all appear actively used. No dead code found. | No action needed. |

### Frontend (React)

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| F-1 | **High** | API Client | `web/src/lib/useChecks.ts:277` | `updateCheck` sends `method: 'PATCH'` but server expects `PUT`. Will return 405. | Change frontend to `PUT` or update server to accept `PATCH`. See B-3. |
| F-2 | **High** | API Client | `web/src/lib/useChecks.ts:~310,316` | Check assignment paths use `/checks/{id}/agents` but server has `/assign`. | Update frontend to `/assign`. See B-4. |
| F-3 | **High** | API Client | `web/src/lib/usePolicies.ts:~213` | Compliance summary path `/policies/compliance/summary` does not match server route `/compliance/summary`. | Update frontend to `/compliance/summary`. See B-5. |
| F-4 | **High** | API Client | `web/src/lib/usePolicies.ts:~220` | Policy agent assignment uses `/policies/{id}/agents` but server has `/assign`. | Update frontend to `/assign`. See B-6. |
| F-5 | **High** | API Client | `web/src/lib/useScripts.ts:293` | Script run cancel uses `/script-runs/{runId}/cancel` but server route is `/scripts/runs/{run_id}/cancel`. | Fix path to match server. See B-7. |
| F-6 | **High** | API Client | `web/src/lib/usePatches.ts:558-619` | All patch operations use `/patches/jobs/{id}` but server has `/patches/{id}`. | Remove `/jobs` segment from all patch API calls. See B-8. |
| F-7 | **High** | API Client | `web/src/lib/usePatches.ts:527` | `/patches/scans` has no corresponding server route. Scan triggers are at `/agents/{id}/patches/scan`. | Fix frontend to use the agent-scoped scan path. See B-9. |
| F-8 | **Medium** | WebSocket | `web/src/lib/websocket.ts:21` | Frontend requests `patches` and `scripts` channels but server only supports 3 channels. | Remove unsupported channels from frontend or wait for server-side implementation. See B-12. |
| F-9 | **Info** | Build Artifact | `web/src/routeTree.gen.ts` | TanStack Router generated file (8KB) checked into git. This is the project default and is acceptable. | No action needed. |

### Infrastructure

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| I-1 | **Medium** | Testing | Repository-wide | Go test coverage is 8.1% (18 of 222 Go files have tests). Core packages (`api/`, `events/`, `auth/`) have zero tests. | Establish a coverage gate (target: 60% for `internal/`, 80% for `pkg/agent/`). Add tests incrementally. |
| I-2 | **Medium** | Build | `cmd/server/main.go` | 15KB monolithic entry point. | Split into focused files. See B-14. |
| I-3 | **Low** | Build Artifact | `bin/oap-agent` | 12MB binary tracked in git. | Add to `.gitignore`. See B-16. |
| I-4 | **Low** | Module Boundary | `mcp-server/` | Separate Go module (48MB directory, 109 of 222 Go files). | Document the deploy boundary. See B-17. |
| I-5 | **Info** | Config | `deploy/nats/certs/.gitkeep` | Placeholder for empty cert directory. Acceptable. | No action. |
| I-6 | **Info** | Config | `scripts/ingest_docs.go.disabled` | Tagged with `//go:build never`. Intentional. | No action. |

### Security

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| S-1 | **Info** | -- | -- | No security review findings were recorded for this sprint cycle. | Run a dedicated security audit before Phase 2. Focus areas: OIDC token handling in `internal/auth/`, secret management in agent config, and NATS TLS in `deploy/nats/certs/`. |

### Documentation

| # | Severity | Category | File | Description | Recommendation |
|---|----------|----------|------|-------------|----------------|
| D-1 | **Info** | -- | -- | No documentation review findings were recorded. | Verify that INDEX_MAP.md and HEADER_MAP.md are current with sprint 1.5 additions (script CRUD, 4-runtime executor, Monaco UI, remote shell, session recording). |

### Cross-Cutting / Integration

| # | Severity | Category | Description | Recommendation |
|---|----------|----------|-------------|----------------|
| X-1 | **Critical** | NATS Messaging | Agent publish subject `oap.agents.<id>.checks.result` does not match server subscription `oap.agents.*.results`. The wildcard `.results` expects the subject to end in `.results` (no `.checks.` segment). Result: zero check results are delivered. | Fix both sides to use a single canonical pattern. Recommended: `oap.agents.<id>.results`. Update agent publish + server subscription + ingest subscription. |
| X-2 | **High** | API Contract | Seven frontend-to-server path mismatches (see F-1 through F-7). Each will produce 404 or 405 at runtime. | Run a contract test between frontend hooks and server routes. Consider generating TypeScript types from the OpenAPI spec (`api/openapi.yaml`). |
| X-3 | **Medium** | Data Model | `CheckDefinition` model is out of sync with the DB schema (6 missing columns). Cannot query/update threshold or template fields via API. | Generate Go models from the Alembic migrations or add a CI check that compares schema vs struct tags. |
| X-4 | **Medium** | WebSocket | Frontend expects 5 channels; server supports 3. `patches` and `scripts` will silently drop. | Implement missing channels or remove them from the frontend `Channel` type union. |
| X-5 | **Medium** | Check Pipeline | `custom` check type passes API validation but has no agent handler. Dispatching a `custom` check will fail on the agent. | Implement `CustomChecker` or reject `custom` type at the API layer. |
| X-6 | **Low** | Data Model | Agent table column names diverge from Go struct (`OS` vs `operating_system`, missing `agent_id`, `tags`, `metadata`, `total_memory_mb`, `total_disk_gb`). | Align struct field names with DB columns via explicit SQL column tags. |

---

## Metrics

### File Counts

| Language | Count | Notes |
|----------|-------|-------|
| Go | 222 | 3 Go modules (root + mcp-server + examples) |
| TypeScript/TSX | 59 | React frontend, VSCode extension, examples |
| Python | 44 | Services, scripts, Alembic migrations, tests |
| YAML | 35 | CI/CD, deployment configs, OpenAPI |
| Rust | 6 | Examples only |
| Swift | 3 | Examples only |
| **Total source files** | **369** | |

### Lines of Code (Estimated)

| Component | Size | Notes |
|-----------|------|-------|
| `internal/` (Go) | 976 KB | Core server logic across 16 packages |
| `pkg/` (Go) | 268 KB | Shared agent library + models |
| `mcp-server/` (Go) | ~2 MB (source) | 109 Go files in distinct module |
| `web/` (TS/TSX) | 736 KB | React + TanStack Router + Monaco |
| `py/` (Python) | 124 KB | Services + Alembic + scripts |

### Build Status

| Check | Status | Notes |
|-------|--------|-------|
| `go mod tidy` (root) | **PASS** | Exit code 0, no errors |
| `go build ./...` (root) | **PASS** | All packages compile |
| `go vet ./...` | **PASS** | No warnings reported |
| CI Pipeline | **PRESENT** | Jenkinsfile and GitLab CI configs exist in `ci/` |
| Docker Setup | **PARTIAL** | Dockerfiles in `deploy/` but no root-level Compose |

### Test Coverage

| Package Area | Test Files | Coverage Estimate |
|--------------|-----------|-------------------|
| `internal/api/` | 0 | ~0% |
| `internal/alerts/` | 0 | ~0% |
| `internal/events/` | 0 | ~0% |
| `internal/notify/` | 0 | ~0% |
| `internal/auth/` | 0 | ~0% |
| `internal/checks/` | 0 | ~0% |
| `internal/checklib/` | 0 | ~0% |
| `internal/patches/` | 2 | ~40% |
| `internal/policy/` | 0 (but `collectors/` has some) | ~10% |
| `internal/remote/` | 0 | ~0% |
| `internal/audit/` | 0 | ~0% |
| `pkg/agent/` | scattered | ~15% |
| `mcp-server/internal/` | scattered | ~20% |
| **Overall Go** | **18 / 222** | **~8.1%** |

---

## Recommendations

### Must-Fix Before Phase 2 (Critical + High)

| Priority | Finding | Action |
|----------|---------|--------|
| 1 | X-1 / B-2 / A-1 | Fix NATS subject mismatch. Standardize on `oap.agents.<id>.results`. Update `pkg/agent/checks.go:50`, `internal/events/nats.go:21`, and `internal/checks/ingest.go:94`. |
| 2 | B-1 | Add `description TEXT` column to `check_definitions` via Alembic migration. |
| 3 | B-3 / F-1 | Resolve PATCH vs PUT for check update. |
| 4 | B-4 / F-2 | Align check assignment paths (`/assign` vs `/agents`). |
| 5 | B-5 / F-3 | Fix policy compliance summary path. |
| 6 | B-6 / F-4 | Align policy agent assignment paths. |
| 7 | B-7 / F-5 | Fix script run cancel path. |
| 8 | B-8 / F-6 | Remove `/jobs` prefix from all frontend patch operations. |
| 9 | B-9 / F-7 | Fix or add `/patches/scans` route. |

### Should-Fix (Medium)

| Finding | Action |
|---------|--------|
| B-10 / X-3 | Extend `CheckDefinition` model with 6 missing DB columns. |
| B-11 | Rename `Agent.OS` to `Agent.OperatingSystem`; add missing agent fields. |
| B-12 / F-8 / X-4 | Implement `patches` and `scripts` WebSocket channels on server. |
| B-13 / A-2 / X-5 | Implement `CustomChecker` or reject `custom` type at API. |
| B-14 / I-2 | Split `cmd/server/main.go` into focused files. |
| I-1 | Establish test coverage baseline; add unit tests for `internal/api/`, `internal/events/`, `internal/auth/`. |

### Nice-to-Have (Low + Info)

| Finding | Action |
|---------|--------|
| B-15 | Delete or wire up `models.CheckAssignment`. |
| B-16 / I-3 | Remove `bin/oap-agent` from git; add to `.gitignore`. |
| B-17 / I-4 | Document `mcp-server` module boundary. |
| B-18 | Add tests for zero-coverage server packages. |
| B-19 | Create root-level `docker-compose.yml` for local dev. |
| S-1 | Run a dedicated security audit before Phase 2. |
| D-1 | Update INDEX_MAP.md and HEADER_MAP.md for sprint 1.5. |

---

## Sign-off

- [ ] All critical findings addressed (X-1, B-1)
- [ ] All high findings addressed (B-3 through B-9, F-1 through F-7, X-2)
- [ ] `go build ./...` passes cleanly with zero warnings
- [ ] Frontend API client paths verified against server routes (contract test)
- [ ] NATS end-to-end check result delivery confirmed
- [ ] DB migrations applied; `pgCheckStore.InsertCheck` succeeds
- [ ] WebSocket channels `patches` and `scripts` implemented or removed from frontend
- [ ] Test coverage baseline established with CI gate
- [ ] Security audit completed
- [ ] Ready for Phase 2 (A2A protocol + Agent Marketplace)
