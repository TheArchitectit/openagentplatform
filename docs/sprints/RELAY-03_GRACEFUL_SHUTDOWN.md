# Sprint RELAY-03: Graceful Shutdown & Relay Lifecycle

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Add a relay-wide shutdown method that stops the listener,
drains forwarding pumps, and closes every active connection through the existing
`CloseConnection`, plus a `StopCleanupLoop` that exits the reaper goroutine.
**Priority:** P1 (Blocking)
**Estimated Effort:** 1.5-2.5 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 T.5; contract §2.4
(`CloseConnection`) and §5.2 (`StartCleanupLoop`) reused unchanged.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` (CloseConnection, StartCleanupLoop) | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No new transport, no auth | [ ] |
| **PRODUCTION FIRST** | `lifecycle.go` before its test | [ ] |
| **TEST/PROD SEPARATION** | Tests in `lifecycle_test.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

A relay that accepts connections needs a defined stop path. Today the only
shutdown-related primitive is `StartCleanupLoop(ctx)`, which exits when its
context is cancelled but does not stop the listener or close active connections.
Without a `Shutdown`, an operator cannot drain a relay cleanly: active
connections leak until reaped, and forwarding goroutines (RELAY-02) have no
coordinated stop. This sprint adds the lifecycle that RELAY-04's binary will call.

**Root Cause:** Lifecycle coordination was never part of the accounting core.

**Where:** `internal/relay/lifecycle.go` (new) + `internal/relay/lifecycle_test.go`.

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/lifecycle.go      NEW
    Change: Shutdown(ctx), StopCleanupLoop(), drain/close-all orchestration
  - File: internal/relay/lifecycle_test.go NEW
    Change: shutdown-drains, close-all, loop-exit, no-leak tests
  - File: internal/relay/relay.go          ADDITIVE ONLY
    Change: track a cleanup-cancel func from StartCleanupLoop (see Step 2)

OUT OF SCOPE (DO NOT TOUCH):
  - No signal handling / binary wiring      (RELAY-04)
  - No changes to CloseConnection or StartCleanupLoop bodies
  - No per-leg authentication or E2E encryption (blockers S.1/S.2)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Shutdown(ctx) ---------------------> listener stops + conns drain
STEP 2: StopCleanupLoop() -----------------> reaper goroutine exits
STEP 3: Orchestrate with pumps -------------> forwarding drains (RELAY-02)
STEP 4: Tests + -race ---------------------> green
DONE:   Commit lifecycle --------------------> RELAY-04 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: `Shutdown(ctx)`

**Action:** In `internal/relay/lifecycle.go`:

```go
func (s *RelayService) Shutdown(ctx context.Context) error
```

`Shutdown` MUST, in order:
1. Stop the listener(s) created by `ListenAndServe`/`Serve` (RELAY-01) — track
   the active listener on the service so Shutdown can close it, or accept the
   listener as a shutdown parameter per signature choice.
2. Signal all active forwarding pumps (RELAY-02) via their shared context to
   stop copying and close their sockets.
3. For every `active` connection, call `CloseConnection` (spec 2.4) so per-tenant
   `ConnectionCount` decrements and close logging emits (spec 6.1). Use
   `ListConnections` per tenant or iterate the internal map under the mutex —
   do NOT re-derive tenants from elsewhere.
4. Return once the drain completes, or after `ctx` is done (bounded drain with
   the passed deadline/timeout).

**Note:** Do NOT delete connections from the map. `CloseConnection` marks them
`closed`; callers can still `GetConnection`. Preserve that observability.

### STEP 2: `StopCleanupLoop()`

`StartCleanupLoop(ctx)` already exits when its ctx is cancelled (spec 5.2). To
stop it deterministically without a caller-owned context, add an additive
tracking field to `RelayService`:

```go
cleanupCancel context.CancelFunc   // unexported, set inside StartCleanupLoop
func (s *RelayService) StopCleanupLoop() // calls cleanupCancel
```

Also have `Shutdown` invoke `StopCleanupLoop` so a full stop cancels the 5-minute
reaper.

### STEP 3: Orchestrate with forwarding

Ensure `Shutdown` and the RELAY-02 pump share cancellation semantics: the pump
already stops on ctx cancel (RELAY-02 Step 3). `Shutdown` MUST reuse the same
root ctx so there is a single cancel path — no duplicate mechanisms.

### STEP 4: Tests

**Action:** Create `internal/relay/lifecycle_test.go`. Reuse RELAY-01/02 test
helpers (TLS listener + `StartForwarding`).

Tests (exact names):
- `TestRelayService_Shutdown_ClosesAllActive` — after 2 active conns, `Shutdown`
  leaves both `Status == closed` and per-tenant `ConnectionCount == 0`.
- `TestRelayService_Shutdown_ListenerStops` — new connects fail after shutdown.
- `TestRelayService_Shutdown_DrainsForwarding` — mid-stream copy terminates;
  no goroutine leak under `-race`.
- `TestRelayService_Shutdown_BoundedByCtx` — with an already-cancelled ctx,
  `Shutdown` returns promptly (does not hang).
- `TestRelayService_StopCleanupLoop_Exits` — loop exits; idempotent when called
  twice.

**Validation loop (max 3):**
```
go build ./internal/relay/
go test -race ./internal/relay/ -run 'TestRelayService_(Shutdown|StopCleanupLoop)' -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Drains + closes all | `..._ClosesAllActive` | All conns closed, counts zeroed |
| 2 | Listener stops | `..._ListenerStops` | New dials fail |
| 3 | Bounded shutdown | `..._BoundedByCtx` | No hang on cancelled ctx |
| 4 | Loop exits | `..._StopCleanupLoop_Exits` | Goroutine returns |
| 5 | No leak + contract intact | `-race` + full `go test ./internal/relay/` | Pass |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/lifecycle.go internal/relay/relay.go
git rm -f internal/relay/lifecycle_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Signal handling (SIGINT/SIGTERM → Shutdown)** — owned by RELAY-04 binary.
- **Per-leg authentication / E2E encryption (spec S.1, S.2)** — not implemented.
- **Persistence** — connection/meter state remains in-memory; no commit expected.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-03 : graceful shutdown + lifecycle                          |
| CREATE:    internal/relay/lifecycle.go, lifecycle_test.go         |
| TOUCH:     relay.go (additive: cleanupCancel tracking)            |
| REUSE:     CloseConnection (2.4), StartCleanupLoop (5.2) verbatim |
| BLOCKERS:  signal wiring (RELAY-04), auth (S.1), E2E (S.2)        |
| ROLLBACK:  git checkout HEAD on lifecycle.go/relay.go; rm test   |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
