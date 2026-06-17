# QA Review Report -- Phase 2

> **Date:** 2026-06-17
> **Sprints:** 2.1, 2.2, 2.3
> **Phase:** A2A (Agent-to-Agent) Communication + Agent Framework Adapters
> **Reviewers:** A2A Backend, Python Adapters, Go Bridge, Frontend A2A, Security, Structure Audit, Cross-Cutting Integration

---

## Executive Summary

### Findings by Severity

| Severity | Count | Description |
|----------|-------|-------------|
| **CRITICAL** | 5 | Runtime failures -- system will not work end-to-end |
| **SIGNIFICANT** | 6 | Data loss, incorrect behavior, or degraded functionality |
| **MINOR** | 2 | Code quality issues, no immediate runtime impact |
| **Total** | **13** | |

### Overall Health Assessment: **RED**

Phase 2 delivers substantial code volume and structural organization, but the subsystem is **not operationally functional**. The Go A2A backend, Python adapter layer, and React frontend were developed as parallel workstreams with insufficient contract synchronization. Every frontend A2A API call will return HTTP 404. The Go-Python bridge has field-name and type mismatches that will cause silent data loss or runtime panics. The Go and Python modules build and vet cleanly in isolation, but the integration between them -- the actual user-facing surface -- is broken.

Phase 1 fixes have all held (14/14 checks pass), indicating that prior remediation work was correctly preserved during Phase 2 development.

### Phase 1 Fix Re-Verification: **14/14 PASS**

All Phase 1 QA fixes remain in place. No regressions detected.

### Top 3 Highest-Priority Issues

1. **All frontend A2A API calls return 404** -- The frontend's `apiFetch` prepends `/api/v1` to all paths, but the Go server mounts A2A routes at `/a2a/...`. Additionally, the path structure itself differs (`/a2a/adapters` vs `/a2a/v1/agents`, missing `/v1` segment, missing endpoints). This means the entire A2A dashboard is non-functional from the UI.

2. **SSE StreamEvent JSON key mismatch** -- Go parses `type` from JSON, but Python serializes the key as `event_type`. Every streaming event will have an empty `Type` field in Go, and the stream will never terminate via the `done`/`error` check (only via `[DONE]` sentinel fallback).

3. **Go-Python contract misalignment on all shared types** -- `InvokeRequest`, `InvokeResponse`, `HealthStatus`, `AdapterInfo`, `UsageReport`, `BudgetInfo`, and `AgentCard` have fundamentally different field names, cardinalities, and nesting structures between Go and Python. Adapter registrations in the A2A registry will have only `name` and `healthy` populated -- all other fields will be empty.

---

## Phase 1 Fix Re-Verification

| # | Check | Expected | Actual | Verdict |
|---|-------|----------|--------|---------|
| **1** | `.go` file count in `a2a/` | -- | **20** Go files across 6 packages: `bridge/` (5), `gateway/` (7), `manager/` (3), `models/` (2), `registry/` (2), `router/` (1), `spec/` (1 proto). Plus `go.mod` and `go.sum`. | **PASS** |
| **2** | Python adapters in `py/oap/adapters/` | -- | **17 files**: Anthropic, AutoGen, CrewAI, LangGraph, OpenAI, Semantic Kernel adapters; API layer, cost tracking, human loop, orchestrator, pool, types, wrapper. | **PASS** |
| **3** | A2A routes (TSX) in `web/src/routes/a2a/` | -- | **5 files**: `index.tsx`, `tasks.tsx`, `tasks/$taskId.tsx`, `costs.tsx`, `agents/$name.tsx` | **PASS** |
| **4** | A2A lib files | -- | **1 file**: `web/src/lib/useA2A.ts` | **PASS** |
| **5** | Disk usage | -- | `a2a/`=280K, `py/oap/`=460K, `web/src/routes/a2a/`=84K | **PASS** |
| **6** | Root `go build ./... && go vet ./...` | PASS | **PASS** -- Build succeeds, vet clean | **PASS** |
| **7** | A2A `go build ./... && go vet ./...` | PASS | **PASS** -- Submodule builds and vets clean | **PASS** |
| **8** | `checks.result` subject in `pkg/agent/` (without `oap.events.checks`) | EMPTY | **EMPTY** -- zero matches | **PASS** |
| **9** | `0010_add_description_to_checks.py` exists | File exists | **File exists** at expected path | **PASS** |
| **10** | `oap.agents.*.results` in `internal/events/nats.go` | >= 1 | **2 matches** | **PASS** |
| **11** | `wsChannelPatches` in `internal/api/websocket.go` | >= 1 | **3 matches** | **PASS** |
| **12** | `wsChannelScripts` in `internal/api/websocket.go` | >= 1 | **3 matches** | **PASS** |
| **13** | `cmd/server/server.go` exists | Exists | **Exists** | **PASS** |
| **14** | `/patches/jobs` in `usePatches.ts` (non-comment) | EMPTY or only list/create | **9 matches** -- all legitimate CRUD endpoints (list, get, create, sub-resources) | **PASS** |

**Result: 14/14 PASS. No Phase 1 regressions detected.**

---

## Findings by Domain

### A2A Backend (Go module)

The A2A Go module is structurally well-organized with 20 Go files across 6 packages (`bridge/`, `gateway/`, `manager/`, `models/`, `registry/`, `router/`) and 1 proto spec. It builds and vets cleanly. However, the route layer does not expose the endpoints the frontend expects.

**Findings:**

| # | Finding | Severity | Details |
|---|---------|----------|---------|
| A2A-1 | **Route prefix mismatch** | CRITICAL | Routes mounted at `/a2a/...` but frontend sends to `/api/v1/a2a/...` (via `apiFetch` adding `/api/v1` prefix). All frontend calls will 404. |
| A2A-2 | **Path structure mismatch** | CRITICAL | Frontend expects `/a2a/adapters`, `/a2a/invoke`, `/a2a/costs/summary`, `/a2a/stream`. Go server has `/a2a/v1/agents`, `/a2a/v1/tasks`. No REST invoke, no cost, no stream endpoints exist. |
| A2A-3 | **Missing endpoints** | CRITICAL | No `/a2a/adapters/{name}/health`, no `/a2a/costs/summary`, no `/a2a/stream`, no `/a2a/tasks/events` (SSE). |
| A2A-4 | **AdapterInfo deserialization broken** | CRITICAL | `AdapterInfo` struct expects flat fields (`framework`, `description`, `version`, `capabilities`, `tags`) but Python returns nested `agent_card` object. All non-name fields will be empty after deserialization. |
| A2A-5 | **HealthStatus incompatible** | SIGNIFICANT | Go expects `name`, `status`, `message`, `last_checked`, `latency_ms`. Python returns `healthy`, `last_error`, `uptime_seconds`, `active_tasks`, `memory_mb`. Completely different structures. |
| A2A-6 | **Build passes** | INFO | `go build ./...` and `go vet ./...` both pass clean in both root and A2A submodule. |

### Python Adapters

The Python adapter layer covers 6 major LLM frameworks (Anthropic, AutoGen, CrewAI, LangGraph, OpenAI, Semantic Kernel) across 17 files. The API surface is well-structured with FastAPI, Pydantic models, cost tracking, and a human-in-the-loop layer. However, the wire format does not match the Go bridge expectations.

**Findings:**

| # | Finding | Severity | Details |
|---|---------|----------|---------|
| PY-1 | **AdapterListEntry nests data in `agent_card`** | CRITICAL | `api_models.py:60-71` returns `{name, agent_card: {AgentCard}}` but Go `AdapterInfo` expects flat fields. Go will only get `name` and `healthy`. |
| PY-2 | **InvokeRequest field mismatch** | CRITICAL | `api_models.py:34` uses `adapter_name` (Go sends `adapter`), expects `messages: list[Part]` (Go sends single `Message`). No `context_id` field (Go sends one). |
| PY-3 | **InvokeResponse type mismatch** | SIGNIFICANT | Returns `messages: list[Part]`, `cost: CostRecord`, `error_message`, `tokens_used`, `duration_ms`. Go expects `messages: []Message`, `usage: *UsageRecord`, `error`, no tokens/duration. |
| PY-4 | **StreamEvent uses `event_type` key** | CRITICAL | `types.py:169` serializes JSON key as `event_type` with values `"delta"`, `"status"`, `"error"`, `"done"`. Go parses key `type` and expects values `"message"`, `"artifact"`, `"status"`, `"error"`, `"done"`. No overlap on streaming payload values. |
| PY-5 | **Cost query expects Unix epoch float** | CRITICAL | `api.py:283`: `from_ts: float = Query(0.0, alias="from")`. Go sends RFC3339 string `"2024-01-01T00:00:00Z"`. FastAPI returns HTTP 422. |
| PY-6 | **Cost response structures incompatible** | SIGNIFICANT | `CostRecord` / UsageReport / BudgetInfo all have different field names from Go equivalents. |
| PY-7 | **AgentCard field set divergent** | SIGNIFICANT | Python has `url`, `provider_name`, `provider_url`, `streaming`, `push_notifications`, `default_input_modes`, `default_output_modes`. Go has `id`, `endpoint`, `framework`, `capabilities`, `tags`, `authentication`. Minimal overlap. |
| PY-8 | **AgentSkill missing `id`** | MINOR | `types.py:44-59`: `AgentSkill` has no `id` field. Go `Skill` model requires `id` (validated at `models.go:147`). Will fail validation. |

### Go-Python Bridge

The bridge is the critical integration layer between the Go backend and Python adapters. It is the layer with the highest concentration of contract mismatches.

**Findings:**

| # | Finding | Severity | Details |
|---|---------|----------|---------|
| BR-1 | **SSE stream never terminates cleanly** | CRITICAL | Go client checks `event.Type == "done"` at `client.go:532` but `Type` is always empty (JSON key is `event_type`, not `type`). Stream only ends via `[DONE]` sentinel or connection close. |
| BR-2 | **Stream event type values incompatible** | CRITICAL | Go expects `"message"` and `"artifact"`; Python sends `"delta"`. Go client will discard all streaming content. |
| BR-3 | **InvokeRequest wire format broken** | CRITICAL | `bridge/models.go:20-35` sends `{adapter, message, context_id, stream}`. Python expects `{adapter_name, messages: list[Part]}`. The `context_id` field is ignored; the `stream` field is dead. |
| BR-4 | **InvokeResponse wire format broken** | CRITICAL | `bridge/models.go:74-84` returns `{messages: []Message, usage: *UsageRecord, error}`. Python expects `{messages: list[Part], cost: CostRecord, error_message, tokens_used, duration_ms}`. Go will get nil for `usage` and empty for `messages`. |
| BR-5 | **HealthStatus wire format broken** | SIGNIFICANT | `bridge/models.go:196-210` and Python `types.py:181-195` have zero compatible fields. Health monitoring is non-functional. |
| BR-6 | **Cost query format broken** | CRITICAL | `client.go:668-669` sends `from.UTC().Format(time.RFC3339)`. Python expects `float` (Unix epoch). Every cost API call returns 422. |
| BR-7 | **NATS subject: `oap.events.agent.offline`** | SIGNIFICANT | Bridge subscribes at `bridge.go:37-66` but no publisher exists anywhere in `internal/`. Dead subscription. |
| BR-8 | **NATS subject: `oap.events.shell.session`** | SIGNIFICANT | Bridge subscribes but no publisher exists in `internal/`. Dead subscription. |
| BR-9 | **NATS subject: `oap.events.policy.violation`** | SIGNIFICANT | Bridge subscribes to `violation` but Phase 1 publishes to `oap.events.policy.evaluate` and `oap.events.policy.evaluated`. Subject name mismatch. |
| BR-10 | **NATS subject: `oap.events.agent.online`** | MINOR | Published via inline string at `internal/api/agents.go:143` instead of a shared constant. Fragile to refactoring. |

### A2A Frontend (React)

The frontend has 5 A2A route components and a library hook (`useA2A.ts`). The UI components are present, but every API call they make will fail at runtime.

**Findings:**

| # | Finding | Severity | Details |
|---|---------|----------|---------|
| FE-1 | **All A2A API calls return 404** | CRITICAL | `apiFetch` prepends `/api/v1` (`web/src/lib/api.ts:45`). `useA2A` passes paths like `/a2a/adapters` to `apiFetch`, resulting in requests to `/api/v1/a2a/adapters`. Go server mounts at `/a2a/...`. Every call 404s. |
| FE-2 | **Path structure does not match Go server** | CRITICAL | Frontend expects `/a2a/adapters`, `/a2a/invoke`, `/a2a/costs/summary`, `/a2a/stream`. None of these exist on the Go server with the correct structure. |
| FE-3 | **SSE stream path wrong** | CRITICAL | `useA2A.ts:274` uses raw `fetch` to `/a2a/stream` (bypassing `apiFetch`, so no `/api/v1` prefix), but this route does not exist on the Go server. |
| FE-4 | **Response shape expectations wrong** | SIGNIFICANT | `A2AAdapter` type (`useA2A.ts:18-33`) expects `display_name`, `provider`, `url`, `icon`, `health`, `streaming`, `models`, `uptime_secs`, `active_tasks`, `memory_mb`. Go `AgentCard` returns `id`, `framework`, `endpoint`, `capabilities`, `tags`, `authentication`. Largely disjoint field sets. |
| FE-5 | **No fallback for missing routes** | MINOR | No error boundary or empty-state handling for when A2A API calls fail. Dashboard will show silent failures. |

### Security

No security-specific findings were reported in the input data. The security review domain produced an empty result set. This does not mean the A2A subsystem is secure -- it means the security audit was not yet performed against this phase's deliverables. The following areas should be prioritized in a dedicated security review:

- **Authentication/Authorization:** A2A endpoints may not enforce org-scoped access controls. The Go AgentCard has an `authentication` field but no evidence it is validated server-side.
- **RPC bridge authentication:** The Go-to-Python RPC bridge (via `RPCBridge`) does not appear to use mutual TLS or token-based auth. Any process with network access to the Python adapter can invoke it.
- **NATS subscription security:** The bridge subscribes to 8 NATS subjects. If NATS is not configured with subject-level ACLs, the bridge may receive unauthorized events.
- **Input validation:** Go structs use raw types (e.g., `string` for `adapter`) without enum constraints. Python Pydantic provides validation on its side, but the Go bridge may pass through unvalidated data to the RPC layer.

### Cross-Cutting / Integration

The cross-cutting integration check revealed that the three parallel workstreams (Go backend, Python adapters, React frontend) were developed without sufficient contract synchronization. The findings below represent the systemic risks.

**Findings:**

| # | Finding | Severity | Details |
|---|---------|----------|---------|
| CC-1 | **No API contract specification** | CRITICAL | There is no OpenAPI spec, protobuf schema (beyond the A2A spec), or shared type definition between Go and Python. Field names, types, and structures were independently designed. |
| CC-2 | **A2A registry will be empty** | CRITICAL | `RPCBridge.SyncAgentCards` (`rpc.go:419-461`) calls `ListAdapters` and deserializes into `AdapterInfo`. Due to the nested vs. flat structure mismatch, all registered AgentCards will have only `name` and `healthy`. No `Description`, `Version`, `Framework`, `Capabilities`, `Tags`, or `Skills`. Skill-based task routing will fail entirely. |
| CC-3 | **Cost reporting is non-functional** | CRITICAL | Frontend calls `/a2a/costs/summary` (which 404s). Even if it routed correctly, the Go server has no cost endpoints. The Python cost API expects Unix epoch floats (Go sends RFC3339 strings -> 422). The response structures are incompatible. |
| CC-4 | **No integration tests across the Go-Python boundary** | SIGNIFICANT | Both modules pass their own build/vet checks, but there is no evidence of end-to-end integration testing. All contract mismatches would have been caught by a single end-to-end test. |
| CC-5 | **Frontend type definitions not derived from API** | SIGNIFICANT | `A2AAdapter` type in `useA2A.ts` is hand-written and does not match the Go `AgentCard` or Python `AdapterListEntry`. Using a code generator (e.g., from OpenAPI) would prevent drift. |

---

## Metrics

### File Counts

| Component | Files | Notes |
|-----------|-------|-------|
| A2A Go module | 20 .go + 1 .proto | bridge(5), gateway(7), manager(3), models(2), registry(2), router(1) |
| Python adapters | 17 .py | 6 framework adapters + API/cost/orchestrator/pool/types/wrapper |
| A2A frontend routes | 5 .tsx | index, tasks, tasks/$taskId, costs, agents/$name |
| A2A frontend lib | 1 .ts | useA2A hook |

### Disk Usage

| Directory | Size |
|-----------|------|
| `a2a/` | 280K |
| `py/oap/` | 460K |
| `web/src/routes/a2a/` | 84K |
| **Total** | **824K** |

### Build Status

| Check | Result |
|-------|--------|
| Root `go build ./...` | **PASS** |
| Root `go vet ./...` | **PASS** |
| A2A `go build ./...` | **PASS** |
| A2A `go vet ./...` | **PASS** |

---

## Recommendations

### Must-Fix Before Phase 3 (Blocking)

1. **Define a single API contract for the Go-Python bridge.** Write an OpenAPI 3.1 spec for the Python adapter REST API. Generate Go client types from it. Generate TypeScript types for the frontend. This is the single highest-leverage action.

2. **Fix the route prefix mismatch.** Either:
   - Mount Go A2A routes under `/api/v1/a2a/...` to match the frontend's `apiFetch` prefix, OR
   - Strip the `/api/v1` prefix from A2A calls in the frontend, OR
   - Add a reverse-proxy rule to rewrite `/api/v1/a2a` to `/a2a`.

3. **Add the `/v1` path segment to the Go server routes** (or remove it from the frontend expectations). Align the path structure so frontend paths match server paths exactly.

4. **Add missing Go server endpoints:** `/a2a/adapters/{name}/health`, `/a2a/invoke` (REST), `/a2a/costs/summary`, `/a2a/stream`, `/a2a/tasks/events` (SSE).

5. **Fix the SSE JSON key mismatch.** Change Go's `StreamEvent` struct to use `json:"event_type"` to match Python serialization. Align event type values: Python should send `"message"` and `"artifact"` instead of `"delta"`, or Go should learn to handle `"delta"`.

6. **Fix the cost query format.** Either change Go to send Unix epoch floats, or change Python to accept RFC3339 strings via a Pydantic validator.

7. **Fix the AdapterInfo / AgentCard deserialization.** Either flatten the Python response or add a `agent_card` field to Go's `AdapterInfo` struct and update `SyncAgentCards` to extract from the nested object.

8. **Add NATS publishers** for `oap.events.agent.offline` and `oap.events.shell.session`. Fix the policy subject naming: either rename bridge subscription to `oap.events.policy.evaluate` or rename the publisher to `oap.events.policy.violation`.

9. **Resolve the `InvokeRequest` field mismatch.** Agree on `adapter` vs `adapter_name`. Agree on `messages: list[Part]` vs single `Message`. Go must match Python's API contract.

### Should-Fix (Significant, not blocking Phase 3 start)

10. **Align `HealthStatus`, `UsageReport`, `BudgetInfo`, and `UsageRecord` structures** between Go and Python. Use the OpenAPI spec from Recommendation 1 as the single source of truth.

11. **Add end-to-end integration tests** that start the Python adapter, start the Go server, and make real HTTP calls. This would have caught all 5 critical findings.

12. **Use shared constants for NATS subjects.** Replace all inline subject strings with named constants in a shared package. This prevents the `agent.online` fragility and the policy subject mismatch.

13. **Add the `id` field to Python's `AgentSkill`** to match Go's validation requirement, or remove `id` as required from Go's `Skill` model.

14. **Add error boundaries and empty-state handling** in the A2A frontend components so that 404s produce visible error messages instead of blank screens.

### Nice-to-Have (Minor)

15. **Add a health check endpoint** to the Go A2A gateway that verifies connectivity to the Python adapter pool.

16. **Generate TypeScript types from the Go API** using a tool like `go2ts` or from the OpenAPI spec. This eliminates the `A2AAdapter` vs `AgentCard` drift.

17. **Add structured logging** to the Go-Python bridge so that contract mismatches are logged as warnings, not silently swallowed.

---

## Sign-off

- [ ] All critical/high findings addressed
- [ ] Build passes cleanly
- [ ] OpenAPI contract spec created and types generated
- [ ] End-to-end integration test suite in place
- [ ] Ready for Phase 3 (Secret Management)

**Current status: NOT READY FOR PHASE 3.** 5 critical findings must be resolved before proceeding. The A2A subsystem is not operationally functional in its current state -- it compiles cleanly but does not work end-to-end. Phase 3 (Secret Management) should not begin until the Go-Python contract is fixed and integration tests pass.
