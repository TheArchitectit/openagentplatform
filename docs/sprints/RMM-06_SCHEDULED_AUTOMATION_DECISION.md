# Sprint: RMM Operations — Scheduled Automation (AutomatedTask) Decision Gate

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Resolve the scheduling-grammar and entity-shape decision for
scheduled automation; execution is contingent on approval. Decision-gated — NOT
a build ticket.
**Priority:** P2 (Normal)
**Estimated Effort:** 4-6 hours (decision) + contingent build
**Status:** PENDING
**Dependencies:** RMM-00 (spec records this as DEFERRED / open decision 10.2)

---

## Overview

An `automated_tasks` JSONB column already exists on `Policy`
(`py/alembic/versions/0005_policies.py:38`), but nothing binds it: there is no
`AutomatedTask` entity, no scheduler, and no recurring-task execution path.
`rmm-core` §2.5 names `AutomatedTask` as planned but not implemented. The reason
it is NOT build-ready is a scheduling-grammar decision that no current code
resolves — not a missing implementation.

## Problem Statement

Two incompatible scheduling models exist in the project's history:

1. **21-bit `schedule_bitmask`** — from the original Django blueprint
   (`docs/plans/MASTER_IMPLEMENTATION_PLAN.md` and the legacy RMM spec): fixed
   weekday/hour/dom/month bitmask. Nothing in the current Go code implements it.
2. **Cron-style recurrence** — the natural fit for the existing Go background
   loops and the check scheduler's `interval_seconds` bounds (`rmm-core` §6.3).

Picking one is a product decision (which schedules must be expressible), not an
engineering choice, and this sprint MUST NOT invent one (spec §3.2, §10.2).

**Why:** the two grammars change the `automated_tasks` JSONB schema, the entity,
and the scheduler. Choosing wrong is a migration + schema rework.
**Where:** decision → `internal/` scheduler + `pkg/models/` entity +
`py/alembic/versions/0005_policies.py` (`automated_tasks`).

## Scope Boundary

```
IN SCOPE (may modify):
  - This decision is documented in: a decision record (sprint notes or the
    rmm-operations spec §10.2 update after approval)
  - Contingent build after approval:
      - pkg/models/           (AutomatedTask entity)
      - internal/patches/ or internal/checks/  (recurring scheduler per the
        approved grammar; follow the rmm-core §12 background-loop convention)
      - py/alembic/versions/  (entity table + validated schedule column)

OUT OF SCOPE (DO NOT TOUCH / DECIDE):
  - Choosing the grammar (cron vs bitmask) without approval — THE blocker
  - AutomaticTask execution of arbitrary LLM agent workflows (ties to A2A, out of
    this sprint's single-domain scope)
  - Conflict with existing check scheduling (rmm-core §6.3 bounds)
```

## Open Decision (restated from spec §10.2)

Scheduling grammar: YES/NO on cron-style recurrence vs 21-bit
`schedule_bitmask`. The decision record MUST capture:
- which schedules must be expressible (weekly day+time? monthly dom? multi-day?)
- the resulting JSONB schema for `automated_tasks`
- acceptance of the chosen grammar before any code is written

## Production-Before-Test Sequence

```
STEP 0 (DECISION GATE): Produce the approved scheduling grammar + schema.
    If not approved → STOP, report BLOCKED; do NOT proceed. TOOL: sign-off

STEP 1 (CONTINGENT — ENTITY): Add the AutomatedTask model per the approved schema.

STEP 2 (CONTINGENT — MIGRATION): Additive table + schedule validation per grammar.

STEP 3 (CONTINGENT — SCHEDULER): Recurring dispatcher following the rmm-core
    §12 in-process loop convention; idempotent execution (at-least-once).

STEP 4 (CONTINGENT — API): CRUD for automated tasks.

STEP 5 (BUILD): go build ./... && go vet ./... before tests.

STEP 6 (TESTS): After production — grammar round-trip, scheduler fires at
    approved times, idempotency on retry.

STEP 7 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests (contingent on approval)

- `go build ./...`, `go vet ./...`.
- `go test <affected pkg>/...` — schedule validation + recurrence + idempotency.

## Rollback

```bash
# If contingent build landed and must be reverted:
git checkout HEAD -- pkg/models/ internal/patches/ internal/checks/ \
    internal/api/ py/alembic/versions/
# The decision record itself is not rolled back (it is the gate artifact).
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Grammar decision approved & recorded | sprint notes / spec §10.2 update | YES/NO recorded with rationale |
| 2 | No mechanism built before approval | git diff on build dirs | No code changes if decision pending |
| 3 | (If approved) schema matches grammar | entity test | automated_tasks schema validated |
| 4 | (If approved) scheduler fires on schedule | recurrence test | Correct times; idempotent retry |

## Reference

- `openspec/specs/rmm-operations/spec.md` §3 (AutomatedTask), §10.2
- `openspec/specs/rmm-core/spec.md` §2.5, §6.3, §12
- `py/alembic/versions/0005_policies.py` (§38 `automated_tasks`)
- `docs/plans/MASTER_IMPLEMENTATION_PLAN.md` (legacy bitmask provenance — read
  for context, not as a mandate)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
