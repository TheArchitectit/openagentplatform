# RELAY-03: Rendezvous Protocol — Approved Design

**Status:** APPROVED (2026-08-26)
**Sprint:** RELAY-03
**Prerequisites:** RELAY-01 (WSS listener) · RELAY-02 ADR (I.3 identity + entitlement)
**Spec:** `openspec/specs/a2a-relay/spec.md` §7.1 R.3–R.4

---

## 1. Handshake Message (the rendezvous request)

After the WSS upgrade completes, the connecting agent sends a single JSON
rendezvous message on the WebSocket. This is the Layer-2 token from the
RELAY-02 ADR §1.2:

```json
{
  "type": "rendezvous",
  "agent_id": "oap:<sourceAgentID>",
  "target_id": "oap:<targetAgentID>",
  "tenant_id": "<uuid>",
  "token": "<base64url-signed-bearer-token>",
  "jti": "<base64url-16-bytes>"
}
```

- `agent_id` MUST match the mTLS client cert principal (RELAY-02 §1.2). Mismatch
  → close leg.
- `target_id` is the agent the caller wants to reach.
- `tenant_id` is taken from the mTLS cert's tenant claim, not from this field
  (the field is informational; the relay never trusts the request body for
  identity or tenant).
- `token` is the signed bearer token (RELAY-02 §1.2); verified against the
  platform public key.
- `jti` is the replay-prevention nonce (RELAY-02 §1.3).

**Versioning:** `type` field. Future protocol changes add new `"type"` values.
Unknown type → close leg with error `unknown_rendezvous_type`.

**Direction:** only the connecting agent sends a rendezvous message. The relay
never sends one; it replies with a match/queue/error status (§3).

---

## 2. Identity and Tenant Binding

| Field | Source | Trust level |
|-------|--------|-------------|
| `agent_id` (principal) | mTLS client cert SAN/CN | Cryptographic (CA-signed) |
| `tenant_id` | mTLS client cert tenant extension | Cryptographic (CA-signed) |
| Entitlement grant | Trust config file (`-trust-config`) | File-based, reloaded on SIGHUP |
| Bearer token claims | Signed Ed25519 token | Cryptographic (platform key) |

The relay never reads `agent_id` or `tenant_id` from the JSON body for
authorization. The JSON values are informational; the mTLS cert is the trust
anchor. If the cert says `oap:agent-A` and the rendezvous says `agent_id:
"oap:agent-B"`, the leg is closed (principal spoof, RELAY-02 §3).

---

## 3. Pairing Lifecycle

### 3.1 States

```
LEG_ADDED → LEG_PENDING → LEG_MATCHED → (forwarding) → LEG_CLOSED
```

- **LEG_ADDED:** mTLS + token + entitlement verified. `EstablishConnection`
  called. The leg is registered and waiting for its counterpart.
- **LEG_PENDING:** the leg is queued. No counterpart exists yet.
- **LEG_MATCHED:** both sides of a pair exist and are entitled. Frame forwarding
  begins.
- **LEG_CLOSED:** normal close, error, idle timeout, or context cancellation.

### 3.2 Matching rule

A match occurs when two legs exist such that:

1. Leg A: `source=agentA, target=agentB, tenant=T`
2. Leg B: `source=agentB, target=agentA, tenant=T` (same tenant, reversed pair)

Cross-tenant matching requires an **explicit cross-tenant entitlement grant**
naming both tenant IDs (RELAY-02 §2.4). Absent that, cross-tenant is denied.

### 3.3 Duplicate policy

Only **one pending leg** per `(tenant, source, target)` triple. If a second leg
arrives for the same triple while the first is still pending, the first leg is
**closed** (`duplicate_leg`) and the second replaces it. This prevents stale
reconnects from blocking the pair.

### 3.4 Queue behavior

No queue depth. One pending leg per triple (replaced on duplicate). There is no
waiting list of multiple sources for the same target — the relay is a 1:1
matchmaker, not a pub/sub fan-out.

---

## 4. Timeouts

| Timeout | Duration | Effect |
|---------|----------|--------|
| Handshake timeout | 30s | Leg must send a valid rendezvous message within 30s of WSS upgrade or be closed. |
| Match timeout | 5m | If no counterpart arrives within 5 minutes, the pending leg is closed with `match_timeout`. |
| Idle timeout | `RelayConfig.IdleTimeout` (default 30m) | If no bytes flow for this duration after matching, the leg is reaped (existing `CleanupIdleConnections`). |
| Write deadline | 10s | Per-frame write deadline. If a write to one leg stalls for >10s, that leg is closed with `write_timeout`. |

---

## 5. Frame Forwarding

### 5.1 Message format

Binary WebSocket frames only. The relay does not inspect or parse payloads
(E.4 blind-forwarder model). Text frames are rejected and the leg is closed.

### 5.2 Message size

Max frame size: **1 MiB** (1,048,576 bytes). Larger frames cause the leg to be
closed with `message_too_large`.

### 5.3 Backpressure

Each matched pair uses two goroutines (A→B and B→A). If a write to the
opposite leg blocks (write deadline exceeded → §4), the source leg is also
closed. No buffering: the relay does not store frames in memory beyond the
in-flight write.

### 5.4 Close propagation

When one leg of a matched pair closes (read returns error, write deadline, or
idle reaping), the relay sends a Close frame to the opposite leg and closes it.
Both legs are always closed together.

---

## 6. Close/Error Behavior

| Event | Behavior |
|-------|----------|
| Normal WebSocket close | Relay sends Close to partner; both legs closed. |
| Read error on one leg | Close partner leg; close both. |
| Write timeout | Close both legs. |
| Match timeout | Close pending leg; no partner to notify. |
| Idle reaping | Close both legs of matched pair. |
| Context cancellation (server shutdown) | Close all legs; server shutdown drains in-flight writes. |
| Rendezvous validation failure | Close leg immediately; never registered. |

All close paths call `CloseConnection` on both legs exactly once. No double-close.

---

## 7. RecordBytes Ownership

- `RecordBytes` is called on every forwarded frame: `len(frame)` bytes on the
  **source** leg's connection (the leg that *sent* the frame). The source is
  the one whose bytes are being relayed.
- `LastActivityAt` is refreshed on both legs of the pair (both are active).
- Byte accounting is per-connection (not per-pair); each `RelayConnection`
  tracks its own `BytesRelayed`. In a matched pair A↔B, A's connection counts
  bytes A sent, and B's counts bytes B sent.

---

## 8. Race Safety

- The `RelayService.mu` mutex protects the `connections` map (existing).
- A new `matchMu` protects the pending/matched leg maps. It is never held while
  performing I/O (reading/writing WebSocket frames).
- `EstablishConnection` and `CloseConnection` are called under `matchMu` where
  needed, but frame forwarding runs outside the lock.

---

## 9. E2E Encryption (E.4 — NOT IMPLEMENTED)

Per the RELAY-00/RELAY-02 decision (E.4 → blind forwarder), the relay forwards
ciphertext frames without decrypting. No E2E encryption code is added in this
sprint. The blind-forwarder guarantee is structural: the relay never has the
session keys, so it cannot read payloads even if it wanted to.

---

**Approved:** architecture/security review (RELAY-00 gate + RELAY-02 ADR +
this design). Implementation may proceed.
