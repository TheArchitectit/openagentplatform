# RELAY-05: Discovery Federation Architecture Decision Record

**Date:** 2026-08-24
**Status:** APPROVED

---

## 1. Data Model

### 1.1 Base record: AgentCard reuse

The discovery record reuses the existing `AgentCard` from the `a2a-agent-registry`
spec (`openspec/specs/a2a-agent-registry/spec.md` §1.1). Fields: ID, Name,
Description, Version, Framework, Endpoint, Capabilities, Tags, Skills,
Authentication. **No parallel data model is invented.**

### 1.2 Federation envelope

Each record is wrapped with a federation envelope for cross-relay propagation:

```
DiscoveryEnvelope {
  record      AgentCard    // the agent card (reuses existing model)
  provenance  Provenance   // who published, where, when
  visibility  Visibility   // who can see this record
  ttl         Duration     // time-to-live; must re-publish or record drops
  version     uint64       // monotonic version within origin relay
}
```

```
Provenance {
  origin_relay_id   string   // unique relay identifier (from relay cert CN)
  tenant_id         string   // owning tenant
  published_at      Timestamp// when the origin relay accepted this record
  publisher_agent   string   // agent ID that published (or "system" for operator)
  signature         bytes    // Ed25519 signature by origin relay's CA key
}
```

```
Visibility {
  scope        enum { tenant_private, tenant_allowlisted, global_public }
  allowlist    []string // tenant IDs allowed to resolve (when scope = allowlisted)
}
```

### 1.3 Namespace

- Agent IDs are globally unique (as in the existing registry spec §1.2).
- Each record is **owned** by the relay+tenant in its provenance. No two relays
  may originate records for the same agent; the origin relay is authoritative
  (see §3 Conflict Resolution).
- Queries are scoped by the caller's tenant identity (from mTLS, same as
  RELAY-02/04). The relay filters results by the visibility rules before
  returning them.

### 1.4 Provenance and signing

Every record published to the federation carries a signature by the origin
relay's CA key (the same Ed25519 CA used for agent certs and admin mTLS).
Consuming relays verify this signature against their CA pool. An unsigned or
invalidly signed record is rejected at ingestion — untrusted provenance is
never stored or forwarded.

### 1.5 Expiry: TTL + explicit withdraw

Records carry a `ttl` field (maximum 24 hours, default 1 hour). The origin
relay must re-publish before expiry or the record silently drops from all
peers. This is the self-cleaning mechanism: a dead relay's records age out
automatically.

Additionally, an **explicit withdraw** message can be issued by the origin
relay (or by an operator with `relay-admin` role) to immediately remove a
record. This is used for:

- Compromised agent takedown
- Operator-initiated deregistration
- Agent self-withdrawal

The withdraw message carries the same provenance (origin_relay_id, agent_id,
tenant_id, signature) and a monotonic version higher than the current record.
Peers apply the withdraw immediately and suppress any stale re-publish with a
lower version.

### 1.6 Replay prevention

Each publish/withdraw carries a `version` (monotonic per agent_id within the
origin relay) and a `published_at` timestamp. A consuming relay rejects any
record whose version is ≤ the stored version for that agent_id. This prevents
replay of stale or withdrawn records.

---

## 2. Federation Protocol

### 2.1 Transport: gRPC (D.2 resolved)

The discovery federation uses a dedicated gRPC service, as resolved in D.2.
This is **not** the NATS bus (explicit choice: standalone service).

Service definition:

```protobuf
service DiscoveryFederation {
  // Push: origin relay notifies peers of a new/updated/withdrawn record.
  rpc PushRecord(PushRequest) returns (PushResponse);

  // Pull: a relay requests the full record set from a peer (reconciliation).
  rpc PullRecords(PullRequest) returns (stream DiscoveryEnvelope);

  // Ping: health check for peer liveness.
  rpc Ping(PingRequest) returns (PingResponse);
}

message PushRequest {
  DiscoveryEnvelope envelope = 1;
  bool withdraw = 2;  // true = this is a withdraw, not a publish
}

message PullRequest {
  string requesting_relay_id = 1;
  uint64 since_version = 2;  // incremental pull: only records newer than this
}

message PushResponse {
  bool accepted = 1;
  string rejection_reason = 2;  // e.g. "version_too_low", "invalid_signature"
}
```

### 2.2 Authentication

gRPC connections between relay peers use mTLS with the platform Ed25519 CA
(the same CA used for agent certs and admin mTLS). Peer relay certificates
carry the CN `oap:relay:<relayID>` and are signed by the platform CA.

### 2.3 Synchronization model: Hybrid push+pull

**Push (low-latency path):** When a relay accepts a publish or withdraw from
an agent, it immediately pushes the record to all known peer relays via
`PushRecord`. This ensures sub-second propagation in the healthy case.

**Pull (self-healing path):** Every 5 minutes, each relay pulls the full
record set from a peer via `PullRecords`. This heals any missed pushes due to
network partitions, peer restarts, or transient failures. The `since_version`
field enables incremental pulls — only records newer than the last-seen
version are transferred.

**Startup reconciliation:** On startup, a relay performs a full pull from each
configured peer before accepting agent connections. This ensures cold-start
consistency.

### 2.4 Peer configuration

Peers are configured via the existing trust config mechanism (`-trust-config`
flag). The trust config is extended with a `federation` section:

```yaml
version: 1
federation:
  peers:
    - relay_id: "relay-west"
      endpoint: "relay-west.internal:8443"
    - relay_id: "relay-east"
      endpoint: "relay-east.internal:8443"
  pull_interval: 5m
  startup_reconcile: true
```

### 2.5 Failure handling

- A failed `PushRecord` is logged and retried on the next pull cycle. No
  immediate retry loop — the pull path is the reliability backstop.
- A failed `PullRecords` is logged and retried on the next pull interval.
- A peer that fails 3 consecutive pulls is marked unhealthy and skipped
  until the next successful ping.
- The `Ping` RPC is called every 30 seconds per peer to detect liveness.
  A peer that fails 3 consecutive pings is marked unhealthy.

### 2.6 Versioning

The gRPC service uses semantic versioning in the protobuf package name:
`oap.discovery.v1`. Breaking changes require a new major version; relays
negotiate the highest mutually supported version at connection time.

---

## 3. Conflict Resolution: Origin-Relay Authoritative

When two relays hold conflicting records for the same agent_id, the **origin
relay** (as identified by `provenance.origin_relay_id`) is authoritative. The
record from the origin relay always wins.

Mechanism:
1. A relay receiving a record checks `provenance.origin_relay_id`.
2. If the origin relay ID matches the local relay, the local copy is
   authoritative — ignore the incoming record.
3. If the origin relay ID is a peer, accept the peer's record and overwrite
   the local copy (if the version is higher).
4. If no provenance is present or the signature is invalid, reject the record.

During a network partition, each relay serves its last-known copy of the
origin relay's records. When the partition heals, the pull-reconcile path
resolves any divergence by deferring to the origin relay's current state.

---

## 4. Cross-Tenant Visibility

Cross-tenant discovery follows a **hybrid model** with three visibility scopes,
controlled per-record at publish time:

| Scope | Who Can Resolve | Use Case |
|-------|----------------|----------|
| `tenant_private` | Only the owning tenant | Internal agents not for external use |
| `tenant_allowlisted` | Owning tenant + explicitly listed tenants | B2B partner agents |
| `global_public` | All tenants | Public APIs, shared tools |

### 4.1 Entitlement grants (granular)

For `tenant_allowlisted` records, the `allowlist` field in the visibility
envelope names the tenant IDs that may resolve the record. This mirrors the
relay's existing I.1 entitlement model — cross-tenant access requires a
persisted grant naming both tenant IDs.

### 4.2 Operator allowlists (broader)

The MSP operator can configure per-tenant allowlists in the trust config,
allowing a tenant to resolve records from specific other tenants without
per-record grants. This is useful for tenant groups that collaborate broadly:

```yaml
tenant_allowlists:
  tenant-a:
    - tenant-b
    - tenant-c
  tenant-b:
    - tenant-a
```

If a record is `tenant_private`, the operator allowlist does NOT override it —
the publisher's explicit scope takes precedence.

### 4.3 Global public namespace (open)

Agents explicitly published with `scope: global_public` are visible to all
tenants on all relays. This is opt-in by the publisher — agents default to
`tenant_private` unless explicitly made public. Use case: public APIs, shared
tool agents, marketplace agents.

### 4.4 Resolution algorithm

When a relay resolves a discovery query for tenant X:

1. Collect all records on the local relay.
2. For each record:
   a. If `visibility.scope == tenant_private` and `provenance.tenant_id != X`:
      skip.
   b. If `visibility.scope == tenant_allowlisted` and `provenance.tenant_id != X`
      and `X not in visibility.allowlist` and `X not in operator_allowlist(provenance.tenant_id)`:
      skip.
   c. If `visibility.scope == global_public` or `provenance.tenant_id == X`:
      include.
3. Return included records, sorted by relevance (skill match, then version).

---

## 5. Privacy and Operator Surface

### 5.1 Authorization model

| Action | Who May Perform |
|--------|----------------|
| Publish record | Agent with mTLS identity in the record's tenant, or `relay-admin` operator |
| Withdraw record | Origin agent, or `relay-admin` operator |
| Resolve by skill | Any agent in a tenant permitted by the visibility rules |
| List all records | `relay-admin` operator (via admin API); `relay-operator` sees only permitted tenants |
| Inspect provenance | `relay-admin` operator |

### 5.2 Admin API integration

Discovery records are queryable through the existing admin API
(`internal/relay/admin.go`) with a new route:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/admin/discovery` | Role required | List discovery records (scoped by role/tenant) |

This route follows the same mTLS + role SAN enforcement as `/admin/metrics`.
`relay-admin` sees all records; `relay-operator` sees only tenants in their
SAN list. No new auth mechanism is introduced.

### 5.3 Audit

Every publish, withdraw, and cross-tenant resolution is logged with:
`agent_id`, `tenant_id`, `origin_relay_id`, `scope`, `action` (publish/withdraw/resolve),
and `result` (accepted/rejected). **No credential material or full card
contents are logged.**

---

## 6. Phase Boundary

### Phase 1: Local discovery (this sprint's successor)

- Local `DiscoveryRegistry` with in-memory store
- `Publish`, `Withdraw`, `Resolve` operations
- TTL enforcement and expiry reaper
- Visibility filtering per tenant
- Admin API route (`/admin/discovery`)
- gRPC service stub (unimplemented federation methods)

### Phase 2: Federation (future sprint)

- `PushRecord`, `PullRecords`, `Ping` implementation
- Peer configuration and mTLS dialing
- Hybrid push+pull synchronization
- Conflict resolution (origin-authoritative)
- Startup reconciliation

---

## 7. Implementation Successor

After this ADR is approved, a separate implementation sprint will:

1. Create `internal/relay/discovery.go` + `discovery_test.go` (local registry)
2. Create `internal/relay/discovery_grpc.go` + `discovery_grpc_test.go` (federation stub)
3. Add `/admin/discovery` route to `admin.go`
4. Wire into `cmd/relay` config and startup
5. Tests: publish, withdraw, TTL expiry, visibility filtering, cross-tenant
   rejection, provenance verification, admin query

No production `.go` or config changes are made in this decision sprint.
