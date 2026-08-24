# Sprint: RMM Operations — Offline-Agent SLA Alerting

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Add a configurable "agent silent > N hours" alert-rule
condition that reuses the existing alert engine and the 120s binary-offline
flip. Build-ready.
**Priority:** P1 (Blocking)
**Estimated Effort:** 4-6 hours
**Status:** PENDING
**Dependencies:** RMM-00 (spec + open decisions)

---

## Overview

The server already flips a silent agent to `offline` after ~120s and emits
`oap.events.agent` lifecycle events (`internal/events/heartbeat.go` `sweepStale`,
threshold `HeartbeatStaleThreshold` in `internal/events/subjects.go`; store
`MarkStaleAgentsOffline` in `cmd/server/server_adapters.go:142`). That is
liveness, not an SLA alarm. This sprint adds an alert-rule condition so a
technician can alert when an agent has been silent longer than a configured
hours threshold, evaluated against the agent's stored `last_seen`.

## Problem Statement

`AlertRule` (`pkg/models/models_alerts.go`) currently scopes against
`check_id` / `agent_id` / `site_id` with a `min_severity`, but has no
time-since-offline / silent-duration condition. There is no "agent hasn't
reported in 24h" alert (G-RMM-004).

**Why:** Technicians need an SLA alarm without changing the (correct) 120s
liveness threshold.
**Where:** `pkg/models/models_alerts.go`, `internal/alerts/store_alerts_rules.go`,
`internal/alerts/` engine where rules are evaluated.

## Scope Boundary

```
IN SCOPE (may modify):
  - pkg/models/models_alerts.go        (AlertRule: add optional condition field,
                                        e.g. offline_silence_seconds)
  - internal/alerts/store_alerts_rules.go   (persist the new condition column)
  - internal/alerts/engine_core.go / engine_handlers.go
                                       (evaluate the silence condition vs agent last_seen)
  - py/alembic/versions/0006_patches.py or a NEW 000X-*.py
                                       (additive column for the rule condition)
  - internal/api/ (alert-rule CRUD binding, if the handler validates fields)

OUT OF SCOPE (DO NOT TOUCH):
  - internal/events/heartbeat.go        (do NOT change the 120s threshold/TTL)
  - internal/events/subjects.go         (do NOT change HeartbeatStaleThreshold)
  - Graded (multi-tier) SLA — open decision 10.4, not resolved here
  - Fleet maintenance windows (RMM-02), patch windows
```

## Open Decision (restated from spec §10.4)

Offline SLA is binary-vs-graded + scoping. **This sprint builds the binary,
additive rule condition against `last_seen` only.** It does not introduce
degraded/partial-SLA tiers. If a graded SLA is later wanted it is a separate
decision and sprint; do not design for it here.

## Production-Before-Test Sequence

```
STEP 1 (MODEL): Add the optional silence-duration condition to the AlertRule
    Go struct in pkg/models/models_alerts.go. TOOL: Edit

STEP 2 (MIGRATION): Add the matching additive column to the alert_rule table.
    Follow existing alembic migration style (0005_policies.py / 0006_patches.py).
    TOOL: Write a new 000X migration (or extend the nearest running migration —
        check the latest head first)

STEP 3 (STORE): Persist/read the new column in store_alerts_rules.go CREATE/UPDATE.

STEP 4 (ENGINE): In the alert evaluation path, when an AlertRule carries the
    silence condition, compare agent.last_seen to now - N; fire only when the
    agent is silent longer than N (and otherwise matches). Reuse existing
    agent lookup. TOOL: Edit

STEP 5 (API VALIDATION): Where alert-rule fields are validated on input, accept
    and bound the new condition. TOOL: Edit

STEP 6 (BUILD): go build ./... and go vet ./... — production compiles BEFORE
    tests are written. TOOL: Bash

STEP 7 (TESTS): Add unit tests AFTER production code:
    - store round-trip of the new condition column
    - engine fires when silent > N and stays quiet when < N
    - existing rules (no condition) unaffected
    TOOL: Bash (go test ./internal/alerts/... ./pkg/models/...)

STEP 8 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests

- `go build ./...`, `go vet ./...` (compile + vet, production first).
- `go test ./internal/alerts/... ./pkg/models/...` — new condition behavior plus
  regression on existing rule evaluation.
- Migration applies cleanly: run the new migration against a scratch DB or the
  migration test path used by the repo before committing.

## Rollback

```bash
# Revert production + test changes for this sprint
git checkout HEAD -- pkg/models/models_alerts.go internal/alerts/ \
    internal/api/ py/alembic/versions/
git status   # confirm clean
# If the migration has already been applied downstream, down-rev it
#   (alembic downgrade <prev>) before reverting files.
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | New condition persisted on AlertRule | CRUD round-trip test | Saved and read back on GET |
| 2 | Alert fires only past configured silence | engine unit test | Fires at last_seen > N; quiet below N |
| 3 | 120s threshold unchanged | git diff internal/events/ | No change to heartbeat.go / subjects.go |
| 4 | Old rules unaffected | regression test | Rules without the condition behave as before |
| 5 | Migration backward-compatible | downgrade/upgrade run | Additive only; live rows intact |

## Reference

- `openspec/specs/rmm-operations/spec.md` §5 (Offline Agent SLA) and §10.4
- `internal/events/heartbeat.go`, `internal/events/subjects.go`
- `cmd/server/server_adapters.go` (`MarkStaleAgentsOffline`)
- `pkg/models/models_alerts.go`, `internal/alerts/`

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
