# Sprint RELAY-04: Dedicated `cmd/relay` Binary & Wiring

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Add the dedicated `cmd/relay` binary that builds a
`RelayConfig` from flags/environment, constructs `NewRelayService`, runs the
listener + cleanup loop, and shuts down on SIGINT/SIGTERM — deliberately NOT
wiring the relay into `cmd/server`.
**Priority:** P1 (Blocking)
**Estimated Effort:** 2 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 T.1, T.2, T.5; §7.2
blockers S.1/S.2 remain out of scope.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `internal/relay/relay.go`, one existing `cmd/*/main.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No auth, no new relay logic in the binary | [ ] |
| **PRODUCTION FIRST** | `main.go` before config test | [ ] |
| **TEST/PROD SEPARATION** | `config_test.go` is build/test-only | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

`internal/relay` is not wired into any binary; the W8 decision deliberately keeps
it out of `cmd/server` until transport + auth exist. With RELAY-01..03 providing
listener, forwarding, and shutdown, a dedicated relay binary (`cmd/relay`) can
now construct and run a `RelayService` end-to-end while still numeric-tenancy
dev-scoped (identifiers from config, not the wire). This proves the wiring and
gives operators a runnable artifact without polluting the main server.

**Root Cause:** No standalone consumer of the relay service exists.

**Where:** `cmd/relay/` (new package).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create):
  - File: cmd/relay/main.go             NEW
    Change: flag/env config, service construction, run + signal shutdown
  - File: cmd/relay/config.go           NEW
    Change: parse RelayConfig from flags/flags; validate TLS
  - File: cmd/relay/config_test.go      NEW
    Change: config parse + validation tests
  - File: cmd/relay/main_test.go        NEW (optional)
    Change: smoke test (build + brief TLS serve)

OUT OF SCOPE (DO NOT TOUCH):
  - cmd/server, cmd/agent, cmd/team-cli — the W8 decision keeps the relay out
    of the main server binary; do NOT wire it there
  - internal/relay/* (RELAY-01..03 already landed; no edits here)
  - No per-leg authentication or E2E encryption (blockers S.1/S.2)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Config parsing --------------------> RelayConfig + validation
STEP 2: main.go run + signals -------------> serve + graceful stop
STEP 3: Dev tenancy wiring ----------------> single-tenant identifiers
STEP 4: Tests/build -----------------------> green
DONE:   Commit binary ----------------------> RELAY-05 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Config parsing

**Action:** Create `cmd/relay/config.go`:

```go
type flags struct {
    ListenAddr     string
    CertFile, KeyFile string
    MaxConnections int
    IdleTimeout    time.Duration
}
func (f *flags) relayConfig() (relay.RelayConfig, error)
```

- Flags/env: `-listen` (default `:7000`), `-cert`, `-key`,
  `-max-connections`, `-idle-timeout`. Use `flag` + manual env fallback.
- Validation: if `-cert` or `-key` is missing, error (managed offering requires
  TLS — spec T.2). `tls.LoadX509KeyPair` must succeed; build
  `relay.RelayConfig{TLSConfig: tlsConf, ...}`. If `MaxConnections <= 0` and
  `IdleTimeout <= 0`, log a warning but allow (library treats 0 as unlimited).

### STEP 2: Run + signal handling

**Action:** Create `cmd/relay/main.go`:

```go
cfg, _ := parseFlags()
svc := relay.NewRelayService(cfg, slog.New(slog.NewJSONHandler(os.Stderr, nil)))
svc.StartCleanupLoop(ctx)
go svc.ListenAndServe(ctx)   // RELAY-01
<-sigc(SIGINT, SIGTERM)       // os/signal.NotifyContext
svc.Shutdown(shutdownCtx)     // RELAY-03
```

- Signal handling via `signal.NotifyContext(context.Background(),
  os.Interrupt, syscall.SIGTERM)`. On signal, call `Shutdown` with a bounded
  context (e.g. 5s), then exit 0; a second signal is a hard exit.

### STEP 3: Dev tenancy wiring

For RELAY-01's identifier requirement (`tenantID`, `srcAgentID`, `dstAgentID`),
add explicit dev-only flags (`-tenant`, `-source-agent`, `-target-agent`,
defaults for local testing) that feed the accept path. **Do NOT derive these
from the wire** — that requires the auth design (spec S.1). Log clearly that
`cmd/relay` is single-tenant development wiring until S.1 lands.

### STEP 4: Tests + build

- `cmd/relay/config_test.go`: parse valid flags; missing `-cert`/`-key` errors;
  `relayConfig()` returns expected `RelayConfig` values.
- `cmd/relay/main_test.go` (optional): build-only smoke + TLS handshake against
  `ListenAndServe` using a generated self-signed cert (test-time only).

**Validation loop (max 3):**
```
go build ./cmd/relay/
go vet ./cmd/relay/
go test ./cmd/relay/ -v
go test ./internal/relay/   # unchanged contract still green
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Config parses/validates | `config_test.go` | Pass |
| 2 | Binary builds | `go build ./cmd/relay/` | Exit 0 |
| 3 | TLS serve smoke | `main_test.go` | Handshake succeeds |
| 4 | Missed TLS config errors | validation test | Error returned |
| 5 | Contract intact | `go test ./internal/relay/` | Prior tests pass |

---

## ROLLBACK PROCEDURE

```bash
git rm -rf cmd/relay
git status
```

---

## BLOCKERS / DEFERRED

- **Per-leg authentication (spec S.1)** — `cmd/relay` is single-tenant dev
  wiring with config-supplied identifiers; wire-derived identity requires the
  auth design. Do NOT invent.
- **E2E encryption (spec S.2)** — not implemented.
- **Wiring into `cmd/server`** — explicitly NOT done (W8 decision); the relay
  stays a dedicated binary.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-04 : dedicated cmd/relay binary                             |
| CREATE:    cmd/relay/main.go, config.go, config_test.go           |
| REUSE:     NewRelayService, ListenAndServe (R-01), Shutdown (R-03)|
| NOT DOING: wiring into cmd/server (W8); auth (S.1); E2E (S.2)     |
| ROLLBACK:  git rm -rf cmd/relay                                   |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
