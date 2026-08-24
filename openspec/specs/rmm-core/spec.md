# RMM Core

> **Phase:** 1 (Core RMM)
> **STATUS: PARTIAL** — core capabilities implemented in Go; named state machines,
> automation entities, and some service boundaries below are still aspirational.
> See §4.7, §12, and the "Not Yet Implemented" notes throughout.
> **Source:** `docs/architecture/RMM_CORE.md` *(historical design doc — partially stale,
> see its correction banner)*
> **App Path:** `internal/checks/`, `internal/alerts/`, `internal/policy/`,
> `internal/patches/`, `internal/api/scripts*.go`, `internal/remote/`,
> `internal/events/`, `internal/checklib/`, `pkg/models/`, `cmd/agent`, `pkg/agent/`

> Reconciliation note (2026-08-23): this spec was previously authored against a
> Python/Django blueprint (`backend/apps/rmm/`, Django mixins, Celery beat, `rmm.*`
> subjects) that was never built. It has been rewritten against the actual Go
> implementation. Audit trail: `docs/QA_REVIEW_OPENSPEC_COVERAGE.md`.

---

## Description

RMM Core is the deterministic backbone of OpenAgentPlatform: device
registration, monitoring checks, policy propagation, patch management, alert
lifecycle, script execution, remote access, and the NATS orchestration layer
that connects them.

In a traditional RMM, automation is rigid — checks fire alerts, alerts notify
humans, humans remediate. RMM Core keeps that reliable spine but adds a second
path: every RMM event can be delegated to an LLM agent over A2A for triage,
risk assessment, and remediation behind human approval gates. The deterministic
layer must be trustworthy on its own, because the agent layer is built on top
of it.

Two transports are used deliberately. REST carries CRUD, queries, and large
idempotent payloads. NATS carries real-time command dispatch, streaming output,
and event fan-out. Neither transport serves both patterns well.

## User Story

**As** an MSP technician managing thousands of endpoints,
**I want** checks that detect problems, alerts that reach the right person,
policies that propagate down my client hierarchy, patches that deploy under
approval, and scripts that run on demand,
**so that** I can operate a fleet reliably — and hand any of those events to an
AI agent for triage without giving up the deterministic guarantees underneath.

---

## Requirements

### 1. Dual-Transport Architecture

1.1. The system MUST use REST for CRUD operations, registration, inventory
check-ins, queries, and reporting; and NATS for real-time agent commands,
streaming output, and event distribution.

1.2. Transport selection MUST follow these rules:

| Pattern | Transport | Rationale |
|---------|-----------|-----------|
| Agent registration | REST | One-time, request/response, token issuance |
| Agent full inventory snapshot | REST | Large, infrequent, idempotent |
| Agent heartbeat | NATS | Small, frequent (60s), fire-and-forget |
| Server dispatches check/script run | NATS | Sub-second delivery |
| Agent streams results/output | NATS | Chunked, real-time, one-way |
| Technician queries agent list | REST | Paginated, filtered, read-heavy |
| Check failure → A2A task | NATS | Event-driven pub-sub fan-out |

1.3. Agent identity MUST persist across restarts: the agent stores its
`agent_id` and `auth_token` in its local config (Windows:
`%PROGRAMDATA%\OpenAgentPlatform\agent.yaml`; Linux/macOS equivalent path) and
re-registers idempotently.

1.4. The agent MUST be a single static Go binary communicating outbound-only
(REST + NATS); no inbound listeners on the endpoint.

### 2. Data Models

Implemented in `pkg/models/` (PostgreSQL via migrations):

2.1. These models MUST exist: `Agent`, `Site`, `User`, `CheckDefinition`,
`CheckAssignment`, `CheckResult`, `Alert`, `AlertRule`, `NotificationRecord`,
`Policy`, `PolicyAssignment`, `PolicyViolation`, `PatchJob`, `PatchJobTarget`,
`ApprovalRecord`, `ScriptDefinition`, `ScriptRun`, `AuditEvent`.

2.2. `CheckDefinition` uses flat-table polymorphism: one table, type
discriminator, nullable type-specific fields — chosen over joined-table
inheritance for high-volume result writes.

2.3. `CheckAssignment` links checks to agents/sites; `CheckResult` is a separate
high-write time-series table with independent retention pruning.

2.4. Org/site scoping MUST index on the scope columns as leading columns so
multi-tenant queries stay selective. Tenant isolation is enforced at the
database level via row-level security (see `internal/tenancy/`).

2.5. Not implemented (previously claimed by this spec, moved to planned):
`WinUpdate` (per-KB Windows update tracking), `AutomatedTask`
(`schedule_bitmask` scheduled automation). See §14.

### 3. Enums and Status Fields

3.1. Status/type fields MUST be typed constants declared next to the domain
that owns them (e.g. severity/states in `internal/alerts`, approval states in
`internal/patches`). A centralized enum package is NOT required, but string
literals scattered at call sites MUST NOT be used where a constant exists.

3.2. Check types supported by the built-in library (`pkg/agent/checkers/`,
`internal/checklib/`): `ping`, `http`, `tcp`, `dns`, `cpu`, `memory`, `disk`,
`service`, `script`.

3.3. `AlertSeverity` levels: `critical`, `high`, `medium`, `low`, `info`.

### 4. State Machines

4.1. State machines MUST use explicit transition tables (a
`ValidTransitions`-style map) and MUST reject invalid transitions with tests
asserting rejection explicitly.

4.2. **Alert** state machine — IMPLEMENTED (`internal/alerts/statemachine.go`):
states covering new / acknowledged / in_progress / snoozed / resolved / closed
with acknowledge, resolve, snooze, close, reopen transitions.

4.3. **PatchJob approval** state machine — IMPLEMENTED
(`internal/patches/approval.go`): pending → approved/rejected → deploying →
completed/failed, with the approving user recorded.

4.4. NOT YET IMPLEMENTED (do not claim COMPLETE):
- **ScriptRun** lifecycle machine (pending/running/success/error/timeout/cancelled).
- **RemoteSession** lifecycle machine (requested/active/closed states).
- **Agent lifecycle** machine (pending/online/degraded/offline/uninstalled) —
  liveness today is heartbeat-TTL based (§13), not a formal machine.
- **WinUpdate** machine — depends on §14 scope decision.

4.5. When any §4.4 machine gets built, it MUST follow the §4.1 transition-table
pattern and reuse the A2A manager's machine conventions (`a2a/manager/statemachine.go`)
where applicable.

### 5. NATS Subject Taxonomy

All platform subjects live under the `oap.` prefix. The legacy `rmm.*` taxonomy
from earlier drafts of this spec was never implemented.

5.1. Agent→Server (per-agent) subjects:
- `oap.agents.{agent_id}.heartbeat`
- `oap.agents.{agent_id}.results` (check results)
- `oap.agents.{agent_id}.scripts` (script output/results)
- `oap.agents.{agent_id}.compliance.results`
- `oap.agents.{agent_id}.patch_scan.results`

5.2. Server→Agent (per-agent command inbox):
- `oap.agents.{agent_id}.checks` (dispatch check runs)
- `oap.agents.{agent_id}.scripts` (dispatch script runs)
- `oap.agents.{agent_id}.scripts.cancel`

Per-agent addressing ensures a command cannot be delivered to the wrong
endpoint. Wildcard consumers (e.g. `oap.agents.*.heartbeat`) are used server-side.

5.3. Event fan-out (server-side pub-sub, consumed by engines/A2A bridge/UI):
`oap.events.agent.online`, `oap.events.alerts.*`, `oap.events.checks.result`,
`oap.events.patches`, `oap.events.policy.evaluate`, `oap.events.policy.evaluated`,
`oap.events.remediation`, `oap.events.scripts`.

### 6. Checks

6.1. Full CRUD MUST be available for check definitions over REST.

6.2. A built-in check library MUST ship with the §3.2 checker set working
end-to-end (agent-side `pkg/agent/checkers/`, server-side catalog
`internal/checklib/`).

6.3. Scheduling bounds: `interval_seconds` ≥ 30; `timeout_seconds` ≤ 3600;
enforced by validation, not call-site convention alone.

6.4. The check pipeline MUST schedule due checks, dispatch them over NATS
(`oap.agents.{id}.checks`), ingest results, and evaluate thresholds.

6.5. A check MUST alert only after `fail_threshold` consecutive failures.

6.6. Check results MUST be pruned on a schedule per retention policy
(retention purger: `internal/tenancy/cleanup.go`).

### 7. Alerts

7.1. The alert engine (`internal/alerts/engine_*.go`) MUST generate
deduplication keys and suppress duplicate alerts for the same condition.

7.2. Notification channels MUST include email, Slack, and webhook
(`internal/notify/`).

7.3. Per-severity routing and snooze/silence MUST be configurable
(`AlertRule`, notification preferences).

7.4. Resolved alerts MUST be pruned per retention policy.

7.5. Alert state changes MUST go exclusively through the Alert state machine.

7.6. Scheduled maintenance windows (suppress alerts during defined periods) are
NOT implemented — see §14.

### 8. Policies

8.1. Policies MUST resolve through the Client > Site > Agent hierarchy,
evaluated bottom-up (Agent → Site → Client → Organization).

8.2. Enforcement modes MUST support inherit/exclude semantics; conflicts
resolved by priority, higher winning.

8.3. OPA MUST evaluate policy; agent actions evaluated against policy produce
`PolicyViolation` records.

8.4. Policy violations MUST create alerts and dispatch notifications.

8.5. Policy evaluation events flow over `oap.events.policy.evaluate` /
`.evaluated`; changes propagate to affected agents by delta computation, not
full-state resend.

8.6. Policy scheduling/evaluation loops run server-side
(`internal/policy/engine_scheduler.go`).

### 9. Patches

9.1. Patch scans MUST be triggerable per agent, results reported over
`oap.agents.{id}.patch_scan.results` and exposed as a per-agent patch list.

9.2. Patches MUST pass through the approval state machine; transitions logged
with the approving user.

9.3. Policy MUST be able to auto-approve patches by severity.

9.4. Batch deployment MUST track progress per target (`PatchJobTarget`) and
retry failures.

9.5. `NeedsReboot` reporting exists on patch targets; full reboot coordination
(reboot prompts, maintenance-window scheduling of reboots) is NOT implemented —
see §14.

9.6. CVE correlation to patch records is NOT yet implemented.

### 10. Scripts and Remote Access

10.1. A script library MUST support CRUD across runtime types including
powershell, cmd, python, shell (`internal/api/scripts.go`, `script_store.go`).

10.2. Script stdout/stderr MUST stream in real time over NATS during execution,
not only on completion (server `internal/api/scripts_run.go`, agent
`pkg/agent/scripts.go` + `executor/` + `shell/`).

10.3. Script runs MUST be prunable per retention policy.

10.4. Remote access MUST support SSH and WinRM transports tunneled over NATS
plus a web terminal (`internal/remote/natsbridge.go`,
`shell_manager.go`, `internal/terminal/`). VNC/RDP proxying is NOT implemented.

10.5. Remote sessions MUST be recorded and replayable
(`internal/session/`, `internal/remote/recorder.go`) with audit trail.

### 11. Engines and Service Boundaries

11.1. Domain logic MUST live in engine/service packages reachable from REST
handlers, NATS consumers, and background loops alike — not inline in handlers:
- Checks: dispatch/ingest pipeline (`internal/checks/`, `internal/events/`)
- Alerts: `internal/alerts/` (state machine + rules + channels)
- Policy: `internal/policy/` (OPA engine, collectors, violations)
- Patches: `internal/patches/` (approval, scheduler, deployer)
- Scripts: `internal/api/scripts*.go` + agent executor
- Remote: `internal/remote/` + `internal/terminal/`

11.2. The formerly specified `ScriptEngine` / `InventoryCollector` /
`Propagation` / `Enforcement` named services were Django-blueprint artifacts;
their responsibilities are distributed as above.

### 12. Background Processing

12.1. Background processing MUST use in-process Go schedulers/loops, not a
separate task queue. Celery is not part of this stack.

12.2. Required background jobs: check scheduling/dispatch, policy evaluation
loop, patch scan scheduling, retention pruning (results, alerts, script runs,
audit per tier quotas — `internal/tenancy/cleanup.go`).

12.3. All background jobs MUST be idempotent (at-least-once delivery means
retries re-execute them).

### 13. Agent Liveness

13.1. Agents MUST heartbeat every 60 seconds over NATS
(`oap.agents.{id}.heartbeat`).

13.2. An agent silent past the heartbeat TTL MUST be treated as offline by the
server (`internal/events/heartbeat handler`).

13.3. Full check-in with an inventory snapshot occurs over REST on registration
and periodically thereafter.

13.4. Offline-agent SLA alerting ("agent silent > N hours" rules) is NOT
implemented — see §14.

### 14. Planned Extensions (not implemented — do not claim)

Scope, readiness (IN vs decision-gated), build order, and open decisions for
the gaps below are the responsibility of
**[`openspec/specs/rmm-operations/spec.md`](../rmm-operations/spec.md)** —
this section links/deduplicates rather than re-specifying them. They originated
from `docs/GAP_ANALYSIS_RMM_PLATFORM.md` and
`docs/QA_REVIEW_OPENSPEC_COVERAGE.md` P3 items 10–11:

14.1. Windows Update management: per-KB tracking, approve/install/fail/reboot
state machine, scan/install dispatch subjects.

14.2. Scheduled automation tasks (`AutomatedTask`): recurring task execution
with weekday/hour/dom/month scheduling.

14.3. Maintenance windows: scheduled alert suppression periods.

14.4. Offline-agent SLA alerting rules.

14.5. Agent self-update channel (version reported today; push/update mechanism
absent).

14.6. Full reboot coordination workflow after patching.

14.7. CVE-to-patch correlation.

14.8. VNC/RDP remote protocols (SSH/WinRM/web-terminal only today).
