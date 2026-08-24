# Sprint: RMM Operations — Fleet Alert-Suppression Maintenance Windows

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Add a fleet-level (org/client/site-scoped) alert-notification
suppression window, distinct from patch-deploy windows and per-user quiet hours.
Build-ready.
**Priority:** P1 (Blocking)
**Estimated Effort:** 6-8 hours
**Status:** COMPLETE
**Dependencies:** RMM-00 (spec + open decisions)

---

## Overview

Alert suppression today is per-user `QuietHours`
(`internal/alerts/preferences.go`: `QuietHours`,
`UserAlertPreferences`, `IsInQuietHours`). There is no fleet-level window that
an operator configures once to suppress alert *notifications* during planned
work. Patch deployment already has its own window concept
(`MaintenanceWindow` / `BlackoutWindow` in
`internal/patches/scheduler_types.go`) — that is a different mechanism and is
NOT reused here (spec §10.3).

## Problem Statement

G-RMM-002: no maintenance-window / silence-window concept to suppress alerts
during planned work. The existing suppression seams are (a) per-user quiet
hours in `internal/alerts/preferences.go` and (b) patch-deploy windows in
`internal/patches/`. Neither is a fleet-level alert-suppression window.

**Why:** Operators need to schedule maintenance without a burst of false alerts.
**Where:** new entity + `internal/alerts/` evaluation; migration in
`py/alembic/versions/`.

## Scope Boundary

```
IN SCOPE (may modify):
  - internal/alerts/  (new suppression-window store + evaluation: extend the
                       Evaluate path, keep quiet-hours code intact)
  - pkg/models/       (new MaintenanceWindow entity for ALERT suppression)
  - py/alembic/versions/  (additive table for alert-suppression windows)
  - internal/api/     (CRUD route for alert-suppression windows, if wired here)
  - cmd/server/       (wire the new store/scheduler if it needs lifecycle)

OUT OF SCOPE (DO NOT TOUCH):
  - internal/patches/scheduler_types.go, scheduler.go, store_crud.go
      (patch MaintenanceWindow/BlackoutWindow stays untouched — different concept)
  - internal/alerts/preferences.go quiet-hours behavior
  - Offline SLA rule condition (RMM-01)
```

## Open Decisions (restated from spec §10.3/10.4)

- Spec §10.3 is RESOLVED: patch-maintenance and alert-suppression remain
  separate. Do not fold them together.
- Recurring vs one-shot window shape is a small design choice the sprint may
  make, but it must be recorded in the sprint notes and match the
  `MaintenanceWindow`-style time semantics already used by patches (reuse the
  time-window representation, not the patches package).

## Production-Before-Test Sequence

```
STEP 1 (MODEL): Add the alert-suppression window entity (scoped org/client/
    site, start/end, optional recurrence) to pkg/models/. Reuse a plain time
    window representation consistent with internal/patches/scheduler_types.go
    semantics but WITHOUT importing that package. TOOL: Edit

STEP 2 (MIGRATION): Additive table. Follow existing alembic style. TOOL: Write

STEP 3 (STORE): Persist/read windows in internal/alerts/ (new store file).
    TOOL: Write

STEP 4 (EVALUATION): In the alert notification path, before delivering a
    notification, suppress it if a scoped window covers now — mirroring how
    quiet hours suppress (internal/alerts/preferences.go Evaluate). Keep
    quiet-hours logic unchanged. TOOL: Edit

STEP 5 (API): Expose CRUD for the windows if the UI needs it. TOOL: Edit

STEP 6 (BUILD): go build ./... && go vet ./... before tests. TOOL: Bash

STEP 7 (TESTS): After production code —
    - store CRUD round-trip + scoping
    - suppression fires inside window, not outside, at boundaries
    - per-user quiet hours still apply independently
    TOOL: Bash (go test ./internal/alerts/... ./pkg/models/...)

STEP 8 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests

- `go build ./...`, `go vet ./...`.
- `go test ./internal/alerts/... ./pkg/models/...` — window suppression +
  boundary + quiet-hours regression.
- Migration up/down cleanly (additive).

## Rollback

```bash
git checkout HEAD -- internal/alerts/ internal/api/ pkg/models/ \
    py/alembic/versions/ cmd/server/
git status   # confirm clean
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Window entity scoped to org/client/site | store test | Correct scoping enforced |
| 2 | Alert notifications suppressed in-window | evaluation test | Suppressed at start boundary, delivered after end |
| 3 | Quiet hours still work | regression | Per-user quiet hours unaffected |
| 4 | Patch windows untouched | git diff internal/patches/ | No change to patches package |
| 5 | Additive migration | up/down run | No alteration of existing tables |

## Reference

- `openspec/specs/rmm-operations/spec.md` §4 (Maintenance Windows) and §10.3
- `internal/alerts/preferences.go` (quiet hours, the pattern to extend)
- `internal/patches/scheduler_types.go` (time-window semantics to mirror, not reuse)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.1

---

## Completion Record

**Status: COMPLETE.** The work was implemented and verified in-tree on `main`
during 2026-08-24. It is **not committed** — no commit hash exists for RMM-02.
The requirements and acceptance criteria above are unchanged.

### Implemented behaviors (verified)

- **Org/client/site scoping.** The alert-suppression window entity is scoped at
  org, client, and site. Scoping is enforced at both the store and the evaluation
  path; a window only suppresses alerts in its own scope.
- **IANA timezone scheduling.** Window start/end are interpreted in IANA
  timezone terms, consistent with the `MaintenanceWindow`-style semantics used by
  patches (mirrored, not imported from `internal/patches/`).
- **Recurring windows.** Recurring windows repeat on their configured schedule
  (e.g. weekly maintenance cadence) across the timezone.
- **Overnight windows.** A window whose end precedes its start wraps across
  midnight and is evaluated correctly at the boundary.
- **Paid-tier gate.** Window management is gated behind paid license tiers; the
  feature is not available on the free/community tier.
- **RBAC.** Window CRUD endpoints enforce role-based access control; unauthorized
  callers are rejected.
- **Audit.** Window create/update/delete events are logged to the hash-chained
  audit log.
- **Tenant isolation.** All window reads and writes are tenant-isolated; a
  tenant cannot observe or affect another tenant's windows.
- **Fail-open delivery.** If the suppression-window store read errors, alert
  notifications are **delivered** (fail-open) rather than silently dropped. A
  store failure never suppresses alerts.
- **Migration 0012.** The alert-suppression window table is additive
  (`py/alembic/versions/0012_...`); up/down run cleanly and no existing table is
  altered.

### Verification evidence

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./internal/alerts/... ./pkg/models/...` — pass: store CRUD
  round-trip + scoping, suppression inside window / delivered outside window,
  boundary semantics (start/end, overnight, recurring), per-user quiet hours
  still apply independently, and the fail-open path on store read error.
- Patch windows untouched (`internal/patches/` unchanged) — acceptance criterion 4
  holds.
- Migration 0012 up/down — additive, no alteration of existing tables — acceptance
  criterion 5 holds.

### Notes

- RMM-01 (offline-agent SLA alerting) shipped as commit `6ff3a17`. RMM-02 has no
  commit; it is verified in-tree and remains uncommitted pending the user's
  closeout decision.
