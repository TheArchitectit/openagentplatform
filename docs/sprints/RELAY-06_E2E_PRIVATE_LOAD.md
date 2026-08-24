# Sprint RELAY-06: E2E / Private / Load Acceptance Stage

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Final acceptance stage: an end-to-end relayed session over the
approved stack, a private (non-open) relay mode, and a load/soak stage — plus a
gate that keeps E2E-payload-encryption blocked pending its design (spec §7.4).
**Priority:** P2 (Normal)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.4 E.1–E.4. Decision gate:
[RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md). Prerequisites: RELAY-01..05.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read the relay package summary + sprint docs RELAY-01..05 | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | Acceptance + private mode + load only | [ ] |
| **PRODUCTION FIRST** | Private-mode enforcement before its tests | [ ] |
| **DO NOT INVENT** | E2E encryption mechanism stays BLOCKED (spec E.4) | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

The stages built RELAY-01..05 in isolation; nothing yet proves the whole system
works as one relayed session, enforces that the relay is NOT an open forwarder in
a production-like restricted deployment, or establishes that per-tenant limits and
metering hold under concurrency. This stage closes that with an E2E acceptance
test, a private relay mode setting, and a load/soak suite. It explicitly does NOT
implement E2E payload encryption, which remains a tracked blocker (spec E.4).

**Root Cause:** End-to-end, production-behavior, and scale validation of the
approved architecture are missing.

**Where:** `internal/relay/` (test-only + a small `private` config mode) + `cmd/relay/`.

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/acceptance_test.go NEW  E2E session across the stack
  - File: internal/relay/privacy.go         NEW  private-mode enforcement (E.2)
  - File: internal/relay/privacy_test.go    NEW  private-mode tests
  - File: internal/relay/load_test.go       NEW  load/soak assertions (E.3)
  - File: cmd/relay/config.go + main.go     ADDATIVE -private toggle
  - File: internal/relay/README.md          NEW  operator + status guide

OUT OF SCOPE (DO NOT TOUCH):
  - NO E2E payload encryption (spec E.4 is BLOCKED) — the acceptance stage
    proves a session relayed in clear frames; it does NOT encrypt them
  - No changes to accounting methods (§1–§6)
  - No discovery protocol work               (recap: D.2 still BLOCKED)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Private mode ---------------------> restricted-only admission (E.2)
STEP 2: E2E acceptance --------------------> one full relayed session (E.1)
STEP 3: Load/soak -------------------------> limits + metering under load (E.3)
STEP 4: README + gate ---------------------> status + E.4 blocker noted
DONE:   Commit stage -----------------------> managed relay base is complete;
                                              E2E crypto remains a blocker
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Private relay mode (E.2)

**Action:** Create `internal/relay/privacy.go` + a `-private` toggle in `cmd/relay`.

```go
// PrivateMode is boolean config carried on RelayConfig (additive field).
func (s *RelayService) enforcePrivateMode(identity *IssuedIdentity) bool
```

- In private mode the relay MUST reject any admission that is not backed by an
  issued + entitled identity (RELAY-02). It is the enforcement that the relay is
  NOT a general open forwarder in restricted deployments. Rejected legs are
  closed and never registered.
- Non-private (dev) mode preserves RELAY-02 behaviour unchanged (still rejects
  unknown/denied, but permits the config-supplied dev identity path).

### STEP 2: E2E acceptance (E.1)

**Action:** Create `internal/relay/acceptance_test.go`. Drive one full session:

issue identities → start WSS listener (RELAY-01) → admit two entitled legs
(RELAY-02) → match (RELAY-03) → forward frames A→B and B→A (RELAY-03) → assert
metering (RELAY-04) reflects bytes → assert `ListConnections`/isolation → close.

Test name: `TestRelayAcceptance_FullSession`.

Gate note: this proves the approved stack end-to-end with **clear** frames;
encrypted frames are the E.4 blocker, not part of this test.

### STEP 3: Load / soak (E.3)

**Action:** Create `internal/relay/load_test.go`.

Tests (exact names):
- `TestRelayLoad_PerTenantLimitUnderConcurrency` — N concurrent sessions per
  tenant stay within `MaxConnections`; no cross-tenant limit bleed (spec 3.2).
- `TestRelayLoad_MeteringStableUnderPressure` — concurrent frame traffic leaves
  `TotalBytesRelayed` and per-conn `BytesRelayed` exact (4.1).
- `TestRelayLoad_SoakNoLeak` — a short soak (`-race`) with sessions churned; no
  goroutine leak, `ConnectionCount` returns to baseline.

### STEP 4: README + status

**Action:** Create `internal/relay/README.md` (< 300 lines): what is implemented
(accounting core §1–§6 + reference to RELAY-01..06), how to run the `cmd/relay`
dev binary, the `-private` mode, and an explicit status table that marks
per-leg-auth-verification crypto (I.3), E2E encryption (E.4), and the discovery
wire protocol (D.2) as NOT implemented.

**Validation loop (max 3):**
```
go build ./internal/relay/ ./cmd/relay/
go test -race ./internal/relay/ -run 'TestRelay(Acceptance|Load|Privacy)' -v
go test ./cmd/relay/ -v
```

**Decision Point:**
- [ ] Green → the managed-relay base series is complete; report that E.4 stays a blocker
- [ ] Red → fix, re-run (ROLLBACK if beyond scope)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Private mode enforced | `..._Privacy*` | Non-issued admission rejected in private mode |
| 2 | Full E2E session | `TestRelayAcceptance_FullSession` | Pass, clear frames |
| 3 | Limits under load | `..._PerTenantLimitUnderConcurrency` | No bleed, per-tenant exact |
| 4 | Metering exact under load | `..._MeteringStableUnderPressure` | No drift |
| 5 | No encryption invented | Manual review of diff | No crypto in diff; E.4 noted in README |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/privacy.go cmd/relay/config.go cmd/relay/main.go
git rm -f internal/relay/acceptance_test.go internal/relay/privacy_test.go internal/relay/load_test.go internal/relay/README.md
git status
```

---

## BLOCKERS / DEFERRED

- **E2E payload encryption (spec E.4)** — BLOCKED; this stage proves clear-frame
  sessions and documents the blocker. Do NOT implement crypto.
- **Identity-verification cryptography (spec I.3)** — BLOCKED; private mode uses
  the registry, not a cryptographic verification mechanism.
- **Discovery wire protocol (spec D.2)** — BLOCKED. 
- **Wiring into `cmd/server`** — NOT done (W8 decision); the relay is a dedicated
  binary.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-06 : E2E / private / load acceptance stage                  |
| CREATE:    internal/relay/{privacy,acceptance_test,load_test}.go |
|            README.md; cmd/relay -private toggle                  |
| PROVES:    full session (E.1), private mode (E.2), load (E.3)    |
| BLOCKED:   E2E crypto (E.4), identity crypto (I.3), discovery    |
|            protocol (D.2) — all documented, none implemented     |
| ROLLBACK:  checkout privacy.go + cmd/relay; rm stage files       |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
