# Event Bus

> **Phase:** 1 (Core RMM) — server-side event backbone; subject taxonomy is
> normatively documented in the `rmm-core` spec §5
> **STATUS: PARTIAL** — the three components (`Client`, `HeartbeatHandler`,
> `CheckDispatcher`) are implemented and wired in `cmd/server`. The heartbeat
> decode contract and duplicate-result persistence gaps from the coverage
> audit are fixed (W1, W2): `models.Heartbeat` now decodes tolerant
> timestamps, and `CheckDispatcher` is the assignment publisher while
> `internal/checks.ResultIngestor` is the sole result-persistence owner.
> Remaining gaps are non-blocking (no JetStream replay semantics, unused
> `assignSub` seam, untracked consumer-span errors — see Known Limitations).
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/events/` (nats.go, heartbeat.go, checkdispatcher.go),
> wired by `cmd/server/main.go`, `cmd/server/server_init.go`,
> `cmd/server/server_start.go`

---

## Description

The Event Bus is the server-side NATS backbone of OpenAgentPlatform. Endpoint
agents publish heartbeats and check results on per-agent subjects; the server
consumes them on wildcard subscriptions, persists state changes, and re-fans
lifecycle and alert events onto `oap.events.*` subjects consumed by alert and
policy engines, the A2A bridges, and the dashboard WebSocket hub.

The capability is three components in `internal/events/`:

1. **`Client`** (`nats.go`) — an instrumented wrapper around `nats.Conn`:
   connect with optional TLS and unlimited reconnects, traced publish,
   wildcard and queue-group subscribe with subscription tracking, and
   drain-based shutdown.
2. **`HeartbeatHandler`** (`heartbeat.go`) — consumes
   `oap.agents.*.heartbeat`, persists agent liveness, and emits
   `AgentOnline` / `AgentOffline` lifecycle events, including a background
   sweeper that flips stale agents offline.
3. **`CheckDispatcher`** (`checkdispatcher.go`) — publishes check assignments
   back to agents on `oap.agents.{id}.checks`. It is assignment-publish-only:
   result consumption (persist → threshold/flap evaluation → lifecycle
   fan-out) is owned by `internal/checks.ResultIngestor`, so persistence has
   exactly one owner (remediation W2). The `CheckStore`/`AlertSink` parameters
   on its constructor are retained for API compatibility and ignored.

Subject constants are centrally declared in `nats.go`; consumers elsewhere
(`internal/checks`, `internal/alerts`, `internal/policy`, `internal/api`)
reference them rather than hard-coding subjects.

## User Story

**As** a platform operator,
**I want** agent heartbeats and check results flowing through NATS with
wildcard and queue-group subscriptions, lifecycle fan-out, and distributed tracing,
**so that** the dashboard shows accurate live endpoint status and downstream
engines (alerts, policies, A2A bridges) react to endpoint events without
polling.

---

## Requirements

### 1. NATS Client Lifecycle

1.1. `events.NewClient(url, certFile, keyFile, caFile, log)`
(`internal/events/nats.go`) MUST dial NATS with connection name
`openagentplatform-server`, unlimited reconnects
(`nats.MaxReconnects(-1)`), and MUST log reconnect, disconnect, close, and
async errors via `slog`.

1.2. When `certFile` and `keyFile` are both set the client MUST present a TLS
client certificate (`nats.ClientCert`); when `caFile` is set it MUST pin root
CAs (`nats.RootCAs`). Wiring reads these from `cfg.NATSURL` /
`cfg.NATSCertFile` / `cfg.NATSKeyFile` / `cfg.NATSCAFile`
(`cmd/server/main.go`); a connect failure MUST abort server startup.

1.3. Every subscription created via `Subscribe` / `SubscribeQueue` MUST be
tracked in the client's internal list. `Close()` MUST drain all tracked
subscriptions and then drain the connection; it MUST be safe to call
repeatedly and on a nil client.

1.4. `IsConnected()` MUST report the underlying connection status
(`nats.CONNECTED`) for readiness probes, returning false on nil client or
nil connection.

### 2. Distributed Tracing over NATS

2.1. `Publish` MUST start an OpenTelemetry producer span
(`openagentplatform/nats` tracer, `SpanKindProducer`) with
`messaging.system=nats`, `messaging.destination`, `messaging.operation`, and
message body size attributes, and MUST record publish errors on the span.

2.2. The producer MUST inject the W3C trace context into NATS message headers
via `NewHeaderCarrier` (a `propagation.TextMapCarrier` over `nats.Header`; a
nil header MUST be replaced with an empty one rather than panicking).

2.3. `Subscribe`/`SubscribeQueue` MUST wrap handlers so each delivered message
extracts the producer's trace context and starts a consumer span
(`SpanKindConsumer`) with source/destination/size attributes; the consumer
span's traceparent MUST be written back into `msg.Header["traceparent"]` in
W3C format (`00-<traceID>-<spanID>-<flags>`) so downstream handlers can join
the trace. Handler-level errors leave the span status unset (nats handlers
have no error return).

### 3. Subject Taxonomy

3.1. The package MUST declare the following subject constants (`nats.go`);
they MUST match the `oap.` taxonomy in the rmm-core spec §5:

| Constant | Subject | Direction (server view) |
|----------|---------|------------------------|
| `SubjectHeartbeatPrefix` | `oap.agents.*.heartbeat` | subscribe (heartbeat handler) |
| `SubjectCheckResultsPrefix` / alias `SubjectCheckResultPrefix` | `oap.agents.*.results` | subscribe (`internal/checks` ingestor only — W2: the dispatcher no longer subscribes) |
| `SubjectCheckAssignmentPrefix` | `oap.agents` | prefix for server→agent dispatch |
| `SubjectAgentEvents` | `oap.events.agent` | publish (lifecycle fan-out) |
| `SubjectAlertEvents` | `oap.events.alerts` | declared here; published by ingest pipeline and policy violations, subscribed by alert engine |
| `SubjectCheckResultEvent` | `oap.events.checks.result` | declared here; published by ingest pipeline, subscribed by policy engine |
| `SubjectPatchEvents` | `oap.events.patches` | declared here for the patch subsystem / dashboard hub |
| `SubjectScriptEvents` | `oap.events.scripts` | declared here for the script subsystem / dashboard hub |

3.2. Per-agent subjects MUST be derived as
`oap.agents.{agent_id}.heartbeat` (agent side:
`pkg/agent/heartbeat.go`), `oap.agents.{agent_id}.results`
(`pkg/agent/checks_helpers.go`), and
`oap.agents.{agent_id}.checks` (`CheckAssignmentSubject`,
`checkdispatcher.go`), consistent with rmm-core §5.1/§5.2.

3.3. Lifecycle events MUST be published on derived subjects
`oap.events.agent.online` and `oap.events.agent.offline`
(`emitLifecycle`: `SubjectAgentEvents + "." + lower(trim(eventType, "Agent"))`),
matching rmm-core §5.3.

3.4. The stale-agent threshold constant `HeartbeatStaleThreshold` (120s,
expressed in nanoseconds) MUST exist as a hint for callers; the sweeper
itself uses `time.Now().Add(-2 * time.Minute)`.

### 4. Heartbeat Ingest

4.1. `HeartbeatHandler.Start` MUST subscribe (plain, non-queue) to
`SubjectHeartbeatPrefix` and start the stale-agent sweeper goroutine; it MUST
fail if the client connection is nil. `Stop()` MUST unsubscribe, signal the
sweeper, and wait for it to exit.

4.2. The handler MUST extract the agent ID from the subject
(`agentIDFromSubject`: split on `.`, require `oap.agents.<…>.<suffix>` with
≥4 parts, join middle segments so dotted IDs survive); malformed subjects
MUST be logged and dropped.

4.3. The payload MUST be parsed as `models.Heartbeat` (agent_id, timestamp,
cpu_percent, mem_percent, disk_percent, uptime_secs, version). If the body's
`agent_id` is non-empty it MUST take precedence over the subject-derived ID.
A zero timestamp MUST default to `time.Now().UTC()`.

4.4. Each heartbeat MUST call `HeartbeatStore.UpdateAgentHeartbeat(ctx,
agentID, "online", timestamp, cpu, mem, disk)` under a fresh 5-second context.

4.5. The handler MUST read the agent's prior status via
`HeartbeatStore.GetAgent`; only when transitioning from a non-`online` status
MUST it record the agent in its in-memory online set and publish an
`AgentOnline` lifecycle event. Errors reading prior status MUST be treated as
"unknown" (still emit).

4.6. `emitLifecycle` MUST publish a JSON event
`{type, agent_id, timestamp(unix)}` plus `version`, `cpu_percent`,
`mem_percent`, `disk_percent` when the heartbeat payload is available.

### 5. Stale-Agent Sweeper

5.1. `sweepStale` MUST tick every 30 seconds and call
`HeartbeatStore.MarkStaleAgentsOffline(ctx, now−2min)`, flipping agents whose
`last_seen` is older than the threshold to `offline` (`internal/api/agent_store.go`).

5.2. Every agent ID returned by the sweep MUST be removed from the online set
and produce an `AgentOffline` lifecycle event on
`oap.events.agent.offline`.

5.3. The sweeper MUST exit on `Stop()` or context cancellation.

### 6. Check Assignment Dispatch

6.1. `AssignCheck(ctx, agentID, assignment)` MUST JSON-marshal the assignment
and publish it to `CheckAssignmentSubject(agentID)` =
`oap.agents.{agent_id}.checks`, the subject the endpoint agent's check
executor subscribes to (`pkg/agent/checks.go`).

6.2. `AssignCheckWithDefinition` MUST build the canonical `CheckAssignment`
payload `{type: "RunCheck", check_id, name, config, interval_seconds,
timeout_seconds, timestamp, org_id}` from a `models.CheckDefinition`
(nil config → empty map) and is the preferred entry point for API handlers.
A nil definition MUST be rejected.

### 7. Check Result Consumption (ingestor-owned)

7.1. Result consumption MUST have a single owner: `internal/checks.ResultIngestor`
subscribes `oap.agents.*.results` under the queue group `oap-check-ingest`
(`SubscribeQueue`) so replicas load-balance, parses each payload as
`models.CheckResult`, persists it via `CheckStore.InsertCheckResult` (the
plain-INSERT path), then runs threshold/flap evaluation, alerts, and lifecycle
fan-out on `oap.events.*`. This is the only subscriber that persists results
(remediation W2).

7.2. `CheckDispatcher` MUST NOT subscribe to the results subject. The
`CheckStore` and `AlertSink` parameters on `NewCheckDispatcher` are retained
only for API compatibility and are ignored; the dispatcher's duties are
publishing assignments (§6) and tolerating a nil client.

7.3. `CheckDispatcher.Start` MUST fail if the client connection is nil and
otherwise log that it is assignment-publish-only. `Stop()` MUST unsubscribe
the (reserved) assignment subscription when present, close the stop channel,
and wait on its WaitGroup without panicking.

### 8. Wiring & Startup Order

8.1. `cmd/server` MUST construct the heartbeat handler and dispatcher against
the shared NATS client and a pgx-backed agent store
(`events.NewHeartbeatHandler(natsClient, agentStore, log)`,
`events.NewCheckDispatcher(natsClient, agentStore, nil, log)` in
`cmd/server/server_init.go`). The dispatcher's `agentStore`/`nil` arguments
are inert compatibility parameters (W2); the result-persistence path is
constructed and started separately as the ingestor (8.2).

8.2. `Server.Start` (`cmd/server/server_start.go`) MUST start the heartbeat
handler before the check dispatcher, then the result ingestor, alert engine,
policy engine, and A2A bridges, so subscriptions are established before
agent traffic arrives.

---

## Known Limitations

- **Result persistence is exactly-once by construction but not idempotent.**
  Since remediation W2 the `oap.agents.*.results` subject has a single
  subscriber (`internal/checks.ResultIngestor`, queue `oap-check-ingest`);
  `CheckDispatcher` no longer consumes it. `InsertCheckResult` is still a
  plain INSERT (no upsert), so a redelivery or a future second subscriber
  would duplicate rows — idempotency is not enforced at the store level.
- **Ingestor vs policy-engine consumers differ.** The ingestor consumes the
  raw `oap.agents.*.results` wildcard (queue `oap-check-ingest`), while the
  policy engine subscribes the derived `oap.events.checks.result` subject
  (queue `oap-policy-engine`) — the two are intentionally chained, not
  competing consumers. No component uses the old `oap-check-evaluator` queue
  group name.
- **No per-message ack/backpressure semantics.** Consumption uses plain NATS
  core (no JetStream); messages lost while the server is down are not
  replayed.
- **`assignSub` field is reserved but never created**; the dispatcher keeps
  the stop/WaitGroup plumbing for an assignment-change subscription that does
  not exist yet.
- **Consumer span errors are untracked.** Because `nats.MsgHandler` has no
  error return, handler failures leave the consumer span status unset.
