# Sprint: RMM Operations — Reboot Coordination

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Wire the already-built `RebootQueue` /
`PatchDeployer.CoordinateReboots` into the deploy-completion path and add a
server→agent `oap.agents.<id>.reboot` command with an agent-side handler.
Build-ready (one ownership decision pending).
**Priority:** P1 (Blocking)
**Estimated Effort:** 6-9 hours
**Status:** PENDING
**Dependencies:** RMM-03 (per-KB state drives which targets need reboot)

---

## Overview

`internal/patches/deployer_strategies.go` defines `RebootQueue` (§299) and
`PatchDeployer.CoordinateReboots` (§350, staggered via `RebootStagger`) — but
nothing calls them. `NeedsReboot` is already reported on patch targets
(`rmm-core` §9.5). This sprint wires the coordinator into the deploy-completion
path and gives it a transport: a server→agent reboot command subject plus an
agent handler, so reboots are orchestrated rather than left to the agent's own
`/r`.

## Problem Statement

G-RMM-005 (rmm-core §9.5 / §14.6): full reboot coordination after patching is
not implemented. The logic (`CoordinateReboots`) exists but is dead code, and
there is no subject for the agent to receive a reboot directive.

**Why:** Predictable, in-window reboots are core to patch operations; the
building blocks already exist. `CoordinateReboots` is an unwired sequencing and
health-check scaffold — it waits, runs pre/post checks, and records results, but
it sends no reboot and has no backoff. An approved ownership decision must define
where the actual reboot action occurs between the pre- and post-checks before
this function can be wired.
**Where:** `internal/patches/deployer_strategies.go` (callers),
`cmd/agent/main.go` + `pkg/agent/patcher/` (new reboot handler),
`internal/patches/store_*.go` (persist reboot scheduling).

## Scope Boundary

```
IN SCOPE (may modify):
  - internal/patches/deployer_strategies.go  (invoke CoordinateReboots at
                                              deploy completion when targets
                                              report NeedsReboot)
  - internal/api/patches.go    (expose reboot scheduling on a deploy/job)
  - internal/events/subjects.go (add RebootSubject helper, oap.agents.<id>.reboot)
  - pkg/agent/patcher/         (new reboot subscribe handler honoring the subject)
  - cmd/agent/main.go          (wire the reboot subscription beside patch scan/install)

OUT OF SCOPE (DO NOT TOUCH):
  - pkg/agent/patcher/installer.go (RC3010 detection and RebootRequired reporting)
  - The reboot ownership model if not already decided — see Open Decision
  - Fleet maintenance windows (RMM-02)
```

## Open Decision (restated from spec §10.6)

Reboot ownership: server-coordinated push (this sprint's default) vs agent
self-reboot after drain. Decide and record before finalizing the agent handler.
If server-coordinated is approved, the `CoordinateReboots` stagger + backoff is
the mechanism; if agent-owned is chosen, the subject becomes a hint and the
agent decides timing — which changes the acceptance criteria. Do not implement
both.

## Production-Before-Test Sequence

```
STEP 1 (SUBJECT): Add RebootSubject(agentID) → oap.agents.<id>.reboot in
    internal/events/subjects.go (mirror patcher.PatchInstallSubject style).
    TOOL: Edit

STEP 2 (AGENT): Add a reboot handler in pkg/agent/patcher/ that subscribes to
    the subject and issues an OS reboot (per platform, guarded by the payload).
    Wire it in cmd/agent/main.go beside the patch handler. TOOL: Write/Edit

STEP 3 (SERVER): Invoke PatchDeployer.CoordinateReboots from the deploy
    completion path when targets report NeedsReboot; publish the reboot
    directives. TOOL: Edit

STEP 4 (API): Expose reboot scheduling on a deploy/job (internal/api/patches.go).

STEP 5 (BUILD): go build ./... && go vet ./... before tests. TOOL: Bash

STEP 6 (TESTS): After production —
    - coordinator staggers and publishes reboot directives (mock transport)
    - agent handler parses and validates the payload; refuses malformed input
    - NeedsReboot-only targets get directives
    TOOL: Bash (go test ./internal/patches/... ./pkg/agent/patcher/... \
        ./internal/events/...)

STEP 7 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests

- `go build ./...`, `go vet ./...`.
- `go test ./internal/patches/... ./pkg/agent/patcher/... ./internal/events/...`.
- Reuse/expand existing `deployer_strategies_test.go` coverage if present.
- The reboot subject construction must be covered (`internal/events/subj_test.go`
  pattern).

## Rollback

```bash
git checkout HEAD -- internal/patches/ internal/api/ internal/events/ \
    pkg/agent/patcher/ cmd/agent/main.go
git status   # confirm clean
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | CoordinateReboots now invoked | call-site test | NeedsReboot targets route through coordinator |
| 2 | Reboot subject delivered to agent | subject unit test | oap.agents.<id>.reboot constructed correctly |
| 3 | Agent refuses malformed payloads | agent handler test | No reboot on bad input |
| 4 | Stagger honored | coordinator test | Reboots spaced by RebootStagger |
| 5 | Ownership decision recorded | sprint notes | One model chosen and implemented, not both |

## Reference

- `openspec/specs/rmm-operations/spec.md` §6 (Reboot Coordination), §10.1, §10.6
- `internal/patches/deployer_strategies.go` §299/§350 (existing unwired code)
- `pkg/agent/patcher/handler.go` (pattern for the new agent handler)
- `internal/events/subjects.go` (subject conventions)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
