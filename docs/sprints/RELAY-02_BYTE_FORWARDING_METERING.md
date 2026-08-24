# Sprint RELAY-02: Bidirectional Byte Forwarding & Metering

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Add the two-goroutine byte-forwarding pump for a relayed
connection, driving the existing `RecordBytes` so metering (spec 4.x) and idle
reaping (spec 5.x) run off real traffic.
**Priority:** P1 (Blocking)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 T.4; accounting
contract §4–§5 MUST remain untouched.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` + `relay_metrics_test.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No listener changes (RELAY-01), no auth | [ ] |
| **PRODUCTION FIRST** | `relay_forward.go` before its test | [ ] |
| **TEST/PROD SEPARATION** | Tests in `relay_forward_test.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

A relayed connection (registered in RELAY-01) has no data path: bytes are never
copied between the two legs, so `BytesRelayed` and `TotalBytesRelayed` never
move for real traffic and `LastActivityAt` never refreshes under load. This
sprint implements the forwarding pump that turns accepted connections into a
live relay while reusing the metering/idle logic verbatim.

**Root Cause:** The forwarding half of the transport was never implemented.

**Where:** `internal/relay/relay_forward.go` (new).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/relay_forward.go      NEW
    Change: bidirectional copy pump, deadlines, half-close, teardown fence
  - File: internal/relay/relay_forward_test.go NEW
    Change: end-to-end copy, metering, idle, no-leak tests
  - File: internal/relay/relay.go              ADDITIVE ONLY
    Change: expose a StartForwarding(connID) entrypoint + cancel registry (if needed)

OUT OF SCOPE (DO NOT TOUCH):
  - No changes to RecordBytes / GetMetrics / CleanupIdleConnections bodies
  - No listener/accept changes                (RELAY-01)
  - No graceful shutdown / drain              (RELAY-03)
  - No per-leg authentication or E2E encryption (blockers S.1/S.2 — do NOT invent)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Forwarding pump -------------------> A<->B byte copy works
STEP 2: Metering + idle hookup -------------> RecordBytes + LastActivityAt live
STEP 3: Teardown fence ---------------------> no leaked goroutines on stop
STEP 4: Tests + -race ----------------------> green
DONE:   Commit pump + test ------------------> RELAY-03 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Bidirectional copy pump

**Action:** Create `internal/relay/relay_forward.go`.

```go
func (s *RelayService) StartForwarding(ctx context.Context, connID string) error
func copyLeg(dst, src net.Conn, meter func(n int)) error // one direction
```

- `copyLeg` copies `src → dst` in a loop, calling the meter callback with the
  byte count of each successful `Write`. Use `io.CopyBuffer` with a fixed
  buffer and a `net.Conn`-backed writer, or an explicit read/write loop — either
  is acceptable; the meter MUST run on the `Write` side with the exact byte
  count so `RecordBytes` stays accurate.
- On EOF of one leg (`io.EOF`), half-close the peer direction
  (`CloseWrite`) so the other direction can finish (RFC-style half-close);
  do NOT tear the whole connection down until both directions end.

### STEP 2: Hook metering and idle

**Action:** Meter every write through the existing `RecordBytes`:
`RecordBytes(ctx, connID, n)` (spec 4.1). Because `RecordBytes` also refreshes
`LastActivityAt` (see `relay.go` W8 comment), idle reaping (spec 5.1) now runs
off live traffic with no change to `CleanupIdleConnections`.

Constraints:
- Unwritten paths MUST NOT partially meter: if `RecordBytes` errors (unknown
  conn), stop the pump and return the error.
- Do NOT call `EstablishConnection` here; RELAY-01 already registered the conn.

### STEP 3: Teardown fence

- `StartForwarding` MUST be idempotent per connection (a second call on the
  same `connID` errors or becomes a no-op — pick one and test it).
- On ctx cancellation or a permanent copy error, both directions stop and the
  connection's socket is closed via the registered `netConn`. The connection
  record itself is NOT closed here (RELAY-03 owns `CloseConnection`).

### STEP 4: Tests

**Action:** Create `internal/relay/relay_forward_test.go`. Use
`net.Pipe()` or real loopback (`net.Listen("tcp","127.0.0.1:0")`) pairs.

Tests (exact names):
- `TestRelayService_StartForwarding_BidirectionalBytes` — bytes written on leg A
  arrive on leg B and vice-versa.
- `TestRelayService_StartForwarding_MetersToTenant` — after copy, `GetMetrics`
  shows `TotalBytesRelayed > 0` and the conn `BytesRelayed` matches.
- `TestRelayService_StartForwarding_IdleDoesNotRefreshWithoutTraffic` — verify
  `LastActivityAt` only advances on traffic; `CleanupIdleConnections` reaps a
  quiescent conn using an artificially-short `IdleTimeout`.
- `TestRelayService_StartForwarding_UnknownConn_Errors` — `RecordBytes` not
  found path surfaces.
- `TestRelayService_StartForwarding_CtxCancel_NoLeak` — cancel mid-stream;
  assert both directions stop (guarded by `-race` + goroutine count).

**Validation loop (max 3):**
```
go build ./internal/relay/
go test -race ./internal/relay/ -run TestRelayService_StartForwarding -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Bidirectional copy | `..._BidirectionalBytes` | Pass |
| 2 | Accurately metered | `..._MetersToTenant` | Bytes match exactly |
| 3 | Idle by activity | `..._IdleDoesNotRefreshWithoutTraffic` | Pass |
| 4 | No leak on cancel | `..._CtxCancel_NoLeak` + `-race` | Pass |
| 5 | Contract intact | full `go test ./internal/relay/` | Prior tests pass |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/relay_forward.go internal/relay/relay.go
git rm -f internal/relay/relay_forward_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Per-leg authentication (spec S.1)** — the pump carries frames between two
  already-accepted legs; it does not authenticate them. Do NOT add framing or
  auth semantics here. Blocked pending design.
- **E2E encryption (spec S.2)** — the pump forwards raw bytes; adding any
  encryption/decryption is a separate design. Do NOT invent one.
- **Graceful shutdown** — owned by RELAY-03 (`CloseConnection`/drain).

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-02 : bidirectional forwarding + metering                    |
| CREATE:    internal/relay/relay_forward.go, relay_forward_test.go |
| REUSE:     RecordBytes (4.1), CleanupIdleConnections (5.1) verbatim|
| BLOCKERS:  auth (S.1), E2E (S.2), shutdown (RELAY-03)            |
| ROLLBACK:  git checkout HEAD on the 2 go files; rm the test file |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
