# Sprint RELAY-00: Managed Relay Transport Roadmap & Baseline Freeze

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Anchor the RELAY-00..RELAY-06 series: confirm the accounting-core
contract and test baseline, freeze the transport architecture, and record blockers.
**Priority:** P1 (Blocking)
**Estimated Effort:** 30-45 minutes
**Status:** PENDING

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read every file before touching it | [ ] |
| **SCOPE LOCK** | Only files listed below | [ ] |
| **NO FEATURE CREEP** | No production code in this sprint | [ ] |
| **BACKUP AWARENESS** | Rollback is a no-op (no changes expected) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

`internal/relay` is a service-layer accounting/control library with no network
transport, no binding to a server binary, and unreachable doc-comment
aspirations ("Authentication on both legs", "End-to-end encryption"). Before any
transport sprint can be written or executed, the series needs a shared baseline:
which code and tests define the contract, what the frozen transport design is,
and which external designs are hard blockers. This sprint produces that anchor
and updates no maps, status fields, or the project plan.

**Why:** Subsequent sprints (RELAY-01..06) must share one transport design and
one blocker list, or they will diverge and invent mechanisms the platform has not
decided on.

**Where:** `internal/relay/relay.go`, `internal/relay/relay_test.go`,
`internal/relay/relay_metrics_test.go`, `openspec/specs/a2a-relay/spec.md` (§7).

---

## SCOPE BOUNDARY

```
IN SCOPE (read-only + verification only):
  - File: internal/relay/relay.go          Lines: all  Read to confirm contract
  - File: internal/relay/relay_test.go     Lines: all  Read to enumerate tests
  - File: internal/relay/relay_metrics_test.go  Lines: all Read to enumerate tests
  - File: openspec/specs/a2a-relay/spec.md Lines: §7    Reference for planned design

OUT OF SCOPE (DO NOT TOUCH in this sprint):
  - No production code changes (relay.go etc.)
  - No new test files
  - No edits to INDEX_MAP.md, HEADER_MAP.md, TOC.md, docs/sprints/INDEX.md
  - No edits to any status field or project plan
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Confirm contract + tests ------> Baseline PASSED
STEP 2: Freeze transport decisions ----> RELAY-01..06 reference them
STEP 3: Record blockers ---------------> auth (S.1) / E2E (S.2) designs
DONE:   Report baseline + hash --------> Series is ready to execute
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Confirm the accounting-core contract and test baseline

**Action:** Read the three relay files and run the existing suite.

```
TOOL: Read   internal/relay/relay.go, relay_test.go, relay_metrics_test.go
TOOL: Bash   go test ./internal/relay/...
```

**Expected Output:** All tests pass. The implemented contract is exactly spec
§1–§6: `NewRelayService`, connection lifecycle, per-tenant isolation/limits,
usage metering, idle reaping, observability. Do NOT modify any of it.

**Decision Point:**
- [ ] Baseline green → proceed
- [ ] Baseline red → HALT; do not start transport sprints; report the failure
      (do not code around a broken contract)

### STEP 2: Freeze the transport architecture

**Action:** Record (in this file's summary / the spec §7 reference) that the
series implements spec §7.1 (Transport) T.1–T.5 and does NOT implement §7.2
(Security). Confirm the sprint order:

| Sprint | Deliverable |
|--------|-------------|
| RELAY-00 | Baseline + this roadmap |
| RELAY-01 | TCP listener + TLS termination (`internal/relay/transport.go`) |
| RELAY-02 | Bidirectional forwarding + metering (`internal/relay/relay_forward.go`) |
| RELAY-03 | Graceful shutdown + lifecycle (`internal/relay/lifecycle.go`) |
| RELAY-04 | Dedicated binary (`cmd/relay`) |
| RELAY-05 | Observability / health + metrics endpoints |
| RELAY-06 | Tenancy hardening, race suite, relay README |

### STEP 3: Record blockers

Blockers for all subsequent sprints:
- **Auth design absent (spec S.1)** — per-leg peer authentication has no
  approved mechanism. Transport sprints MUST NOT invent one; identifiers are
  supplied by configuration for single-tenant dev only.
- **E2E encryption design absent (spec S.2)** — no approved mechanism. MUST NOT
  be implemented by any RELAY-00..06 sprint.
- **No TLS test certificates in-repo** — RELAY-01/04 need locally generated
  self-signed certs for tests; do not commit real credentials.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Baseline suite green | `go test ./internal/relay/` | Exit 0 |
| 2 | Contract confirmed | Read diff vs spec §1–§6 | No contract drift identified |
| 3 | Blockers recorded | This file §STEP 3 | Auth/E2E listed, not invented |
| 4 | No files changed | `git status` | No `.go` modifications |

---

## ROLLBACK PROCEDURE

This sprint intentionally changes no production files. If the baseline fails,
the rollback is to STOP and report; there is nothing to revert.

```bash
git status        # confirm no unexpected changes
```

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-00 : baseline + design freeze (NO CODE CHANGE)             |
| FILES:     internal/relay/*.go (read-only), spec.md §7           |
| BLOCKERS:  auth (S.1) design, E2E (S.2) design, TLS test certs   |
| ROLLBACK:  none needed (no production edits)                     |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
