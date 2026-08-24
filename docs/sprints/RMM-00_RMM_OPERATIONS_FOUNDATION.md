# Sprint: RMM Operations — Foundation & Scope Baselining

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Land the `rmm-operations` spec; freeze the 8-domain scope, the
IN/DEFERRED split, and the open-decision register that every later RMM sprint
gates on.
**Priority:** P1 (Blocking)
**Estimated Effort:** 3-4 hours
**Status:** PENDING
**Dependencies:** none (baselines against current `main`)

---

## Overview

RMM Core (`openspec/specs/rmm-core/spec.md`) deliberately lists eight RMM
capabilities as planned extensions in §14. This foundation sprint records, once,
what the actual Go/Python code already provides for each domain and what is
genuinely missing — so the eight build/decision sprints RMM-01..RMM-08 do not
reinvent existing mechanisms or fabricate behavior. It produces the
`rmm-operations` spec and the shared decisions register, and it edits
`rmm-core` §14 only to link/deduplicate (pointing at the new spec) without
rewriting RMM Core's owned requirements.

## Problem Statement

RMM Core tracks eight parity gaps (`docs/GAP_ANALYSIS_RMM_PLATFORM.md`
G-RMM-002/003/004 and `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` P3 items 10-11) but
does not record which building blocks already exist. Without a single anchored
spec, later sprint work would either rebuild what exists (e.g. re-derive
`RebootQueue`/`CoordinateReboots`, which have zero callers today) or invent a
mechanism where a design decision is actually unresolved (e.g. AutomatedTask's
scheduling grammar).

**Why:** The IN/DEFERRED boundary must be fixed before any build sprint starts.
**Where:** `openspec/specs/rmm-core/spec.md` §14; new
`openspec/specs/rmm-operations/spec.md`.

## Scope Boundary

```
IN SCOPE (may modify):
  - openspec/specs/rmm-operations/spec.md        (NEW spec — primary artifact)
  - openspec/specs/rmm-core/spec.md              (§14 ONLY: link/deduplicate
                                                 to rmm-operations; nothing else)
  - docs/sprints/RMM-00_RMM_OPERATIONS_FOUNDATION.md   (this doc)

OUT OF SCOPE (DO NOT TOUCH):
  - INDEX_MAP.md, HEADER_MAP.md, TOC.md, PROJECT_PLAN.md, STATUS.md
    (the lead updates these)
  - All .go / .py / .tsx source files — this sprint writes no runtime code
  - rmm-core spec sections other than §14
```

## Production-Before-Test Sequence

This sprint produces documentation only (no runtime code), so the "production
artifact" is the spec itself, source-anchored to existing code. Sequence:

```
STEP 1 (READ/VERIFY): Re-verify the source anchors cited in the spec so that
    every claim is true on current main:
      - internal/patches/deployer_strategies.go  RebootQueue/CoordinateReboots (zero callers)
      - internal/events/heartbeat.go sweepStale + subjects.go HeartbeatStaleThreshold
      - cmd/server/server_adapters.go MarkStaleAgentsOffline
      - internal/alerts/preferences.go QuietHours (only per-user suppression)
      - internal/patches/scheduler_types.go MaintenanceWindow/BlackoutWindow (patch windows)
      - pkg/agent/patcher/{windows,installer,handler}.go  WinUpdate scan/install/dispatch
      - py/alembic/versions/0005_policies.py (win_update_policy:40, automated_tasks:38)
      - py/alembic/versions/0006_patches.py (cve_ids:37)
      - web/src/lib/usePatches_types.ts (cve_ids, cvss_score)
    TOOL: grep / Read

STEP 2 (WRITE): Author openspec/specs/rmm-operations/spec.md — the 8 domains,
    IN/DEFERRED split, open-decision register. TOOL: Write

STEP 3 (DEDUPE): Edit rmm-core/spec.md §14 to link to rmm-operations so §14 no
    longer implies its items are un-scoped. Do not touch any other §.
    TOOL: Edit

STEP 4 (VALIDATE): 500-line / broken-link / trailing-whitespace checks on the two
    specs + this doc; git diff --check. See Validation section.

STEP 5 (COMMIT): Commit with the project trailer convention (see Commit).
```

## Tests

- **Spec-length gate:** both `openspec/specs/*/spec.md` files and this sprint
  doc must be ≤ 500 lines (CI `documentation-check.yml` `check-doc-length`).
- **Link gate:** every relative `.md` link in both specs and this doc must
  resolve to an existing file (CI `check-broken-links`). The RMM-00..08 links
  under `Cross-References` resolve only after all RMM sprint docs exist — author
  this doc last, or verify the full set together.
- **Trailing whitespace:** no line ends with a space (CI `check-trailing-whitespace`).
- **Content gate (manual):** every `Already implemented` claim in the spec maps
  to a real file/symbol found in STEP 1. No requirement states a mechanism that
  does not exist in code.

## Rollback

```bash
# Discard spec + dedupe edits (this sprint only)
# Record touched files first, then revert only those exact paths:
git diff --name-only > /tmp/rmm-00-touched.txt
git checkout HEAD -- openspec/specs/rmm-operations/spec.md openspec/specs/rmm-core/spec.md docs/sprints/RMM-00_RMM_OPERATIONS_FOUNDATION.md
git status   # confirm clean
# If a file was created (not modified) by this sprint, remove it with
# `git rm -f` rather than checkout.
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | `rmm-operations/spec.md` exists and covers all 8 domains | Read spec §Description, §2-§9 | Every domain 1-8 present with IN/DEFERRED marked |
| 2 | Every "already implemented" claim is source-anchored | grep for the cited symbols | Each resolves to a real file/symbol on current main |
| 3 | Open decisions recorded, none resolved arbitrarily | Read spec §10 | 8 rows; blocked ones (10.2/10.5/10.8) left open |
| 4 | `rmm-core` §14 links to rmm-operations; nothing else changed | `git diff openspec/specs/rmm-core/spec.md` | Changes confined to §14; rest byte-identical |
| 5 | All files ≤ 500 lines and no broken internal links | CI checks above | Passes |

## Reference

- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.1 (G-RMM-002/003/004)
- `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` P3 items 10-11, §5
- `openspec/specs/rmm-core/spec.md` §14
- Sprint order loader: see `docs/sprints/INDEX.md`

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
