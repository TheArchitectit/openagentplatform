# Sprint RELAY-06: Tenancy Hardening, Race Suite & Relay README

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Cross-cutting hardening of the RELAY-00..05 work: a race suite
over the full relay stack, network-edge validation coverage, a per-tenant
isolation check under load, and a relay-specific README. No new transport logic.
**Priority:** P2 (Normal)
**Estimated Effort:** 2 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §3 (isolation/limits), §7.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go`, `metrics_http.go`, existing test files | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | Hardening/tests/docs only — no production behavior change | [ ] |
| **TEST/PROD SEPARATION** | Hardening tests in a dedicated file | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

When RELAY-01..05 land, the relay gains concurrency (accept/forward/shutdown/
metrics) that the original pure-library tests never exercised. The remaining risk
is not correctness of any one method but interaction: data races between the
accept loop, forwarding pumps, `Shutdown`, the cleanup loop, and the metrics
handler — plus a guarantee that per-tenant isolation holds under load and that
empty identifiers are rejected at the network edge, not just in the library.

**Root Cause:** Single-threaded library tests do not cover the concurrent
transport surface added by RELAY-01..05.

**Where:** `internal/relay/` (new test files + `README.md`).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/relay_hardening_test.go NEW
    Change: race suite, load/isolation, edge-validation tests
  - File: internal/relay/README.md              NEW
    Change: relay usage, implemented transport status, remaining blockers

OUT OF SCOPE (DO NOT TOUCH):
  - No production `.go` logic changes (hardening is tests/docs only)
  - No edits to INDEX_MAP.md, HEADER_MAP.md, TOC.md, docs/sprints/INDEX.md
  - No project-plan or STATUS-field changes anywhere
  - No per-leg authentication or E2E encryption (blockers S.1/S.2 —
    document them, do NOT implement)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Race + interaction suite -----> concurrent surface is data-race free
STEP 2: Isolation + edge validation --> per-tenant + empty-ID enforced
STEP 3: Relay README -----------------> implementer/operator guide
STEP 4: Full validation ---------------> everything green under -race
DONE:   Commit hardening ---------------> series complete (blocks tracked)
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Race + interaction suite

**Action:** Create `internal/relay/relay_hardening_test.go`. Drive the full stack
concurrently (accept + forward + shutdown + metrics) and run under `-race`.

Tests (exact names):
- `TestRelayService_Hardening_RaceAcceptForwardMetrics` — N connections
  accepted/forwarding while a goroutine hits `/metrics` and another calls
  `Shutdown`; assert no `-race` report.
- `TestRelayService_Hardening_RaceCleanupDuringTraffic` — `CleanupIdleConnections`
  runs on a ticker while bytes flow and `RecordBytes` updates counters.
- `TestRelayService_Hardening_ConcurrentEstablish` — many concurrent
  `EstablishConnection` calls (same tenant) stay within `MaxConnections` and
  `ConnectionCount` is exact afterwards.

### STEP 2: Isolation + edge validation

- `TestRelayService_Hardening_IsolationUnderLoad` — tenants A/B interleaved;
  `ListConnections(A)` never contains B, `GetMetrics(A)` unaffected by B
  (spec 3.1, 4.3).
- `TestRelayService_Hardening_EmptyIdsRejectedAtEdge` — config-supplied empty
  tenant/src/dst at the accept path hits the `EstablishConnection` validation
  error (spec 3.3) and the socket is closed, not registered.

### STEP 3: Relay README

**Action:** Create `internal/relay/README.md` (< 300 lines). Content:
- What is implemented: accounting/control core (§1–§6) + transport as shipped
  by RELAY-01..05 (listener/TLS, forwarding, shutdown, admin endpoints).
- How to run the `cmd/relay` dev binary (RELAY-04 flags).
- Explicit status: per-leg auth (S.1) and E2E encryption (S.2) are NOT
  implemented — see `openspec/specs/a2a-relay/spec.md` §7. Boundaries: relay is
  intentionally absent from `cmd/server` (W8).
- Pointers to sprint docs RELAY-00..06.

### STEP 4: Full validation

**Validation loop (max 3):**
```
go build ./...                                 # (scoped to ./internal/relay ./cmd/relay if repo is large)
go vet ./internal/relay/ ./cmd/relay/
go test -race ./internal/relay/ ...
go test ./cmd/relay/ -v
```

**Decision Point:**
- [ ] Green (no race reports, all tests pass) → proceed
- [ ] A race or failure surfaces → fix the underlying issue, re-run; if it is a
      real bug in RELAY-01..05 logic, treat as a defect and fix in that sprint's
      file, then re-run. ROLLBACK if beyond this sprint's scope.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Race-free transport surface | `-race` on hardening suite | No reports |
| 2 | Isolation under load | `..._IsolationUnderLoad` | Zero cross-tenant leakage |
| 3 | Edge validation | `..._EmptyIdsRejectedAtEdge` | Socket closed, not registered |
| 4 | README written | `internal/relay/README.md` | < 300 lines, blockers stated |
| 5 | No scope drift | `git status` | Only test/docs + no .go logic diffs |

---

## ROLLBACK PROCEDURE

```bash
git rm -f internal/relay/relay_hardening_test.go internal/relay/README.md
git status
```

---

## BLOCKERS / DEFERRED

- **Per-leg authentication (spec S.1)** — documented in README; implementation
  requires a dedicated auth design. Not invented here.
- **E2E encryption (spec S.2)** — documented in README; requires a dedicated
  design. Not invented here.
- **Documentation maps / project plan** — deliberately NOT updated by this
  series (per instruction); a future documentation sprint owns those updates.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-06 : hardening + race suite + relay README                  |
| CREATE:    internal/relay/relay_hardening_test.go, README.md      |
| RULE:      tests/docs only — NO production .go logic changes      |
| BLOCKERS:  auth (S.1) design, E2E (S.2) design (readme-documented)|
| ROLLBACK:  git rm the test file + README                          |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
