# RMM Core

> **Phase:** 1 (Core RMM)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/RMM_CORE.md`
> **App Path:** `backend/apps/rmm/`

---

## Description

RMM Core is the deterministic backbone of OpenAgentPlatform: device
registration, monitoring checks, policy propagation, patch management, alert
lifecycle, script execution, remote access, and the NATS JetStream
orchestration layer that connects them.

In a traditional RMM, automation is rigid — checks fire alerts, alerts notify
humans, humans remediate. RMM Core keeps that reliable spine but adds a second
path: every RMM event can be delegated to an LLM agent over A2A for triage,
risk assessment, and remediation behind human approval gates. The deterministic
layer must be trustworthy on its own, because the agent layer is built on top
of it.

Two transports are used deliberately. REST carries CRUD, queries, and large
idempotent payloads. NATS JetStream carries real-time command dispatch,
streaming output, and event fan-out. Neither transport serves both patterns
well, and this split is validated in production by Tactical RMM and
MeshCentral.

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

1.1. The system MUST use REST for CRUD operations, periodic check-ins, queries,
and reporting; and NATS JetStream for real-time commands, streaming output, and
event distribution.

1.2. Transport selection MUST follow these rules:

| Pattern | Transport | Rationale |
|---------|-----------|-----------|
| Agent full inventory snapshot | REST | Large, infrequent, idempotent |
| Agent heartbeat | NATS | Small, frequent (60s), fire-and-forget |
| Server dispatches script | NATS | Sub-second delivery, streaming response |
| Agent streams stdout | NATS | Chunked, real-time, one-way |
| Technician queries agent list | REST | Paginated, filtered, read-heavy |
| Check failure → A2A task | NATS | Event-driven pub-sub fan-out |

1.3. Agent→Server messages MUST use msgpack (binary efficiency on constrained
endpoints). Server→Agent commands MUST use JSON (debuggable; agent dispatches
on a `Func` string field).

1.4. msgpack fields MUST use numeric tags so schema can evolve without breaking
older deployed agents.

### 2. Data Models

2.1. Ten models MUST be implemented: `Agent`, `Check`, `AgentCheck`,
`CheckResult`, `Policy`, `PolicyScope`, `WinUpdate`, `AutomatedTask`, `Alert`,
`ScriptResult`.

2.2. Models MUST compose shared mixins: `UUIDPrimaryKeyMixin`,
`TimestampedMixin`, `OrgScopedMixin`, `SoftDeleteMixin`.

2.3. `Check` MUST use **flat-table polymorphism**: all check types on one table
with a `check_type` discriminator and nullable type-specific fields. Joined-table
inheritance MUST NOT be used, because a single table outperforms it at 100K+
check results.

2.4. `AgentCheck` MUST enforce `UNIQUE(agent, check)`.

2.5. `CheckResult` MUST be a separate table from `Check`, because results are
high-write time-series data requiring independent retention and pruning.

2.6. `WinUpdate` MUST enforce `UNIQUE(agent, kb)`.

2.7. `PolicyScope` MUST enforce a XOR constraint: exactly one of `client`,
`site`, or `agent` set per row.

2.8. `AutomatedTask` MUST encode schedules in a 21-bit `schedule_bitmask`
(bits 0-6 weekdays, 7-10 hours, 11-17 days of month, 18-20 months). Cron string
parsing MUST NOT be used.

2.9. All org-scoped models MUST index on `org` as the leading column so
multi-tenant queries remain selective.

### 3. Enums

3.1. Twelve enums MUST be defined: `AgentStatus`, `AgentPlatform`, `CheckType`,
`CheckStatus`, `PolicyEnforcementMode`, `WinUpdateState`,
`AutomatedTaskActionType`, `AlertSeverity`, `AlertState`, `ScriptRuntime`,
`RemoteSessionProtocol`, `RemoteSessionState`.

3.2. `CheckType` MUST support 10 types: `ping`, `cpu`, `memory`, `disk`,
`service`, `script`, `event_log`, `process`, `wmi`, `custom`.

3.3. `AlertSeverity` MUST have 5 levels: `critical`, `high`, `medium`, `low`,
`info`.

### 4. State Machines

4.1. Five state machines MUST be implemented with explicit transition tables:
Alert (6 states), WinUpdate (8 states), Agent (5 states), ScriptResult
(6 states), RemoteSession (7 states).

4.2. **Alert** transitions MUST be: `acknowledge` (new/snoozed → acknowledged),
`resolve` (new/acknowledged/in_progress → resolved), `snooze`
(new/acknowledged/in_progress → snoozed), `close` (any → closed), `reopen`
(resolved/closed → new).

4.3. **WinUpdate** transitions MUST be: `auto_approve` and `approve`
(pending_approval → approved), `reject` (pending_approval → rejected),
`start_install` (approved → installing), `complete_install` (installing →
installed), `fail_install` (installing → failed), `mark_reboot_required`
(installed → reboot_required), `retry` (failed → installing).

4.4. **Agent** transitions MUST be: `check_in`
(pending/offline/degraded → online), `mark_offline` (online → offline, on 90s
heartbeat TTL breach), `mark_degraded` (online → degraded), `recover`
(degraded → online), `uninstall` (any → uninstalled).

4.5. **ScriptResult** transitions MUST be: `start` (pending → running),
`complete` (running → success), `fail` (running → error), `timeout` (running →
timeout), `cancel` (pending/running → cancelled).

4.6. Invalid transitions MUST be rejected, and tests MUST assert rejection
explicitly — not merely that valid transitions succeed.

### 5. NATS Subject Taxonomy

5.1. Agent→Server subjects (msgpack) MUST include: `rmm.agent.heartbeat`,
`rmm.agent.checkin`, `rmm.check.result.{agent_id}`,
`rmm.script.result.{agent_id}`, `rmm.script.chunk.{agent_id}`,
`rmm.winupdate.scan.{agent_id}`, `rmm.winupdate.install.{agent_id}`,
`rmm.agent.inventory.{agent_id}`, `rmm.remote.session.event.{agent_id}`.

5.2. Server→Agent subjects (JSON, per-agent inbox) MUST include:
`rmm.cmd.{agent_id}.script.run`, `.script.cancel`, `.check.run`,
`.winupdate.install`, `.winupdate.scan`, `.sync`, `.agent.update`,
`.remote.open`, `.remote.close`, `.policy.push`, `.inventory.refresh`.

5.3. Broadcast subjects MUST include `rmm.broadcast.all` and
`rmm.broadcast.org.{org_id}`.

5.4. Commands MUST be addressed to a per-agent inbox subject so a command
cannot be delivered to the wrong endpoint.

### 6. Checks

6.1. Full CRUD MUST be available for check definitions over REST, covering all
10 check types.

6.2. A built-in check library MUST ship with ping, CPU, memory, disk, and
service checks working end-to-end.

6.3. `interval_seconds` MUST be at least 30; `timeout_seconds` MUST be at most
3600. Both MUST be enforced by database constraints, not application code alone.

6.4. The CheckEngine MUST schedule due checks, dispatch them over NATS, ingest
results, and evaluate thresholds.

6.5. A check MUST alert only after `fail_threshold` consecutive failures, so a
single transient failure does not page a human.

6.6. Check results MUST be pruned on a schedule per `check_history_prune_days`
(default 30).

### 7. Alerts

7.1. The AlertEngine MUST generate deduplication keys and suppress duplicate
alerts for the same underlying condition.

7.2. Notification channels MUST include email, Slack, and webhook.

7.3. Per-severity routing and silence periods MUST be configurable.

7.4. Resolved alerts MUST be pruned per `resolved_alerts_prune_days`.

7.5. Alert state changes MUST be driven exclusively through the Alert state
machine.

### 8. Policies

8.1. Policies MUST resolve through a **Client > Site > Agent** hierarchy,
evaluated bottom-up: Agent → Site → Client → Organization.

8.2. `enforcement_mode` MUST support `inherit`, `enforce`, and `exclude`. The
`enforce` mode MUST discard agent-level overrides.

8.3. `block_policy_inheritance` MUST stop propagation at that level.

8.4. Conflicts MUST be resolved by `priority`, higher winning.

8.5. OPA MUST be integrated for policy evaluation; agent actions MUST be
evaluated against policy and violations logged.

8.6. Policy violations MUST create alerts and dispatch notifications.

8.7. Policy changes MUST propagate to affected agents by delta computation and
NATS publish, not by full-state resend to every agent.

### 9. Patches

9.1. Patch scans MUST be triggerable per agent, with results stored and exposed
as a per-agent patch list.

9.2. Patches MUST pass through an approval workflow; state transitions MUST be
logged with the approving user.

9.3. Policy MUST be able to auto-approve patches by severity.

9.4. Batch deployment MUST track progress per agent and retry failures.

9.5. Reboot coordination MUST be supported, including `needs_reboot` reporting
and reboot prompts.

9.6. CVE IDs MUST be correlated to patch records.

### 10. Scripts and Remote Access

10.1. A script library MUST support CRUD with metadata across 5 runtime types
(`powershell`, `cmd`, `python`, `shell`, `nushell`).

10.2. Script stdout/stderr MUST stream in real time over
`rmm.script.chunk.{agent_id}` rather than only being delivered on completion.

10.3. Script results MUST be pruned per `agent_history_prune_days` (default 60).

10.4. Remote access MUST support SSH, WinRM, VNC, RDP, and web terminal
protocols.

10.5. Remote sessions MUST be audited: duration logged, recording available for
replay, complete audit trail.

### 11. Services

11.1. Ten services MUST be implemented: `CheckEngine`, `PolicyEngine`,
`AlertEngine`, `PatchEngine`, `ScriptEngine`, `InventoryCollector`,
`CheckinHandler`, `Propagation`, `Enforcement`, `RemoteAccess`.

11.2. Business logic MUST live in the service layer, not in API handlers or
model methods, so it is reachable from REST, NATS consumers, and background
tasks alike.

### 12. Background Tasks

12.1. Seven Celery task packages MUST be implemented: `check_tasks`,
`patch_tasks`, `alert_tasks`, `policy_tasks`, `inventory_tasks`,
`script_tasks`, and beat registration.

12.2. Beat schedules MUST be: checks every 30s, alerts every 60s, scripts every
30s, inventory hourly, patches daily at 02:00, policies on change.

12.3. Tasks MUST be idempotent, because at-least-once delivery means retries
will re-execute them.

### 13. Agent Liveness

13.1. Agents MUST heartbeat every 60 seconds over NATS.

13.2. An agent MUST be marked `offline` after a 90-second heartbeat TTL breach.

13.3. Full check-in with an inventory snapshot MUST occur every 5-15 minutes
over REST.

13.4. Consistent check failures MUST move an agent to `degraded`; a healthy
check streak MUST recover it to `online`.
