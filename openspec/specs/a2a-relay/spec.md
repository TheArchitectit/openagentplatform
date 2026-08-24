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
whose age exceeds `RelayConfig.IdleTimeout`, decrementing the tenant's
active connection count, and MUST return the number of connections
closed.

5.2. `StartCleanupLoop(ctx)` MUST run the cleanup on a fixed 5-minute
ticker in a background goroutine that exits when the supplied context is
cancelled.

5.3. Idle reaping MUST log the count of closed connections only when at
least one connection was closed.

### 6. Observability

6.1. Establishing and closing connections MUST be logged with the
connection ID, tenant ID, and (for establish) source/target agent IDs;
close logging MUST include the connection's total `BytesRelayed`.

### 7. Planned Transport & Security Decisions (PLANNED — NOT IMPLEMENTED)

> This section records the **approved direction** for turning the accounting
> core into a network-facing managed relay. Every requirement here is marked
> `[PLANNED]` and is **not implemented**. Requirements 1–6 above remain the
> only implemented contract, so STATUS stays PARTIAL until this section's
> transport work ships. Nothing here authorizes a claim of implementation.
> Sub-sections are tracked as sprint plan RELAY-00..RELAY-06 (`docs/sprints/`).

#### 7.1 Transport

T.1. `[PLANNED]` A dedicated relay binary `cmd/relay` MUST construct
`NewRelayService` from a `RelayConfig` populated by flags/environment:
`ListenAddr`, `TLSConfig` (cert/key file paths), `MaxConnections`, and
`IdleTimeout`. The core is NOT wired into the existing `cmd/server`
process (kept separate per the W8 wiring decision).

T.2. `[PLANNED]` `internal/relay` MUST add a listener accept loop that
binds `ListenAddr` and terminates TLS using `RelayConfig.TLSConfig`
(`tls.NewListener`). A nil `TLSConfig` MUST fail configuration validation
for the managed offering rather than serving plaintext.

T.3. `[PLANNED]` Each accepted connection MUST be registered by the
existing `EstablishConnection` path so per-tenant `MaxConnections`
enforcement (3.2) applies at the network edge. Deriving the tenant/source/
target identifiers from the wire is part of the security design (S.2);
until then, a single-tenant development wiring supplies them from
configuration and multi-tenant leg attribution is a blocker.

T.4. `[PLANNED]` Each established connection MUST run a pairing of
forwarding goroutines that copy bytes in both directions between the two
legs, calling `RecordBytes` on every write so metering (4.x) and idle
reaping (5.x) — driven by `LastActivityAt` — continue to work unchanged.
Forwarding pairs MUST stop on EOF, read/write error, or context
cancellation, and MUST enforce `IdleTimeout`-derived deadlines.

T.5. `[PLANNED]` The relay binary MUST add a shutdown method that closes
the listener, drains forwarding goroutines, and closes every active
connection through `CloseConnection` before exiting on SIGINT/SIGTERM.

#### 7.2 Security

S.1. `[PLANNED]` Per-leg peer authentication so the relay is **not an open
forwarder** (the package comment's "Authentication on both legs"). The
specific mechanism (mTLS vs. token vs. another scheme) is **not decided
here** and MUST be the subject of a dedicated authentication design before
implementation; until then this remains a documented limitation, not a
feature.

S.2. `[PLANNED]` End-to-end encryption between the two communicating
agents so the relay cannot read secrets in flight (the package comment's
"End-to-end encryption (relay cannot read secrets)"). The specific
mechanism (session keys, frame pass-through, ratchet) is **not decided
here** and MUST resolve in a dedicated design; it is a blocker, not
something implemented by the transport sprints.

S.3. `[PLANNED]` All security properties that the operator actually
ships today build on the existing per-tenant accounting core (3.x):
tenant data isolation, connection-limit isolation, and bias-free
list/filter semantics. These are not conditional on S.1/S.2.

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
