# Sprint: RMM Operations — WinUpdate per-KB Tracking & State Machine

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Add a per-KB tracking table and an explicit WinUpdate state
machine over the already-implemented scan/install/dispatch path. Prerequisite
for RMM-04 (reboot) and RMM-05 (CVE).
**Priority:** P0 (Critical) — prerequisite for RMM-04/05
**Estimated Effort:** 8-12 hours
**Status:** COMPLETE
**Dependencies:** RMM-00

---

## Overview

Agent-side scan and install are done: `pkg/agent/patcher/windows.go`
(`WindowsScanner`, WMI QFE / Get-HotFix / winget), `installer.go`
(`WindowsInstaller`, wusa/msiexec/winget, `RebootRequired` on exit code 3010),
and NATS subjects in `handler.go`
(`oap.agents.<id>.patch_scan` / `.patch_scan.results` / `.patch_install` /
`.patch_install.results`). Server-side approval + batch deploy exist
(`internal/patches/`, `internal/api/patches.go`). What was missing was the
server-side per-KB tracking table and the WinUpdate state machine — the
"approve → installing → installed / reboot_required / failed" per-KB lifecycle
`rmm-core` §4.4 calls out. This sprint adds it.

## Problem Statement

Without per-KB state, the server cannot report which KBs are installed, pending
approval, or reboot-required on which agents, nor drive CVE correlation
(RMM-05) or reboot orchestration (RMM-04) at KB granularity.

**Why:** WinUpdate state is the shared prerequisite for RMM-04 and RMM-05.
**Where:** new tracking table + state machine alongside
`internal/patches/approval.go`; policy seam `win_update_policy`
(`py/alembic/versions/0005_policies.py:40`) — auto-approve only (see
Decisions).

## Scope Boundary

```
IN SCOPE (CREATED/MODIFIED):
  - py/alembic/versions/0013_rmm03_winupdate_kb_state.py (new table)
  - internal/patches/winupdate_states.go (8-state machine + NextState)
  - internal/patches/store_kb.go (ingest + query methods on pgPatchStore)
  - internal/patches/kb_ingest.go (NATS consumer on patch_kb.* subjects)
  - internal/api/patches.go (handleGetKBBatch)
  - internal/api/routes_sub.go (GET /patches/kb)
  - pkg/models/models_extra.go (WinUpdateKBState struct)
  - pkg/agent/patcher/handler.go (sibling subjects + ReportRebootDone)

OUT OF SCOPE (NOT TOUCHED):
  - pkg/agent/patcher/{windows,installer,patcher}.go
  - cmd/agent/main.go (call-site deferred to RMM-04)
  - Reboot handling (RMM-04) and CVE matching (RMM-05)
  - Legacy rmm.winupdate.* subjects — forbidden (spec §1.3, §10.1)
```

## Decisions (locked)

1. **Subjects — ADD SIBLINGS under `oap.agents.<id>.*`.** The per-KB tracking
   is fed by NEW subjects that do not extend or replace the existing
   `patch_scan` / `patch_install` / `*.results` subjects. The existing
   handlers remain authoritative and unchanged for scan/install dispatch.
   New subjects:
   - `oap.agents.<id>.patch_kb.scan` (PatchKBScanEnvelope)
   - `oap.agents.<id>.patch_kb.install` (PatchKBInstallEnvelope)
   - `oap.agents.<id>.patch_kb.reboot_done` (PatchKBRebootEnvelope)

   The agent's `handleScan` / `handleInstall` publish their existing
   envelopes onto the new sibling subjects after their current publish —
   existing behavior is unchanged.

2. **Reboot trigger — agent self-reports.** The agent publishes a
   `PatchKBRebootEnvelope` (list of KBs) on the reboot_done subject,
   transitioning `reboot_required` → `installed`. The server-side consumer
   (`KBConsumer.handleRebootDone`) and the agent-side publisher method
   (`Handler.ReportRebootDone`) are in scope. The `cmd/agent/main.go`
   startup call-site that wires this into the agent's reboot lifecycle is
   DEFERRED to RMM-04 (must NOT be edited here).

3. **Licensing — community, no gate.** No `RequireFeature` /
   `FeatureWinUpdateState` / `DefaultGateConfig` changes. The new endpoint
   behaves like the existing `/patches` read routes: any authenticated org
   member can read; scoped to their org.

4. **Auto-approve — mirror `ApprovalWorkflow.ApplyPolicy`.** Critical
   severity auto-approves on first scan; all other severities queue to
   `pending_approval`. No new policy-evaluation engine is built and no JSON
   schema is invented for the `win_update_policy` JSONB column (it remains
   an empty `{}` default with no upstream schema). This deferral is noted
   here; a richer policy engine is future work.

## Canonical 8-state vocabulary

Reused verbatim from `ck_patch_job_targets_state` on `patch_job_targets`
(`py/alembic/versions/0006_patches.py`):
`scanned`, `pending_approval`, `approved`, `rejected`, `installing`,
`installed`, `failed`, `reboot_required`.

## Transition table (single source of truth)

```
scanned          -> approve: approved,        queue: pending_approval,   reject: rejected
pending_approval -> approve: approved,        reject: rejected
approved         -> install: installing,      reject: rejected
rejected         -> rescan: scanned
installing       -> complete: installed,      fail: failed,              reboot: reboot_required
failed           -> install: installing
reboot_required  -> reboot_done: installed
installed        -> (terminal, no outgoing)
```

## Implementation semantics

- `IngestKBScan`: upsert (`INSERT ... ON CONFLICT (agent_id, kb) DO NOTHING`,
  first-seen state `scanned`). Auto-approve: severity `critical` → event
  `approve` → `approved`; otherwise event `queue` → `pending_approval`.
  Double-ingest with same inputs is a no-op (idempotent).
- `IngestKBInstall`: tolerant at-least-once ingest. If current state is
  `approved`/`scanned`/`pending_approval`, first transition to `installing`;
  then apply outcome: success && rebootRequired → `reboot`; success →
  `complete`; !success → `fail` (stores errMsg in `result`).
- `IngestKBRebootDone`: transition each listed KB `reboot_done` →
  `installed`; already-`installed` is a no-op.
- Every store query includes `org_id` in the WHERE clause; the API handler
  scopes via `claims.OrgID`.

## Files

Created:
- `py/alembic/versions/0013_rmm03_winupdate_kb_state.py`
- `internal/patches/winupdate_states.go`
- `internal/patches/store_kb.go`
- `internal/patches/kb_ingest.go`
- `internal/patches/winupdate_states_test.go`
- `internal/patches/store_kb_test.go`
- `internal/patches/kb_ingest_test.go`
- `internal/api/kb_patch_test.go`
- `pkg/models/kbstate_test.go`

Modified (additive):
- `pkg/models/models_extra.go` (WinUpdateKBState struct)
- `internal/patches/store_types.go` (Store interface + patchPoolConn)
- `internal/api/patches.go` (handleGetKBBatch + checkAgentOrg)
- `internal/api/routes_sub.go` (GET /patches/kb)
- `pkg/agent/patcher/handler.go` (sibling subjects + ReportRebootDone)
- `internal/auth/middleware.go` (WithUser test helper)

## Completion Record

Verified on the main working tree (no worktree):

```
$ gofmt -l <new+edited files>
  (empty — all formatted)

$ go build ./...
  build OK

$ go vet ./internal/patches/... ./internal/api/... ./pkg/models/...
  vet OK

$ go test ./internal/patches/... ./internal/api/... ./pkg/models/...
  ok  internal/patches   (25 passing incl. new RMM-03 tests)
  ok  internal/api       (incl. 4 new handleGetKBBatch tests)
  ok  pkg/models        (2 new WinUpdateKBState JSON tests)

$ git grep -n "rmm.winupdate" -- '*.go' '*.py'
  NO rmm.winupdate.* in Go/Python source
```

Test coverage confirmed RED→GREEN for every new function: each test was
written against not-yet-existing symbols (state constants, store methods,
handler envelopes, route) and failed to compile / failed on expectation
mismatch before the production code was added; re-running after the
implementation made them pass. Illegal-transition assertions return
`errors.Is(err, ErrInvalidTransition)` via the wrapped sentinel.

Consumer wiring line (cmd/server/server_init.go, next to patchStore):
```go
kbConsumer := patches.NewKBConsumer(natsClient.Conn(), patchStore, agentStore, log)
if _, err := kbConsumer.Subscribe(); err != nil {
    log.Warn("winupdate kb consumer not started", "error", err)
} else {
    log.Info("winupdate kb consumer started")
}
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Per-KB state queryable per agent | API test | State reflects scan + install history |
| 2 | WinUpdate machine rejects illegal transitions | state-machine test | Asserted rejection |
| 3 | Ingestion idempotent | double-ingest test | No duplicate/incorrect state |
| 4 | Subjects stay under oap.agents.<id>. | git grep | No rmm.winupdate.* introduced |
| 5 | Additive migration | up/down | Existing patch tables untouched |

## Remediation (post-implementation audit, 2026-08-24)

Independent verification surfaced three defects; all fixed RED-first in the
main tree.

1. **Unfiltered GET /kb returned 500 in production.** `GetKBStatesByAgent`
   rejected an empty `agent_id`, but the API documents `agent_id` as
   optional. Fixed the store: empty `agent_id` now issues an org-wide query
   (`WHERE org_id = $1`, uniform `ORDER BY agent_id ASC, kb ASC`, LIMIT 200).
   New pgxmock test `TestGetKBStatesByAgent_OrgWide` asserts the org-wide SQL
   and row mapping.

2. **IngestKBInstall not idempotent on NATS redelivery.** A second identical
   delivery to a row already at `installed`/`reboot_required`/`failed` hit
   `WinUpdateNextState(cur, "install")` and returned the wrapped
   `ErrInvalidTransition`. Fixed: if `cur.State == desired` the ingest
   returns `(cur.State, nil)` with no writes. Also, `scanned`/`pending_approval`
   rows now walk the legal path (approve → install) so a first-ever delivery
   still reaches the outcome. New tests: `TestIngestKBInstall_IdempotentRedelivery`
   and `TestIngestKBInstall_FirstDeliveryFromScanned`.

3. **Server-side consumer never constructed.** `git grep NewKBConsumer` found
   no production call site, so nothing subscribed to
   `oap.agents.*.patch_kb.*`. Wired in `cmd/server/server_init.go` next to
   `patchStore := patches.NewPGStore(pool)`:
   `patches.NewKBConsumer(natsClient.Conn(), patchStore, agentStore, log)`
   with `Subscribe()`, following the surrounding init-file error-handling
   convention (warn-and-continue on subscribe failure). The agent-store
   adapter now selects `org_id` so the consumer can scope rows.
   `cmd/agent/main.go` remains deferred to RMM-04.

## Reference

- `openspec/specs/rmm-operations/spec.md` §2 (WinUpdate), §10.1, §11
- `openspec/specs/rmm-core/spec.md` §4.1 (transition-table convention), §4.4, §12.3
- `pkg/agent/patcher/{windows,installer,handler}.go` (existing agent side)
- `internal/patches/approval.go` (pattern to follow)

---

**Created:** 2026-08-24
**Completed:** 2026-08-24
**Authored by:** Agent-RMM-03
**Version:** 2.0
