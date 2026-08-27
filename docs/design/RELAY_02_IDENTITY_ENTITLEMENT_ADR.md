# RELAY-02: Issued Identity & Entitlement — Approved Contract

**Status:** APPROVED (2026-08-26)
**Sprint:** RELAY-02 (decision gate)
**Builds on:** RELAY-00 (I.3 resolved → layered mTLS + bearer token)
**Spec:** `openspec/specs/a2a-relay/spec.md` §7.1 R.3, §7.2 I.1–I.3
**Implementation sprints:** RELAY-03 (admission wiring), RELAY-06 (E2E proof)

---

## 1. Authentication Contract (I.3 — frozen)

Two layers. mTLS proves *who* connects; the bearer token proves *authorization
to reach a target*.

### 1.1 Layer 1 — mTLS (transport authentication)

- **Presentation:** every agent connecting to the relay presents a client X.509
  certificate during the WSS/TLS handshake. The cert's SAN or CN carries the
  principal `oap:<agentID>` and is signed by the **platform Ed25519 CA** —
  the *same* CA that RMM-09 uses to sign SSH user-certificates
  (`internal/mesh/key_manager.go`). Reuse, do not fork, the CA.
- **Verification:** the relay terminates TLS with a `tls.Config` whose
  `ClientCAs` is the platform CA pool and `ClientAuth = RequireAndVerifyClientCert`.
  The handshake fails closed for any chain that does not verify, *before* the
  WebSocket upgrade begins. This is strictly stronger than the RELAY-01
  fail-closed no-op: unverified legs never reach HTTP.
- **Binding:** the verified principal `oap:<agentID>` becomes the connection's
  `SourceAgentID` and the tenant is derived from the cert (see §3.1).

### 1.2 Layer 2 — signed bearer token (entitlement authorization)

- **Presentation:** each rendezvous request (the first application message on
  the upgraded WebSocket) carries a short-lived token:
  `agentID | targetAgentID | tenantID | iat | exp`, signed (Ed25519) by the
  platform key. Encoding: base64url(payload).base64url(ed25519-sig).
- **Verification:** the relay checks signature against the platform public key
  (from `-trust-config`), that `exp > now` (with `±1m` clock skew tolerance),
  and that the token's `agentID` matches the mTLS principal (§1.1). A mismatch
  or missing token closes the leg.
- **Purpose:** mTLS = *authentication* (you are `oap:agent-A`); the token =
  *authorization* (you may relay **to** `agent-B`). Different questions; not
  redundant.

### 1.3 Replay prevention

- Token carries a single-use `jti` (random 16 bytes) cached in an in-memory
  LRU (TTL = token lifetime). A repeated `jti` is rejected. Relying on short
  TTL + `jti` is sufficient at relay scale; no server-side nonce issuance.
- mTLS sessions are themselves TLS-unique; replay of the *handshake* is not
  possible under TLS.

### 1.4 Credential lifecycle

| Phase | Owner | Contract |
|-------|-------|----------|
| Issuance | Platform CA service (out of relay scope; relay only *consumes* certs) | Agent cert minted with principal `oap:<agentID>`, bound to tenant, TTL ≤ 24h. |
| Renewal | Agent, before TTL expiry | Re-handshake with a freshly minted cert; no in-place renewal on the relay. |
| Expiry | Relay (enforced) | mTLS cert past `NotAfter` → handshake fails. Token past `exp` → rejected. |
| Revocation | Platform CA service | Revoked agent IDs pushed to the relay's trust-config / CRL feed; relay reloads on SIGHUP. (Threat-response: a compromised agent is de-certified at the CA; in-flight legs are closed on next entitlement re-check — see §2.4.) |
| Compromise response | Platform | CA private key in a secret/KMS (NOT in-memory, per `key_manager.go` note). Rotation invalidates all outstanding certs; re-keying is a planned operational event, not silent. |

---

## 2. Entitlement Contract (I.1 / I.2 — frozen)

### 2.1 Schema

An entitlement record (persisted by the platform, loaded by the relay from
`-trust-config` or a replica):

```yaml
version: 1
tenant_id: "<uuid>"
grant:
  - source_agent_id: "<agentID>"
    target_agent_id: "<agentID>"   # or "*" for any in-tenant target
    action: "relay"
```

The relay holds these in memory; reload on SIGHUP or file mtime change.

### 2.2 Default-deny

No entitlement record ⇒ **denied**. Unknown identities and denied entitlements
are rejected at admission (mirroring the empty-id rejection in `EstablishConnection`
3.3). There is no allow-by-default path.

### 2.3 Check points

- **At admission (R.3):** mTLS principal verified + token signature/claims
  verified + an entitlement grant for `source→target` exists. All three must
  pass before `EstablishConnection` is called.
- **At match (R.4):** the target leg must *also* present a valid mTLS + token
  and hold an entitlement for `target→source` (bidirectional grant, or a
  symmetric rule). A one-sided grant never matches.
- **Each frame (RELAY-04/RELAY-06):** entitlement is NOT re-checked per byte;
  the admission + match grants are authoritative for the session lifetime.

### 2.4 Tenant + target binding

- Tenant is taken from the mTLS cert's tenant claim (embedded at issuance), not
  from the request body. Cross-tenant relay requires an explicit grant naming
  both tenant IDs; absent that, cross-tenant is denied (per-tenant isolation 3.1).
- `ListConnections` already filters strictly by `TenantID`; entitlement
  enforcement is additive on top.

### 2.5 Audit

Every allow/deny is logged with: `connection_id` (post-establish), `tenant_id`,
`source_agent_id`, `target_agent_id`, `decision` (allow/deny), `reason`
(mtls_fail | token_fail | no_entitlement | ok). **No credential material is
logged** — never the cert, token, or signature.

---

## 3. Edge Failure Behavior (frozen)

Every rejection below closes the leg **without** `EstablishConnection`. Fail-
closed supersedes fail-open in all cases.

| Case | Behavior |
|------|----------|
| Unknown identity (no/invalid cert) | TLS handshake rejected; never upgrades. |
| Expired cert | Handshake rejected. |
| Revoked identity | Handshake rejected (after reload) or leg closed at match. |
| Malformed token | Leg closed immediately. |
| Expired token | Leg closed. |
| Token `agentID` ≠ mTLS principal | Leg closed (principal spoof attempt). |
| No entitlement `source→target` | Leg closed at admission. |
| No symmetric grant at match | One leg already admitted is closed; no match formed. |
| Trust-config reload failure | Keep last-good config; log error; do NOT fall back to allow-all. |

---

## 4. Consequence for Implementation

- **RELAY-03** wires the mTLS `ClientCAs` + `RequireAndVerifyClientCert` and the
  post-upgrade token check into `internal/relay/ws.go`, replacing the current
  close-without-register boundary.
- RELAY-02 itself ships **no Go code** — decision/spec only, per its scope
  boundary.
- The platform CA service that *issues* agent certs is out of relay scope; the
  relay only consumes the CA public key and verifies.

---

**Approved by:** architecture/security review (RELAY-00 gate populated 2026-08-25;
contract frozen 2026-08-26). **Supersedes:** any "BLOCKED" marking on I.3.
