# Sprint RELAY-00: Managed Relay Architecture & Security Decisions

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Establish the approved architecture/security decision gate for
the managed relay: WSS rendezvous, issued identity + entitlement, discovery
federation, and the E2E/private/load acceptance stage. Confirm the accounting-core
baseline and record every unapproved choice as an explicit blocker.
**Priority:** P1 (Blocking)
**Estimated Effort:** 45-60 minutes
**Status:** PENDING

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read every file before touching it | [ ] |
| **SCOPE LOCK** | Only files listed below | [ ] |
| **NO FEATURE CREEP** | No production code in this sprint | [ ] |
| **DECISION GATE** | No later sprint may proceed on an unapproved choice | [ ] |
| **BACKUP AWARENESS** | Rollback is a no-op (no code changes expected) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

`internal/relay` is an accounting/control library with no network transport, no
binary, and unreachable doc-comment claims ("Authentication on both legs",
"End-to-end encryption"). Before any build work, the series needs a single
authoritative architecture/security decision set so sprints do not diverge or
invent mechanisms the platform has not approved. This sprint freezes that
decision gate (and its blockers) and touches no code.

**Why:** The approved direction is WSS rendezvous + issued identity/entitlement +
discovery federation + an E2E/private/load stage. A raw TCP forwarder was
explicitly rejected; without this freeze, sprints could resurrect it.

**Where:** `internal/relay/relay.go`, `relay_test.go`, `relay_metrics_test.go`
(read-only), `openspec/specs/a2a-relay/spec.md` §7.

---

## SCOPE BOUNDARY

```
IN SCOPE (read-only + verification only):
  - File: internal/relay/relay.go                     Lines: all  Read to confirm contract
  - File: internal/relay/relay_test.go                Lines: all  Read to enumerate tests
  - File: internal/relay/relay_metrics_test.go        Lines: all  Read to enumerate tests
  - File: openspec/specs/a2a-relay/spec.md            Lines: §7   Reference approved decisions

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
STEP 1: Confirm contract + baseline --------> accounting core is green
STEP 2: Freeze architecture/security --------> decision gate recorded
STEP 3: Enumerate blockers ------------------> unapproved choices blocked
DONE:   Report baseline + gate --------------> series is approved to proceed
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Confirm the accounting-core contract and baseline

**Action:** Read the three relay files and run the existing suite.

```
TOOL: Read   internal/relay/relay.go, relay_test.go, relay_metrics_test.go
TOOL: Bash   go test ./internal/relay/...
```

**Expected Output:** All tests pass. The implemented contract is exactly spec
§1–§6. Do NOT modify any of it. The series builds ON these, never changes them.

**Decision Point:**
- [ ] Baseline green → proceed
- [ ] Baseline red → HALT; do not start build sprints; report the failure
      (never code around a broken contract)

### STEP 2: Freeze the architecture/security decision gate

Record the approved decision set (mirrors spec §7). This is the gate every later
sprint must pass; a sprint that needs a decision absent here is BLOCKED.

| # | Decision | Status | Sprint |
|---|----------|--------|--------|
| R | WSS rendezvous (agents connect over WSS; relay matches legs) | APPROVED | RELAY-01/03 |
| R | Dedicated `cmd/relay` binary, NOT wired into `cmd/server` (W8) | APPROVED | RELAY-01 |
| I | Issued identity + entitlement is required before admission | OUTCOME APPROVED; MECHANISM BLOCKED by I.3 | RELAY-02 |
| D | Discovery federation is a product outcome | OUTCOME APPROVED; SEMANTICS BLOCKED by D.2 | RELAY-05 |
| E | E2E/private/load acceptance is required | OUTCOME APPROVED; EXECUTION BLOCKED by I.3/D.2/E.4 | RELAY-06 |
| — | Raw TCP byte forwarder | **UNAPPROVED** | blocked |

### STEP 3: Record blockers (unapproved choices)

The following MUST remain unimplemented until a dedicated design exists; any
sprint found inventing one is in scope violation:
- **Identity-presentation cryptography (spec I.3)** — how an issued identity is
  cryptographically presented/verified (mTLS vs. token vs. other).
- **End-to-end encryption mechanism (spec E.4)** — how the relay is kept from
  reading payload secrets.
- **Discovery wire protocol / federation semantics (spec D.2)**.
- **Rendezvous handshake and matching semantics** — fields, namespace and tenant
  binding, pairing lifecycle, timeouts, duplicate policy, and close/error behavior.
- **Operator API security and metric contracts** — authentication, authorization,
  listener binding, tenant visibility, metric names, and units.
- **TLS/WSS test certificates** — generate at test time only; commit none.

RELAY-02 through RELAY-06 MUST stop at their decision gates until every mechanism
they depend on is approved. Approved outcomes are not authorization to invent the
implementation contract.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Baseline suite green | `go test ./internal/relay/` | Exit 0 |
| 2 | Contract confirmed | Read diff vs spec §1–§6 | No contract drift |
| 3 | Decision gate recorded | This file §STEP 2 + spec §7 | All decisions listed |
| 4 | Unapproved choices blocked | This file §STEP 3 + spec `[BLOCKED]` | Explicitly blocked |
| 5 | No files changed | `git status` | No `.go` modifications |

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
| RELAY-00 : architecture/security decision gate (NO CODE CHANGE)  |
| FILES:     internal/relay/*.go (read-only), spec.md §7           |
| APPROVED:  WSS rendezvous, cmd/relay binary, identity+entitlement|
|            discovery federation, E2E/private/load stage          |
| BLOCKED:   TCP forwarder, identity crypto (I.3), E2E crypto (E.4)|
|            discovery protocol (D.2), TLS test certs              |
| ROLLBACK:  none needed (no production edits)                     |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
