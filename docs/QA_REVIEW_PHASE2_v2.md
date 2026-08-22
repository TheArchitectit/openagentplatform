# Phase 2 QA Review — A2A + Agents (v2 Synthesis)

> Date: 2026-08-12 | Scope: Phase 2 A2A gateway (Go), Python adapter service (`py/oap/adapters/`), React A2A dashboard (`web/src/.../a2a*`) | Method: three parallel domain reviews (Go / Python / React) + root-cause cross-read of `cmd/server/a2a_routes.go`, `web/src/lib/useA2A.ts`, `useA2A_types.ts`, `api.ts`
> Priority process: per user instruction, all fixes MUST pass `scripts/regression_check.py` and the `deploy.sh` secret-scan + build gate before push. Each fix needs a `.guardrails/failure-registry.jsonl` FAIL-* entry + a regression test (REGRESSION_PREVENTION.md).

## Executive Summary

- **Overall health: RED.** The entire A2A dashboard is non-functional — every call the React frontend makes 404s.
- **Root cause is a 3-way contract divergence**, not a simple prefix slip:
  - **Go A2A gateway** mounts at `/a2a/` + `/a2a/v1/...` (`/v1/tasks`, `/v1/agents`, `/v1/tasks/{id}/subscribe` SSE). `cmd/server/a2a_routes.go`.
  - **Python adapter service** mounts at `/api/v1/...` (`/adapters/invoke`, `/adapters/stream`, `/adapters/{name}/card|health|models`, `/cost/usage`, `/cost/budgets`). `py/oap/adapters/api.py`. **ORPHANED** — no proxy from either the Go API or the gateway.
  - **React frontend** calls `/api/v1/a2a/adapters`, `/api/v1/a2a/invoke`, `/api/v1/a2a/costs/summary`, `/api/v1/a2a/stream`, `/api/v1/a2a/tasks/events`. `apiFetch` prepends `/api/v1`; `A2A = '/a2a'` (`useA2A_types.ts:166`).
- The frontend's vocabulary (`/adapters`, `/invoke`, `/costs/summary`, `/stream`, `/tasks/{id}/cancel`, `/tasks/events`) matches **neither** the Go gateway's primitives **nor** the Python service's actual mounted paths. There is no adapter/proxy layer bridging the three.
- **Finding counts (current review):** 2 CRITICAL, 7 HIGH, 4 MEDIUM, 2 LOW. (Earlier `QA_REVIEW_PHASE2.md` flagged 5 CRITICAL + 6 SIGNIFICANT; most hold — see "Prior findings status" below. The divergence was understated previously.)
- **Go backend compiles cleanly** (`go build ./...` exit 0, verified). The breakage is integration/routing, not the compile break already fixed in Phase 1 (Bridge / PatchScheduler).

## TL;DR — what has to happen

A prefix-only fix is insufficient. Remediation must reconcile **all three layers at once**:
1. Decide the canonical A2A URL surface (recommend: keep `/a2a/*` Go-native, and make the gateway Proxy the Python adapter service under `/a2a/adapters`, `/a2a/invoke`, `/a2a/stream`, `/a2a/costs/*`, and expose task SSE at a path the frontend uses).
2. Fix the frontend `A2A` base + SSE paths to match the agreed surface.
3. Reconcile the Python ↔ Go model field names (cost fields, auth_schemes, AgentSkill schema, UsageReport shape) so JSON round-trips.

## Prioritized Findings

### CRITICAL

| ID | Category | Files | Description | Recommendation |
|----|----------|-------|-------------|----------------|
| P2-1 | Routing / 3-way contract | `cmd/server/a2a_routes.go`, `web/src/lib/useA2A.ts:31,36,45,57,70,89,95,108`, `web/src/lib/useA2A_types.ts:166`, `web/src/lib/api.ts:6` | Frontend calls `/api/v1/a2a/{adapters,invoke,costs/summary,stream,tasks/{id}/cancel,tasks/events}`. Go mounts A2A at `/a2a/*` and has NO `/api/v1/a2a/*` route and NO proxy to the Python service. Every A2A call 404s. | Mount an A2A adapter/proxy layer (or remount gateway at `/api/v1/a2a`) and forward to the Python adapter service. Align frontend `A2A` constant + SSE paths to the chosen surface. |
| P2-2 | Orphaned service / missing proxy | `cmd/server/a2a_routes.go`, `py/oap/adapters/api.py:60` | The Python adapter service (`/api/v1/adapters/...`, `/api/v1/cost/...`) is not reachable from the Go API or the gateway. No reverse proxy / mount exists. | Add a Go reverse-proxy handler forwarding the agreed A2A adapter paths to the Python service (e.g. `http://localhost:8001`). |

### HIGH

| ID | Category | Files | Description | Recommendation |
|----|----------|-------|-------------|----------------|
| P2-3 | Route mismatch (resource names) | `useA2A.ts:31,36,57,89,108` vs `a2a_routes.go:40-45` | Frontend uses `/a2a/adapters/{name}/{card,health}`, `/a2a/invoke`, `/a2a/costs/summary`, `/a2a/stream`, `/a2a/tasks/{id}/cancel`. Go exposes `/a2a/v1/agents`, `/a2a/v1/agents/{name}`, `/a2a/v1/tasks`. No invoke/stream/costs/adapter-health endpoints. | Extend gateway REST surface + Python proxy to serve the frontend contract (or rewrite frontend to Go-native primitives). Pick ONE contract. |
| P2-4 | Missing SSE endpoints | `useA2A.ts:139` (`/api/v1/a2a/stream`), `useA2A.ts:255` (`TASK_SSE_PATH=/api/v1/a2a/tasks/events`) | Frontend opens two SSE streams at `/a2a/stream` (fetch+ReadableStream) and `/a2a/tasks/events` (EventSource). Go only has `/a2a/v1/tasks/{id}/subscribe`. Python has `/adapters/stream` (different path). Neither path the frontend hits exists. | Expose task SSE + adapter stream at the frontend's expected paths (proxy to Python `/adapters/stream` and a gateway task feed). |
| P2-5 | Field mismatch — cost model | `py/oap/adapters/api_models.py:119-123` vs `useA2A_types.ts:54-59`, `bridge/models.go:165-195` (Go) | Python returns `input_per_1k`/`output_per_1k`; frontend expects `input_cost_per_1k`/`output_cost_per_1k`. Cost fields arrive `undefined`. | Rename Python fields to `*_cost_per_1k` (or add JSON aliases) to match the shared contract. |
| P2-6 | Serialization bug — auth_schemes | `py/oap/adapters/types.py:97` vs `bridge/models.go:58` | Python serializes `auth_schemes` as `list[str]` (e.g. `["api_key"]`). Go `models.AgentCard.AuthSchemes` is `[]AuthScheme` (objects with `Type`+`Config`). Deserializes to empty structs / fails. | Align types: either Go accepts `[]string` or Python emits `AuthScheme` objects. |
| P2-7 | Field mismatch — UsageReport shape | `py/oap/adapters/cost_models.py:165-174` vs `bridge/models.go:165-195` | Python: `time_range_start/end` float unix, `total_tokens/total_prompt_tokens/total_completion_tokens` ints, `by_model/by_adapter` dicts. Go: `From/To time.Time`, `PromptTokens/CompletionTokens int64`, no `by_model/by_adapter`. Fields dropped. | Reconcile UsageReport DTO across Python + Go + frontend `A2ACostSummary` (`useA2A_types.ts:118`). |
| P2-8 | Field mismatch — AgentSkill vs Skill | `py/oap/adapters/types.py` (`AgentSkill`: name/description/tags/input_schema/output_schema) vs `bridge/models.go` (`Skill`: ID/Name/Description/Tags) | Gateway extracts Python `AgentCard` into Go `models.AgentCard`; `Skill.input_schema`/`output_schema` silently dropped on deserialization. | Add schema fields to Go `Skill` or document intentional divergence. |
| P2-9 | InvokeRequest — unpopulated fields | `py/oap/adapters/api_models.py:23-38` vs `bridge/models.go:70-79` | Go bridge sends `AdapterName/TaskID/Messages`; Python accepts `metadata`+`timeout` but Go never populates them (defaults apply silently). | Either send `metadata`/`timeout` from Go or drop the unused Python fields. |

### MEDIUM

| ID | Category | Files | Description | Recommendation |
|----|----------|-------|-------------|----------------|
| P2-10 | Type mismatch — response envelope | `useA2A.ts:31,90` vs gateway `RESTAgentsHandler`/`RESTTasksHandler` | Frontend parses dual-format `{adapters:[]}|[]` and `{tasks:[]}|[]`. Verify gateway REST handlers return the exact envelope the frontend dual-parses; mismatch → empty lists. | Lock the envelope contract; add a regression test asserting the shape. |
| P2-11 | Unused error swallowed | `a2a/bridge/rpc_bridge.go:188` (from Go review #23) | A returned error is ignored at the RPC bridge. | Capture/log the error; add a regression check. |
| P2-12 | Dead code | `a2a/bridge/...generateTaskID` (Go review #23, NEW-5) | `generateTaskID` unused. | Remove or wire up. |
| P2-13 | SSE failure has no UI | `web/src/routes/a2a/tasks.tsx` (useA2ATasks SSE) | `fetchTasks` + SSE used for KPI stats; SSE connection failures render nothing (the `/tasks/events` path 404s per P2-4). | Surface SSE connection errors in the UI. |

### LOW

| ID | Category | Files | Description | Recommendation |
|----|----------|-------|-------------|----------------|
| P2-14 | Unimplemented cost dashboard | `web/src/routes/a2a/costs.tsx:42` (`fetchCostSummary` → `/a2a/costs/summary`) | Calls non-existent endpoint (see P2-3/P2-5). Will always error. | Resolved once P2-3 + P2-5 land. |
| P2-15 | SSE auto-refresh silent failure | `web/src/routes/a2a/tasks.tsx` | EventSource `/api/v1/a2a/tasks/events` fails; stats silently stale. | Resolved with P2-4. |

## Prior Findings Status (vs `QA_REVIEW_PHASE2.md`)

- **A2A-1 (route prefix `/api/v1` vs `/a2a`)** — CONFIRMED, and root cause is broader (see P2-1/P2-2). The old report framed it as a prefix fix; it is a missing proxy + resource-name divergence.
- **A2A-2 (path structure)** — CONFIRMED + WORSENED. Frontend hits 8+ distinct paths; gateway exposes 4 patterns. No invoke/costs/stream/adapter-health.
- **A2A-3 (missing endpoints)** — CONFIRMED. `/adapters/{name}/health`, `/costs/summary`, `/stream`, `/tasks/events` all absent at frontend paths.
- **A2A-4 (AdapterInfo flat vs nested agent_card)** — RESOLVED on Go side (`AdapterInfo.AgentCard *models.AgentCard` nests correctly). The *adjacent* issue is field loss inside the nested card (P2-8).
- **A2A-5 (HealthStatus)** — subsumed by P2-3 (no `/adapters/{name}/health` route).
- **PY-1 (AdapterListEntry nesting / Skill field loss)** — HOLDS, refined as P2-8.
- **PY-2 (InvokeRequest field mismatch)** — PARTIALLY RESOLVED (`Part` types match; `metadata`/`timeout` unpopulated → P2-9).
- **NEW (from Go review #23):** P2-11 (swallowed error), P2-12 (dead code `generateTaskID`).

## Metrics

- `go build ./...`: PASS (exit 0). `go vet ./...`: PASS conceptually (verified by #23).
- `pnpm build` + lint: not re-verified in this pass (gate runs in `deploy.sh` during remediation).
- Python adapter service: router defined but orphaned (not mounted in Go).
- Frontend: A2A dashboard compiles; all network calls 404 against current server.

## Regression Plan (mandatory per REGRESSION_PREVENTION.md + deploy.sh)

Every CRITICAL/HIGH fix below requires, before push:
1. A new line in `.guardrails/failure-registry.jsonl` (`FAIL-A2A-<n>`).
2. A regression test (Go handler test for the proxy/route; TS/Jest fetch test or Playwright assertion for the frontend contract; Python pytest for the model serialization).
3. `python3 scripts/regression_check.py --all --soft-as-hard --pre-commit` green.
4. `./deploy.sh` secret-scan (×2, public-repo hard gate) + build gate green before `git push --follow-tags`.

| Fix | FAIL entry | Regression test |
|-----|-----------|-----------------|
| P2-1 gateway proxy + frontend base align | `FAIL-A2A-001` | Go: GET `/a2a/adapters` returns 200 from proxied Python; TS: `fetchAdapters()` resolves against dev server. |
| P2-2 mount/forward Python service | `FAIL-A2A-002` | Go: `/a2a/invoke` forwards to Python `/adapters/invoke` (mock upstream). |
| P2-3 resource-name contract | `FAIL-A2A-003` | Go/TS: each frontend path has a server route returning the expected envelope. |
| P2-4 SSE endpoints | `FAIL-A2A-004` | Go: SSE connects at `/a2a/tasks/events` and `/a2a/stream`. |
| P2-5 cost field rename | `FAIL-A2A-005` | Python/Go: `input_cost_per_1k` round-trips; TS parses non-`undefined`. |
| P2-6 auth_schemes type | `FAIL-A2A-006` | Go: `[]AuthScheme` populates from Python `auth_schemes`. |
| P2-7 UsageReport shape | `FAIL-A2A-007` | Python/Go: UsageReport DTO symmetric. |
| P2-8 AgentSkill schema | `FAIL-A2A-008` | Go: `Skill.input_schema/output_schema` survive deserialization. |
| P2-9 invoke metadata/timeout | `FAIL-A2A-009` | Go→Python invocation carries metadata/timeout or fields removed. |

## Remediation Order (recommended)

1. **P2-1 + P2-2** (proxy + mount) — unblocks everything; highest leverage.
2. **P2-3 + P2-4** (resource names + SSE) — makes the dashboard reachable & live.
3. **P2-5 → P2-9** (model/field reconciliation) — makes data correct.
4. **P2-10 → P2-15** (envelope lock, dead code, UI error states) — hardening.

Do NOT begin fixing until this plan is acknowledged. Follow Phase 1 sequencing: QA pass → report → fix in priority order, each through the deploy gate.

## Findings Status (resolved 2026-08-22)

All 15 findings (P2-1 → P2-15) are **CLOSED**. Verified by `go build ./...` (clean) and a fully-empty `.guardrails/failure-registry.jsonl` active set (FAIL-A2A-001..009 all `status: resolved`).

- **P2-1..P2-9** (critical/high contract divergence): fixed in `6c473cb` — translation proxy `internal/api/a2a_proxy.go` + SSE `a2a_proxy_sse.go`, gateway wiring `cmd/server/server_init_a2a.go`, Python field aliases `py/oap/adapters/api_models.py`.
- **P2-10** (envelope contract): locked + regression test `web/src/lib/a2a.test.ts` (`fetchTasks` dual-parses `{tasks:[]}` and bare array).
- **P2-11** (swallowed RPC error): fixed in `78c6045` — `a2a/bridge/rpc_bridge.go` now captures/logs the `GetTaskInternal` error.
- **P2-12** (dead `generateTaskID`): removed in `78c6045`.
- **P2-13** (SSE failure UI): fixed in `e203d30` — `useA2ATasks` exposes `sseConnected` + error; `web/src/routes/a2a/tasks.tsx` shows a reconnecting indicator.
- **P2-14 / P2-15** (cost dashboard + silent EventSource): resolved by the proxy layer routes (`/costs/summary`, `/tasks/events`) added in `6c473cb` and surfaced via P2-13. Note: verified at route/handler/mock level only; live end-to-end against a running Python adapter service is a future runtime check.

Infra fixes for the deploy gate landed in `466876e`, `fd2e374` (Dockerfile `secrets/go.sum` removal, `.dockerignore`, web base `node:22`, `network: host` build), and release `v1.1.1` was tagged and pushed.

