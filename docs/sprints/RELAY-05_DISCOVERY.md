# Sprint RELAY-05: Discovery (Federation)

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Add capability/agent discovery and federation so the relay can
publish what/how it serves and resolve peers across relays, enabling agents in
different tenants or networks to find each other (spec §7.3).
**Priority:** P2 (Normal)
**Estimated Effort:** 2 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.3 D.1–D.2. Decision gate:
[RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md). Prerequisites: RELAY-01..04.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `match.go` (RELAY-03) + `metrics_http.go` (RELAY-04) | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No new forwarding/identity logic | [ ] |
| **PRODUCTION FIRST** | Discovery code before tests | [ ] |
| **TEST/PROD SEPARATION** | Tests in a dedicated file | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

A relay can match legs and meter them, but nothing tells a caller which targets
are reachable, and there is no mechanism for relays to learn about each other
across tenant/network boundaries. Discovery addresses both: an internal
resolution API on the relay, and federation so a relay can register exchanges
with peer relays. This is the approved direction (spec D.1); the wire protocol is
a tracked blocker (spec D.2) and must NOT be invented here.

**Root Cause:** Discovery and federation were never part of the accounting core.

**Where:** `internal/relay/discovery.go` (new) + `cmd/relay/`.

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/discovery.go      NEW  discovery registry + federation hooks
  - File: internal/relay/discovery_test.go NEW  publish/resolve/federate tests
  - File: internal/relay/metrics_http.go   MODIFY (additive) handler for discovery
  - File: cmd/relay/config.go + main.go    ADDITIVE  -peer relays / -advertise knobs

OUT OF SCOPE (DO NOT TOUCH):
  - NO wire protocol / federation semantics invention (spec D.2 is BLOCKED —
    implement the local registry + hook seam only; leave the transport as a
    clearly-marked stub interface)
  - No changes to matching (RELAY-03), metering (RELAY-04) internals
  - No E2E encryption (spec E.4 BLOCKED)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Discovery registry ---------------> announce + resolve reachable targets
STEP 2: Federate hook --------------------> seam for peer relays (protocol TBD)
STEP 3: Expose via admin ------------------> discovery readable by operators
STEP 4: Tests ----------------------------> green
DONE:   Commit discovery --------------------> RELAY-06 stage-ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Discovery registry

**Action:** Create `internal/relay/discovery.go`:

```go
type DiscoveryRecord struct {
    TargetID    string
    TenantID    string
    Capabilities []string
    RelayID     string
}
type DiscoveryRegistry struct { mu sync.RWMutex; records map[string]*DiscoveryRecord }

func (s *RelayService) Publish(record DiscoveryRecord) error
func (s *RelayService) Resolve(targetID string) (*DiscoveryRecord, bool)
func (s *RelayService) ListCapabilities(tenantID string) []string
```

- Follows the core's in-memory + `RWMutex` pattern. `Publish` upserts a record;
  `Resolve` returns whether a target is reachable on this relay (feeds RELAY-03
  matching's "is the target known" path without changing matcher internals).

### STEP 2: Federation hook (seam only — no protocol)

Add an integration seam, NOT an implementation of a wire format:

```go
// Federator is the seam for cross-relay discovery. Its wire protocol is
// UNSPECIFIED (spec D.2 BLOCKED); only a local in-memory Federator or a no-op
// must exist now.
type Federator interface {
    Announce(context.Context, DiscoveryRecord) error
    Resolve(context.Context, string) (*DiscoveryRecord, error)
    Peers() []string
}
```

- Wire `DiscoveryRegistry` behind this interface. Default at startup is the
  in-memory no-op; a real remote federator is a later stage gated on the D.2
  design. Do NOT invent JSON/REST/gRPC federation payloads or semantics.

### STEP 3: Expose via admin

Add (additive to RELAY-04) an admin handler `GET /discovery?tenant=ID` returning
reachable targets/capabilities for an operator, from `ListCapabilities`/registry.
No new transport.

### STEP 4: Tests

**Action:** Create `discovery_test.go` (+ extend `metrics_http_test.go`).

Tests (exact names):
- `TestDiscoveryRegistry_PublishAndResolve`.
- `TestDiscoveryRegistry_ResolveUnknown_False`.
- `TestDiscoveryRegistry_ListCapabilities_ByTenant` — tenant A never sees
  tenant-B records (mirrors 3.1 isolation).
- `TestDiscovery_Seam_NoopFederator` — default federator is in-memory/no-op and
  does NOT attempt a network call.
- `TestDiscoveryMetrics_Admin_Endpoint` — `/discovery?tenant=X` returns JSON.

**Validation loop (max 3):**
```
go build ./internal/relay/ ./cmd/relay/
go test ./internal/relay/ -run 'TestDiscovery' -v
go test ./cmd/relay/ -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Publish/resolve | `..._PublishAndResolve` | Pass |
| 2 | Unknown false | `..._ResolveUnknown_False` | Pass |
| 3 | Tenant isolation | `..._ListCapabilities_ByTenant` | No cross-tenant leakage |
| 4 | No protocol invented | `..._Seam_NoopFederator` + review | Only the seam interface |
| 5 | Admin readable | `..._Admin_Endpoint` | JSON returned |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/discovery.go internal/relay/metrics_http.go cmd/relay/config.go cmd/relay/main.go
git rm -f internal/relay/discovery_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Discovery wire protocol / federation semantics (spec D.2)** — BLOCKED; this
  sprint ships only the local registry + `Federator` seam.
- **Cross-relay discovery** — requires the D.2 design after the seam exists.
- **E2E encryption (spec E.4)** — BLOCKED.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-05 : discovery (federation seam)                            |
| CREATE:    internal/relay/discovery.go, discovery_test.go         |
| ADMIN:     GET /discovery?tenant=<id> (additive to RELAY-04)      |
| SEAM:      Federator interface; default in-memory/no-op           |
| BLOCKED:   wire protocol + federation semantics (D.2) — do NOT    |
|            invent JSON/REST/gRPC discovery payloads               |
| ROLLBACK:  checkout discovery.go + admin files; rm test          |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
