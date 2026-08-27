# Event Bus — NATS Client & Dispatcher

> **Phase:** 0 (Foundation) — shared messaging substrate
> **STATUS: COMPLETE** — NATS client, heartbeat decode, check dispatch, subject
> vocabulary all implemented and wired into the server binary
> **Source:** authored 2026-08-25 from code (`internal/events/`)
> **App Path:** `internal/events/`
> **Source files:** `internal/events/nats.go`, `internal/events/subjects.go`,
> `internal/events/heartbeat.go`, `internal/events/checkdispatcher.go`,
> `internal/events/tracing.go`

---

## Description

`internal/events/` is the platform's NATS client and dispatch layer. It owns the
connection lifecycle (`nats.go`), the subject vocabulary (`subjects.go`), the
heartbeat ingest pipeline (`heartbeat.go`), and the check-assignment dispatcher
(`checkdispatcher.go`). Every server-side subsystem that publishes to or consumes
from NATS routes through this package.

The client is a single long-lived connection with unlimited reconnects and
optional mTLS. `Publish` injects W3C trace context into message headers; `Subscribe`
and `SubscribeQueue` wrap handlers so each delivered message becomes a consumer
span linked to the producer span. Subscriptions are tracked so `Close()` can
drain them on shutdown.

## User Story

**As** a platform engineer,
**I want** the server to publish check assignments to agents and consume their
heartbeats and results over a single, reconnecting NATS connection,
**so that** I do not have to manage connection state in every subsystem.

---

## Requirements

### 1. Client Lifecycle

1.1. `NewClient(url, certFile, keyFile, caFile, log)` dials NATS with unlimited
reconnects, optional mTLS (client cert + CA), and handlers for disconnect,
close, and async errors. Returns `(*Client, error)`.

1.2. `IsConnected()` reports `CONNECTED` status; used by readiness probes.

1.3. `Close()` drains every tracked subscription and the underlying connection.
Safe to call multiple times; safe on a nil receiver.

1.4. `Conn()` exposes the underlying `*nats.Conn` for callers that need
low-level access.

### 2. Publish / Subscribe

2.1. `Publish(ctx, subject, payload)` sends a message and records a producer
span (`messaging.system=nats`, `messaging.destination=subject`,
`messaging.operation=publish`, `messaging.message.body.size`). Trace context is
injected into message headers via the global propagator. Returns an error if
not connected.

2.2. `Subscribe(subject, handler)` registers a handler for the literal subject;
`SubscribeQueue(subject, queue, handler)` joins a queue group for
load-balanced work distribution. Both wrap the handler with a consumer span
and track the subscription for drain on `Close()`.

2.3. **Known limitation:** `Publish`'s `ctx` is reserved for future use; the
underlying nats-go API is synchronous and does not respect context cancellation
in this client version.

### 3. Subject Vocabulary (`subjects.go`)

3.1. **Agent→server:** `oap.agents.<id>.heartbeat`, `oap.agents.<id>.results`.
3.2. **Server→agent:** `oap.agents.<id>.checks` (check assignments).
3.3. **Server→server:** `oap.events.agent` (lifecycle), `oap.events.alerts`
(alert transitions), `oap.events.checks.result` (new results), `oap.events.patches`
(patch lifecycle), `oap.events.scripts` (script lifecycle).

3.4. `SubjectCheckResultPrefix` is an alias for `SubjectCheckResultsPrefix`,
kept for backward compatibility with the ingest pipeline.

3.5. `HeartbeatStaleThreshold = 120s` — the duration after which a silent agent
is considered offline (a hint; consumers compute their own windows).

### 4. Heartbeat Ingest (`heartbeat.go`)

4.1. Decodes `oap.agents.<id>.heartbeat` messages into `models.Heartbeat` and
updates agent liveness in the store. The decode path is exercised by
`heartbeat_decode_integration_test.go`.

4.2. A silent agent beyond `HeartbeatStaleThreshold` transitions to offline;
this is the foundation for the offline-SLA alerting rule (RMM-01).

### 5. Check Dispatch (`checkdispatcher.go`)

5.1. `CheckDispatcher` publishes check assignments on `oap.agents.<id>.checks`
and consumes results. `CheckAssignmentSubject(agentID)` is the per-agent subject.

5.2. `IntervalSecs` is read from `CheckDefinition.IntervalSeconds` and propagated
to the agent; the dispatcher enforces the rmm-core §6.3 interval bounds.

5.3. The dispatcher previously also subscribed to `oap.agents.*.results` under a
separate alias; that path is deprecated in favour of `SubjectCheckResultPrefix`.

---

## Known Limitations

- **Synchronous publish.** `Publish` does not await acknowledgement; a slow
  broker can buffer messages in the client without surfacing an error.
- **No message ordering guarantees** across queue groups — `SubscribeQueue`
  distributes work by subject hash, so a single agent's messages may be
  processed out of order across workers.
- **HeartbeatStaleThreshold is a constant**, not configurable per-agent.

---

## Cross-References

- `observability-telemetry` — trace spans are produced here
- `data-model` — `Heartbeat` and `CheckResult` are the wire payloads
- `rmm-operations` §10.1 — sibling-subject naming convention
- `rmm-core` §6.3 — check interval bounds