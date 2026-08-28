# Sprint RELAY-00: Managed Relay Architecture & Security Decisions

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Establish the approved architecture/security decision gate for
the managed relay: WSS rendezvous, issued identity + entitlement, discovery
federation, and the E2E/private/load acceptance stage. Confirm the accounting-core
baseline and record every unapproved choice as an explicit blocker.
**Priority:** P1 (Blocking)
**Estimated Effort:** 45-60 minutes
**Status:** APPROVED (decision gate populated 2026-08-25; downstream RELAY-01..06 await per-sprint go-ahead)

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
| I | Issued identity + entitlement is required before admission | APPROVED (I.3 resolved; layered mTLS + bearer token) | RELAY-02 |
| D | Discovery federation is a product outcome | APPROVED (D.2 resolved; dedicated gRPC discovery service) | RELAY-05 |
| E | E2E/private/load acceptance is required | APPROVED (E.4 resolved; blind forwarder) | RELAY-06 |
| — | Raw TCP byte forwarder | **UNAPPROVED** | blocked |

#### Resolved mechanisms (formerly BLOCKED) — recorded 2026-08-25

- **I.3 — Identity presentation (RESOLVED → layered):** Two layers.
  - *Layer 1 (mTLS, RMM-09 Ed25519 CA):* agents present a client cert chained to
    the platform Ed25519 CA, principal `oap:<agentID>`. The relay terminates WSS
    and proves *who* is connecting at the socket layer (R.3 admission).
  - *Layer 2 (signed bearer token):* each rendezvous request also carries a
    short-lived token (agent ID + target + expiry, signed by the platform key).
    The relay verifies it for *entitlement* (I.1/I.2 — authorization to reach the
    target). mTLS = authentication; token = authorization. Not redundant: they
    answer different questions. Reuses the shipped RMM-09 CA + cert model.
- **D.2 — Discovery wire protocol / federation (RESOLVED → dedicated gRPC):**
  a standalone gRPC discovery service carrying capability/agent records, with an
  explicit federation handshake between relay instances. Clean contract boundary;
  does not reuse the NATS bus (separate service by choice).
- **E.4 — Relay-blind payloads (RESOLVED → blind forwarder):** agents establish
  session keys *out-of-band* (WireGuard/SSH model from RMM-09); the relay only
  ever moves ciphertext it cannot decrypt. Zero payload attack surface on the
  relay. This is the strongest option (per-leg DTLS adds handshake attack surface;
  relay-readable payloads contradict the spec's stated intent). Reuses the RMM-09
  data-plane model.

### STEP 3: Record blockers (unapproved choices)

The following MUST remain unimplemented until a dedicated design exists; any
sprint found inventing one is in scope violation:
- ~~**Identity-presentation cryptography (spec I.3)**~~ — **RESOLVED 2026-08-25**
  (layered mTLS + signed bearer token; see §STEP 2). RELAY-02 may proceed.
- ~~**End-to-end encryption mechanism (spec E.4)**~~ — **RESOLVED 2026-08-25**
  (blind forwarder; see §STEP 2). RELAY-06 is unblocked.
- ~~**Discovery wire protocol / federation semantics (spec D.2)**~~ —
  **RESOLVED 2026-08-25** (dedicated gRPC discovery service; see §STEP 2).
  RELAY-05 may proceed.
- **Rendezvous handshake and matching semantics** — fields, namespace and tenant
  binding, pairing lifecycle, timeouts, duplicate policy, and close/error behavior.
- **Operator API security and metric contracts** — authentication, authorization,
  listener binding, tenant visibility, metric names, and units.
- **TLS/WSS test certificates** — generate at test time only; commit none.

RELAY-02, RELAY-03, RELAY-05, and RELAY-06 are now unblocked. RELAY-01 and
RELAY-04 were never blocked. Each sprint still passes through its own decision
gate before building; approved outcomes are not authorization to invent an
implementation contract that conflicts with §STEP 2.

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
| BLOCKED:   TCP forwarder, TLS test certs                        |
| RESOLVED:  identity crypto (I.3), E2E crypto (E.4), D.2        |
| ROLLBACK:  none needed (no production edits)                     |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
