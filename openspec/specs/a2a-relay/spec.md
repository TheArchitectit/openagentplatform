# A2A Relay

> **Phase:** 6 (Commercial)
> **STATUS: PARTIAL**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/relay/`

---

## Description

The A2A Relay is the **managed, multi-tenant relay offering** for
Agent-to-Agent traffic. It is distinct from the self-hosted A2A gateway
(`a2a/`, spec `a2a-gateway`): the gateway is the protocol ingress a tenant
runs for their own agents (JSON-RPC / REST / gRPC bindings, task routing,
SSE fan-out), while the relay is the platform-operated intermediary that
lets agents in different tenants or networks reach each other without
direct connectivity.

As implemented, `internal/relay` is the relay's **accounting and control
core**: a `RelayService` tracks relay connections per tenant, enforces
per-tenant connection limits, meters relayed bytes for billing, and reaps
idle connections. All state is held in memory behind a `sync.RWMutex`.

The package was delivered in Sprint 6.3 (Story 6.3.4, commit `fcf49af`).
It is a service-layer library: the network listener, byte forwarding, and
leg authentication described in the package doc comment are **not yet
implemented**, and the package is not wired into any server binary.

## User Story

**As** a platform operator offering the managed relay,
**I want** per-tenant connection records with enforced connection limits
and byte-level usage metering,
**so that** I can isolate tenants from one another, cap relay usage, and
bill each tenant for the traffic relayed through the service.

---

## Requirements

### 1. Service Model and Configuration

1.1. The relay MUST be constructed with `NewRelayService(config
RelayConfig, log *slog.Logger)` (`internal/relay/relay.go`). A `nil`
logger MUST fall back to `slog.Default()`.

1.2. `RelayConfig` MUST carry `ListenAddr` (string), `TLSConfig`
(`*tls.Config`), `MaxConnections` (int), and `IdleTimeout`
(`time.Duration`).

1.3. The service MUST hold all state (connections and metrics) in
process memory guarded by a `sync.RWMutex`, so that concurrent
establish/close/meter calls are safe.

### 2. Connection Lifecycle

2.1. `EstablishConnection(ctx, tenantID, sourceAgentID, targetAgentID)`
MUST create a `RelayConnection` with a deterministic record shape:
`ID`, `TenantID`, `SourceAgentID`, `TargetAgentID`, `EstablishedAt`
(UTC), `BytesRelayed`, and `Status`.

2.2. New connections MUST be created in status `ConnectionStatusActive`
(`"active"`). The status vocabulary MUST additionally define `"closed"`
and `"error"` (`ConnectionStatus` constants).

2.3. Connection IDs MUST be unique per call and embed the tenant and
both agent IDs plus a nanosecond timestamp (format
`relay_{tenant}_{source}_{target}_{unixnano}`).

2.4. `CloseConnection(ctx, connectionID)` MUST mark an active connection
`closed`; closing an unknown connection MUST error, and closing a
connection that is not active MUST also error (no double-close).

2.5. `GetConnection(ctx, connectionID)` MUST return the connection or a
not-found error; `ListConnections(ctx, tenantID)` MUST return only that
tenant's connections.

### 3. Per-Tenant Isolation and Limits

3.1. Every connection MUST be attributed to exactly one tenant; listing
MUST filter strictly by `TenantID` so one tenant can never observe
another tenant's connections (enforced in
`TestRelayService_ListConnections`).

3.2. When `RelayConfig.MaxConnections` is greater than zero,
`EstablishConnection` MUST count only the calling tenant's `active`
connections and MUST reject a new connection with an error once the
tenant has reached the limit. The limit check MUST NOT count closed or
errored connections, and MUST NOT apply globally across tenants.

3.3. An empty `tenantID`, `sourceAgentID`, or `targetAgentID` MUST be
rejected by `EstablishConnection` with a validation error (covered by
`TestRelayService_EstablishConnection_Validation`).

### 4. Usage Metering

4.1. `RecordBytes(ctx, connectionID, bytes)` MUST accumulate relayed
bytes on the connection (`BytesRelayed`) and on the owning tenant's
aggregate metrics (`TotalBytesRelayed`); recording against an unknown
connection MUST error.

4.2. Per-tenant metrics MUST be maintained as `RelayMetrics`: active
`ConnectionCount`, `TotalBytesRelayed`, and lifetime `TotalConnections`.
Counters MUST be incremented on establish and `ConnectionCount` MUST be
decremented on close.

4.3. `GetMetrics(ctx, tenantID)` MUST return the tenant's metrics, and
for a tenant with no recorded activity MUST return a zeroed
`RelayMetrics` bearing that tenant's ID (never nil, never an error).

### 5. Idle Connection Reaping

5.1. `CleanupIdleConnections(ctx)` MUST close every active connection
whose inactivity since `LastActivityAt` exceeds `RelayConfig.IdleTimeout`,
decrementing the tenant's active connection count, and MUST return the number
of connections closed. `EstablishConnection` initializes `LastActivityAt` to
the establishment time.

5.2. `StartCleanupLoop(ctx)` MUST run the cleanup on a fixed 5-minute
ticker in a background goroutine that exits when the supplied context is
cancelled.

5.3. Idle reaping MUST log the count of closed connections only when at
least one connection was closed.

### 6. Observability

6.1. Establishing and closing connections MUST be logged with the
connection ID, tenant ID, and (for establish) source/target agent IDs;
close logging MUST include the connection's total `BytesRelayed`.

### 7. Planned Architecture & Security Decisions (PLANNED — NOT IMPLEMENTED)

> This section records the **approved architecture/security direction** for
> turning the accounting core into a managed relay. Every requirement here is
> `[PLANNED]` and is **not implemented**. Requirements 1–6 above remain the
> only implemented contract, so STATUS stays PARTIAL until the approved work
> ships. Anything **not listed here is UNAPPROVED** and MUST be treated as
> BLOCKED, never implemented. Tracked by sprint plan RELAY-00..RELAY-06
> (`docs/sprints/`); every sprint passes through an architecture/security
> decision gate before proceeding.

#### 7.1 Rendezvous & Transport (WSS)

R.1. `[PLANNED]` The relay MUST use **WebSocket Secure (WSS)** as its
rendezvous transport: agents connect to the relay over WSS and the relay
**matches** legs, rather than acting as a raw TCP forwarder. A plain TCP
listener that relays bytes directly between sockets is UNAPPROVED and MUST
remain unimplemented.

R.2. `[PLANNED]` A dedicated binary `cmd/relay` MUST exist, configured by
flags/environment (WSS listen address, trust config for the issued-identity
registry, per-tenant limits, idle timeout). It MUST NOT be wired into
`cmd/server` (W8 decision).

R.3. `[BLOCKED]` The WSS listener MUST terminate TLS and validate a presented
agent identity against the issued-identity registry (§7.2) before admitting a
rendezvous. Admission SHALL reuse `EstablishConnection` so per-tenant limits
(3.2) and validation (3.3) apply at the edge. Implementation is blocked until
the I.3 authentication design is approved; unauthenticated legs MUST be closed
without registration.

R.4. `[PLANNED]` Two admitted legs are matched by the relay only after the
entitlement check passes for the target (I.1); matched legs then exchange
frames through the relay. `RecordBytes` (4.x) and `LastActivityAt` idle reaping
(5.x) MUST run on real frames.

#### 7.2 Issued Identity & Entitlement

I.1. `[PLANNED]` An **issued-identity registry**: every agent connecting
through the relay MUST present an identity ISSUED by the platform, and the
relay MUST check **entitlement** (authorization to relay to a given
target/tenant) before matching. This is the "not an open forwarder" property.

I.2. `[PLANNED]` Unknown identities and denied entitlements MUST be rejected at
admission and never registered, mirroring the empty-id rejection (3.3).

I.3. `[PLANNED]` The cryptographic mechanism by which an identity is presented
and verified is **RESOLVED 2026-08-25 as layered mTLS + signed bearer token**:
agents present a client cert chained to the platform Ed25519 CA (principal
`oap:<agentID>`) at WSS admission (transport authentication, R.3), and each
rendezvous request carries a short-lived signed token (agent ID + target +
expiry) for entitlement (I.1/I.2). mTLS = authentication; token = authorization.
Reuses the RMM-09 Ed25519 CA + cert model.

#### 7.3 Discovery Federation

D.1. `[PLANNED]` The relay MUST expose capability/agent discovery and MUST
federate discovery records across relays so agents in different tenants or
networks can resolve each other.

D.2. `[PLANNED]` The discovery wire protocol and federation semantics are
**RESOLVED 2026-08-25 as a dedicated gRPC discovery service** with an explicit
federation handshake between relay instances. Standalone service (does not reuse
the NATS bus by choice); carries capability/agent records across tenants/networks.

#### 7.4 E2E / Private / Load Acceptance

E.1. `[PLANNED]` An **E2E acceptance stage** proving a full relayed session
across the approved stack (WSS + identity/entitlement + matching + metering).

E.2. `[PLANNED]` A **private relay mode**: a restricted deployment in which the
relay is not a general open forwarder — only issued, entitled clients are
admitted.

E.3. `[PLANNED]` A **load stage** validating per-tenant limits and metering
under concurrency.

E.4. `[PLANNED]` End-to-end ENCRYPTION so the relay cannot read payload secrets
is **RESOLVED 2026-08-25 as a blind forwarder**: agents establish session keys
out-of-band (WireGuard/SSH model from RMM-09); the relay only ever moves
ciphertext it cannot decrypt. Zero payload attack surface on the relay. The
load/E2E stage (RELAY-06) validates the relayed session end-to-end.

---

## Known Limitations

- **No network transport.** Despite `RelayConfig.ListenAddr` and
  `RelayConfig.TLSConfig`, the package contains no listener, no TLS
  termination, and no byte forwarding — `RecordBytes` is bookkeeping
  that callers must drive. The config fields are currently unused.
  The approved direction for this is recorded in §7 (PLANNED,
  tracked by RELAY-00..RELAY-06).
- **Parked: not wired into any binary (W8 decision).** Nothing under
  `cmd/` constructs a `RelayService`. The package stays a library with
  correct bookkeeping semantics; wiring it into a dedicated relay binary
  requires the missing network transport (below) and an authentication
  design, so it is deliberately left out of the main server process until
  both exist.
- **Doc-comment features unimplemented.** The package comment claims
  "Authentication on both legs (not open forwarder)" and "End-to-end
  encryption (relay cannot read secrets)"; neither exists in the code.
- **`ConnectionStatusError` is dead code** — declared and tested as a
  constant but never assigned by any state transition.
- **Idleness is now tracked by last activity** (fixed in the W8 commit):
  `RelayConnection.LastActivityAt` refreshes on every `RecordBytes` call
  and `CleanupIdleConnections` reaps on inactivity, not establishment age.
  A busy long-lived connection is no longer closed for being old.
- **No persistence.** All connection and metric state is lost on restart.
