# Sprint RELAY-01: Relay Binary / Config / Deployment Foundation (no forwarding)

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Stand up the `cmd/relay` binary, its config surface, and the
deployment foundation for a WSS listener — with **no forwarding** yet. This
sprint only establishes the runnable foundation + WSS listener skeleton; byte
matching/forwarding lands in RELAY-03.
**Priority:** P1 (Blocking)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 R.1–R.2. Accounting
contract §1–§6 untouched. Decision gate: [RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md).

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go`; model on an existing `cmd/*/main.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | NO forwarding, NO identity, NO metering endpoints here | [ ] |
| **PRODUCTION FIRST** | `main.go`/`config.go` before tests | [ ] |
| **TEST/PROD SEPARATION** | Tests in `config_test.go` + `ws_test.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

The relay has no runnable entrypoint. Before identity (RELAY-02), matching and
forwarding (RELAY-03), metering (RELAY-04), or discovery (RELAY-05) can attach,
there must be a `cmd/relay` binary with a config surface and a WSS listener
foundation that binds and upgrades but deliberately does not forward. This keeps
each stage reviewable and blocks a raw-TCP forwarder at the door (spec R.1).

**Root Cause:** No standalone consumer of the relay service exists; the W8
decision keeps it out of `cmd/server`.

**Where:** `cmd/relay/` (new package) + `internal/relay/` (WSS listener skeleton).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create):
  - File: cmd/relay/main.go                 NEW  flags/env → RelayConfig; run WSS listener
  - File: cmd/relay/config.go               NEW  parse + validate configuration
  - File: cmd/relay/config_test.go          NEW  config parse/validation tests
  - File: cmd/relay/main_test.go            NEW  build + start/stop smoke
  - File: internal/relay/ws.go              NEW  WSS listener + upgrade (admission hook stubbed)
  - File: internal/relay/ws_test.go         NEW  WSS handshake/upgrade tests
  - File: deploy/relay/ (new)               NEW  deployment foundation (systemd unit + README)

OUT OF SCOPE (DO NOT TOUCH — listed so we do not creep):
  - No byte forwarding / matching — this is RELAY-03. Accepted WSS conns are
    admitted (registered) but not matched/forwarded.
  - No identity issuance / entitlement    — RELAY-02
  - No metering/observability endpoints   — RELAY-04
  - No discovery federation               — RELAY-05
  - NO raw TCP listener/forwarder (spec R.1: UNAPPROVED).
  - No changes to existing internal/relay/*.go accounting methods.
  - No wiring into cmd/server             (W8).
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Config surface ---------------> RelayConfig from flags/env, validated
STEP 2: main.go + WSS listener --------> binds + upgrades WSS, no forwarding
STEP 3: Deployment foundation ---------> systemd unit + deploy README
STEP 4: Tests + build -----------------> green
DONE:   Commit foundation ---------------> RELAY-02 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Config surface

**Action:** Create `cmd/relay/config.go`:

```go
type Flags struct {
    WSSListenAddr  string   // e.g. :7000
    CertFile, KeyFile string // TLS for WSS
    TrustConfigPath string  // issued-identity trust config (consumed in RELAY-02)
    MaxConnections int
    IdleTimeout    time.Duration
}
func ParseFlags() (*Flags, error)
func (f *Flags) relayConfig() (relay.RelayConfig, error)
```

- Flags/env: `-listen`, `-cert`, `-key`, `-trust-config`, `-max-connections`,
  `-idle-timeout`.
- Validation: missing `-cert`/`-key` MUST error (WSS requires TLS — spec R.1);
  `tls.LoadX509KeyPair` must succeed. `RelayConfig` gains the fields needed by
  later stages as additive-only; do not remove any current field.

### STEP 2: `main.go` + WSS listener skeleton (no forwarding)

**Action:** Create `cmd/relay/main.go` and `internal/relay/ws.go`.

- `ws.go`: `func (s *RelayService) ServeWS(ctx context.Context, ln net.Listener,
  upgrader Upgrader) error` that accepts TCP, terminates TLS, and upgrades to a
  WebSocket. It registers each admitted connection via `EstablishConnection`
  (admission hook — identity/entitlement fills this in RELAY-02, so the hook is
  a clearly-marked TODO stub here).
- After upgrade, the connection is admitted/registered but **not matched or
  forwarded** — this is the deliberate boundary for RELAY-01.
- `main.go`: build config, `NewRelayService`, start `ServeWS` in a goroutine,
  wait on SIGINT/SIGTERM (`signal.NotifyContext`), then a bounded shutdown that
  stops the WSS listener. (Explicit connection drain/`CloseConnection` ownership
  moves into RELAY-03 alongside matching.)

**WebSocket dependency:** add a WSS stack (e.g. `github.com/coder/websocket` or
`nhooyr.io/websocket`) via the project's dependency governance:
[docs/standards/DEPENDENCY_GOVERNANCE.md](../standards/DEPENDENCY_GOVERNANCE.md).
If an allowed library is not available, HALT and report — do not hand-roll a
WebSocket parser.

### STEP 3: Deployment foundation

**Action:** Create `deploy/relay/systemd/relay.service` (or the project's IaC home
per [docs/standards/INFRASTRUCTURE_STANDARDS.md](../standards/INFRASTRUCTURE_STANDARDS.md))
plus `deploy/relay/README.md` documenting the three config knobs and the trust
config path. No real certificates committed — references only.

### STEP 4: Tests

**Action:** Create `config_test.go`, `main_test.go`, `ws_test.go`. Generate
self-signed TLS certs at test time (test-only helper; never commit keys).

Tests (exact names):
- `TestConfig_ParseAndValidate` — valid flags parse; missing `-cert`/`-key`
  errors.
- `TestRelayWS_ServeWS_UpgradesAndRegisters` — WSS client handshake succeeds;
  connection visible via `ListConnections(tenant)` as `active` (no forwarding).
- `TestRelayWS_ServeWS_HandshakeNoUpgrade_Rejected` — non-upgrade HTTP request
  rejected.
- `TestRelayBinary_Smoke_StartStop` — binary starts, upgrades, stops on signal.

**Validation loop (max 3):**
```
go build ./cmd/relay/ ./internal/relay/
go vet ./cmd/relay/ ./internal/relay/
go test ./internal/relay/ -run TestRelayWS -v
go test ./cmd/relay/ -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Config parses/validates | `TestConfig_ParseAndValidate` | Pass |
| 2 | WSS upgrades + registers | `..._UpgradesAndRegisters` | active, no forwarding |
| 3 | Non-WSS rejected | `..._HandshakeNoUpgrade_Rejected` | Pass |
| 4 | No forwarding proven | test asserts no frame relay | Matched legs = 0 |
| 5 | Contract intact | full `go test ./internal/relay/` | Prior tests pass |

---

## ROLLBACK PROCEDURE

```bash
git rm -rf cmd/relay deploy/relay
git checkout HEAD -- internal/relay/ws.go
git rm -f internal/relay/ws_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Identity admission (spec I.3 crypto)** — the admission hook stays a TODO
  stub; real identity/entitlement is RELAY-02, and the verification crypto is a
  dedicated-design blocker.
- **Matching/forwarding** — RELAY-03; deliberately absent here.
- **Raw TCP forwarder** — UNAPPROVED (spec R.1); must never be added.
- **WSS library availability** — HALT if no approved dependency.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-01 : binary/config/deployment foundation (NO forwarding)   |
| CREATE:    cmd/relay/{main,config,config_test,main_test}.go      |
|            internal/relay/ws.go + ws_test.go, deploy/relay/      |
| BOUNDARY:  admits WSS conns, does NOT match/forward              |
| BLOCKERS:  identity crypto (I.3) → RELAY-02; matching (RELAY-03) |
| REJECTED:  raw TCP forwarder (R.1); cmd/server wiring (W8)       |
| ROLLBACK:  git rm -rf cmd/relay deploy/relay; checkout ws.go     |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
