# Sprint RELAY-03: WSS Matching & Frame Forwarding

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Implement the WSS rendezvous core: match two admitted legs
(it was only issued + entitled in RELAY-02) and forward frames between them
through the relay, aligning with `RecordBytes`/idle so metering (RELAY-04) runs
off real frames.
**Priority:** P1 (Blocking)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 R.3–R.4, §7.2.
Decision gate: [RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md).
Prerequisites: [RELAY-01](./RELAY-01_BINARY_CONFIG_DEPLOYMENT.md) listener,
[RELAY-02](./RELAY-02_ISSUED_IDENTITY_ENTITLEMENT.md) admission.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` (RecordBytes), `ws.go`, `entitlement.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No metering endpoints (RELAY-04), no discovery (RELAY-05) | [ ] |
| **PRODUCTION FIRST** | Matching/forwarding code before tests | [ ] |
| **TEST/PROD SEPARATION** | Tests in a dedicated file | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

The WSS listener admits issued, entitled legs (RELAY-01/02) but nothing joins two
legs into a live relay session or moves frames. This sprint adds the rendezvous
core: when a caller advertises a target and an entitled caller for that target is
connected, the relay matches them and forwards frames in both directions,
driving `RecordBytes` and `LastActivityAt` so the metering/idle machinery
(§4–§5) runs on real traffic. It is the heart of the "relay matches legs" design
(spec R.4) and explicitly replaces any raw-TCP forwarder idea (R.1).

**Root Cause:** The matching + frame-forward half of the approved architecture
is unimplemented.

**Where:** `internal/relay/` (new `match.go`, `forward.go`).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/match.go       NEW  rendezvous registry + matching logic
  - File: internal/relay/match_test.go  NEW  match/unmatch/single-leg tests
  - File: internal/relay/forward.go     NEW  bidirectional WSS frame forwarding
  - File: internal/relay/forward_test.go NEW relay A<->B frame + error tests
  - File: internal/relay/ws.go          MODIFY  hand admitted legs to the matcher

OUT OF SCOPE (DO NOT TOUCH):
  - No metering/observability endpoints        (RELAY-04)
  - No discovery federation                    (RELAY-05)
  - No E2E encryption of payloads (spec E.4 is BLOCKED — relay may read frames)
  - No handling of unentitled or unknown legs   (RELAY-02 owns admission)
  - No changes to existing accounting methods (§1–§6)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Rendezvous matcher ---------------> match entitled legs by target
STEP 2: Frame forwarding ------------------> bidirectional frame relay
STEP 3: RecordBytes + idle hookup ---------> metering runs off real frames
STEP 4: Tests + teardown ------------------> green, no leaked sessions
DONE:   Commit matching/forwarding --------> RELAY-04 metering on top
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Rendezvous matcher

**Action:** Create `internal/relay/match.go`:

```go
type Rendezvous struct { mu sync.RWMutex; waiting map[string]*Leg } // key: targetID
type Leg struct { conn *websocket.Conn; identity *IssuedIdentity; target string }

func (s *RelayService) AnnounceTarget(ctx, leg)           // offers a leg as reachable
func (s *RelayService) Match(ctx, requester *Leg) (*Leg, error)
func (s *RelayService) Unannounce(target string)
```

- A leg announces the target it wants to reach. `Match` pairs a requester with an
  announced target **only after** `Entitled(requester.identity, target, targetTenant)`
  passes (RELAY-02). No entitled match → `ErrNoPeer` (wait/queue per policy).
- Matching MUST run under the `RWMutex`; a target has at most one matched peer at
  a time and duplicates are rejected.

### STEP 2: Frame forwarding

**Action:** Create `internal/relay/forward.go`:

```go
func (s *RelayService) ForwardPair(ctx context.Context, a, b *Leg) error
```

- Bidirectional: a goroutine per direction copies WSS **frames** from one leg to
  the other. On one direction's EOF/error, half-close the peer direction; end the
  session when both close (mirrors the accounting core's half-close intent).
- Frames, not raw sockets: the relay forwards WebSocket messages, NOT a byte
  stream spliced between TCP sockets (spec R.1). A session ends on ctx
  cancellation or terminal error, and the connection record is marked via
  `CloseConnection` (RELAY-03 owns this lifecycle; the accounting method itself
  is reused, not edited).

### STEP 3: Metering + idle hookup

- Every frame written on either leg calls `RecordBytes(ctx, connID, n)` so
  `BytesRelayed` / tenant `TotalBytesRelayed` (4.1) and `LastActivityAt` (5.x
  idle) advance on real frames. If `RecordBytes` errors (unknown conn), stop that
  direction.
- Do NOT create new counters here — RELAY-04 adds the observability surface.

### STEP 4: Tests

**Action:** Create `match_test.go` + `forward_test.go`. Use two loopback WSS
client legs with issued identities (RELAY-02 helpers).

Tests (exact names):
- `TestRelayMatch_SameTarget_Matches`.
- `TestRelayMatch_NoEntitlement_NoMatch` — requester not entitled to target →
  `ErrNoPeer`.
- `TestRelayMatch_SingleTarget_SinglePeer` — second requester for a taken target
  waits/queues (assert policy).
- `TestRelayForward_BidirectionalFrames` — frames A→B and B→A arrive intact.
- `TestRelayForward_MetersBytes` — `GetMetrics` reflects frames relayed.
- `TestRelayForward_HalfCloseAndSessionEnd` — one leg EOF ends the session,
  `ConnectionCount` returns to baseline.
- `TestRelayForward_CtxCancel_NoLeak` — cancel mid-session; no goroutine leak
  under `-race`.

**Validation loop (max 3):**
```
go build ./internal/relay/ ./cmd/relay/
go test -race ./internal/relay/ -run 'TestRelay(Match|Forward)' -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Entitled legs matched | `TestRelayMatch_SameTarget_Matches` | Pass |
| 2 | Unentitled never matched | `TestRelayMatch_NoEntitlement_NoMatch` | ErrNoPeer |
| 3 | Bidirectional frames | `TestRelayForward_BidirectionalFrames` | Bytes intact both ways |
| 4 | Frames metered | `TestRelayForward_MetersBytes` | TotalBytesRelayed > 0 |
| 5 | No leak / contract intact | `-race` + full `go test ./internal/relay/` | Pass |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/match.go internal/relay/forward.go internal/relay/ws.go
git rm -f internal/relay/match_test.go internal/relay/forward_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **E2E payload encryption (spec E.4)** — BLOCKED; the relay forwards frames and
  can read them. Do NOT add encryption here.
- **Metering/observability surface** — RELAY-04 (only the accounting hooks land
  here).
- **Discovery federation** — RELAY-05 (matching here is within one relay).

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-03 : WSS matching + forwarding                              |
| CREATE:    internal/relay/{match,forward}.go + tests             |
| WIRE:      ws.go hands admitted legs to the matcher               |
| RULE:      match only if Entitled (RELAY-02); frames not sockets |
| HOOKS:     RecordBytes (4.1) + LastActivityAt (5.x) on frames    |
| BLOCKED:   E2E encryption (E.4) — relay may read frames          |
| ROLLBACK:  checkout match/forward/ws.go; rm the two test files  |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
