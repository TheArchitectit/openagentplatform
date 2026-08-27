# Sprint RELAY-02: Issued Identity & Entitlement Decision Gate

**Sprint Date:** 2026-08-23 (Saturday)
**Sprint Focus:** Approve the authentication, issuance, revocation, trust, and
entitlement contract required before any relay admission code is implemented.
**Priority:** P1 (Blocking)
**Status:** APPROVED (decision gate frozen 2026-08-26; see ADR; build lands in RELAY-03)

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1 R.3 and §7.2 I.1–I.3.
Prerequisite: [RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md). RELAY-01 remains
fail-closed while this sprint is blocked.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read relay spec §7 and RELAY-00 blockers | [ ] |
| **DECISION ONLY** | No production admission/identity/entitlement code | [ ] |
| **FAIL CLOSED** | Unverified WSS legs remain unregistered | [ ] |
| **NO INVENTED CRYPTO** | Do not choose mTLS, tokens, signing, or key storage without approval | [ ] |
| **NO DEFAULT GRANT** | Unknown identity/entitlement must remain denied | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

The product outcome is approved: only platform-issued, entitled identities may
use the relay. The mechanism is not. Spec I.3 leaves identity presentation and
verification unapproved, and no contract exists for issuance, revocation, trust
source, entitlement records, tenant binding, or credential lifecycle. Implementing
an in-memory registry or trusting a config-provided identity would create a
spoofable security boundary and misrepresent the blocker as solved.

---

## SCOPE BOUNDARY

```
IN SCOPE:
  - Decision record for identity presentation and verification
  - Issuance, renewal, expiry, and revocation lifecycle
  - Trust source and persistence ownership
  - Entitlement schema, tenant/target binding, and default-deny behavior
  - Audit requirements and failure/close behavior
  - Approval evidence and updates to the planned spec/sprint contract

OUT OF SCOPE:
  - No internal/relay/identity.go or entitlement.go
  - No admission wiring in ws.go
  - No config-supplied or test-harness identity accepted in production
  - No matching, forwarding, discovery, or E2E encryption
```

---

## EXECUTION DIRECTIONS

### STEP 1: Freeze authentication decisions

Obtain explicit approval for all of the following; do not supply defaults:

1. How the client presents identity over WSS.
2. How the relay cryptographically verifies that presentation.
3. Which component issues credentials and binds them to tenant + agent.
4. Credential expiry, renewal, revocation, and compromise response.
5. Trust anchors, storage, rotation, and reload behavior.
6. Replay prevention and clock-skew behavior where applicable.

### STEP 2: Freeze entitlement decisions

Approve the entitlement record and evaluator contract:

1. Tenant and target namespaces and cross-tenant policy.
2. Default-deny behavior and grant/revoke lifecycle.
3. Persistence owner, consistency requirements, and cache behavior.
4. Whether entitlement is checked at admission, target request, match, or each.
5. Audit fields for allow/deny outcomes without leaking credential material.

### STEP 3: Freeze edge failure behavior

Define exact close/error behavior for unknown, expired, revoked, malformed, and
unentitled clients. All cases MUST close without `EstablishConnection` until the
verification and entitlement checks succeed.

### STEP 4: Update contracts only after approval

Record the approved choices in an ADR or approved design document, then update
spec I.3 and this sprint's successor implementation scope. If any decision remains
open, keep this sprint BLOCKED and do not begin RELAY-03.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | Authentication contract approved | Presentation + verification + replay behavior explicit |
| 2 | Credential lifecycle approved | Issuance through revocation and rotation explicit |
| 3 | Entitlement contract approved | Schema, persistence, checks, and default deny explicit |
| 4 | Failure behavior approved | Every rejection closes without registration |
| 5 | No mechanism invented | No production `.go` changes in this sprint |
| 6 | Downstream gate accurate | RELAY-03 remains blocked until approval is complete |

---

## ROLLBACK PROCEDURE

This sprint changes decision/spec documents only. Revert only the current sprint's
documentation commit if approval is withdrawn. Never rewrite prior history.

---

## CLOSEOUT (2026-08-26)

RELAY-02 is a DECISION-ONLY sprint (its own scope boundary forbids `.go`
changes). The full identity + entitlement contract is frozen in
`docs/design/RELAY_02_IDENTITY_ENTITLEMENT_ADR.md` and mirrored into spec
§7.2 I.1–I.3:

- **I.3 (auth mechanism):** layered mTLS (platform Ed25519 CA, principal
  `oap:<agentID>`, `RequireAndVerifyClientCert`) + signed bearer token
  (agentID|target|tenant|iat|exp|jti, Ed25519). mTLS = auth, token = authz.
- **Lifecycle:** issuance/renewal/expiry/revocation/replay all specified; CA
  key from secret/KMS, not in-memory.
- **Entitlement:** YAML grant schema, default-deny, checked at admission +
  symmetric grant at match, audit logs with no credential material.
- **Edge failure:** every rejection closes the leg without `EstablishConnection`.

Sprint flips BLOCKED → APPROVED. Admission wiring (the actual `.go`) is
RELAY-03's scope, now unblocked.

---

## BLOCKERS / DEFERRED

- RELAY-03 matching and forwarding is blocked until this gate passes.
- Operator APIs must separately resolve authentication and tenant visibility.
- Discovery protocol (D.2) and payload encryption (E.4) remain separate blockers.

---

**Created:** 2026-08-23
**Version:** 1.1
