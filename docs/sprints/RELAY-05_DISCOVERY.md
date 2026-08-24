# Sprint RELAY-05: Discovery Federation Decision Gate

**Sprint Date:** 2026-08-23 (Saturday)
**Sprint Focus:** Approve discovery namespaces, privacy, authorization,
provenance, expiry, conflicts, and federation semantics before implementation.
**Priority:** P2 (Normal)
**Status:** BLOCKED — spec D.2 is unresolved

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.3 D.1–D.2.
Prerequisites: RELAY-01..04 contracts must be approved and implemented.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read spec D.1/D.2 and approved identity/tenant contracts | [ ] |
| **DECISION ONLY** | No registry, federator interface, peer flags, or handler yet | [ ] |
| **PRIVACY FIRST** | Cross-tenant visibility requires explicit approval | [ ] |
| **NO PROTOCOL INVENTION** | No REST/gRPC/NATS/JSON federation choice | [ ] |
| **NO ADMIN LEAKAGE** | No discovery endpoint before operator auth decisions | [ ] |

---

## PROBLEM STATEMENT

Federated discovery is an approved outcome, but its contract is undefined. Even a
local registry or no-op `Federator` freezes record fields, lookup namespaces,
upsert behavior, expiry, conflict handling, and API shape. Those choices affect
privacy and authorization across tenants and relays and cannot be inferred from
the in-memory accounting core.

---

## SCOPE BOUNDARY

```
IN SCOPE:
  - Target/capability/tenant/relay namespaces
  - Publication and resolution authorization
  - Cross-tenant privacy and entitlement rules
  - Record provenance, signing/trust, expiry, withdrawal, and replay behavior
  - Conflict resolution and consistency expectations
  - Federation transport/wire semantics and failure handling
  - Operator visibility and audit requirements
  - Approved phase boundary: local discovery versus federation

OUT OF SCOPE:
  - No DiscoveryRecord/DiscoveryRegistry implementation
  - No Federator interface or peer flags
  - No discovery admin handler
  - No assumed transport or payload format
  - No E2E encryption mechanism
```

---

## EXECUTION DIRECTIONS

### STEP 1: Approve discovery data model

Define identifiers, required fields, ownership, visibility, provenance, expiry,
withdrawal, and conflict rules. Clarify whether capabilities and agents share a
namespace and how authenticated tenant identity constrains queries.

### STEP 2: Approve federation protocol

Choose and document transport, authentication, authorization, message/version
contract, synchronization model, retries, deduplication, partition behavior, and
revocation propagation. Do not infer these choices from other OAP transports.

### STEP 3: Approve privacy and operator surface

Define who can publish, resolve, list, and inspect records. Cross-tenant or
all-tenant discovery MUST be default-deny unless an approved entitlement permits
it. Reuse only an approved RELAY-04 operator-security contract.

### STEP 4: Define implementation successor

After approval, create a separate implementation sprint with exact files, APIs,
tests, migration/persistence boundaries, and rollback. Until then, RELAY-06 stays
blocked and no discovery seam is added.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | Data model approved | Namespace, provenance, expiry, conflicts explicit |
| 2 | Privacy approved | Publish/resolve/list authorization explicit |
| 3 | Protocol approved | Wire, trust, sync, failure, versioning explicit |
| 4 | Phase boundary approved | Local versus federated delivery stated |
| 5 | No mechanism invented | No production `.go` or config changes |

---

## ROLLBACK PROCEDURE

This sprint changes decision/spec documents only. Revert only its documentation
commit if approval is withdrawn; do not rewrite history.

---

## BLOCKERS / DEFERRED

- Discovery implementation remains blocked by D.2.
- RELAY-06 full-stack acceptance cannot claim federation until implementation
  from the approved successor sprint exists.
- Payload encryption E.4 remains independent and blocked.

---

**Created:** 2026-08-23
**Version:** 1.1
