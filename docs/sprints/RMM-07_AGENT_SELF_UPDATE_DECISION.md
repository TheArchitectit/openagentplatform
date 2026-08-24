# Sprint: RMM Operations — Agent Self-Update Decision Gate

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Resolve the self-update trust/signing model and update channel
via security review; execution is contingent on approval. Decision-gated — NOT
a build ticket.
**Priority:** P1 (Blocking) — safety-sensitive
**Estimated Effort:** 4-8 hours (security decision) + contingent build
**Status:** PENDING
**Dependencies:** RMM-00 (spec records this as DEFERRED / open decision 10.5); a
security review

---

## Overview

The agent reports its version today — `AgentVersion` in
`pkg/agent/register.go:24` and `pkg/agent/hostinfo.go:29`, surfaced by
`cmd/agent/main.go -version` — but there is no mechanism to push or fetch an
updated agent binary (G-RMM-003). "The agent can update itself" is
fundamentally a trust question: any self-update channel is equivalent to remote
code execution on the endpoint. It therefore requires a signing/trust model and
a security review BEFORE any build, per `docs/AGENT_GUARDRAILS.md` and the
project's security posture.

## Problem Statement

A self-updating agent that accepts an unsigned or wrongly-signed binary is a
fleet-wide RCE vector. The project's agents already hold credentials and run
with elevated privileges on managed endpoints. There is no defined trust anchor,
update signature scheme, or rollout/cadence model in the codebase.

**Why:** safety-sensitive; must not be built before the trust model is approved.
**Where:** decision → agent binary distribution + update path; anchors
`pkg/agent/register.go`, `pkg/agent/hostinfo.go`, `cmd/agent/main.go`.

## Scope Boundary

```
IN SCOPE (may modify):
  - This sprint produces a decision + security-review record (not runtime code
    until approved)
  - Contingent build after approval:
      - pkg/agent/ or a new pkg/agent/updater/  (verified update fetch + apply)
      - internal/  (server-side binary registry / rollout, per approved model)
      - deploy/    (signing + artifact publication pipeline, per approved model)

OUT OF SCOPE (DO NOT TOUCH / DECIDE):
  - Choosing the trust/signing model (Ed25519 vs platform update services vs
    existing package manager) without security approval — THE blocker
  - Silent auto-update of the agent without user/operator control
  - Any mechanism that bypasses signature verification
```

## Open Decision (restated from spec §10.5)

Trust model + update channel. The decision record MUST capture, per security
review:
- the signing key + verification source (who signs, who trusts)
- the update transport (server push subject vs periodic poll) — consistent with
  spec §10.1 subject governance
- the rollout control (operator-gated updates, not silent)

## Production-Before-Test Sequence

```
STEP 0 (DECISION GATE): Security review approves the trust/signing model and
    rollout control. If not approved → STOP, report BLOCKED; do NOT proceed.
    TOOL: security review sign-off

STEP 1 (CONTINGENT — SIGNING): Publish pipeline that signs agent binaries per
    the model. (deploy/)

STEP 2 (CONTINGENT — VERIFY): Agent-side verification before apply; refusal on
    signature mismatch.

STEP 3 (CONTINGENT — TRANSPORT): Update command/feedback wired under the
    approved subject, per spec §10.1 governance.

STEP 4 (CONTINGENT — ROLLOUT): Operator-gated rollout / version pinning.

STEP 5 (BUILD): go build ./... && go vet ./... before tests.

STEP 6 (TESTS): After production — verification rejects bad signatures; rollout
    gating works; feedback recorded.

STEP 7 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests (contingent on approval)

- `go build ./...`, `go vet ./...`.
- Signature verification negative tests (tampered binary MUST be refused).
- Rollout gating tests.

## Rollback

```bash
# If contingent build landed and must be reverted:
git checkout HEAD -- pkg/agent/ internal/ deploy/
git status
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Trust/signing model approved by security | security-review record | Signed-off; recorded in spec §10.5 |
| 2 | No update mechanism built before approval | git diff on build dirs | No code changes if decision pending |
| 3 | (If approved) bad signature refused | negative test | Tampered binary never applied |
| 4 | (If approved) rollout is operator-gated | rollout test | No silent auto-update |

## Reference

- `openspec/specs/rmm-operations/spec.md` §8 (Self-Update), §10.5
- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` G-RMM-003
- `pkg/agent/register.go:24`, `pkg/agent/hostinfo.go:29`, `cmd/agent/main.go`
- `docs/AGENT_GUARDRAILS.md` (security guardrails)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
