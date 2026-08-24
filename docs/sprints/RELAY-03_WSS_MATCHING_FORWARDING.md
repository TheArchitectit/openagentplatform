# Sprint RELAY-03: WSS Matching & Frame Forwarding

**Sprint Date:** 2026-08-23 (Saturday)
**Sprint Focus:** Approve the rendezvous protocol, then implement matching and
frame forwarding only after verified identity and entitlement are available.
**Priority:** P1 (Blocking)
**Status:** BLOCKED — requires RELAY-02 and rendezvous decisions

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 R.3–R.4.
Prerequisites: RELAY-01 foundation and an approved, implemented RELAY-02 security
contract. A successful WSS upgrade alone never authorizes matching.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read spec R.1–R.4 and approved RELAY-02 design | [ ] |
| **DECIDE BEFORE CODE** | Freeze handshake and pairing semantics below | [ ] |
| **NO RAW TCP** | Forward WSS frames only | [ ] |
| **VERIFIED LEGS ONLY** | Unknown/unentitled legs never register or match | [ ] |
| **NO E2E INVENTION** | Payload encryption E.4 stays blocked | [ ] |

---

## PROBLEM STATEMENT

The approved direction says the relay matches entitled WSS legs and forwards
frames, but it does not define the rendezvous protocol. Target announcements,
identifiers, tenant binding, queue policy, duplicate handling, timeouts, close
behavior, and accounting ownership are product/security decisions. Hard-coding
any of them would invent a protocol engineers could mistake for approved.

---

## SCOPE BOUNDARY

```
DECISION SCOPE (must finish first):
  - Handshake message fields and versioning
  - Identity/tenant/target namespaces and binding
  - Pairing lifecycle, duplicate and queue policy
  - Match, idle, and handshake timeouts
  - Close/error and reconnect behavior
  - Frame/message limits and backpressure
  - RecordBytes ownership and exact byte accounting semantics

CONTINGENT IMPLEMENTATION SCOPE (only after approval):
  - internal/relay/match.go + match_test.go
  - internal/relay/forward.go + forward_test.go
  - internal/relay/ws.go admission-to-matcher wiring

OUT OF SCOPE:
  - No raw TCP forwarding
  - No unverified/config-supplied identity path
  - No operator metrics API or discovery protocol
  - No E2E encryption mechanism
```

---

## EXECUTION DIRECTIONS

### STEP 1: Approve rendezvous protocol

Produce an approved design covering every decision in the scope list. It MUST
state how the authenticated tenant/agent identity is bound to each requested
target and how cross-tenant requests are authorized. If any item is unresolved,
HALT without production changes.

### STEP 2: Implement matching (contingent)

After approval, implement only the approved data structures and transitions.
Matching MUST happen only after RELAY-02 verification and entitlement succeed.
No implicit default, queue, duplicate, or target policy is permitted.

### STEP 3: Implement frame forwarding (contingent)

Forward WSS messages according to the approved message-size, backpressure,
cancellation, and close semantics. Do not splice raw sockets. On every terminal
path, close accounting exactly once according to the approved lifecycle.

### STEP 4: Connect metering and idle activity (contingent)

Call `RecordBytes` according to the approved units/ownership so
`LastActivityAt` advances on real frames. Do not add or rename billing metrics;
RELAY-04 owns the externally visible observability contract.

### STEP 5: Test (contingent)

Tests MUST cover authenticated/entitled matching, tenant binding, denied matches,
duplicate/timeout/close behavior, bidirectional frames, exact byte accounting,
context cancellation, race safety, and zero leaked sessions. Use names derived
from the approved protocol rather than freezing names before approval.

Validation:

```bash
go build ./internal/relay/ ./cmd/relay/
go test -race ./internal/relay/
go test ./cmd/relay/
```

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | Rendezvous contract approved | All handshake/pairing/error decisions explicit |
| 2 | Security prerequisite satisfied | Verified identity + entitlement enforced |
| 3 | No invented protocol | Implementation matches approved design exactly |
| 4 | WSS frames forwarded | No raw socket splice |
| 5 | Lifecycle and metering exact | Race/error/cancel tests pass with no leaks |
| 6 | E2E blocker preserved | E.4 remains explicit and unimplemented |

---

## ROLLBACK PROCEDURE

For contingent implementation, remove newly created `match.go`, `forward.go`, and
their tests with `git rm -f`; restore only pre-existing modified files such as
`ws.go` from the sprint baseline. Downstream applied configuration or deployments
must be rolled back before reverting source.

---

## BLOCKERS / DEFERRED

- RELAY-02 authentication/credential/entitlement gate.
- Approved rendezvous protocol described above.
- E2E payload encryption (E.4).

---

**Created:** 2026-08-23
**Version:** 1.1
