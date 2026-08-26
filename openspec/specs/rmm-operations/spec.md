# RMM Operations

> **Phase:** 1 (Core RMM) — extends `rmm-core` §14 planned extensions
> **STATUS: PARTIAL** — the eight domains below are tracked parity gaps, scoped
> and source-anchored here. RMM-01..RMM-05 are COMPLETE (shipped); RMM-06 is DESIGN
> APPROVED (cron grammar resolved 2026-08-25, build pending); RMM-07/08 were merged
> into RMM-09 and shipped (`5ea6076`). The spec body is reconciled against shipped
> code; remaining DEFERRED markers in §8/§9 are historical records of the original
> decision-gate framing, now resolved.
> **Source:** `docs/GAP_ANALYSIS_RMM_PLATFORM.md` (G-RMM-002/003/004),
> `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` P3 items 10–11,
> `openspec/specs/rmm-core/spec.md` §14
> **App Path:** `internal/patches/`, `internal/alerts/`, `internal/events/`,
> `internal/remote/`, `internal/api/patches.go`, `pkg/agent/patcher/`,
> `pkg/agent/shell/`, `pkg/models/`, `cmd/agent/main.go`,
> `cmd/server/server_adapters.go`, `py/alembic/versions/0005_policies.py`,
> `py/alembic/versions/0006_patches.py`
> **Build sprints:** `docs/sprints/RMM-00_RMM_OPERATIONS_FOUNDATION.md` ..
> `docs/sprints/RMM-08_VNC_RDP_DECISION.md`

---

## Description

RMM Core (see `openspec/specs/rmm-core/spec.md`) provides checks, alerts,
policy, patch deployment, script execution, and remote shell. RMM Operations is
the next tier of parity with a mature RMM (Ninja-RMM parity per
`docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §5): the eight operational capabilities
that RMM Core deliberately leaves as planned extensions in `rmm-core`
§14.1–§14.8:

1. Windows Update per-KB management (WinUpdate)
2. Scheduled automation (AutomatedTask)
3. Maintenance windows (fleet alert suppression)
4. Offline-agent SLA alerting
5. Agent self-update
6. Reboot coordination after patching
7. CVE-to-patch correlation
8. VNC/RDP remote protocols

This spec does **not** invent mechanisms. Each domain records what exists in the
current Go/Python codebase, what is genuinely missing, and — where a design
decision is unresolved — states the ambiguity explicitly and refuses to
fabricate a resolution.

Anti-scope: cloud control, hypervisor/EVE, UPS/power, and active cyber-defense
are separate P-gaps tracked elsewhere
(`docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.4/2.5/2.6/2.8) and are OUT of RMM
Operations. Do not fold them in.

## User Story

**As** an MSP technician,
**I want** per-KB Windows patch state, predictable patch reboots inside
maintenance windows, CVE-linked patch triage, fleet-wide alert suppression
during planned work, and an SLA alarm when an agent silently drops off,
**so that** I can run patch and maintenance operations at parity with a mature
RMM without adding unbounded scope or inventing behavior the codebase does not
support.

---

## Requirements

### 1. Scope, Readiness, and Ordering

1.1. The eight domains (§7 below) rely on WinUpdate state (RMM-03) as the
prerequisite for both reboot coordination (RMM-04) and CVE correlation
(RMM-05). Build order is therefore fixed: RMM-01 → RMM-02 → RMM-03 → RMM-04 →
RMM-05 (build-ready), then RMM-06 → RMM-07 → RMM-08 (decision-gated).

1.2. A domain is IN (build-ready) only when its requirement text here is
source-anchored to existing code. A domain is DEFERRED when it needs a design
decision that no current code resolves; the linked sprint is then a decision
gate, not a build ticket.

1.3. All new work MUST extend the `oap.*` NATS taxonomy
(`internal/events/subjects.go`). The legacy `rmm.*` subjects from earlier
blueprints MUST NOT be resurrected (see `rmm-core` §5).

### 2. Windows Update per-KB State — IN (RMM-03)

2.1. Already implemented, agent-side: scan
(`pkg/agent/patcher/windows.go` `WindowsScanner`; WMI QFE / Get-HotFix /
winget), install (`pkg/agent/patcher/installer.go` `WindowsInstaller`
wusa/msiexec/winget, `RebootRequired` when exit code 3010), and the NATS
dispatch/result subjects (`pkg/agent/patcher/handler.go`:
`oap.agents.<id>.patch_scan` / `.patch_scan.results` / `.patch_install` /
`.patch_install.results`). Server-side approval and batch deploy exist
(`internal/patches/`, `internal/api/patches.go`).

2.2. The gap is partial, not total. The migration already defines per-agent,
per-catalog tracking with a lifecycle:
`py/alembic/versions/0006_patches.py` defines `patch_job_targets` keyed by
agent and `patch_catalog_id`, including an eight-state per-patch lifecycle
constraint. `WinUpdate` is listed as not implemented in `rmm-core` §2.5/§4.4,
and the policy seam exists (`win_update_policy` JSONB on `Policy`,
`py/alembic/versions/0005_policies.py:40`). The live Go model/store does not
expose or consistently use the migration's catalog and state columns.

2.3. IN scope (RMM-03): reconcile the migration's tracking/lifecycle columns
with the live Go model and store, then add whatever per-KB tracking or
transition-table state machine is still missing — following the `rmm-core` §4.1
convention and driven by the existing scan/install subjects and the
`win_update_policy` seam. Do not create a second tracking table where the
migration already defines one.

2.4. OUT of scope: inventing new NATS subjects or resurrecting `rmm.winupdate.*`
topics. Reuse the existing `oap.agents.<id>.patch_*` subjects or add a sibling
under `oap.agents.<id>.` per open decision 10.1.

### 3. Scheduled Automation (AutomatedTask) — DESIGN APPROVED (RMM-06)

3.1. An `automated_tasks` JSONB column already exists on `Policy`
(`py/alembic/versions/0005_policies.py:38`). No scheduler or entity binds it.

3.2. **Grammar decision resolved (open decision 10.2 → Resolved):** cron-style
recurrence, reusing the `internal/reports/` scheduler convention (`cron_expr`
TEXT, 30s tick loop, `computeNextRun`, `@hourly/@daily/@weekly/@monthly`
aliases + `M H DoM Mon DoW` 5-field parser). The 21-bit `schedule_bitmask` is
rejected — zero implementations exist in-tree, its encoding is undocumented, and
cron is strictly more expressive and auditable. Decision record:
`docs/sprints/RMM-06_SCHEDULED_AUTOMATION_DECISION.md`.

3.3. Each `automated_tasks` element is a cron-scheduled task object with a
discriminated `action` union (`patch_deploy | reboot | script_run |
check_enable`), an IANA `timezone` (default UTC), and a persisted `next_run_at`.
`cron_expr` is validated against the existing report-parser and rejected on
parse failure (fail-closed).

3.4. IN scope (contingent build): a recurring dispatcher following the
rmm-core §12 in-process loop convention and the `internal/reports/` tick pattern;
idempotent (at-least-once) execution by `id` + `last_run_at` comparison. OUT of
scope: arbitrary LLM agent workflow automation (ties to A2A).

### 4. Maintenance Windows (Fleet Alert Suppression) — IN (RMM-02)

4.1. Patch-deployment windows (the "patch maintenance window" concept) already
exist: `MaintenanceWindow` and `BlackoutWindow` in
`internal/patches/scheduler_types.go`, persisted via
`internal/patches/store_crud.go`, enforced by `internal/patches/scheduler.go`
and the deploy API (`internal/api/patches.go`). That is a different concept and
MUST NOT be conflated with alert suppression.

4.2. Only per-user quiet hours exist for alert suppression today:
`QuietHours` / `UserAlertPreferences` / `IsInQuietHours` in
`internal/alerts/preferences.go`. There is no fleet-level, client/site-scoped
alert-suppression window.

4.3. IN scope (RMM-02): a fleet-level alert-suppression window entity
(org/client/site-scoped, recurring or one-shot) that suppresses alert
*notifications* during the window, reused by `internal/alerts/` — distinct from
patch windows (§4.1) and from per-user quiet hours (§4.2). Two design choices
must be approved before model/migration work: whether client scope is persisted
directly or derived through site membership, and the one-shot/recurrence
representation.

4.4. OUT of scope: merging with the patch `MaintenanceWindow`; changing
quiet-hours behavior.

### 5. Offline-Agent SLA Alerting — IN (RMM-01)

5.1. The binary offline flip already exists:
`internal/events/heartbeat.go` `sweepStale` runs every 30s and flips any agent
whose `last_seen` exceeds the 120s threshold (`HeartbeatStaleThreshold`,
`internal/events/subjects.go`) to `offline`, emitting `oap.events.agent`
lifecycle events. The store method is
`MarkStaleAgentsOffline(ctx, threshold any)`, implemented by `eventStoreAdapter`
(`cmd/server/server_adapters.go:142`).

5.2. Capability already present: an agent is binary offline after ~2 minutes of
silence. That is liveness, not an SLA alarm. The gap: no *alert-rule condition*
"agent silent > N hours".

5.3. IN scope (RMM-01): add a configurable condition so an `AlertRule`
(`pkg/models/models_alerts.go`) can fire when an agent has been silent longer
than a configured duration (e.g. 24h), evaluated against `last_seen`. Reuse the
existing alert engine (`internal/alerts/`) and rule store
(`internal/alerts/store_alerts_rules.go`); do not change liveness semantics.
Note: the alert engine reacts to incoming alert events and has no agent-lookup
seam, so a new rule field alone cannot detect "silent for N hours" — RMM-01
must first specify a source-backed trigger (periodic stale-agent evaluator with
an explicit query seam, or conversion of lifecycle/staleness events into alert
events with delayed evaluation) plus idempotency/deduplication and recovery
semantics.

5.4. OUT of scope: changing the 120s offline threshold or heartbeat TTL, or
introducing graded SLA (open decision 10.4).

### 6. Reboot Coordination — IN (RMM-04)

6.1. Building blocks already exist but are unwired: `RebootQueue` and
`PatchDeployer.CoordinateReboots` in `internal/patches/deployer_strategies.go`
(types at §299, coordinator at §350, staggered via `RebootStagger`) have ZERO
callers. `NeedsReboot` is already reported on patch targets (`rmm-core` §9.5).

6.2. IN scope (RMM-04): wire the existing `CoordinateReboots` sequencing and
health-check scaffold into the deploy-completion path. `CoordinateReboots` only
waits, runs pre/post health checks, and records results — it does not invoke any
reboot transport and contains no backoff. An approved ownership decision must
define where the actual reboot action occurs between the pre- and post-checks
before an `oap.agents.<id>.reboot` command subject and agent handler can be
added.

6.3. OUT of scope / open decision (10.6): whether reboots are server-coordinated
(push) vs agent self-reboot — decide before finalizing any reboot transport or
agent handler.

### 7. CVE-to-Patch Correlation — IN (RMM-05)

7.1. The data-layer seam already exists: `cve_ids` JSONB on the patch catalog
(`py/alembic/versions/0006_patches.py:37`); web types ready
(`web/src/lib/usePatches_types.ts`: `cve_ids`, `cvss_score`). There is no
server-side ingestion, matching, or lookup.

7.2. IN scope (RMM-05): server-side CVE intake (source + cadence per open
decision 10.7), matching to patch KB records (populating `cve_ids`), and a
new CVE↔KB look-up API contract. The web type file
(`web/src/lib/usePatches_types.ts`) already exposes `cve_ids` and `cvss_score`
fields, but no endpoint contract exists for it — RMM-05 must design and approve
that contract (OpenAPI/handler shape) before building it.

7.3. OUT of scope / open decision (10.7): the CVE data source (NVD/OSV/MSRC)
and cadence are undecided — the sprint MUST resolve this before building the
ingester.

### 8. Agent Self-Update — COMPLETE (RMM-07 → merged into RMM-09, shipped `5ea6076`)

8.1. ~~The agent reports its version today ... No push/update mechanism exists.~~
**Resolved.** RMM-09 ships Ed25519 self-update: `pkg/agent/mesh/updater.go`
verifies a base64 Ed25519 signature over the binary's SHA-256 before applying.
The trust model (open decision 10.5) is **Resolved** — Ed25519 + SHA-256 gate,
security review pending.

8.2. The original RMM-07 framing (§8.2 in earlier versions) treated this as a
decision gate; it was merged into RMM-09 and is now shipped.

### 9. VNC/RDP Remote Protocols — COMPLETE (RMM-08 → merged into RMM-09, shipped `5ea6076`)

9.1. Only SSH over a text-PTY NATS bridge was operational originally
(`pkg/agent/shell/`, `internal/remote/natsbridge.go`, `shell_manager.go`). The
agent also has a WinRM protocol branch, but it is an explicit PowerShell
`Read-Host` stub pending a real library and credentials — not operational WinRM.

9.2. ~~DEFERRED: RMM-08 is the highest-risk decision gate — a new binary proxy
data plane ...~~ **Resolved.** RMM-08 was merged into RMM-09, which shipped VNC/RDP
over **SSH port-forward** (design-only Step 6) — reusing the tunnel fabric instead
of building a new binary proxy. Open decision 10.8 is **Resolved**.

### 10. Open Decisions (never invent)

These are the eight decision points this spec keeps explicit. Each sprint doc
MUST restate the relevant one and gate on a recorded approval before building:

| # | Decision | Domains | Status |
|---|----------|---------|--------|
| 10.1 | Reuse `oap.agents.<id>.patch_*` vs add sibling subjects | WinUpdate, Reboot | **Resolved-by-implementation** — add sibling subjects under `oap.agents.<id>.` (`internal/events/checkdispatcher.go:136`, `pkg/agent/mesh/updater.go`) |
| 10.2 | AutomatedTask scheduling grammar: cron vs 21-bit bitmask | AutomatedTask | **Resolved — cron approved** (reuse `internal/reports/` scheduler; bitmask rejected) |
| 10.3 | Patch-maintenance ≠ alert-suppression (keep separate) | Maintenance | Resolved by this spec |
| 10.4 | Offline SLA: binary vs graded + scoping | Offline SLA | **Resolved-by-implementation** — RMM-01 shipped binary-liveness + additive SLA rule (`6ff3a17`) |
| 10.5 | Self-update trust/signing model | Self-update | **Resolved** (Ed25519 + SHA-256 gate, `pkg/agent/mesh/updater.go`) — security review pending |
| 10.6 | Reboot ownership: server-coordinated vs agent /r | Reboot | **Resolved-by-implementation** — server-coordinated `PatchDeployer.CoordinateReboots` (RMM-04, `internal/api/patches.go:398`) |
| 10.7 | CVE data source + cadence (NVD/OSV/MSRC) | CVE | **Resolved-by-implementation** — NVD source (RMM-05, `internal/patches/nvd_ingest.go`) |
| 10.8 | VNC/RDP binary proxy data plane | VNC/RDP | **Resolved** — reuse SSH tunnel port-forward (RMM-08→09), not a new proxy |

### 11. Data Model and Migration Notes

11.1. Only additive, backward-compatible migrations are anticipated, extending
the patcher/policy relations in `py/alembic/versions/0005_policies.py` and
`0006_patches.py`. New tracking entities (WinUpdate per-KB, fleet maintenance
windows, offline-SLA rule condition) are added, not reshaped; no existing table
referenced by live rows is altered in place.

11.2. `pkg/models/` additions MUST follow the existing flat Go struct + migration
split (`models.go`, `models_extra.go`).

---

## Cross-References

- `openspec/specs/rmm-core/spec.md` §14 (this spec supersedes the §14 tracking
  body) and §2.5 / §4.4 / §7.6 / §9.5 / §9.6 / §10.4 / §13.4 (per-domain gaps)
- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.1 (G-RMM-002/003/004)
- `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` P3 items 10–11 and §5 (Ninja-RMM parity)
- `docs/sprints/RMM-00_RMM_OPERATIONS_FOUNDATION.md` through
  `docs/sprints/RMM-08_VNC_RDP_DECISION.md`
