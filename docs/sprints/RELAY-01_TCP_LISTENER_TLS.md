# Sprint RELAY-01: Managed Relay TCP Listener & TLS Termination

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Add the network listener/accept path to `internal/relay`,
terminating TLS from `RelayConfig.TLSConfig` and registering accepted
connections through the existing `EstablishConnection` so per-tenant
`MaxConnections` enforcement applies at the edge.
**Priority:** P1 (Blocking)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 T.1–T.3. The accounting
contract (§1–§6) MUST remain untouched.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` before editing | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No forwarding, no shutdown, no auth in this sprint | [ ] |
| **PRODUCTION FIRST** | `transport.go` written before its test | [ ] |
| **TEST/PROD SEPARATION** | Tests in `transport_test.go`, prod in `transport.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)
Baseline/roadmap: [docs/sprints/RELAY-00_TRANSPORT_ROADMAP.md](./RELAY-00_TRANSPORT_ROADMAP.md)

---

## PROBLEM STATEMENT

`internal/relay` exposes `RelayConfig.ListenAddr` and `RelayConfig.TLSConfig`
but nothing binds or accepts a connection; these fields are unused. This sprint
implements the transport edge: a listener, TLS termination, and registration of
accepted connections via the already-tested `EstablishConnection` path, so
connection-limit enforcement (spec 3.2) works on the network boundary.

**Root Cause:** The transport half of the package was never implemented (see
spec §Known Limitations).

**Where:** `internal/relay/` (new `transport.go`).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/transport.go        NEW
    Change: Serve/listen/accept + TLS termination + config validation
  - File: internal/relay/transport_test.go   NEW
    Change: listener, TLS, registration, limit-at-edge tests
  - File: internal/relay/relay.go            ADD ONLY (no edits to existing funcs)
    Change: (if needed) export a small helper for identifier wiring OR nil

OUT OF SCOPE (DO NOT TOUCH):
  - No byte forwarding / copying loops        (RELAY-02)
  - No shutdown / drain lifecycle             (RELAY-03)
  - No cmd/ binary wiring                     (RELAY-04)
  - No per-leg authentication or E2E encryption (blockers S.1/S.2 — do NOT invent)
  - No edits to existing relay.go methods — the accounting contract is frozen
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Add Serve + TLS listener ----------> binding works
STEP 2: Register accepted conns ------------> limit enforced at edge
STEP 3: Config validation ------------------> nil TLSConfig rejected
STEP 4: Tests + -race ---------------------> green
DONE:   Commit transport + test -------------> RELAY-02 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Listener and TLS termination

**Action:** Create `internal/relay/transport.go`.

```go
func (s *RelayService) Serve(ctx context.Context, ln net.Listener) error
func (s *RelayService) ListenAndServe(ctx context.Context) error // binds config.ListenAddr
func (s *RelayService) acceptLoop(ctx context.Context, ln net.Listener) error
```

- `ListenAndServe` MUST fail configuration validation with a clear error when
  `s.config.TLSConfig == nil` (managed offering never serves plaintext — spec
  T.2). Wrap `ln` in `tls.NewListener(ln, s.config.TLSConfig)` unless the
  caller already supplied a TLS listener (`tlsListener`, `*tls.Listener`).
- The accept loop retries temporary accept errors (`errors.Is(err, syscall.ECONNABORTED)`-style
  or `net.Error().Temporary()`) with a short backoff, never exiting on a
  transient error; a permanent error or `ctx.Done()` returns.

### STEP 2: Register accepted connections at the edge

**Action:** In the accept loop, register each accepted connection:

```go
conn, err := s.EstablishConnection(ctx, tenantID, srcAgentID, dstAgentID)
```

- Derive `tenantID`/`srcAgentID`/`dstAgentID` from the values passed to
  `Serve` for a single-tenant development wiring. **Do NOT derive them from the
  wire** — that is the auth design (spec S.1) and is OUT OF SCOPE (blocker).
- Honour the returned error: connection-limit-reached / validation errors
  (spec 3.2, 3.3) MUST close the accepted socket without registering, and be
  logged (spec 6.1 logging fields: connection/tenant/source/target as available).
- Keep the accepted `net.Conn` on the `RelayConnection` via a private
  `c.conn net.Conn` field added in `TransportConfig`/struct (see Step 2 note:
  wire the socket alongside registration so RELAY-02 can copy on it).

**Note:** To let RELAY-02 forward bytes, attach the accepted `net.Conn` to the
registered connection. Add an unexported `netConn net.Conn` field to
`RelayConnection` (additive; the JSON/marshaled contract is unchanged because the
field is unexported).

### STEP 3: Tests

**Action:** Create `internal/relay/transport_test.go`. Generate self-signed TLS
certs at test time with `crypto/tls` + `crypto/x509` (test-only helper; never
commit private keys).

Tests (exact names, mirroring repo style):
- `TestRelayService_ListenAndServe_RejectsNilTLS` — nil `TLSConfig` returns
  config error, does not bind.
- `TestRelayService_Serve_TLSAcceptAndRegister` — `tls.Dial` succeeds; connection
  is visible via `ListConnections(tenant)` and `Status == active`.
- `TestRelayService_Serve_RejectsConnectionLimitAtEdge` — tenant at
  `MaxConnections`; next accepted connection triggers the limit error and the
  socket is closed (dial returns error / conn closed).
- `TestRelayService_Serve_TransientAcceptErrors` — inject a temporary accept
  error; loop retries and continues serving.

**Validation loop (max 3):**
```
go build ./internal/relay/
go test ./internal/relay/ -run 'TestRelayService_(ListenAndServe|Serve)' -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Listens + TLS | `TestRelayService_Serve_TLSAcceptAndRegister` | Pass |
| 2 | Nil TLS rejected | `..._RejectsNilTLS` | Pass |
| 3 | Limit at edge | `..._RejectsConnectionLimitAtEdge` | Pass |
| 4 | Existing contract intact | `go test ./internal/relay/` full | All prior tests still pass |
| 5 | No goroutine leak | `go test -race ./internal/relay/` | Pass |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/transport.go internal/relay/relay.go
git rm -f internal/relay/transport_test.go   # if committed previously
git status
```

---

## BLOCKERS / DEFERRED

- **Per-leg authentication (spec S.1)** — not implemented; identifiers are
  config-supplied for single-tenant dev. Requires a separate auth design.
- **E2E encryption (spec S.2)** — not implemented. Requires a separate design.
- **TLS test certificates** — generated at test time only; none committed.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-01 : TCP listener + TLS termination                         |
| CREATE:    internal/relay/transport.go, transport_test.go        |
| TOUCH:     relay.go (additive only: netConn field)               |
| BLOCKERS:  auth (S.1), E2E (S.2), TLS certs                      |
| ROLLBACK:  git checkout HEAD -- internal/relay/transport.go ...  |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
