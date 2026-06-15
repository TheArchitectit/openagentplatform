# Endpoint API Architecture

> **Status:** Authoritative Design Document
> **Audience:** Engineers implementing or extending the OAP endpoint-api subsystem
> **Date:** 2026-06-15
> **Source Plan:** `docs/plans/MASTER_IMPLEMENTATION_PLAN.md` S5

---

## Table of Contents

1. [Overview](#1-overview)
2. [REST API](#2-rest-api)
3. [NATS Bus](#3-nats-bus)
4. [Agent Binary](#4-agent-binary)
5. [Check Execution Protocol](#5-check-execution-protocol)
6. [Real-Time Event Streaming](#6-real-time-event-streaming)
7. [gRPC Service](#7-grpc-service)
8. [API Versioning](#8-api-versioning)
9. [Rate Limiting](#9-rate-limiting)
10. [OpenAPI 3.1](#10-openapi-31)
11. [SDK Generation](#11-sdk-generation)
12. [Implementation Steps](#12-implementation-steps)

---

## 1. Overview

### 1.1 What the Endpoint API Subsystem Is

The Endpoint API is the agent-management surface of OpenAgentPlatform. It is the boundary between the central RMM core and the thousands of devices (Windows servers, Linux boxes, macOS workstations) that the platform monitors and controls. The subsystem comprises three components:

1. **`endpoint-api` server** (Go, Echo v4) -- the HTTP/gRPC service that accepts REST calls from the frontend, A2A gateway, and external automation.
2. **NATS JetStream bus** -- the real-time message fabric that connects the server to every deployed agent binary.
3. **`oap-agent` binary** (Go, cross-compiled) -- the lightweight daemon installed on every managed device.

The subsystem extends the existing `mcp-server` Go codebase, which already provides JWT validation, structured logging, and a NATS client wrapper. The extension adds Echo v4 for HTTP routing, gRPC for synchronous RPC, and a full agent-lifecycle state machine.

### 1.2 Dual-Transport Pattern

OAP uses two transports by design. Each has a specific purpose, and neither is sufficient alone.

| Transport | Direction | Purpose | Latency Target | Reliability |
|-----------|-----------|---------|----------------|-------------|
| REST/HTTP (Echo v4) | Client -> Server | CRUD on agents, checks, scripts, patches, events; auth; system queries | < 100 ms p99 | Synchronous request/response; TLS 1.3 |
| NATS JetStream | Server <-> Agent | Agent registration, heartbeat, command dispatch, check results, script output, real-time events | < 10 ms p99 | At-least-once; durable streams; ack/nack |
| gRPC (internal) | Service <-> Service | Synchronous RPC between OAP services (A2A gateway, secret service) | < 20 ms p99 | Streaming + unary; mTLS |

**Why both:** REST gives operators and the dashboard a familiar, debuggable interface. NATS JetStream gives agents a connection-oriented, reconnect-tolerant, queue-group-aware transport that survives intermittent network loss. gRPC gives internal services a strongly-typed, high-performance RPC layer with streaming.

### 1.3 High-Level Component Diagram

```
+--------------------------------------------------------------------------+
|                        EXTERNAL CLIENTS                                   |
|   Frontend (React)  |  CLI  |  A2A Gateway  |  Third-party Integrations  |
+----------+-----------+-------+-------+------+-----------------------------+
           |                   |       |      |
           | REST/JSON         |       |      | gRPC + mTLS
           v                   v       v      v
+----------+-------------------+-------+------+-----------------------------+
|                     ENDPOINT-API SERVER (Go)                               |
|  +---------------------------------------------------------------------+ |
|  |  Echo v4 HTTP Router                                                 | |
|  |  +-------+ +-------+ +-------+ +-------+ +-------+ +-------+        | |
|  |  | Agent | | Check | |Script | | Event | | Patch | | Auth  |        | |
|  |  | Group | | Group | | Group | | Group | | Group | | Group |        | |
|  |  +---+---+ +---+---+ +---+---+ +---+---+ +---+---+ +---+---+        | |
|  |      |         |         |         |         |         |             | |
|  |  +---+---------+---------+---------+---------+---------+---+         | |
|  |  |              Middleware Chain                            |         | |
|  |  |  Recovery -> Logging -> CORS -> RateLimit -> Auth        |         | |
|  |  +---+---------+---------+---------+---------+---------+---+         | |
|  +------+---------------------------------------------------------+     |
|  |  gRPC Server (:50051)   |  NATS Client (Publisher/Consumer)        | |
|  +---+-----------------+---+---+-------------+---------------+---------+ |
|      |                 |       |             |               |
+------+-----------------+-------+-------------+---------------+
       | gRPC            |       | NATS JetStream
       v                 v       v
+------+------+   +------+------+  +-------------------------------+
| Secret Svc  |   | A2A Gateway  |  |  NATS JETSTREAM CLUSTER      |
| (Go)        |   | (Go)         |  |  AGENTS  CHECKS  SCRIPTS     |
+-------------+   +-------------+  |  WINUPDATE                   |
                                    +-----------+-------------------+
                                                |
                                                | NATS msgpack/JSON
                                                v
                                    +-----------+-------------------+
                                    |  AGENT BINARIES (Go)         |
                                    |  Windows  Linux  macOS        |
                                    |  amd64 / arm64               |
                                    +-------------------------------+
```

### 1.4 Why This Design

| Concern | REST | NATS | gRPC |
|---------|------|------|------|
| Operator dashboard queries | Primary | -- | -- |
| Agent-to-server telemetry | -- | Primary (durable) | -- |
| Check/script command dispatch | Trigger (initiates) | Carries payload | -- |
| Service-to-service RPC | -- | -- | Primary |
| Event fan-out to many consumers | -- | Primary (subject wildcards) | -- |
| Offline resilience for agents | -- | JetStream durable + replay | -- |
| Strongly-typed contracts | OpenAPI schema | Protobuf messages | Protobuf |
| Human debugging | curl/browser | `nats sub` CLI | grpcurl |

---

## 2. REST API

All endpoints are served by the `endpoint-api` service on `:8080` (HTTP) and `:50051` (gRPC). REST routes are namespaced under `/api/v1`. Every endpoint requires a valid JWT bearer token unless marked otherwise.

### 2.1 Agents Group (6 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| POST | `/api/v1/agents/register` | Register a new agent binary with the platform | Agent JWT (enrollment token) | 201, 400, 401, 409, 429 |
| GET | `/api/v1/agents` | List all registered agents with filters | Operator JWT | 200, 401, 403, 429 |
| GET | `/api/v1/agents/{id}` | Get full detail for one agent | Operator JWT | 200, 401, 403, 404, 429 |
| DELETE | `/api/v1/agents/{id}` | Deregister and remove an agent | Operator JWT | 204, 401, 403, 404, 429 |
| POST | `/api/v1/agents/{id}/heartbeat` | Agent reports liveness + status | Agent JWT (self) | 200, 401, 404, 429 |
| POST | `/api/v1/agents/{id}/commands` | Dispatch a command to a specific agent | Operator JWT | 202, 400, 401, 403, 404, 429 |

### 2.2 Checks Group (4 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| GET | `/api/v1/checks` | List all check definitions | Operator JWT | 200, 401, 403, 429 |
| POST | `/api/v1/checks` | Create a new check definition | Operator JWT | 201, 400, 401, 403, 429 |
| POST | `/api/v1/checks/{id}/run` | Trigger an immediate execution of a check | Operator JWT | 202, 400, 401, 403, 404, 429 |
| GET | `/api/v1/checks/{id}/results` | Retrieve historical results for a check | Operator JWT | 200, 401, 403, 404, 429 |

### 2.3 Scripts Group (4 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| GET | `/api/v1/scripts` | List all stored scripts | Operator JWT | 200, 401, 403, 429 |
| POST | `/api/v1/scripts` | Upload a new script definition | Operator JWT | 201, 400, 401, 403, 429 |
| POST | `/api/v1/scripts/{id}/execute` | Execute a script on one or more agents | Operator JWT | 202, 400, 401, 403, 404, 429 |
| GET | `/api/v1/scripts/{id}/status` | Check execution status and output of a script run | Operator JWT | 200, 401, 403, 404, 429 |

### 2.4 Events Group (2 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| GET | `/api/v1/events` | Query events with filters (time range, type, agent, severity) | Operator JWT | 200, 401, 403, 429 |
| GET | `/api/v1/events/{id}` | Get full event detail by ID | Operator JWT | 200, 401, 403, 404, 429 |

### 2.5 System Group (2 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| GET | `/api/v1/system/health` | Liveness and readiness probe | None | 200, 503 |
| GET | `/api/v1/system/version` | Service version, build hash, uptime | None | 200 |

### 2.6 Patches Group (3 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| POST | `/api/v1/patches/scan` | Initiate a patch scan across selected agents | Operator JWT | 202, 400, 401, 403, 429 |
| POST | `/api/v1/patches/{id}/approve` | Approve a pending patch for deployment | Operator JWT | 200, 401, 403, 404, 429 |
| POST | `/api/v1/patches/{id}/deploy` | Deploy an approved patch to target agents | Operator JWT | 202, 400, 401, 403, 404, 429 |

### 2.7 Auth Group (3 endpoints)

| Method | Path | Description | Auth | Status Codes |
|--------|------|-------------|------|--------------|
| POST | `/api/v1/auth/login` | Exchange username/password for JWT access + refresh tokens | None | 200, 400, 401, 429 |
| POST | `/api/v1/auth/refresh` | Exchange a valid refresh token for a new access token | Refresh JWT | 200, 401, 403 |
| POST | `/api/v1/auth/logout` | Invalidate the current session | Access JWT | 204, 401 |

### 2.8 Endpoint Total

**24 endpoints** across 7 resource groups. All non-system, non-auth endpoints require a valid JWT. All mutating endpoints return `202 Accepted` when the work is dispatched asynchronously.

### 2.9 Request/Response Schemas

#### 2.9.1 POST /api/v1/agents/register

**Request body:**
```json
{
  "hostname": "dc1-prod-web-01",
  "fqdn": "dc1-prod-web-01.corp.example.com",
  "os": "windows",
  "os_version": "10.0.19045",
  "arch": "amd64",
  "agent_version": "1.4.2",
  "ip_addresses": ["10.20.30.41", "fe80::1234:5678:9abc:def0"],
  "mac_addresses": ["00:1A:2B:3C:4D:5E"],
  "tags": ["production", "web-tier", "iis"],
  "capabilities": ["check.exec", "script.exec.python3", "script.exec.powershell", "patch.scan"],
  "enrollment_token": "etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c"
}
```

**Response (201 Created):**
```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "status": "REGISTERED",
  "nats_subject": "oap.endpoint.a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "heartbeat_interval_seconds": 30,
  "heartbeat_stale_after_seconds": 120,
  "jwt_expires_at": "2026-06-16T00:00:00Z",
  "config": {
    "check_poll_interval_seconds": 60,
    "script_output_buffer_kb": 256,
    "log_level": "info"
  }
}
```

#### 2.9.2 POST /api/v1/checks/{id}/run

**Request body:**
```json
{
  "target_agent_ids": [
    "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "b2c3d4e5-f6a7-8901-bcde-f12345678901"
  ],
  "timeout_seconds": 120,
  "parameters": {
    "threshold_ms": 500,
    "retries": 3
  }
}
```

**Response (202 Accepted):**
```json
{
  "dispatch_id": "disp-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d",
  "check_id": "chk-disk-space-critical",
  "agents_dispatched": 2,
  "dispatched_at": "2026-06-15T14:30:00.123Z",
  "status_url": "/api/v1/checks/chk-disk-space-critical/results?dispatch_id=disp-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d"
}
```

#### 2.9.3 POST /api/v1/scripts/{id}/execute

**Request body:**
```json
{
  "target_agent_ids": ["a1b2c3d4-e5f6-7890-abcd-ef1234567890"],
  "runtime": "python3",
  "arguments": ["--check-port", "443", "--tls-version", "1.3"],
  "environment": {
    "LOG_LEVEL": "debug"
  },
  "timeout_seconds": 300,
  "capture_stdout": true,
  "capture_stderr": true,
  "run_as": "SYSTEM"
}
```

**Response (202 Accepted):**
```json
{
  "execution_id": "exec-9f8e7d6c-5b4a-3210-fedc-ba0987654321",
  "script_id": "scr-tls-audit",
  "runtime": "python3",
  "agents_dispatched": 1,
  "status": "PENDING",
  "dispatched_at": "2026-06-15T14:35:00.456Z",
  "status_url": "/api/v1/scripts/scr-tls-audit/status?execution_id=exec-9f8e7d6c-5b4a-3210-fedc-ba0987654321"
}
```

#### 2.9.4 POST /api/v1/auth/login

**Request body:**
```json
{
  "username": "admin",
  "password": "s3cureP@ssw0rd!2026",
  "tenant_id": "tnt-corp-01"
}
```

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600,
  "refresh_expires_in": 86400,
  "scope": "agents:read agents:write checks:read checks:write scripts:execute events:read"
}
```

### 2.10 Middleware Chain (applied in order)

```
Request
  |
  v
[1] Recovery         -- panic recovery, returns 500 with trace ID
  |
  v
[2] RequestID        -- injects/propagates X-Request-Id header
  |
  v
[3] StructuredLog    -- slog JSON with method, path, latency, status
  |
  v
[4] CORS             -- cross-origin headers for dashboard
  |
  v
[5] RateLimit        -- token bucket per JWT subject (see Section 9)
  |
  v
[6] Auth (JWT)       -- validates Bearer token, sets context claims
  |
  v
[7] Handler          -- business logic
  |
  v
Response
```

---

## 3. NATS Bus

### 3.1 JetStream Architecture

NATS JetStream provides durable, at-least-once message delivery with replay capability. The `endpoint-api` service acts as both publisher and consumer on four streams. Each stream has a defined retention policy, storage limit, and subject pattern.

```
+------------------------------------------------------------------+
|                     NATS JETSTREAM CLUSTER                        |
|                                                                   |
|  +----------------+ +----------------+ +----------------+         |
|  | AGENTS         | | CHECKS          | | SCRIPTS       |         |
|  | Subjects:      | | Subjects:        | | Subjects:     |         |
|  | oap.endpoint.> | | oap.check.>      | | oap.script.>  |         |
|  | Retention:     | | Retention:        | | Retention:    |         |
|  |  Limits        | |  Limits           | |  Limits       |         |
|  | Storage: File  | | Storage: File     | | Storage: File |         |
|  +-------+--------+ +-------+-----------+ +-----+---------+       |
|          |                    |                   |               |
|  +-------+--------+                                               |
|  | WINUPDATE      |                                               |
|  | Subjects:      |                                               |
|  | oap.winupdate.>|                                               |
|  | Retention:     |                                               |
|  |  Limits        |                                               |
|  | Storage: File  |                                               |
|  +----------------+                                               |
+------------------------------------------------------------------+
```

### 3.2 Stream Definitions

| Stream | Subject Pattern | Retention | Max Messages | Max Bytes | Storage | Discard Policy |
|--------|----------------|-----------|-------------|-----------|---------|----------------|
| AGENTS | `oap.endpoint.>` | Limits | 10,000,000 | 50 GB | File | Old |
| CHECKS | `oap.check.>` | Limits | 50,000,000 | 200 GB | File | Old |
| SCRIPTS | `oap.script.>` | Limits | 5,000,000 | 20 GB | File | Old |
| WINUPDATE | `oap.winupdate.>` | Limits | 20,000,000 | 100 GB | File | Old |

All streams use `File` storage (persistent across restarts) and `Limits` retention (delete old messages when max is reached). The `Discard` policy is `Old`, meaning the oldest messages are dropped first when limits are hit.

### 3.3 Subject Taxonomy

| Stream | Subject | Direction | Purpose |
|--------|---------|-----------|---------|
| AGENTS | `oap.endpoint.{agentID}.register` | Agent -> Server | Agent self-registration |
| AGENTS | `oap.endpoint.{agentID}.heartbeat` | Agent -> Server | Liveness ping (every 30s) |
| AGENTS | `oap.endpoint.{agentID}.status` | Agent -> Server | Detailed status update (CPU, mem, disk) |
| AGENTS | `oap.endpoint.{agentID}.deregister` | Agent -> Server | Graceful shutdown notice |
| AGENTS | `oap.endpoint.{agentID}.commands` | Server -> Agent | Command dispatch (run check, exec script, reboot) |
| AGENTS | `oap.endpoint.{agentID}.commands.ack` | Agent -> Server | Command acknowledgment |
| CHECKS | `oap.check.dispatch.{checkID}` | Server -> Agent | Check execution dispatch |
| CHECKS | `oap.check.results.{checkID}.{agentID}` | Agent -> Server | Check execution result |
| CHECKS | `oap.check.events.{checkID}.{agentID}` | Agent -> Server | Check state-change event |
| SCRIPTS | `oap.script.execute.{scriptID}` | Server -> Agent | Script execution request |
| SCRIPTS | `oap.script.output.{executionID}.{agentID}` | Agent -> Server | Script stdout/stderr chunks |
| SCRIPTS | `oap.script.status.{executionID}.{agentID}` | Agent -> Server | Script completion status (exit code, duration) |
| WINUPDATE | `oap.winupdate.events.{agentID}` | Agent -> Server | Windows update event (available, installed, failed) |
| WINUPDATE | `oap.winupdate.scan.{agentID}` | Server -> Agent | Trigger patch scan on Windows agent |

### 3.4 Consumer Groups (5)

| Consumer Group | Stream | Subject Filter | Durable | Deliver Policy | Ack Policy | Purpose |
|----------------|--------|----------------|---------|----------------|------------|---------|
| `api-cmd-dispatcher` | AGENTS | `commands.>` | Yes | All | Explicit | Receive all command-ack messages from agents |
| `check-result-ingester` | CHECKS | `results.>` | Yes | All | Explicit | Persist check results to PostgreSQL |
| `script-output-relay` | SCRIPTS | `output.>` | Yes | All | Explicit | Buffer and forward script output to WebSocket subscribers |
| `winupdate-processor` | WINUPDATE | `events.>` | Yes | All | Explicit | Process Windows update events, generate alerts |
| `event-persister` | CHECKS | `events.>` | Yes | All | Explicit | Write check state-change events to event_log table |

All consumers are **durable** (survive server restart), use **deliver-all** policy (receive all messages including those published while offline), and require **explicit ack** (messages are only removed after the consumer confirms processing).

### 3.5 Serialization

The subsystem uses two serialization formats, selected by the `Content-Encoding` header on each NATS message.

| Format | Used For | Rationale |
|--------|----------|-----------|
| **msgpack** | AGENTS, CHECKS, SCRIPTS streams (agent traffic) | ~30% smaller than JSON, faster encode/decode, native Go support via `github.com/vmihailenco/msgpack/v5` |
| **JSON (orjson)** | Server-side consumers, WINUPDATE events (interop with PowerShell agents) | Human-readable for debugging; `orjson` is 3-5x faster than encoding/json in Python |
| **Protobuf** | gRPC service messages | Strongly-typed, schema-evolution-safe, compact binary |

### 3.6 Schema Evolution with Tagged Fields

All message structs use numeric field tags to ensure forward and backward compatibility when fields are added or removed.

**msgpack example (Go):**
```go
type CheckResult struct {
    CheckID     string             `msgpack:"1"`
    AgentID     string             `msgpack:"2"`
    Status      string             `msgpack:"3"`  // PASS, FAIL, ERROR
    ExitCode    int                `msgpack:"4"`
    Output      string             `msgpack:"5"`
    DurationMs  int64              `msgpack:"6"`
    Timestamp   time.Time          `msgpack:"7"`
    Tags        map[string]string  `msgpack:"8"`  // Added in v1.1
    // Field 9 reserved for future use
}
```

**Evolution rules:**
- Adding a new field with a new tag number is **non-breaking** (old consumers ignore unknown fields).
- Removing a field is **breaking** (mark deprecated, do not remove for 2 minor versions).
- Changing a field's type is **breaking** (add a new field with a new tag instead).
- Renaming a field (changing the Go struct name) is **non-breaking** as long as the tag number stays the same.

### 3.7 Message Envelope

All NATS messages are wrapped in a standard envelope for traceability:

```json
{
  "message_id": "msg-uuid-v4",
  "correlation_id": "corr-uuid-v4",
  "timestamp": "2026-06-15T14:30:00.123Z",
  "source": "agent|a2a-gateway|api-server|secret-service",
  "source_id": "agent-id or service-instance-id",
  "schema_version": 1,
  "payload": { /* message-specific body */ }
}
```

---

## 4. Agent Binary

### 4.1 What It Is

The `oap-agent` is a single static Go binary deployed to every managed device. It communicates with the `endpoint-api` server exclusively over NATS JetStream. It has no direct database access and no inbound HTTP port (all communication is outbound).

### 4.2 Cross-Compilation Targets

| OS | Arch | CGO | Output |
|----|------|-----|--------|
| windows | amd64 | Disabled (pure Go) | `oap-agent-windows-amd64.exe` |
| windows | arm64 | Disabled | `oap-agent-windows-arm64.exe` |
| linux | amd64 | Disabled | `oap-agent-linux-amd64` |
| linux | arm64 | Disabled | `oap-agent-linux-arm64` |
| darwin | amd64 | Disabled | `oap-agent-darwin-amd64` |
| darwin | arm64 | Disabled | `oap-agent-darwin-arm64` |

All builds are static (no CGO) for maximum portability. The Windows builds use `GOOS=windows` with `-H windowsgui` to run as a tray application.

### 4.3 Lifecycle State Machine

```
                     +-----------+
                     |   NEW     |
                     | (just     |
                     | installed)|
                     +-----+-----+
                           |
                           | startRegistration()
                           v
                     +-----+-----+
                     |REGISTERING|
                     | (sending  |
                     | register  |
                     | message)  |
                     +-----+-----+
                           |
               +-----------+-----------+
               |                       |
        success|                       |failure/timeout
               v                       v
      +--------+--------+      +-------+-------+
      |   REGISTERED    |      |    STALE      |
      | (receiving      |      | (registration |
      |  heartbeats,    |      |  expired)     |
      |  processing     |      +-------+-------+
      |  commands)      |              |
      +--------+--------+              | retry exhausted
               |                       v
               |              +--------+--------+
               |              |    OFFLINE      |
               |              | (max retries    |
               |              |  exceeded,      |
               |              |  backoff active)|
               |              +--------+--------+
               |                       |
               |   heartbeat           | heartbeat
               |   timeout (120s)      | recovers
               v                       |
      +--------+--------+              |
      |     STALE       |<-------------+
      | (missed 2+      |
      |  heartbeats)    |
      +--------+--------+
               |
               | re-registration
               | success
               v
      +--------+--------+
      |   REGISTERED    |
      +-----------------+

      Any state --(DELETE or manual deregister)--> DEREGISTERED
```

### 4.4 State Transitions

| From | To | Trigger | Action |
|------|----|---------|--------|
| NEW | REGISTERING | Agent starts for the first time | Read UUID from disk; if absent, generate new one |
| REGISTERING | REGISTERED | Server accepts registration (201) | Save JWT; start heartbeat goroutine; subscribe to command subject |
| REGISTERING | STALE | Registration fails or times out (3 retries) | Enter backoff; log error |
| REGISTERED | STALE | Missed 2 consecutive heartbeats (60s gap) | Pause command subscription; attempt re-registration |
| STALE | REGISTERED | Re-registration succeeds | Resume command subscription |
| STALE | OFFLINE | Re-registration fails 5 times | Enter long backoff (30s cap); log critical error |
| OFFLINE | REGISTERED | Backoff timer fires and re-registration succeeds | Resume normal operation |
| Any | DEREGISTERED | DELETE /api/v1/agents/{id} or agent sends deregister message | Unsubscribe; close NATS connection; exit |

### 4.5 Per-Agent NATS Subscription

Each agent subscribes to its own dedicated subject. The subject includes the agent's UUID so that commands are delivered exclusively to the target agent.

**Subject format:** `oap.endpoint.{agent_id}.commands`

**Subscription details:**
- **Type:** Pull-based with a 1-second batch timeout
- **Queue group:** None (each agent is its own consumer; no sharing)
- **Max ack pending:** 100 (backpressure if the agent falls behind)
- **Ack wait:** 30 seconds (if not acked, the message is redelivered)

**Dispatch pattern (server -> agent):**
```
Server publishes to: oap.endpoint.a1b2c3d4-...-7890.commands
   |
   v
Agent's NATS subscription receives message
   |
   v
Func dispatch switch on message type:
   - "run_check"   -> validate, execute, publish result
   - "exec_script" -> validate, execute, stream output, publish status
   - "reboot"      -> validate, schedule reboot, publish ack
   - "patch_scan"  -> validate, trigger scan, publish results
   - "deregister"  -> publish ack, unsubscribe, exit
```

### 4.6 UUID Persistence

The agent generates a UUID v4 on first launch and persists it to disk. This ensures the agent maintains the same identity across restarts and upgrades.

| OS | Path |
|----|------|
| Linux | `/var/lib/oap-agent/agent.uuid` |
| macOS | `/var/lib/oap-agent/agent.uuid` |
| Windows | `%PROGRAMDATA%\oap-agent\agent.uuid` |

If the file does not exist, a new UUID is generated with `uuid.New().String()`. The file is written with `0600` permissions (owner read/write only).

### 4.7 Reconnect Strategy

NATS connections can drop due to network interruptions, server restarts, or NAT timeouts. The agent implements exponential backoff with jitter.

```
Attempt 1:  wait 1s
Attempt 2:  wait 2s
Attempt 3:  wait 4s
Attempt 4:  wait 8s
Attempt 5:  wait 16s
Attempt 6+: wait 30s (capped)
```

**Backoff configuration:**
- Initial delay: 1 second
- Multiplier: 2x
- Max delay: 30 seconds
- Jitter: +/- 20% (applied to each delay to avoid thundering herd)

**On reconnect:**
1. Open new NATS connection with the stored JWT
2. If JWT is expired (401 from NATS), request a new JWT from the server via REST (`POST /api/v1/agents/{id}/heartbeat` returns a new token in the response)
3. Re-subscribe to `oap.endpoint.{agent_id}.commands`
4. State machine transitions from STALE/OFFLINE back to REGISTERING -> REGISTERED

### 4.8 Script Executor

The agent can execute scripts in four runtimes. The runtime is specified in the script execution message.

| Runtime | Command | OS | Notes |
|---------|---------|----|-------|
| Python3 | `python3 -I -E -s {script_path} {args}` | Linux, macOS | `-I` = isolated mode; `-E` = ignore env vars; `-s` = don't add user site |
| Python3 | `python -I -E -s {script_path} {args}` | Windows | Uses `python` launcher |
| Bash | `/bin/bash --noprofile --norc -e -o pipefail {script_path} {args}` | Linux, macOS | `-e` = exit on error; `-o pipefail` = catch pipe failures |
| PowerShell | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File {script_path} {args}` | Windows | `-NoProfile` = no profile loading; `-NonInteractive` = no prompts |
| Node | `node --no-warnings --experimental-vm-modules {script_path} {args}` | Linux, macOS, Windows | VM modules for ES module isolation |

**Security controls:**
- Scripts are written to a temp directory with `0600` permissions
- The temp directory is cleaned up after execution
- Script content is validated (no null bytes, max size 1 MB)
- Arguments are passed as a separate argv slice (not shell-interpolated)
- Environment variables are filtered to a whitelist

### 4.9 prlimit Resource Constraints

On Linux, the agent uses `prlimit` (via `syscall.Syscall` with `SYS_prlimit64`) to enforce resource limits on child processes. macOS uses `setrlimit` equivalents.

| Resource | Limit | prlimit Constant | Purpose |
|----------|-------|------------------|---------|
| CPU time | 300 seconds (or script timeout) | `RLIMIT_CPU` | Kill runaway scripts |
| File size | 100 MB | `RLIMIT_FSIZE` | Prevent disk fill |
| Open files | 256 | `RLIMIT_NOFILE` | Prevent fd exhaustion |
| Address space | 2 GB | `RLIMIT_AS` | Prevent memory bombs |
| File locks | 256 | `RLIMIT_LOCKS` | Prevent lock exhaustion |
| Pending signals | 128 | `RLIMIT_SIGPENDING` | Prevent signal queue overflow |
| Message queue | 1 MB | `RLIMIT_MSGQUEUE` | Prevent IPC abuse |
| Nice priority | +19 (lowest) | `RLIMIT_NICE` | Don't starve system |
| Realtime priority | 0 (disabled) | `RLIMIT_RTPRIO` | No real-time scheduling |
| Resident set | 512 MB | `RLIMIT_RSS` | Cap physical memory |
| Timeout | Wall-clock via `context.WithTimeout` | N/A | Kill if exceeds script timeout |

On Windows, the equivalent controls are job objects: `JOB_OBJECT_LIMIT_PROCESS_MEMORY`, `JOB_OBJECT_LIMIT_JOB_MEMORY`, `JOB_OBJECT_LIMIT_ACTIVE_PROCESS`, and `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`.

---

## 5. Check Execution Protocol

### 5.1 Overview

A check is a health or compliance test that runs on an agent and reports a PASS/FAIL/ERROR result. The full lifecycle spans the API, NATS, and the agent.

```
Operator / Dashboard
  |
  | POST /api/v1/checks/{id}/run
  v
endpoint-api server
  |
  | (1) Validate check exists, agent targets are REGISTERED
  | (2) Generate dispatch_id
  | (3) Publish to oap.check.dispatch.{checkID}
  |     Payload: { dispatch_id, check_id, agent_ids, parameters, timeout }
  v
NATS JetStream (CHECKS stream)
  |
  | (4) Deliver to subscribed agents
  v
Agent (on target device)
  |
  | (5) Receive message
  | (6) Validate check type and parameters
  | (7) Execute check locally
  | (8) Collect result (status, output, exit code, duration)
  | (9) Publish to oap.check.results.{checkID}.{agentID}
  |     Payload: { dispatch_id, agent_id, status, output, exit_code, duration_ms }
  v
NATS JetStream (CHECKS stream)
  |
  | (10) check-result-ingester consumer receives
  | (11) Persist to PostgreSQL check_results table
  | (12) event-persister consumer receives on events subject
  | (13) Write to event_log table
  v
endpoint-api server -> PostgreSQL
  |
  | (14) GET /api/v1/checks/{id}/results returns stored data
  v
Operator / Dashboard
```

### 5.2 Dispatch Message (Server -> Agent)

**Subject:** `oap.check.dispatch.{checkID}`

**Payload (msgpack):**
```go
type CheckDispatch struct {
    DispatchID    string            `msgpack:"1"`
    CheckID       string            `msgpack:"2"`
    AgentID       string            `msgpack:"3"`
    CheckType     string            `msgpack:"4"`  // "disk_space", "service_running", "http_probe", "custom_script"
    Parameters    map[string]string `msgpack:"5"`
    TimeoutSec    int               `msgpack:"6"`
    ScheduledAt   time.Time         `msgpack:"7"`
    CorrelationID string            `msgpack:"8"`
}
```

**Example JSON (for documentation):**
```json
{
  "dispatch_id": "disp-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d",
  "check_id": "chk-disk-space-critical",
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "check_type": "disk_space",
  "parameters": {
    "path": "C:\\",
    "min_free_gb": "10"
  },
  "timeout_sec": 30,
  "scheduled_at": "2026-06-15T14:30:00.123Z",
  "correlation_id": "corr-9f8e7d6c-5b4a-3210-fedc-ba0987654321"
}
```

### 5.3 Result Message (Agent -> Server)

**Subject:** `oap.check.results.{checkID}.{agentID}`

**Payload (msgpack):**
```go
type CheckResult struct {
    CheckID       string            `msgpack:"1"`
    AgentID       string            `msgpack:"2"`
    DispatchID    string            `msgpack:"3"`
    Status        string            `msgpack:"4"`  // "PASS", "FAIL", "ERROR"
    ExitCode      int               `msgpack:"5"`
    Output        string            `msgpack:"6"`
    DurationMs    int64             `msgpack:"7"`
    Timestamp     time.Time         `msgpack:"8"`
    Tags          map[string]string `msgpack:"9"`
    CorrelationID string            `msgpack:"10"`
}
```

**Example JSON (for documentation):**
```json
{
  "check_id": "chk-disk-space-critical",
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "dispatch_id": "disp-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d",
  "status": "FAIL",
  "exit_code": 1,
  "output": "Drive C: has 7.2 GB free (minimum: 10 GB)",
  "duration_ms": 145,
  "timestamp": "2026-06-15T14:30:01.268Z",
  "tags": {
    "drive": "C:",
    "free_gb": "7.2",
    "threshold_gb": "10"
  },
  "correlation_id": "corr-9f8e7d6c-5b4a-3210-fedc-ba0987654321"
}
```

### 5.4 Agent-Side Execution Phases (4)

| Phase | Description | Time Budget |
|-------|-------------|-------------|
| **Receive** | Agent's NATS subscription delivers the message; the msgpack payload is decoded into the `CheckDispatch` struct | < 10 ms |
| **Validate** | Check that the check_type is recognized, parameters are well-formed, the agent has the required capability, and the timeout is within bounds | < 50 ms |
| **Run** | Execute the check logic (shell command, HTTP probe, disk query, custom script). All output is captured to a buffer | Up to `timeout_sec` |
| **Report** | Build the `CheckResult` struct, encode as msgpack, publish to the results subject, and ack the original message | < 100 ms |

### 5.5 Server-Side Result Handling

When `check-result-ingester` receives a result message:

1. Decode the msgpack payload
2. Insert into `check_results` table (PostgreSQL) with an UPSERT (if the same `dispatch_id` + `agent_id` already exists, update the existing row)
3. If `status == "FAIL"` or `status == "ERROR"`, create an alert record in the `alerts` table
4. Publish a `check.state_changed` event to the events subject for the `event-persister` consumer
5. Ack the NATS message (removes it from the stream)

---

## 6. Real-Time Event Streaming

### 6.1 Event Types

| Event Type | Source | Subject | Description |
|------------|--------|---------|-------------|
| `agent.registered` | Agent | `oap.endpoint.{id}.register` | Agent successfully registered |
| `agent.deregistered` | Agent/Server | `oap.endpoint.{id}.deregister` | Agent deregistered |
| `agent.stale` | Server | Internal | Agent missed heartbeats |
| `agent.offline` | Server | Internal | Agent connection lost |
| `check.dispatched` | Server | `oap.check.dispatch.{id}` | Check sent to agent(s) |
| `check.completed` | Agent | `oap.check.results.{id}.{agent}` | Check finished (any status) |
| `check.failed` | Agent | `oap.check.results.{id}.{agent}` | Check result was FAIL/ERROR |
| `script.started` | Agent | `oap.script.status.{exec}.{agent}` | Script execution began |
| `script.completed` | Agent | `oap.script.status.{exec}.{agent}` | Script finished |
| `script.timeout` | Agent | `oap.script.status.{exec}.{agent}` | Script exceeded timeout |
| `winupdate.available` | Agent | `oap.winupdate.events.{agent}` | New update available |
| `winupdate.installed` | Agent | `oap.winupdate.events.{agent}` | Update installed |
| `winupdate.failed` | Agent | `oap.winupdate.events.{agent}` | Update installation failed |
| `patch.scan.completed` | Server | Internal | Patch scan finished |
| `patch.deployed` | Server | Internal | Patch deployed to agent(s) |

### 6.2 Event Schema

All events share a common envelope:

```json
{
  "event_id": "evt-uuid-v4",
  "event_type": "check.failed",
  "timestamp": "2026-06-15T14:30:01.268Z",
  "severity": "warning",
  "source": "agent",
  "source_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "tenant_id": "tnt-corp-01",
  "correlation_id": "corr-9f8e7d6c-5b4a-3210-fedc-ba0987654321",
  "data": {
    "check_id": "chk-disk-space-critical",
    "dispatch_id": "disp-7a8b9c0d-1e2f-3a4b-5c6d-7e8f9a0b1c2d",
    "status": "FAIL",
    "output": "Drive C: has 7.2 GB free (minimum: 10 GB)"
  },
  "tags": {
    "environment": "production",
    "tier": "web"
  }
}
```

### 6.3 JetStream Consumer Configuration

The `event-persister` consumer is configured for reliable, ordered processing:

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Durable name | `event-persister` | Survives server restart |
| Deliver policy | `all` | Receive all messages including those published while consumer was offline |
| Ack policy | `explicit` | Manual ack after successful database write |
| Ack wait | 30 seconds | If not acked, redeliver |
| Max deliver | 5 | After 5 failed attempts, send to dead-letter queue |
| Filter subject | `oap.check.events.>` | Only consume check state-change events |
| Max ack pending | 1000 | Backpressure limit |
| Flow control | 100 msg/sec | Smooth processing rate |

### 6.4 Materialized View Updates

Events flow into a denormalized read model for fast dashboard queries:

```
NATS event message
  |
  v
event-persister consumer
  |
  v
INSERT INTO event_log (event_id, event_type, timestamp, severity, source_id, data, tags)
  |
  v
PostgreSQL trigger (AFTER INSERT on event_log)
  |
  v
UPDATE events_recent (materialized view) -- last 24h, indexed by type+severity
  |
  v
Dashboard queries SELECT * FROM events_recent -- sub-millisecond
```

### 6.5 Event Ordering Guarantees

- **Per-agent ordering:** All events from a single agent are delivered in publish order (NATS JetStream guarantees per-subject ordering).
- **Global ordering:** Not guaranteed across agents. Events from different agents may arrive out of order at the consumer.
- **Timestamp ordering:** Consumers and the database use the `timestamp` field from the event payload, not the NATS message timestamp, for ordering decisions.

### 6.6 Replay Capability

JetStream retains messages according to stream limits. The `event-persister` consumer can be reset to replay messages:

```bash
# Reset consumer to beginning of stream
nats consumer reset event-persister --all

# Reset to a specific time
nats consumer reset event-persister --since=2026-06-15T00:00:00Z
```

This is used for:
- Disaster recovery (rebuild event_log from JetStream after database corruption)
- Backfill (populate a new materialized view)
- Audit (reprocess a specific time window)

---

## 7. gRPC Service

### 7.1 Overview

The `endpoint-api` service exposes a gRPC server on `:50051` for internal service-to-service communication. The gRPC interface is defined in `proto/endpoint/v1/endpoint.proto` and provides 30+ message types.

### 7.2 Proto Definition Overview

```protobuf
syntax = "proto3";
package oap.endpoint.v1;

import "google/protobuf/timestamp.proto";

// --- Service Definition ---
service EndpointService {
  // Agent management
  rpc RegisterAgent(RegisterAgentRequest) returns (RegisterAgentResponse);
  rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse);
  rpc GetAgent(GetAgentRequest) returns (AgentDetail);
  rpc DeregisterAgent(DeregisterAgentRequest) returns (DeregisterAgentResponse);
  
  // Check management
  rpc CreateCheck(CreateCheckRequest) returns (CreateCheckResponse);
  rpc RunCheck(RunCheckRequest) returns (RunCheckResponse);
  rpc GetCheckResults(GetCheckResultsRequest) returns (GetCheckResultsResponse);
  
  // Script management
  rpc CreateScript(CreateScriptRequest) returns (CreateScriptResponse);
  rpc ExecuteScript(ExecuteScriptRequest) returns (ExecuteScriptResponse);
  rpc GetScriptStatus(GetScriptStatusRequest) returns (GetScriptStatusResponse);
  
  // Event streaming
  rpc StreamEvents(StreamEventsRequest) returns (stream Event);
  rpc GetEvent(GetEventRequest) returns (Event);
  
  // System
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
  rpc GetVersion(GetVersionRequest) returns (VersionInfo);
  
  // Patch management
  rpc ScanPatches(ScanPatchesRequest) returns (ScanPatchesResponse);
  rpc ApprovePatch(ApprovePatchRequest) returns (ApprovePatchResponse);
  rpc DeployPatch(DeployPatchRequest) returns (DeployPatchResponse);
}
```

### 7.3 Message Types (30+)

**Agent messages (5):**
- `RegisterAgentRequest`, `RegisterAgentResponse`
- `ListAgentsRequest`, `ListAgentsResponse`, `AgentSummary`
- `GetAgentRequest`, `AgentDetail`
- `DeregisterAgentRequest`, `DeregisterAgentResponse`
- `HeartbeatRequest`, `HeartbeatResponse`

**Check messages (6):**
- `CheckDefinition`, `CheckType` (enum)
- `CreateCheckRequest`, `CreateCheckResponse`
- `RunCheckRequest`, `RunCheckResponse`
- `GetCheckResultsRequest`, `GetCheckResultsResponse`, `CheckResult`
- `ListChecksRequest`, `ListChecksResponse`

**Script messages (6):**
- `ScriptDefinition`, `ScriptRuntime` (enum: PYTHON3, BASH, POWERSHELL, NODE)
- `CreateScriptRequest`, `CreateScriptResponse`
- `ExecuteScriptRequest`, `ExecuteScriptResponse`
- `GetScriptStatusRequest`, `GetScriptStatusResponse`, `ScriptStatus`
- `ScriptOutputChunk` (streaming)

**Event messages (4):**
- `Event`, `EventSeverity` (enum)
- `StreamEventsRequest` (with filter fields)
- `GetEventRequest`

**System messages (3):**
- `HealthCheckRequest`, `HealthCheckResponse`, `HealthStatus` (enum)
- `GetVersionRequest`, `VersionInfo`

**Patch messages (6):**
- `ScanPatchesRequest`, `ScanPatchesResponse`
- `ApprovePatchRequest`, `ApprovePatchResponse`
- `DeployPatchRequest`, `DeployPatchResponse`
- `PatchInfo`, `PatchStatus` (enum)

**Common messages (2):**
- `Pagination` (offset, limit, total)
- `Filter` (time range, severity, tags)

**Total: 30+ message types**

### 7.4 Server Reflection

gRPC server reflection is enabled, allowing tools like `grpcurl` to discover services and methods at runtime:

```bash
# List all services
grpcurl -plaintext localhost:50051 list

# List methods of EndpointService
grpcurl -plaintext localhost:50051 list oap.endpoint.v1.EndpointService

# Describe a message type
grpcurl -plaintext localhost:50051 describe oap.endpoint.v1.CheckResult

# Call a method
grpcurl -plaintext -d '{"check_id": "chk-disk-space"}' \
  localhost:50051 oap.endpoint.v1.EndpointService/GetCheckResults
```

**Go server reflection registration:**
```go
import "google.golang.org/grpc/reflection"

func registerGRPC(gs *grpc.Server) {
    endpointpb.RegisterEndpointServiceServer(gs, &endpointServer{})
    healthpb.RegisterHealthServer(gs, &healthServer{})
    reflection.Register(gs)  // Enables grpcurl discovery
}
```

### 7.5 Health Checking Protocol

The gRPC server implements the standard `grpc.health.v1.Health` service:

```protobuf
service Health {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Watch(HealthCheckRequest) returns (stream HealthCheckResponse);
}
```

**Health states:**
- `SERVING` -- all dependencies (PostgreSQL, NATS, Redis) are reachable
- `NOT_SERVING` -- one or more critical dependencies are down
- `UNKNOWN` -- health status has not been determined yet

**Health check logic:**
```go
func (s *healthServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
    if s.db.Ping() != nil || s.nats.Status() != nats.CONNECTED {
        return &healthpb.HealthCheckResponse{
            Status: healthpb.HealthCheckResponse_NOT_SERVING,
        }, nil
    }
    return &healthpb.HealthCheckResponse{
        Status: healthpb.HealthCheckResponse_SERVING,
    }, nil
}
```

### 7.6 Interceptors

| Interceptor | Purpose | Order | Configuration |
|-------------|---------|-------|---------------|
| **Auth** | Validates JWT in gRPC metadata (`authorization` header) | 1 (first) | Reject if missing/invalid; extract `sub` claim to context |
| **Logging** | Structured slog with method, duration, status, peer | 2 | Log level: info for unary, debug for streaming |
| **Recovery** | Catches panics, logs stack trace, returns `Internal` status | 3 (last) | Prevent goroutine crash; preserve connection |

**Interceptor chain (Go):**
```go
func grpcInterceptors() []grpc.UnaryServerInterceptor {
    return []grpc.UnaryServerInterceptor{
        authInterceptor,
        loggingInterceptor,
        recoveryInterceptor,
    }
}
```

### 7.7 Streaming RPC Patterns

| Pattern | RPC | Use Case |
|---------|-----|----------|
| **Unary** | `RegisterAgent`, `RunCheck`, `HealthCheck` | Single request, single response |
| **Server streaming** | `StreamEvents` | Client requests events; server pushes a stream of `Event` messages |
| **Client streaming** | (not currently used) | Reserved for future bulk ingestion |
| **Bidirectional** | (not currently used) | Reserved for future interactive sessions |

**Server streaming example (StreamEvents):**
```go
func (s *endpointServer) StreamEvents(req *pb.StreamEventsRequest, stream pb.EndpointService_StreamEventsServer) error {
    sub, err := s.js.Subscribe("oap.check.events.>", nats.DeliverAll())
    if err != nil {
        return status.Errorf(codes.Internal, "subscribe failed: %v", err)
    }
    defer sub.Unsubscribe()
    
    for {
        msg, err := sub.NextMsg(30 * time.Second)
        if err == nats.ErrTimeout {
            continue  // Send a keepalive
        }
        if err != nil {
            return status.Errorf(codes.Internal, "next msg: %v", err)
        }
        
        event := decodeEvent(msg.Data)
        if !matchesFilter(event, req.Filter) {
            msg.Ack()
            continue
        }
        
        if err := stream.Send(event); err != nil {
            return err  // Client disconnected
        }
        msg.Ack()
    }
}
```

---

## 8. API Versioning

### 8.1 URL-Path Versioning

All REST endpoints are namespaced under `/api/v{version}`:

```
/api/v1/agents
/api/v2/agents      (future, breaking changes only)
```

The version is part of the URL path, not a header or query parameter. This is explicit, cacheable, and easy to route.

### 8.2 Backward Compatibility Rules

| Change Type | Breaking? | Action |
|-------------|-----------|--------|
| Add a new endpoint | No | Deploy immediately |
| Add a new optional field to a request | No | Deploy immediately |
| Add a new field to a response | No | Deploy immediately |
| Add a new enum value | No | Deploy immediately (consumers should handle unknown values) |
| Remove an endpoint | Yes | Deprecate first, remove after sunset |
| Remove a field from a response | Yes | Deprecate first, remove after sunset |
| Change a field's type | Yes | Add a new field with a new name; deprecate the old one |
| Rename an endpoint | Yes | Add new route; redirect old route; deprecate; remove |
| Change an enum value's meaning | Yes | Add a new value; deprecate the old one |
| Make an optional field required | Yes | Provide a default; deprecate; make required after sunset |

### 8.3 Deprecation Policy

When an endpoint or field is deprecated:

1. **Mark in code:** Add a `// Deprecated: use /api/v2/foo instead` comment
2. **Deprecation header:** Add `Deprecation: true` to all responses
3. **Sunset header:** Add `Sunset: Wed, 01 Dec 2027 00:00:00 GMT` (minimum 6 months out)
4. **OpenAPI:** Mark as `deprecated: true` in the spec
5. **Documentation:** Add a deprecation notice to the API docs
6. **Monitoring:** Alert when deprecated endpoints receive traffic
7. **Removal:** After the sunset date, remove the endpoint and return `410 Gone` for a transition period, then `404`

**Deprecation response headers:**
```
HTTP/1.1 200 OK
Deprecation: true
Sunset: Wed, 01 Dec 2027 00:00:00 GMT
Link: </api/v2/agents/{id}>; rel="successor-version"
Content-Type: application/json
```

### 8.4 Migration Guide Process

When a breaking change is introduced:

1. **RFC document:** Write an `docs/rfcs/NNNN-api-v2-migration.md` describing the changes
2. **v2 development:** Implement v2 alongside v1 (no removal of v1)
3. **Migration period:** Run both versions for minimum 6 months
4. **Deprecation announcement:** Blog post, email to integrators, dashboard banner
5. **v1 sunset:** After 6 months, add Sunset header to v1 responses
6. **v1 removal:** After sunset date, return 410 Gone for v1 routes
7. **v1 removal (final):** Remove v1 routes entirely after 30-day grace period

---

## 9. Rate Limiting

### 9.1 Algorithm

Rate limiting uses the **token bucket** algorithm via `golang.org/x/time/rate`. Each JWT subject gets a bucket that refills at a steady rate.

**Token bucket parameters:**
- **Capacity (burst):** The maximum number of requests that can be made in a burst
- **Refill rate:** Tokens added per second

When a request arrives, one token is consumed. If no tokens are available, the request is rejected with `429 Too Many Requests`.

### 9.2 Per-JWT-Subject Limiting

Each authenticated request is rate-limited by the `sub` claim of the JWT. This means:
- One user with 10 browser tabs shares one bucket
- 100 different users each get their own bucket
- Unauthenticated requests (login, health) are rate-limited by IP address

**Bucket storage:** The token bucket state is stored in memory for the local instance and replicated to Redis for multi-instance deployments (so a user can't exceed the limit by hitting different server instances).

### 9.3 Per-Endpoint Limits

| Endpoint | Limit (req/min) | Burst | Rationale |
|----------|-----------------|-------|-----------|
| `POST /api/v1/agents/register` | 10 | 5 | Registration is infrequent; prevent enrollment storms |
| `POST /api/v1/agents/{id}/heartbeat` | 120 | 20 | Heartbeats are every 30s (2/min) but allow for retries |
| `POST /api/v1/agents/{id}/commands` | 60 | 10 | Command dispatch is operator-driven |
| `POST /api/v1/checks/{id}/run` | 60 | 10 | Check runs are expensive (agent execution) |
| `POST /api/v1/scripts/{id}/execute` | 30 | 5 | Script execution is the most expensive operation |
| `POST /api/v1/patches/{id}/deploy` | 10 | 3 | Patch deployment affects many devices |
| `GET /api/v1/agents` | 300 | 50 | Dashboard polling |
| `GET /api/v1/agents/{id}` | 300 | 50 | Dashboard detail view |
| `GET /api/v1/checks` | 300 | 50 | Dashboard listing |
| `GET /api/v1/checks/{id}/results` | 300 | 50 | Dashboard chart data |
| `GET /api/v1/scripts` | 300 | 50 | Dashboard listing |
| `GET /api/v1/scripts/{id}/status` | 300 | 50 | Dashboard execution status |
| `GET /api/v1/events` | 300 | 50 | Dashboard event feed |
| `GET /api/v1/events/{id}` | 600 | 100 | Event detail (frequent polling) |
| `POST /api/v1/auth/login` | 10 | 3 | Prevent brute force |
| `POST /api/v1/auth/refresh` | 60 | 10 | Token refresh is frequent |
| `POST /api/v1/auth/logout` | 30 | 5 | Logout is infrequent |
| **Default** (all other endpoints) | 100 | 20 | Conservative default |

### 9.4 Rate Limit Response Headers

Every rate-limited response includes:

```
X-RateLimit-Limit: 300        # Total tokens in the bucket
X-RateLimit-Remaining: 247     # Tokens remaining after this request
X-RateLimit-Reset: 1718463600  # Unix timestamp when the bucket is fully refilled
```

### 9.5 429 Too Many Requests Response

When the bucket is empty:

```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Retry after 12 seconds.",
  "retry_after_seconds": 12
}
```

With HTTP headers:
```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 12
X-RateLimit-Limit: 300
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1718463612
```

---

## 10. OpenAPI 3.1

### 10.1 Spec Generation

The OpenAPI 3.1 spec is generated from Go struct tags and Echo route registrations. The `oapi-codegen` tool reads the spec and generates server stubs, while route handlers are annotated with `// @Summary`, `// @Description`, `// @Tags`, `// @Param`, `// @Success`, and `// @Failure` comments.

**Example handler annotation:**
```go
// RegisterAgent godoc
// @Summary Register a new agent
// @Description Registers a new agent binary with the platform using an enrollment token
// @Tags agents
// @Accept json
// @Produce json
// @Param request body RegisterAgentRequest true "Agent registration request"
// @Success 201 {object} RegisterAgentResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /api/v1/agents/register [post]
func (h *AgentHandler) RegisterAgent(c echo.Context) error {
    // ...
}
```

### 10.2 Documentation Endpoints

| Path | Content |
|------|---------|
| `GET /api/openapi.json` | Full OpenAPI 3.1 spec in JSON format |
| `GET /api/openapi.yaml` | Full OpenAPI 3.1 spec in YAML format |
| `GET /api/docs` | Swagger UI (HTML page with interactive API explorer) |
| `GET /api/redoc` | ReDoc UI (alternative documentation renderer) |

### 10.3 Swagger UI Integration

Swagger UI is embedded as a static asset and served at `/api/docs`. It reads `/api/openapi.json` and provides:
- Interactive "Try it out" buttons for every endpoint
- Schema documentation with examples
- Authentication support (paste a JWT to test authenticated endpoints)
- Request/response visualization

### 10.4 Schema Validation

Incoming requests are validated against the OpenAPI schema using `kin-openapi` before reaching the handler. Validation errors return `400 Bad Request` with details:

```json
{
  "error": "validation_error",
  "message": "Request body failed schema validation",
  "details": [
    {
      "field": "hostname",
      "rule": "required",
      "message": "hostname is required"
    },
    {
      "field": "os",
      "rule": "enum",
      "message": "os must be one of: windows, linux, darwin"
    }
  ]
}
```

### 10.5 Code Generation Pipeline

```
Go source code (handlers + structs)
  |
  | go generate ./...
  v
swag/swag CLI extracts comments
  |
  v
OpenAPI 3.1 spec (docs/api/openapi.json)
  |
  +---> Swagger UI (/api/docs)
  |
  +---> openapi-generator-cli
  |     |
  |     +---> Python SDK (sdks/python/)
  |     +---> Go SDK (sdks/go/)
  |     +---> TypeScript SDK (sdks/typescript/)
  |
  +---> Contract tests (verify spec matches implementation)
```

---

## 11. SDK Generation

### 11.1 Overview

Three official client SDKs are generated from the OpenAPI 3.1 spec. Each SDK is versioned in lockstep with the API and published to its respective package registry.

### 11.2 Python SDK

| Property | Value |
|----------|-------|
| Generator | `openapi-generator-cli generate -g python` |
| Output directory | `sdks/python/` |
| Package name | `oap-endpoint` |
| HTTP library | `urllib3` (default) or `httpx` (async option) |
| Async support | Yes (`oap_endpoint.AgentApi` async methods) |
| Type hints | Full PEP 484 type hints |
| Tests | pytest with `responses` mock library |
| Publish target | PyPI (`pip install oap-endpoint`) |

**Example usage:**
```python
from oap_endpoint import ApiClient, Configuration
from oap_endpoint.api import AgentApi
from oap_endpoint.models import RegisterAgentRequest

config = Configuration(host="https://api.oap.example.com")
config.access_token = "eyJhbGciOiJSUzI1NiIs..."

with ApiClient(config) as client:
    agent_api = AgentApi(client)
    
    request = RegisterAgentRequest(
        hostname="dc1-prod-web-01",
        os="windows",
        os_version="10.0.19045",
        arch="amd64",
        agent_version="1.4.2",
        enrollment_token="etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c"
    )
    
    response = agent_api.register_agent(request)
    print(f"Agent registered: {response.agent_id}")
```

### 11.3 Go SDK

| Property | Value |
|----------|-------|
| Generator | `openapi-generator-cli generate -g go` |
| Output directory | `sdks/go/` |
| Module path | `github.com/openagentplatform/endpoint-sdk-go` |
| HTTP library | `net/http` (stdlib) |
| Context support | Full `context.Context` support |
| Type safety | Strongly typed structs with validation tags |
| Tests | Standard `testing` package with `httptest` |
| Publish target | pkg.go.dev (`go get github.com/openagentplatform/endpoint-sdk-go`) |

**Example usage:**
```go
package main

import (
    "context"
    "fmt"
    oap "github.com/openagentplatform/endpoint-sdk-go"
)

func main() {
    cfg := oap.NewConfiguration()
    cfg.Host = "api.oap.example.com"
    cfg.AccessToken = "eyJhbGciOiJSUzI1NiIs..."
    
    client := oap.NewAPIClient(cfg)
    
    req := oap.RegisterAgentRequest{
        Hostname:        "dc1-prod-web-01",
        Os:              "windows",
        OsVersion:       "10.0.19045",
        Arch:            "amd64",
        AgentVersion:    "1.4.2",
        EnrollmentToken: "etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c",
    }
    
    resp, _, err := client.AgentAPI.RegisterAgent(context.Background()).
        RegisterAgentRequest(req).
        Execute()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Agent registered: %s\n", resp.AgentId)
}
```

### 11.4 TypeScript SDK

| Property | Value |
|----------|-------|
| Generator | `openapi-generator-cli generate -g typescript-fetch` |
| Output directory | `sdks/typescript/` |
| Package name | `@openagentplatform/endpoint-sdk` |
| HTTP library | `fetch` (native browser/Node 18+) |
| Type safety | Full TypeScript types (no `any`) |
| Framework support | Works with React, Vue, Svelte, Node.js |
| Tests | Jest with `nock` mock library |
| Publish target | npm (`npm install @openagentplatform/endpoint-sdk`) |

**Example usage:**
```typescript
import { Configuration, AgentApi } from "@openagentplatform/endpoint-sdk";

const config = new Configuration({
  basePath: "https://api.oap.example.com",
  accessToken: "eyJhbGciOiJSUzI1NiIs...",
});

const agentApi = new AgentApi(config);

const response = await agentApi.registerAgent({
  registerAgentRequest: {
    hostname: "dc1-prod-web-01",
    os: "windows",
    osVersion: "10.0.19045",
    arch: "amd64",
    agentVersion: "1.4.2",
    enrollmentToken: "etok_8f3a2b1c9d4e5f6a7b8c9d0e1f2a3b4c",
  },
});

console.log(`Agent registered: ${response.agentId}`);
```

### 11.5 Versioning and Publishing

| SDK | Version Scheme | Publish Trigger | Registry |
|-----|----------------|-----------------|----------|
| Python | SemVer (1.0.0) | Git tag `sdk-python-v1.0.0` | PyPI |
| Go | SemVer / module path (v1.0.0) | Git tag `sdk-go-v1.0.0` | pkg.go.dev |
| TypeScript | SemVer (1.0.0) | Git tag `sdk-typescript-v1.0.0` | npm |

SDK versions are aligned with API versions:
- API `v1.0.0` -> SDKs `1.0.0`
- API `v1.1.0` (additive, non-breaking) -> SDKs `1.1.0`
- API `v2.0.0` (breaking) -> SDKs `2.0.0`

### 11.6 CI/CD Pipeline

```
Git push to main
  |
  v
CI: Run tests
  |
  v
CI: Run `make generate-sdks`
  |
  v
CI: Commit generated SDK code to sdks/ directory
  |
  v
CI: Create Git tag (e.g., sdk-python-v1.2.3)
  |
  v
CD: Publish to PyPI / pkg.go.dev / npm
  |
  v
CD: Create GitHub release with changelog
```

---

## 12. Implementation Steps

The following 22 ordered steps describe the complete implementation of the Endpoint API subsystem. Each step builds on the previous one.

### Step 1: Initialize Go Module

Create the `endpoint-api` Go module:

```bash
cd /home/user/openagentplatform
mkdir -p services/endpoint-api
cd services/endpoint-api
go mod init github.com/openagentplatform/endpoint-api
```

Add core dependencies:
```bash
go get github.com/labstack/echo/v4
go get github.com/nats-io/nats.go
go get github.com/golang-jwt/jwt/v5
go get github.com/vmihailenco/msgpack/v5
go get golang.org/x/time/rate
go get google.golang.org/grpc
go get google.golang.org/protobuf
go get github.com/google/uuid
go get github.com/swaggo/swag
go get github.com/swaggo/echo-swagger
go get go.uber.org/zap
```

### Step 2: Define Protobuf Messages

Create `proto/endpoint/v1/endpoint.proto` with all 30+ message types:

- Agent messages (5): RegisterAgentRequest, RegisterAgentResponse, ListAgentsRequest, ListAgentsResponse, AgentSummary, GetAgentRequest, AgentDetail, DeregisterAgentRequest, DeregisterAgentResponse, HeartbeatRequest, HeartbeatResponse
- Check messages (6): CheckDefinition, CreateCheckRequest, CreateCheckResponse, RunCheckRequest, RunCheckResponse, GetCheckResultsRequest, GetCheckResultsResponse, CheckResult, ListChecksRequest, ListChecksResponse
- Script messages (6): ScriptDefinition, CreateScriptRequest, CreateScriptResponse, ExecuteScriptRequest, ExecuteScriptResponse, GetScriptStatusRequest, GetScriptStatusResponse, ScriptStatus
- Event messages (4): Event, StreamEventsRequest, GetEventRequest
- System messages (3): HealthCheckRequest, HealthCheckResponse, GetVersionRequest, VersionInfo
- Patch messages (6): ScanPatchesRequest, ScanPatchesResponse, ApprovePatchRequest, ApprovePatchResponse, DeployPatchRequest, DeployPatchResponse
- Common messages (2): Pagination, Filter

### Step 3: Generate gRPC Go Stubs

```bash
# Install protoc plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go code
protoc --go_out=. --go-grpc_out=. proto/endpoint/v1/endpoint.proto
```

This produces:
- `proto/endpoint/v1/endpoint.pb.go` -- message types
- `proto/endpoint/v1/endpoint_grpc.pb.go` -- client/server interfaces

### Step 4: Implement Echo HTTP Server with Middleware Chain

Create `internal/server/server.go`:

```go
package server

import (
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
)

func New() *echo.Echo {
    e := echo.New()
    e.HideBanner = true
    e.HidePort = true
    
    // Middleware chain (order matters)
    e.Use(middleware.Recover())
    e.Use(middleware.RequestID())
    e.Use(middleware.LoggerWithConfig(loggerConfig))
    e.Use(middleware.CORSWithConfig(corsConfig))
    e.Use(middleware.RateLimiter(rateLimiterConfig))  // Custom: per-JWT
    e.Use(authMiddleware)                              // Custom: JWT validation
    
    return e
}
```

### Step 5: Implement JWT Auth Middleware

Create `internal/middleware/auth.go`:

```go
func authMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        authHeader := c.Request().Header.Get("Authorization")
        if authHeader == "" {
            return echo.NewHTTPError(401, "missing Authorization header")
        }
        
        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        claims := &Claims{}
        token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
            return jwtPublicKey, nil
        })
        if err != nil || !token.Valid {
            return echo.NewHTTPError(401, "invalid token")
        }
        
        // Set claims in context
        c.Set("jwt_subject", claims.Subject)
        c.Set("jwt_scopes", claims.Scopes)
        c.Set("jwt_tenant", claims.TenantID)
        
        return next(c)
    }
}
```

### Step 6: Implement Rate Limiting Middleware

Create `internal/middleware/ratelimit.go`:

```go
import "golang.org/x/time/rate"

var buckets = sync.Map{}  // map[string]*rate.Limiter

func getBucket(subject string) *rate.Limiter {
    if b, ok := buckets.Load(subject); ok {
        return b.(*rate.Limiter)
    }
    limiter := rate.NewLimiter(rate.Limit(5.0), 20)  // 5 req/sec, burst 20
    buckets.Store(subject, limiter)
    return limiter
}

func rateLimitMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        subject := c.Get("jwt_subject").(string)
        bucket := getBucket(subject)
        
        if !bucket.Allow() {
            c.Response().Header().Set("X-RateLimit-Limit", "300")
            c.Response().Header().Set("X-RateLimit-Remaining", "0")
            c.Response().Header().Set("Retry-After", "12")
            return echo.NewHTTPError(429, "rate limit exceeded")
        }
        
        return next(c)
    }
}
```

### Step 7: Implement Agent REST Endpoints (6)

Create `internal/handlers/agents.go` with handlers for:
- `POST /api/v1/agents/register`
- `GET /api/v1/agents`
- `GET /api/v1/agents/{id}`
- `DELETE /api/v1/agents/{id}`
- `POST /api/v1/agents/{id}/heartbeat`
- `POST /api/v1/agents/{id}/commands`

Each handler:
1. Parses and validates the request body
2. Calls the service layer (business logic)
3. Publishes events to NATS (for registration, deregistration, commands)
4. Returns the response with appropriate status code

### Step 8: Implement Check REST Endpoints (4)

Create `internal/handlers/checks.go` with handlers for:
- `GET /api/v1/checks`
- `POST /api/v1/checks`
- `POST /api/v1/checks/{id}/run` -- publishes to `oap.check.dispatch.{checkID}`
- `GET /api/v1/checks/{id}/results`

### Step 9: Implement Script REST Endpoints (4)

Create `internal/handlers/scripts.go` with handlers for:
- `GET /api/v1/scripts`
- `POST /api/v1/scripts`
- `POST /api/v1/scripts/{id}/execute` -- publishes to `oap.script.execute.{scriptID}`
- `GET /api/v1/scripts/{id}/status`

### Step 10: Implement Event Query Endpoints (2)

Create `internal/handlers/events.go` with handlers for:
- `GET /api/v1/events` -- query event_log with filters
- `GET /api/v1/events/{id}` -- single event detail

### Step 11: Implement System Endpoints (2)

Create `internal/handlers/system.go` with handlers for:
- `GET /api/v1/system/health` -- checks DB, NATS, Redis connectivity
- `GET /api/v1/system/version` -- returns version, build hash, uptime

### Step 12: Implement Patch Endpoints (3)

Create `internal/handlers/patches.go` with handlers for:
- `POST /api/v1/patches/scan`
- `POST /api/v1/patches/{id}/approve`
- `POST /api/v1/patches/{id}/deploy`

### Step 13: Implement Auth Endpoints (3)

Create `internal/handlers/auth.go` with handlers for:
- `POST /api/v1/auth/login` -- validates credentials, issues JWT
- `POST /api/v1/auth/refresh` -- validates refresh token, issues new access token
- `POST /api/v1/auth/logout` -- invalidates session

### Step 14: Set Up NATS JetStream Client and Connect

Create `internal/nats/client.go`:

```go
import "github.com/nats-io/nats.go"

func Connect(url string) (*nats.Conn, error) {
    nc, err := nats.Connect(url,
        nats.Name("endpoint-api"),
        nats.MaxReconnects(-1),
        nats.ReconnectWait(2*time.Second),
        nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
    )
    if err != nil {
        return nil, err
    }
    return nc, nil
}
```

### Step 15: Declare 4 JetStream Streams

Create `internal/nats/streams.go`:

```go
func DeclareStreams(js jetstream.JetStream) error {
    streams := []jetstream.StreamConfig{
        {
            Name:     "AGENTS",
            Subjects: []string{"oap.endpoint.>"},
            Retention: jetstream.LimitsPolicy,
            MaxMsgs:  10_000_000,
            MaxBytes: 50 * 1024 * 1024 * 1024,
            Storage:  jetstream.FileStorage,
            Discard:  jetstream.DiscardOld,
        },
        {
            Name:     "CHECKS",
            Subjects: []string{"oap.check.>"},
            Retention: jetstream.LimitsPolicy,
            MaxMsgs:  50_000_000,
            MaxBytes: 200 * 1024 * 1024 * 1024,
            Storage:  jetstream.FileStorage,
            Discard:  jetstream.DiscardOld,
        },
        {
            Name:     "SCRIPTS",
            Subjects: []string{"oap.script.>"},
            Retention: jetstream.LimitsPolicy,
            MaxMsgs:  5_000_000,
            MaxBytes: 20 * 1024 * 1024 * 1024,
            Storage:  jetstream.FileStorage,
            Discard:  jetstream.DiscardOld,
        },
        {
            Name:     "WINUPDATE",
            Subjects: []string{"oap.winupdate.>"},
            Retention: jetstream.LimitsPolicy,
            MaxMsgs:  20_000_000,
            MaxBytes: 100 * 1024 * 1024 * 1024,
            Storage:  jetstream.FileStorage,
            Discard:  jetstream.DiscardOld,
        },
    }
    
    for _, cfg := range streams {
        _, err := js.CreateOrUpdateStream(cfg)
        if err != nil {
            return fmt.Errorf("create stream %s: %w", cfg.Name, err)
        }
    }
    return nil
}
```

### Step 16: Implement 5 Consumer Groups

Create `internal/nats/consumers.go`:

```go
func CreateConsumers(js jetstream.JetStream) error {
    consumers := []struct {
        Stream    string
        Name      string
        Filter    string
    }{
        {"AGENTS", "api-cmd-dispatcher", "oap.endpoint.*.commands.ack"},
        {"CHECKS", "check-result-ingester", "oap.check.results.>"},
        {"SCRIPTS", "script-output-relay", "oap.script.output.>"},
        {"WINUPDATE", "winupdate-processor", "oap.winupdate.events.>"},
        {"CHECKS", "event-persister", "oap.check.events.>"},
    }
    
    for _, c := range consumers {
        _, err := js.CreateOrUpdateConsumer(c.Stream, jetstream.ConsumerConfig{
            Durable:       c.Name,
            FilterSubject: c.Filter,
            DeliverPolicy: jetstream.DeliverAllPolicy,
            AckPolicy:     jetstream.AckExplicitPolicy,
            AckWait:       30 * time.Second,
            MaxDeliver:    5,
            MaxAckPending: 1000,
        })
        if err != nil {
            return fmt.Errorf("create consumer %s: %w", c.Name, err)
        }
    }
    return nil
}
```

### Step 17: Implement Agent Binary Go Module

```bash
mkdir -p agents/oap-agent
cd agents/oap-agent
go mod init github.com/openagentplatform/oap-agent
```

Add dependencies:
```bash
go get github.com/nats-io/nats.go
go get github.com/google/uuid
go get github.com/vmihailenco/msgpack/v5
go get golang.org/x/sys/unix  # For prlimit on Linux
```

### Step 18: Implement Agent Lifecycle State Machine

Create `agents/oap-agent/internal/lifecycle/state.go`:

```go
type State int

const (
    StateNew State = iota
    StateRegistering
    StateRegistered
    StateStale
    StateOffline
    StateDeregistered
)

type StateMachine struct {
    mu    sync.Mutex
    state State
    onTransition func(from, to State)
}

func (sm *StateMachine) Transition(to State) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    from := sm.state
    if !isValidTransition(from, to) {
        return  // Ignore invalid transitions
    }
    sm.state = to
    if sm.onTransition != nil {
        sm.onTransition(from, to)
    }
}

func isValidTransition(from, to State) bool {
    valid := map[State][]State{
        StateNew:          {StateRegistering, StateDeregistered},
        StateRegistering:  {StateRegistered, StateStale, StateDeregistered},
        StateRegistered:   {StateStale, StateDeregistered},
        StateStale:        {StateRegistered, StateOffline, StateDeregistered},
        StateOffline:      {StateRegistering, StateDeregistered},
    }
    for _, s := range valid[from] {
        if s == to {
            return true
        }
    }
    return false
}
```

### Step 19: Implement Agent NATS Subscription and Reconnect

Create `agents/oap-agent/internal/transport/nats.go`:

```go
type Client struct {
    nc     *nats.Conn
    agentID string
    js     jetstream.JetStream
    sub    jetstream.StreamConsumer
}

func (c *Client) Connect(url, token string) error {
    nc, err := nats.Connect(url,
        nats.Name(fmt.Sprintf("oap-agent-%s", c.agentID)),
        nats.Token(token),
        nats.MaxReconnects(-1),
        nats.ReconnectWait(1*time.Second),
        nats.ReconnectJitter(200*time.Millisecond, 6*time.Second),
    )
    if err != nil {
        return err
    }
    c.nc = nc
    c.js, _ = nc.JetStream()
    return nil
}

func (c *Client) SubscribeCommandChannel(handler func([]byte)) error {
    subject := fmt.Sprintf("oap.endpoint.%s.commands", c.agentID)
    
    consumer, err := c.js.PullSubscribe(subject, "agent-cmd-consumer", jetstream.PullMaxMessages(1))
    if err != nil {
        return err
    }
    c.sub = consumer
    
    go func() {
        for {
            msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(1*time.Second))
            if err != nil {
                continue
            }
            for _, msg := range msgs {
                handler(msg.Data)
                msg.Ack()
            }
        }
    }()
    return nil
}
```

### Step 20: Implement Script Executor with 4 Runtimes and prlimit

Create `agents/oap-agent/internal/executor/script.go`:

```go
func Execute(runtime, scriptPath string, args []string, timeout time.Duration) (*Result, error) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    
    var cmd *exec.Cmd
    switch runtime {
    case "python3":
        cmd = exec.CommandContext(ctx, "python3", append([]string{"-I", "-E", "-s", scriptPath}, args...)...)
    case "bash":
        cmd = exec.CommandContext(ctx, "/bin/bash", append([]string{"--noprofile", "--norc", "-e", "-o", "pipefail", scriptPath}, args...)...)
    case "powershell":
        cmd = exec.CommandContext(ctx, "powershell.exe", append([]string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath}, args...)...)
    case "node":
        cmd = exec.CommandContext(ctx, "node", append([]string{"--no-warnings", "--experimental-vm-modules", scriptPath}, args...)...)
    default:
        return nil, fmt.Errorf("unsupported runtime: %s", runtime)
    }
    
    // Apply prlimit on Linux
    if runtime.GOOS == "linux" {
        cmd.SysProcAttr = &syscall.SysProcAttr{
            // prlimit settings applied via setrlimit in Start() callback
        }
        cmd.Cancel = func() error {
            // Apply RLIMIT_CPU kill
            return syscall.Kill(cmd.Process.Pid, syscall.SIGKILL)
        }
    }
    
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    
    start := time.Now()
    err := cmd.Run()
    duration := time.Since(start)
    
    result := &Result{
        ExitCode:   getExitCode(err),
        Stdout:     stdout.String(),
        Stderr:     stderr.String(),
        DurationMs: duration.Milliseconds(),
    }
    return result, err
}
```

### Step 21: Generate OpenAPI 3.1 Spec

```bash
# Install swag CLI
go install github.com/swaggo/swag/cmd/swag@latest

# Generate spec from Go comments
cd services/endpoint-api
swag init -g cmd/server/main.go -o docs/api/
```

This produces:
- `docs/api/openapi.json` -- OpenAPI 3.1 spec
- `docs/api/docs.go` -- Generated Go package for serving Swagger UI

### Step 22: Generate Python, Go, TypeScript SDKs

```bash
# Install openapi-generator-cli
npm install -g @openapitools/openapi-generator-cli

# Generate Python SDK
openapi-generator-cli generate \
  -i services/endpoint-api/docs/api/openapi.json \
  -g python \
  -o sdks/python/ \
  --additional-properties=packageName=oap_endpoint

# Generate Go SDK
openapi-generator-cli generate \
  -i services/endpoint-api/docs/api/openapi.json \
  -g go \
  -o sdks/go/ \
  --additional-properties=packageName=oap,modulePath=github.com/openagentplatform/endpoint-sdk-go

# Generate TypeScript SDK
openapi-generator-cli generate \
  -i services/endpoint-api/docs/api/openapi.json \
  -g typescript-fetch \
  -o sdks/typescript/ \
  --additional-properties=npmName=@openagentplatform/endpoint-sdk

# Publish
cd sdks/python && python -m build && twine upload dist/*
cd sdks/go && git tag sdk-go-v1.0.0 && git push --tags
cd sdks/typescript && npm publish
```

---

**End of Document**
