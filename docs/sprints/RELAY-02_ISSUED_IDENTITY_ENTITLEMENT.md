# Sprint RELAY-02: Issued Identity & Entitlement

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Implement the issued-identity registry and entitlement
(authorization) enforcement so the relay admits only issued, entitled agents and
rejects unknown identities before any matching. Enables the "not an open
forwarder" property (spec §7.2).
**Priority:** P1 (Blocking)
**Estimated Effort:** 2-3 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.2 I.1–I.3. Decision gate:
[RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md). Builds on the admission hook
stubbed in [RELAY-01](./RELAY-01_BINARY_CONFIG_DEPLOYMENT.md).

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `ws.go` (RELAY-01 hook) + `relay.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No matching/forwarding (RELAY-03), no metering (RELAY-04) | [ ] |
| **PRODUCTION FIRST** | Identity/entitlement code before tests | [ ] |
| **TEST/PROD SEPARATION** | Tests in a dedicated file | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

A WSS listener (RELAY-01) registers any connection that completes a handshake —
an open forwarder in the making. The relay must instead admit only agents that
present an **issued identity** and are **entitled** to relay to a given target.
Without this stage, matching (RELAY-03) would relay to unvetted targets,
violating spec §7.2 I.1 ("not an open forwarder").

**Root Cause:** Identity issuance and entitlement policy do not exist; admission
currently trusts a successful handshake.

**Where:** `internal/relay/` (new `identity.go`, `entitlement.go`) + `cmd/relay/`
(trust-config consumption).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/identity.go         NEW  issued-identity registry store
  - File: internal/relay/identity_test.go    NEW  issue/lookup/unknown tests
  - File: internal/relay/entitlement.go      NEW  entitlement policy + evaluation
  - File: internal/relay/entitlement_test.go NEW  allow/deny/unknown tests
  - File: internal/relay/ws.go               MODIFY  wire admission hook (I.1/I.2)
  - File: cmd/relay/config.go + main.go      MODIFY  load TrustConfigPath (RELAY-01 flag)

OUT OF SCOPE (DO NOT TOUCH):
  - No WSS matching / frame forwarding        (RELAY-03)
  - No metering / observability endpoints     (RELAY-04)
  - No discovery federation                   (RELAY-05)
  - NO cryptographic verification of identity (spec I.3 is BLOCKED — see Step 3)
  - No changes to existing accounting methods (§1–§6)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Identity registry ---------------> issue + lookup issued identities
STEP 2: Entitlement policy --------------> authorize target/tenant access
STEP 3: Admission wiring -----------------> reject unknown/denied before matching
STEP 4: Tests ----------------------------> green
DONE:   Commit identity/entitlement ------> RELAY-03 can match only entitled pairs
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Issued-identity registry

**Action:** Create `internal/relay/identity.go`:

```go
type IssuedIdentity struct {
    IdentityID string   // platform-issued identifier
    TenantID   string
    AgentID    string
    IssuedAt   time.Time
}
type IdentityRegistry struct { mu sync.RWMutex; byID map[string]*IssuedIdentity }
func NewIdentityRegistry() *IdentityRegistry
func (r *IdentityRegistry) Issue(id IssuedIdentity) error  // rejects duplicates
func (r *IdentityRegistry) Lookup(identityID string) (*IssuedIdentity, bool)
```

- Semantics mirror the accounting core's patterns: in-memory behind an
  `RWMutex`, deterministic record shape, and the tenant/agent attribution
  required by §7.2 I.1. `Issue` rejects a duplicate `IdentityID`; `Lookup` of an
  unknown ID returns `false`.

### STEP 2: Entitlement policy

**Action:** Create `internal/relay/entitlement.go`:

```go
func (s *RelayService) Entitled(identity *IssuedIdentity, targetID, targetTenant string) bool
```

- Returns `true` only when the identity belongs to `targetTenant` (or an
  explicitly granted cross-tenant entitlement) AND is authorized to relay to
  `targetID`. There is NO default-grant: absent an explicit rule, entitlement is
  `false` (§7.2 I.2). Denial MUST be logged.

- Persistence: policy is loaded from `TrustConfigPath` at startup (in-memory);
  durable policy CRUD is out of scope (no persistence, matching the core's
  Known Limitations).

### STEP 3: Admission wiring + explicit crypto blocker

**Action:** Wire the RELAY-01 admission hook in `internal/relay/ws.go`:

- On WSS admission, resolve the presented `identityID` via `IdentityRegistry`.
  Unknown → reject and close the socket, never register (I.2, mirroring spec
  3.3 empty-id rejection). Not-entitled → reject.
- Only issued + entitled identities reach `EstablishConnection`.

**IMPORTANT — do NOT invent crypto.** HOW the presented `identityID` is
cryptographically bound/verified (mTLS cert identity, signed token, etc.) is
spec I.3 `[BLOCKED]`. In this sprint the identity is presented as an explicit
field from the trust config / test harness (dev wiring), and the
verification mechanism is left as a tracked blocker. If a sprint-member tries to
add signature verification, HALT and report.

### STEP 4: Tests

**Action:** Create `identity_test.go` + `entitlement_test.go` (extend `ws_test.go`).

Tests (exact names):
- `TestIdentityRegistry_IssueAndLookup`.
- `TestIdentityRegistry_IssueDuplicate_Errors`.
- `TestRelayEntitlement_SameTenantDenied` — an identity issued for tenant A is
  NOT entitled to a tenant-B target without an explicit grant.
- `TestRelayEntitlement_ExplicitGrant_Allowed`.
- `TestRelayWS_Admission_RejectsUnknownIdentity` — unknown `identityID` → socket
  closed, `ListConnections` empty.
- `TestRelayWS_Admission_RejectsNotEntitled`.
- `TestRelayWS_Admission_AcceptsIssuedEntitled`.

**Validation loop (max 3):**
```
go build ./internal/relay/ ./cmd/relay/
go test ./internal/relay/ -run 'Test(IdentityRegistry|RelayEntitlement|RelayWS_Admission)' -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Issue/lookup | `TestIdentityRegistry_IssueAndLookup` | Pass |
| 2 | No default grant | `TestRelayEntitlement_SameTenantDenied` | Untrusted cross-tenant denied |
| 3 | Explicit grant works | `TestRelayEntitlement_ExplicitGrant_Allowed` | Pass |
| 4 | Unknown rejected | `..._Admission_RejectsUnknownIdentity` | Socket closed, not registered |
| 5 | No crypto invented | Manual review | No signature mechanism in diff |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/identity.go internal/relay/entitlement.go internal/relay/ws.go cmd/relay/config.go cmd/relay/main.go
git rm -f internal/relay/identity_test.go internal/relay/entitlement_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Identity-presentation cryptography (spec I.3)** — HOW the issued identity is
  presented/verified is UNAPPROVED and MUST NOT be implemented here. Tracked.
- **Policy durability** — entitlement policy is in-memory (matches core).
- **Matching/forwarding** — RELAY-03; this sprint only admits.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-02 : issued identity + entitlement                          |
| CREATE:    internal/relay/{identity,entitlement}.go + tests       |
| WIRE:      ws.go admission hook (I.1/I.2); cmd/relay trust config |
| RULE:      no default grant — unknown/denied rejected pre-match   |
| BLOCKED:   identity crypto (I.3) — do not implement verification  |
| ROLLBACK:  checkout identity/entitlement/ws.go; rm test files     |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
