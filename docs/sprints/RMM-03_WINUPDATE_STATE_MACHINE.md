# Sprint: RMM Operations — WinUpdate per-KB Tracking & State Machine

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Add a per-KB tracking table and an explicit WinUpdate state
machine over the already-implemented scan/install/dispatch path. Prerequisite
for RMM-04 (reboot) and RMM-05 (CVE).
**Priority:** P0 (Critical) — prerequisite for RMM-04/05
**Estimated Effort:** 8-12 hours
**Status:** PENDING
**Dependencies:** RMM-00

---

## Overview

Agent-side scan and install are done: `pkg/agent/patcher/windows.go`
(`WindowsScanner`, WMI QFE / Get-HotFix / winget), `installer.go`
(`WindowsInstaller`, wusa/msiexec/winget, `RebootRequired` on exit code 3010),
and NATS subjects in `handler.go`
(`oap.agents.<id>.patch_scan` / `.patch_scan.results` / `.patch_install` /
`.patch_install.results`). Server-side approval + batch deploy exist
(`internal/patches/`, `internal/api/patches.go`). What is missing is the
server-side per-KB tracking table and the WinUpdate state machine — the
"approve → installing → installed / reboot_required / failed" per-KB lifecycle
`rmm-core` §4.4 calls out as not implemented.

## Problem Statement

Without per-KB state, the server cannot report which KBs are installed, pending
approval, or reboot-required on which agents, nor drive CVE correlation
(RMM-05) or reboot orchestration (RMM-04) at KB granularity.

**Why:** WinUpdate state is the shared prerequisite for RMM-04 and RMM-05.
**Where:** new tracking package/table; state machine alongside
`internal/patches/approval.go`; policy seam `win_update_policy`
(`py/alembic/versions/0005_policies.py:40`).

## Scope Boundary

```
IN SCOPE (may modify):
  - internal/patches/  (new per-KB tracking store + WinUpdate state machine
                        following the rmm-core §4.1 transition-table pattern,
                        reusing approval.go conventions)
  - pkg/models/        (WinUpdate tracking struct)
  - py/alembic/versions/  (additive per-KB tracking table; reuse existing
                          patch_scan/patch_install subject payloads)
  - internal/api/patches.go  (per-KB query endpoint for the patch list/state)

OUT OF SCOPE (DO NOT TOUCH):
  - pkg/agent/patcher/{windows,installer}.go  (scan/install logic — already done)
  - Reboot handling (RMM-04) and CVE matching (RMM-05) — later sprints
  - Legacy rmm.winupdate.* subjects — forbidden (spec §1.3, §10.1)
  - cmd/agent/main.go subject wiring beyond what RMM-04 adds later
```

## Open Decisions (restated from spec §10.1)

Subject reuse: extend existing `oap.agents.<id>.patch_*` subjects vs add
siblings. The spec default is extending `oap.agents.<id>.` rather than adding
new top-level subjects. The sprint MUST record which choice is made and keep
the existing `patch_scan`/`patch_install` handlers authoritative for scan and
install dispatch.

## Production-Before-Test Sequence

```
STEP 1 (RECONCILE): Compare the migration constraint
    (`py/alembic/versions/0006_patches.py` `patch_job_targets`, keyed by agent +
    patch_catalog_id with its eight-state per-patch lifecycle), the live Go
    deployment statuses, and the desired contract. Approve ONE canonical
    transition table — do not invent a second lifecycle. The migration already
    includes states such as `pending_approval` and `rejected` that any new table
    must account for. TOOL: Write

STEP 2 (MODEL): Adapt the existing table/model/store to expose the approved
    state columns; only add what the migration does not already define.
    TOOL: Edit

STEP 3 (MIGRATION): Additive only; do not replace the existing
    `patch_job_targets` table unless an approved migration explicitly requires it.
    TOOL: Write alembic version

STEP 4 (STORE): Persist scan results from existing patch_scan.results into the
    per-KB table (link by KB id), and apply install transitions from the
    existing patch_install path. TOOL: Write

STEP 5 (API): Per-KB query endpoint returning state per agent (the patch list
    the UI already shows). TOOL: Edit

STEP 6 (BUILD): go build ./... && go vet ./... before tests. TOOL: Bash

STEP 7 (TESTS): After production —
    - legal transitions advance; illegal transitions rejected (assert rejects)
    - scan-result ingestion writes rows idempotently (at-least-once safe)
    - policy win_update_policy auto-approve by severity feeds approval
    TOOL: Bash (go test ./internal/patches/... ./pkg/models/...)

STEP 8 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests

- `go build ./...`, `go vet ./...`.
- `go test ./internal/patches/... ./pkg/models/...`.
- State-machine tests MUST assert rejection of illegal transitions (rmm-core
  §4.1) and idempotent ingestion (rmm-core §12.3).
- Migration up/down (additive, live rows intact).

## Rollback

```bash
git checkout HEAD -- internal/patches/ internal/api/ pkg/models/ py/alembic/versions/
git status   # confirm clean
# Down-rev the new migration if already applied downstream (alembic downgrade <prev>).
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Per-KB state queryable per agent | API test | State reflects scan + install history |
| 2 | WinUpdate machine rejects illegal transitions | state-machine test | Asserted rejection |
| 3 | Ingestion idempotent | double-ingest test | No duplicate/incorrect state |
| 4 | Subjects stay under oap.agents.<id>. | git grep | No rmm.winupdate.* introduced |
| 5 | Additive migration | up/down | Existing patch tables untouched |

## Reference

- `openspec/specs/rmm-operations/spec.md` §2 (WinUpdate), §10.1, §11
- `openspec/specs/rmm-core/spec.md` §4.1 (transition-table convention), §4.4, §12.3
- `pkg/agent/patcher/{windows,installer,handler}.go` (existing agent side)
- `internal/patches/approval.go` (pattern to follow)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
