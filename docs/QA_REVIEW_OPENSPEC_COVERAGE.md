# OpenAgentPlatform — OpenSpec Coverage Audit

**Audit date:** 2026-08-23
**Repo:** `/tmp/openagentplatform` (clone, `--depth 1`, read-only)
**Scope:** 21 capability specs in `openspec/specs/*/spec.md` vs. live Go/Python/React code

---

## Executive Summary

OpenSpec coverage is **NOT complete, accurate, or trustworthy**. The `openspec/`
tree contains 21 capability specs, but they were authored against a **Python/Django
"Project Sentinel" / RMM blueprint** and have not been reconciled with the **actual Go
monorepo**. The single most important spec — `rmm-core` (the whole RMM differentiator) —
is marked `COMPLETE` yet points at `backend/apps/rmm/`, references "Django mixins" and
"Celery task packages," and names `internal/scripts` as the script engine — **none of
which exist**. The real implementation lives in Go (`internal/*`, `pkg/agent/*`,
`cmd/agent`, `cmd/server`), uses FastAPI (not Django) for the adapter service, Sahara
migrations (not Django ORM migrations) in `py/alembic/`, and `oap.agents.*` NATS
subjects (not `rmm.*`). Net result: 9 specs are STALE/incorrect as written, 5 specs are
marked `PLANNED` but are **fully implemented in code**, several specs are **path-drifted**
by a whole subtree (`a2a/internal/*` → `a2a/*`, `internal/middleware` → `internal/auth`),
and ~17 substantive `internal/*`/`pkg/*` packages have **no spec at all**. Additionally,
`STATUS.md` and `PROJECT_PLAN.md` describe entirely different projects ("Guardrail MCP
Server" and "Project Sentinel 3.0.0-Enterprise").

---

## 1. Spec Inventory Table (all 21)

Verdict legend: **COMPLETE** = spec matches code; **PARTIAL** = implemented but spec
path/structure drifts; **STALE** = spec describes a non-existent or different-reality
implementation; **MISSING** = code exists with no spec.

| # | Spec | Phase | Status (spec) | App Path (spec) | Path-drift | Verdict |
|---|------|-------|---------------|-----------------|------------|---------|
| 1 | rmm-core | 1 | COMPLETE | `backend/apps/rmm/` | **YES — entire path is Python/Django fiction** | **STALE** |
| 2 | endpoint-agent | 0/1 | COMPLETE | `agents/oap-agent/` | YES → `cmd/agent` + `pkg/agent/` | STALE/PARTIAL |
| 3 | a2a-agent-registry | 2 | COMPLETE | `a2a/internal/registry/agent_card.go` | YES → `a2a/registry/registry.go` | STALE(path) |
| 4 | a2a-task-manager | 2 | COMPLETE | `a2a/internal/states/`, `a2a/internal/models/` | YES → `a2a/manager/statemachine.go`, `a2a/models/` | STALE(path) |
| 5 | event-task-bridge | 2 | PLANNED | `a2a/internal/bridge/event.go` | YES → `a2a/bridge/event.go` | **MISSING(marked PLANNED but built)** |
| 6 | a2a-framework-adapters | 2 | PLANNED | `py/` | no (py exists) | **MISSING(marked PLANNED but built)** |
| 7 | a2a-gateway | 2 | COMPLETE | `a2a/` | no | COMPLETE |
| 8 | process-pool | 2 | PLANNED | `py/` | no (py exists) | **MISSING(marked PLANNED but built)** |
| 9 | auth-rbac | 0/2 | COMPLETE | `internal/middleware/auth.go`, `internal/handlers/auth.go` | YES → `internal/auth/middleware.go` | STALE(path) |
| 10 | secret-management | 3 | PLANNED | *(none listed)* | n/a | **MISSING(marked PLANNED but built)** |
| 11 | commercial-licensing | 5/6 | PLANNED | *(none listed)* | n/a | **MISSING(marked PLANNED but built)** |
| 12 | frontend-react | 4 | PARTIAL | `web/` | no | PARTIAL |
| 13 | hitl-approval | 2 | PLANNED | *(none listed)* | n/a | **MISSING(marked PLANNED but built)** |
| 14 | ci-pipeline | Infra | COMPLETE | `.github/workflows/` | claims "8 workflows"; 10 exist | PARTIAL |
| 15 | deploy-pipeline | Infra | COMPLETE | `deploy.sh`, `deploy/` | no | COMPLETE |
| 16 | infrastructure-standards | Infra | COMPLETE | `deploy/` | no | COMPLETE |
| 17 | documentation-standards | Infra | COMPLETE | `docs/`, `openspec/specs/` | no | COMPLETE |
| 18 | pattern-scanning | Infra | COMPLETE | `scripts/pattern-scan.sh` | no | COMPLETE |
| 19 | schema-health | Infra | COMPLETE | `scripts/schema-health-check.sh` | no | COMPLETE |
| 20 | secret-scanning | Infra | COMPLETE | `scripts/scan-secrets.sh` | no | COMPLETE |
| 21 | semantic-scanning | Infra | COMPLETE | `scripts/semantic-scan.sh` | no | COMPLETE |

**Count:** 6 COMPLETE-verified · 2 PARTIAL · 4 STALE(path) · 1 STALE(rmm-core) ·
6 "PLANNED but actually implemented" (MISSING as true specs) · 2 no-spec-code-drift.

---

## 2. Spec-vs-Implementation Mismatches (specifics)

### 2.1 `rmm-core` — the flagship STALE spec (highest severity)
Spec claims (`openspec/specs/rmm-core/spec.md`):
- **App Path:** `backend/apps/rmm/` — **does not exist.** No `backend/`, no Django, no Celery.
- **Data models (Req 2):** 10 Django models (`Agent`, `Check`, `AgentCheck`, `CheckResult`,
  `Policy`, `PolicyScope`, `WinUpdate`, `AutomatedTask`, `Alert`, `ScriptResult`) with
  `UUIDPrimaryKeyMixin`, `TimestampedMixin`, `OrgScopedMixin`, `SoftDeleteMixin`,
  flat-table `check_type` discriminator, `schedule_bitmask` (21-bit), XOR constraints.
  **Reality:** `pkg/models/` has ~26 Go structs with a **different schema**: `Agent`,
  `CheckDefinition` (not `Check`), `CheckAssignment`, `CheckResult`, `Alert`, `AlertRule`,
  `Policy`, `PolicyAssignment`, `PolicyViolation`, `PatchJob`, `PatchJobTarget`,
  `ScriptDefinition`, `ScriptRun` (in `models_extra.go`), `User`, `Site`. **No**
  `WinUpdate`, `AutomatedTask`, `Check`, `PolicyScope`, `AgentCheck`, `RemoteSession`.
  **No** `schedule_bitmask` anywhere in the codebase.
- **Enums (Req 3):** 12 Django-style enums. **Reality:** status/type fields are plain Go
  `string` with ad-hoc string constants scattered in each package (`internal/alerts`,
  `internal/patches`). No centralized enum type set.
- **State machines (Req 4):** 5 machines (Alert 6-state, WinUpdate 8-state, Agent 5-state,
  ScriptResult 6-state, RemoteSession 7-state). **Reality:** only **two** state machines
  exist in code — `internal/alerts/statemachine.go` (alert lifecycle with
  `ValidTransitions` map) and `internal/patches/approval.go` (PatchJob 8-state approval).
  **No** ScriptResult, RemoteSession, WinUpdate, or Agent-lifecycle state machines.
- **NATS subjects (Req 5):** `rmm.agent.heartbeat`, `rmm.cmd.{agent_id}.*`,
  `rmm.broadcast.*`. **Reality:** code uses **`oap.agents.<id>.heartbeat`** and
  `oap.agents.<id>.*` (see `pkg/agent/heartbeat.go`, `pkg/agent/nats.go`).
- **Services (Req 11):** 10 named services incl. `ScriptEngine`. **Reality:** no
  `ScriptEngine` type exists; equivalents are split across `internal/alerts/engine_*.go`,
  `internal/policy/engine_*.go`, `internal/patches/*`, and plain API handlers in
  `internal/api/scripts.go`, `scripts_run.go`, `script_store.go`.
- **Background tasks (Req 12):** "Seven Celery task packages." **Reality:** zero Celery;
  `internal/tenancy/cleanup.go` (retention purger) and `internal/policy/engine_scheduler.go`
  are the closest Go analogues.

### 2.2 `endpoint-agent` — path + lifecycle drift
- Spec **App Path:** `agents/oap-agent/` (Go module). **Reality:** the Go daemon is
  `cmd/agent/main.go` (doc-comment "Command oap-agent") with engine in `pkg/agent/`
  (`checks.go`, `heartbeat.go`, `scripts.go`, `executor/`, `checkers/`, `patcher/`, `shell/`).
- Spec **Req 2** requires a 6-state lifecycle machine
  (NEW/REGISTERING/REGISTERED/STALE/OFFLINE/DEREGISTERED) with a mutex-guarded
  transition table and `onTransition` callback. **Reality:** no such state machine in
  `pkg/agent/`. `register.go` is a plain API client that POSTs registration and stores a
  token; heartbeat is fire-and-forget. The lifecycle machine is **unimplemented**.
- Spec **Req 1.3** requires Windows `-H windowsgui` tray build and a 6-target
  cross-compile matrix; not verifiable from tree (no such build target found).
- Otherwise the "single static Go binary, outbound-only, NATS, prlimit/job-object" posture
  is directionally correct — but the state-machine and identity-persistence requirements
  (`/var/lib/oap-agent/agent.uuid`) diverge from `pkg/agent/config.go`, which uses
  `%PROGRAMDATA%\OpenAgentPlatform\agent.yaml` and stores `agent_id`+`auth_token`, not a
  UUID4 at `/var/lib/oap-agent/`.

### 2.3 Path-drift by a whole subtree (`a2a/internal/*`)
- `a2a-agent-registry` → `a2a/internal/registry/agent_card.go`; actual: `a2a/registry/registry.go`
- `a2a-task-manager` → `a2a/internal/states/`, `a2a/internal/models/`; actual: `a2a/manager/statemachine.go` + `a2a/models/models.go`
- `event-task-bridge` → `a2a/internal/bridge/event.go`; actual: `a2a/bridge/event.go` (and `event_bridge.go`, `dedup.go`, `ratelimit.go`)
- There is **no `a2a/internal/` directory at all** — the A2A Go module is flat (`bridge/`,
  `gateway/`, `hitl/`, `manager/`, `models/`, `pool/`, `registry/`, `router/`, `spec/`).

### 2.4 `auth-rbac` path drift
- Spec → `internal/middleware/auth.go`, `internal/handlers/auth.go`. **Reality:**
  `internal/auth/middleware.go`, `internal/auth/oidc.go`. No `internal/middleware` or
  `internal/handlers` directories.

### 2.5 `ci-pipeline` count drift
- Spec claims **8 workflows** and lists 7 by name. **Reality:** `.github/workflows/`
  contains **10** `.yml` files — including a `security-scan.yml` and a `release.yml` that
  the spec does not mention.

---

## 3. "PLANNED" specs that are actually fully built (spec lag, not code gap)

These specs say `STATUS: PLANNED` but the implementation is present and substantial:

- **event-task-bridge** (PLANNED) — `a2a/bridge/` is fully built: `EventBridge`
  (`ProcessEvent`), `Deduplicator`, `RateLimiter`, `CircuitBreaker`, `RMMEvent` types,
  `mappings.go`, plus `event_bridge_test.go`.
- **hitl-approval** (PLANNED) — `a2a/hitl/` has `approval.go` (state machine + errors
  `ErrInvalidTransition`), `escalation.go`, `store.go`, `hitl_test.go`.
- **process-pool** (PLANNED) — both `py/oap/adapters/pool.py` + `pool_worker.py` (LRU,
  health checks, subprocess isolation) **and** `a2a/pool/{pool,instance,maintenance}.go`
  with `pool_test.go`.
- **a2a-framework-adapters** (PLANNED) — `py/oap/adapters/` implements **7 adapters**:
  anthropic, openai, crewai, langgraph, autogen, semantic_kernel, ozore, plus
  `orchestrator.py`, `human_loop.py`, `cost.py`, `types.py`.
- **secret-management** (PLANNED) — `secrets/` implements all 5 backends named by the
  spec: `vault/`, `infisical/`, `db_backend.go`, `env_backend.go`, `memory_backend.go`,
  plus `resolver/`, `inject/`, `rotation/`, `oauth/`, `k8s_csi.go` (the spec says "Vault
  and Infisical backends are planned" — they exist).
- **commercial-licensing** (PLANNED) — `internal/license/` (Ed25519-signed keys,
  feature gating, HTTP 402), `internal/billing/` (Stripe), `internal/tenancy/` (RLS
  migration, quotas, isolation), `internal/relay/` (managed A2A relay).

**Implication:** the spec `Phase/Status` metadata lags the code by a full phase in both
directions — some features are labeled COMPLETE that don't exist (rmm-core's Celery),
and some are labeled PLANNED that are done (A2A bridge, HITL, pool, adapters, secrets,
licensing/billing/tenancy).

---

## 4. Features with NO spec at all

Substantive `internal/*` / `pkg/*` packages with **zero OpenSpec coverage**:

| Package | What it does |
|---------|-------------|
| `internal/billing/` | Stripe subscriptions + metering → Stripe meter events |
| `internal/tenancy/` | per-tenant RLS isolation, per-tier quotas, retention purger |
| `internal/license/` + `internal/licensing/` | offline Ed25519 license keys, feature gating, HTTP 402 (two parallel impls) |
| `internal/relay/` | managed A2A relay service |
| `internal/reporting/` + `internal/reports/` | enterprise reporting + delivery engine |
| `internal/audit/` | hash-chained audit events + chain verification |
| `internal/telemetry/` | OTel tracing, Prometheus metrics, otelpgx |
| `internal/monitoring/` | health checks |
| `internal/notify/` | email / Slack / webhook notifiers (SSRF-guarded) |
| `internal/resilience/` | rate limiter, circuit breaker |
| `internal/checklib/` | server-side built-in check catalog |
| `internal/events/` | NATS client, heartbeat handler, check dispatcher |
| `internal/terminal/` | local PTY process manager for remote sessions |
| `internal/session/` | session recording |
| `internal/schema/` | OpenAPI export (`openapi.yaml`, `openapi.go`) |
| `internal/config/` | config loader |
| `internal/db/` | Postgres pool bootstrap |
| `pkg/models/` | **the entire data model** (only partially covered via stale rmm-core spec) |
| `pkg/agent/checkers/` | 9 concrete checkers (ping/http/tcp/dns/cpu/memory/disk/service/script) |

Also **no spec** for the **Python adapter service** (`py/oap/`) as a deployable unit, nor
for the **agent-side cross-compile / packaging** beyond the drifted `endpoint-agent` spec.

---

## 5. RMM parity gap vs Ninja RMM

Roger's stated goal: match Ninja RMM on RMM functionality + differentiate via the agent
framework. The code is further along than the specs suggest, but capability-by-capability:

### Present in code AND (roughly) in specs
- **Checks/monitoring** — `internal/checks/`, `internal/checklib/`, `pkg/agent/checkers/`
  (9 checkers). Spec `rmm-core` §6 covers this conceptually. ✅ reasonable parity.
- **Alerts** — `internal/alerts/` (6-state machine, rules, channels, prefs, dedup).
  Spec §7. ✅ strong parity — *but only for alerts, not the other 4 state machines.*
- **Patches** — `internal/patches/` (approval workflow, scheduler, deployer). Spec §9.
  ✅ present; approval workflow implemented.
- **Policies** — `internal/policy/` (OPA engine, collectors, violations). Spec §8. ✅ present.
- **Script execution** — real in `internal/api/scripts*.go` + `pkg/agent/executor/` +
  `pkg/agent/scripts.go` + `pkg/agent/shell/`. **Spec §10/§11 name "ScriptEngine" and
  `internal/scripts` — neither exists**, so scripting is implemented but spec-inaccurate.
- **Remote access** — `internal/remote/`, `internal/terminal/`, `cmd/*/shell`. Spec §10.4.
  ✅ present (SSH/WinRM over NATS, recordings); **but RemoteSession state machine (Req 4)
  is missing.**

### Present in code but MISSING from specs (RMM capabilities with no spec)
- Tenancy / multi-org isolation (RLS) — no spec.
- Billing / licensing / feature gating — spec exists but is PLANNED while code is complete.
- Audit log (hash-chained) — no spec.
- Enterprise reporting / report delivery — no spec.

### Entirely ABSENT (both code and spec) — the real RMM parity gaps
From `docs/GAP_ANALYSIS_RMM_PLATFORM.md` (confirmed by keyword sweep, nothing in source):
- **WinUpdate / AutomatedTask automation** — no Windows-update state machine, no
  `schedule_bitmask`, no scheduled tasks (spec §2.8/§4.2 claims them; code has none).
- **Patch scheduling / reboot coordination** — patch approval exists, but no
  `needs_reboot` rebroadcast workflow beyond a `NeedsReboot bool` field.
- **Cloud control** — zero AWS/Azure/GCP SDK imports (inventory, cost, IAM drift). ❌
- **Hypervisor / virtualization monitoring** — no Proxmox/libvirt/vSphere/ESXi code. ❌
- **Active security** — posture collectors detect AV/firewall state, but no EDR/IDS/SIEM
  ingest, no threat-intel, no vulnerability-scan integration. ⚠️ partial.
- **Power / UPS monitoring** — absent.
- **Maintenance windows / silence windows** — absent (alerts have snooze but no
  scheduled maintenance-window concept).
- **Agent auto-update channel** — version reported but no push/update mechanism.
- **Offline-agent SLA alerting** — no "agent silent >N hours" rule.

---

## 6. Stale top-level metadata (corroborating the surfaced findings)

- **`STATUS.md`** — title "Project Status - Guardrail MCP Server", v2.8.0, Sprint 001–004
  about "document ingestion", "26 API endpoints in api.js", "6 SPA pages". This describes
  a **different project** (the `mcp-server/` subtree — a separate deployable Go module
  `github.com/thearchitectit/guardrail-mcp`). The real platform (`cmd/server`, `internal/`,
  `web/`) has no `STATUS.md` entry.
- **`PROJECT_PLAN.md`** — title "Project Sentinel: Comprehensive Implementation Plan",
  v3.0.0-Enterprise, "governance layer for Autonomous AI Agents", "Cortex/Jailor/Interceptor/
  Polyglot" pillars, Q2 2026. This is **not** the OpenAgentPlatform roadmap (which is an
  agent-first RMM + A2A platform, BSL 1.1, `README.md` = "OpenAgentPlatform"). Two entirely
  different product stories coexist at repo root.
- **`docs/PYTHON_TO_GO_MIGRATION.md`**, **`mcp-server/README.md`**, **`mcp-server/CHANGELOG.md`**
  — all describe the Guardrail MCP Server, not OpenAgentPlatform.
- The RMM architecture doc (`docs/architecture/RMM_CORE.md`) itself carries the stale
  `backend/apps/rmm/` App Path and `nats/subjects.py`/`client.py` references — the spec
  drift originates in the source doc, not just the spec file.

---

## 7. Recommendations (ranked by severity)

### P0 — Correct the truth layer (blocks everything else)
1. **Rewrite `STATUS.md`** to describe OpenAgentPlatform (Go server + Go agent + Python
   adapter + React SPA + A2A gateway). Archive the Guardrail MCP Server sprint content
   (it belongs to the `mcp-server/` module, not this repo). *(critical — actively
   misleads contributors)*
2. **Retitle or replace `PROJECT_PLAN.md`.** "Project Sentinel" is not this product;
   replace with an OpenAgentPlatform roadmap keyed to the 6 phases already used by the
   specs (0 Foundation → 6 Commercial).
3. **Fix the `rmm-core` spec** (and its source `docs/architecture/RMM_CORE.md`):
   - App Path → `internal/` + `pkg/models/` + `cmd/agent` (no `backend/`, no Django, no Celery).
   - Replace Django-mixin model list (Req 2) with the actual Go structs (`Agent`,
     `CheckDefinition`, `CheckAssignment`, `CheckResult`, `Alert`, `PatchJob`,
     `ScriptDefinition`, `ScriptRun`, `Policy`, `PolicyAssignment`, `PolicyViolation`).
   - Recompose the 5-state-machine requirement (Req 4) to the 2 that exist and mark the
     other 3 (ScriptResult, RemoteSession, WinUpdate) as **not implemented** — do not
     claim COMPLETE.
   - Swap `rmm.*` subjects → `oap.agents.*` (Req 5).
   - Delete the "seven Celery task packages" requirement (Req 12); replace with the Go
     scheduler/retention-purger reality.
   - Remove `schedule_bitmask` / `WinUpdate` / `AutomatedTask` if not built, or move them
     to a clearly-labeled "planned" subsection.

### P1 — Fix path drift + flip PLANNED→COMPLETE where code exists
4. **Correct the `a2a/internal/*` App Paths** in `a2a-agent-registry`, `a2a-task-manager`,
   `event-task-bridge` → `a2a/registry/`, `a2a/manager/`, `a2a/bridge/`. There is no
   `a2a/internal/` directory.
5. **Flip the 6 "PLANNED but built" specs to COMPLETE** (or PARTIAL with an accurate
   "current state" note): event-task-bridge, hitl-approval, process-pool,
   a2a-framework-adapters, secret-management, commercial-licensing. Their "Current state"
   notes (e.g. secret-management's "Vault and Infisical backends are planned") are already
   false — the backends are implemented.
6. **Fix `auth-rbac` path** → `internal/auth/`; **fix `endpoint-agent` path** →
   `cmd/agent` + `pkg/agent/`.
7. **Update `ci-pipeline`** from "8 workflows" to the accurate 10, and include
   `security-scan.yml` + `release.yml`.

### P2 — Author missing specs for uncovered code
8. Write specs for the high-value undocumentated packages: **tenancy** (RLS + quotas),
   **billing/licensing** (already PLANNED but understated), **audit** (hash-chained),
   **reporting/reports**, **notify**, **relay**, **telemetry**, **resilience**,
   **checklib**, and **`pkg/models`** (the data model — currently only reachable through
   the stale rmm-core spec).
9. Add one capability spec for **`py/oap`** (the Python adapter service) as a deployable
   unit — currently referenced only by `process-pool`/`a2a-framework-adapters` App Paths.

### P3 — Close real RMM parity gaps (decide scope first)
10. Decide whether **WinUpdate/AutomatedTask/full remote-session state machine** are in
    scope. If "match Ninja RMM" is the goal, they are gaps **in code**, not just specs —
    add them as PLANNED specs, not COMPLETE.
11. If cloud control / hypervisor / active-security are roadmap targets, author PLANNED
    specs for them (currently zero code and zero spec).

---

## Method note

Verification was read-only. For each spec I located the stated App Path, compared it to the
on-disk directories/Go packages, and spot-checked key requirements (models, state machines,
subjects, endpoints) against `pkg/models/`, `internal/*/`, `a2a/*/`, `pkg/agent/`,
`cmd/agent/`, `py/oap/`. Counts of requirement headers per spec ranged 5–15. The `a2a/`,
`secrets/`, and root modules are separate Go modules in `go.work` (`.`, `./a2a`,
`./secrets`); `py/` is a FastAPI project (`pyproject.toml` name `oap`); `mcp-server/` is a
**separate, unrelated** Go module (`github.com/thearchitectit/guardrail-mcp`).
