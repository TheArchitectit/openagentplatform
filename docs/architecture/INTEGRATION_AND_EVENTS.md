# Integration & Event Flow Architecture

> **Document:** INTEGRATION_AND_EVENTS.md
> **Version:** 1.0
> **Last Updated:** 2026-06-15
> **Status:** Living Document

---

## Table of Contents

1. [Overview](#1-overview)
2. [Service Communication Map](#2-service-communication-map)
3. [Complete Event Flow Map (NATS Subject Taxonomy)](#3-complete-event-flow-map-nats-subject-taxonomy)
4. [Critical Data Flow Diagrams](#4-critical-data-flow-diagrams)
5. [Shared Schemas](#5-shared-schemas)
6. [Error Propagation](#6-error-propagation)
7. [Consistency Patterns](#7-consistency-patterns)

---

## 1. Overview

OpenAgentPlatform (OAP) is an agent-first Remote Monitoring and Management (RMM) platform that embeds LLM-based agents as first-class citizens. The architecture is composed of ten independently deployable services that communicate over a dual-transport core (REST for CRUD + NATS JetStream for real-time messaging) and an A2A (Agent-to-Agent) protocol backbone for inter-agent orchestration.

### 1.1 Subsystem Inventory

| # | Subsystem | Language | Port | Purpose |
|---|-----------|----------|------|---------|
| S1 | API Server | Python 3.12 | 8080 | REST/gRPC API, NATS publishers/consumers, RMM core logic, Celery workers |
| S2 | A2A Gateway | Go | 8082 | Central A2A broker, Agent Card registry, Task lifecycle router |
| S3 | Agent Adapter | Python 3.12 | 8090 | Framework-native wrappers (LangGraph, CrewAI, AutoGen, SK, OpenAI, Claude) |
| S4 | Secret Service | Go | 8200 | Vault/Infisical abstraction, SecretReference resolver, lease management |
| S5 | Agent (endpoint) | Go | N/A | Cross-compiled binary deployed to managed devices |
| S6 | Frontend | TypeScript/React | 3000 | TanStack Router/Query, real-time WebSocket dashboards |
| S7 | PostgreSQL | C | 5432 | Primary persistence (agents, checks, alerts, tasks, audit) |
| S8 | Redis | C | 6379 | Idempotency keys, rate limiting, session cache, dashboard cache |
| S9 | NATS JetStream | Go | 4222 | Primary message bus for agent communication and event distribution |
| S10 | OTel Collector | Go | 4317 | OpenTelemetry observability (traces, metrics, logs) |

### 1.2 The Big Picture

```
+--------------------------------------------------------------------------+
|                          FRONTEND (TypeScript/React)                      |
|                    TanStack Query + WebSocket (ws://)                      |
+-------------------------------------+------------------------------------+
                                      |
                                      | REST/JSON + WebSocket
                                      v
+--------------------------------------------------------------------------+
|                           API SERVER (Python)                             |
|  +-----------+ +-----------+ +-----------+ +-----------+ +-----------+  |
|  |  RMM Core | |  Check    | |  Alert    | |  Script   | |  Patch    |  |
|  |  (Django) | |  Engine   | |  Engine   | |  Engine   | |  Engine   |  |
|  +-----------+ +-----------+ +-----------+ +-----------+ +-----------+  |
|  |  Policy   | |  Checkin  | | Inventory | | Propagation| |  Celery   |  |
|  |  Engine   | |  Handler  | | Collector | |            | |  Workers  |  |
|  +-----------+ +-----------+ +-----------+ +-----------+ +-----------+  |
+-------------------+-------------------+----------------------------------+
        |           |                   |                    |
        | NATS      | Go channel +      | REST + Bearer      |
        | msgpack   | JSON-RPC          |                    |
        v           v                   v                    |
+-------+---+   +---+-------+    +------+------+             |
|  AGENT   |   |  A2A      |    |  SECRET     |             |
|  (Go)    |   |  GATEWAY  |    |  SERVICE    |             |
|  binary  |   |  (Go)     |    |  (Go)       |             |
+----------+   +----+------+    +------+------+             |
                    |                                        |
                    | JSON-RPC 2.0 + SSE                    |
                    v                                        |
              +-----+------+                                 |
              |  AGENT     |                                 |
              |  ADAPTER   |                                 |
              |  (Python)  |                                 |
              +----+-------+                                 |
                   |                                         |
                   | Framework-native (LangGraph.astream,   |
                   | CrewAI.kickoff, etc.)                   |
                   v                                         |
              +----+-------+         +--------------+        |
              |  LLM       |         |   HashiCorp  |        |
              |  Provider  |         |   Vault /    |        |
              |  (external)|         |   Infisical  |        |
              +------------+         +--------------+        |
                                                          |
+---------------------------------------------------------+
|  INFRASTRUCTURE LAYER (shared by all services)
|  PostgreSQL :5432  |  Redis :6379  |  NATS :4222  |  OTel :4317
+----------------------------------------------------------+
```

### 1.3 Design Principles

1. **Dual-Transport Core:** REST/HTTP for CRUD operations and periodic check-ins; NATS for real-time command execution, streaming output, and event distribution.
2. **Agent-First:** LLM agents are first-class citizens, not add-ons. Every subsystem is designed to be agent-accessible via the A2A protocol.
3. **Protocol-Agnostic A2A:** Agents running on different frameworks (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Claude Agent SDK) interoperate through a standardized JSON-RPC 2.0 + SSE interface.
4. **Resilience by Default:** Every inter-service dependency has a defined fallback path (see Section 6). No single component failure should cascade into total platform outage.
5. **Multi-Tenant Isolation:** Every data entity carries a `tenant_id` (org_id) and every query is scoped. Cross-tenant access is structurally impossible.

---

## 2. Service Communication Map

Every service-to-service connection in the platform, with protocol, data contract, port, and purpose.

### 2.1 Agent <-> API Server

| Attribute | Value |
|-----------|-------|
| **Protocol** | NATS JetStream with msgpack serialization |
| **Direction** | Bidirectional |
| **Subjects (Agent -> Server)** | `rmm.agent.heartbeat`, `rmm.agent.checkin`, `rmm.check.result.{agent_id}`, `rmm.script.result.{agent_id}`, `rmm.script.chunk.{agent_id}`, `rmm.winupdate.scan.{agent_id}`, `rmm.winupdate.install.{agent_id}`, `rmm.agent.inventory.{agent_id}` |
| **Subjects (Server -> Agent)** | `rmm.cmd.{agent_id}.script.run`, `rmm.cmd.{agent_id}.script.cancel`, `rmm.cmd.{agent_id}.check.run`, `rmm.cmd.{agent_id}.winupdate.install`, `rmm.cmd.{agent_id}.winupdate.scan`, `rmm.cmd.{agent_id}.sync`, `rmm.cmd.{agent_id}.agent.update`, `rmm.cmd.{agent_id}.remote.open`, `rmm.cmd.{agent_id}.remote.close`, `rmm.cmd.{agent_id}.policy.push`, `rmm.cmd.{agent_id}.inventory.refresh` |
| **Data Contract** | AgentEvent (protobuf) wrapped in msgpack envelope; per-agent inbox subject pattern |
| **Purpose** | Bidirectional RMM control plane: agent reports state, server dispatches commands |
| **Fallback** | REST polling: agent calls `GET /api/v1/agents/{id}/commands` every 10s; server queues to `command_queue` table |

### 2.2 API Server <-> A2A Gateway

| Attribute | Value |
|-----------|-------|
| **Protocol** | In-process Go channel + JSON-RPC 2.0 |
| **Direction** | Bidirectional |
| **Subjects** | Event bridge: RMM event -> A2A task creation; task status -> RMM action |
| **Data Contract** | JSON-RPC 2.0 over Unix domain socket (same pod) or TCP |
| **Purpose** | Bridge RMM domain events into A2A tasks for agent orchestration; relay task state changes back to RMM for action execution |
| **Fallback** | `pending_a2a_tasks` table in PostgreSQL for durable queueing when gateway is down |

### 2.3 A2A Gateway <-> Agent Adapter

| Attribute | Value |
|-----------|-------|
| **Protocol** | JSON-RPC 2.0 over HTTP + Server-Sent Events (SSE) |
| **Direction** | Bidirectional |
| **Endpoints** | `POST /a2a/v1/message:send`, `POST /a2a/v1/message:stream`, `GET /a2a/v1/agent-card` |
| **Data Contract** | A2A Task (protobuf + JSON), Artifact (protobuf) |
| **Purpose** | Route `SendMessage` / `SendStreamingMessage` to the correct framework-specific adapter; stream LLM responses back via SSE |
| **Fallback** | Retry with exponential backoff (3 attempts); task transitions to FAILED if all adapters unreachable |

### 2.4 Agent Adapter <-> LLM Provider

| Attribute | Value |
|-----------|-------|
| **Protocol** | Framework-native (not OAP-controlled) |
| **Direction** | Bidirectional |
| **Implementations** | LangGraph `.astream()`, CrewAI `.kickoff()`, AutoGen `initiate_chat()`, Semantic Kernel `invoke()`, OpenAI Agents SDK `Runner.run()`, Claude Agent SDK `query()` |
| **Data Contract** | Framework-specific message/prompt format; normalized to A2A Message at adapter boundary |
| **Purpose** | Invoke LLM-based agent logic; stream tokens back through A2A Gateway |
| **Fallback** | Retry 3x with exponential backoff (1s, 2s, 4s); then `InvokeError(retryable=true)`; task state -> FAILED |

### 2.5 API Server <-> Secret Service

| Attribute | Value |
|-----------|-------|
| **Protocol** | REST/HTTPS + Bearer token authentication |
| **Direction** | Bidirectional |
| **Endpoints** | `GET /v1/secret/{ref}`, `POST /v1/secret/{ref}/rotate`, `DELETE /v1/secret/{ref}/lease` |
| **Data Contract** | SecretReference (JSON URI) for requests; raw secret value in Vault Transit-wrapped response |
| **Purpose** | Resolve `ref:oap://...` references to actual credential values; manage lease lifecycle; inject into script/check execution context |
| **Fallback** | Scripts requiring secrets return `status=error, "secret_unavailable"`; non-secret checks continue normally |

### 2.6 Agent <-> Secret Service (direct)

| Attribute | Value |
|-----------|-------|
| **Protocol** | gRPC streaming |
| **Direction** | Agent subscribes |
| **Subject** | `secret.Watch` (gRPC server-streaming RPC) |
| **Data Contract** | SecretRotationEvent protobuf |
| **Purpose** | Agents receive credential rotation notifications without re-polling the API server; enables zero-downtime credential refresh for long-running processes |
| **Fallback** | Agent falls back to polling Secret Service REST endpoint every 60s |

### 2.7 Frontend <-> API Server

| Attribute | Value |
|-----------|-------|
| **Protocol** | REST/JSON (queries/mutations) + WebSocket (real-time push) |
| **Direction** | Bidirectional |
| **REST Endpoints** | `/api/v1/agents`, `/api/v1/checks`, `/api/v1/alerts`, `/api/v1/scripts`, `/api/v1/patches`, `/api/v1/tasks` |
| **WebSocket** | `ws://api:8080/ws?token=<JWT>` |
| **Data Contract** | JSON for REST; AgentEvent protobuf over WebSocket frames |
| **Purpose** | Dashboard data fetching via TanStack Query; real-time agent status, check results, and alert notifications via WebSocket |
| **Fallback** | WebSocket auto-reconnect with exponential backoff; TanStack Query refetch on reconnect |

### 2.8 All Services -> OTel Collector

| Attribute | Value |
|-----------|-------|
| **Protocol** | OTLP gRPC (primary) / OTLP HTTP (fallback) |
| **Direction** | Unidirectional (export) |
| **Port** | 4317 (gRPC) / 4318 (HTTP) |
| **Data Contract** | OpenTelemetry protobuf (traces, metrics, logs) |
| **Purpose** | Centralized observability: distributed tracing across service boundaries, RED metrics per service, structured log aggregation |
| **Fallback** | Local file logging; metrics buffer in-process (1k ring) with flush on recovery |

### 2.9 Communication Summary Table

| # | Source | Target | Protocol | Port | Data Contract | Purpose |
|---|--------|--------|----------|------|---------------|---------|
| 1 | Agent | API Server | NATS msgpack | 4222 | AgentEvent | Agent state reporting |
| 2 | API Server | Agent | NATS JSON | 4222 | Command envelopes | Command dispatch |
| 3 | API Server | A2A Gateway | Go channel + JSON-RPC | IPC | JSON-RPC 2.0 | RMM -> A2A event bridge |
| 4 | A2A Gateway | Agent Adapter | JSON-RPC 2.0 + SSE | 8090 | Task/Artifact | Task routing and streaming |
| 5 | Agent Adapter | LLM Provider | Framework-native | varies | Provider-specific | LLM invocation |
| 6 | API Server | Secret Service | REST + Bearer | 8200 | SecretReference | Credential resolution |
| 7 | Agent | Secret Service | gRPC streaming | 8200 | SecretRotationEvent | Credential rotation watch |
| 8 | Frontend | API Server | REST/JSON + WebSocket | 8080 | JSON / AgentEvent | Dashboard and real-time push |
| 9 | All services | OTel Collector | OTLP gRPC | 4317 | OTel protobuf | Observability export |

---

## 3. Complete Event Flow Map (NATS Subject Taxonomy)

The NATS JetStream bus is the primary real-time communication channel. All subjects follow the pattern `rmm.<domain>.<action>.<scope>`. JetStream consumer groups provide at-least-once delivery with replay capability.

### 3.1 Agent Lifecycle Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.agent.heartbeat` | Agent -> Server | Agent (every 60s) | CheckinHandler | Scheduled timer | Update `last_seen` in DB; extend online TTL (90s) |
| `rmm.agent.checkin` | Agent -> Server | Agent (every 5-15 min) | CheckinHandler | Scheduled timer | Full state merge: agent info, services, disks, public IP, WMI; emit inventory delta events |
| `rmm.agent.inventory.{agent_id}` | Agent -> Server | Agent (on change) | InventoryCollector | Software/hardware change detected locally | Diff against last inventory; emit `inventory.changed` event if delta |
| `rmm.agent.status.changed` | Server -> Internal | CheckinHandler | All services | Agent transitions online/offline/degraded | WebSocket push to Frontend; update dashboard cache |
| `rmm.broadcast.all` | Server -> All Agents | API Server (admin) | All connected agents | Admin broadcast | Display message / run maintenance action |
| `rmm.broadcast.org.{org_id}` | Server -> Tenant Agents | API Server (admin) | Agents in tenant | Tenant-scoped broadcast | Tenant-wide notification or action |

### 3.2 Check Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.check.dispatch.{agent_id}` | Server -> Agent | CheckEngine | Agent | Scheduled check due | Execute check locally; collect result |
| `rmm.check.result.{agent_id}` | Agent -> Server | Agent | CheckEngine | Check execution complete | Evaluate against thresholds; create alert if failed; update dashboard |
| `rmm.check.result.aggregated` | Internal | CheckEngine | AlertEngine, Dashboard | Result evaluated | If failing: generate dedup key, check alert state machine, create/update alert |
| `rmm.check.schedule.updated` | Internal | API Server | CheckEngine | Check CRUD or policy change | Recompute next-due times; re-dispatch affected agents |

### 3.3 Script Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.cmd.{agent_id}.script.run` | Server -> Agent | ScriptEngine | Agent | User/A2A task initiates script | Resolve secret references; spawn process (PowerShell/Python/Bash/Node); stream output |
| `rmm.cmd.{agent_id}.script.cancel` | Server -> Agent | ScriptEngine | Agent | User cancellation or timeout | Kill process tree; emit final result with status=cancelled |
| `rmm.script.chunk.{agent_id}` | Agent -> Server | Agent (streaming) | ScriptEngine | stdout/stderr line emitted | Forward chunk to WebSocket subscribers; persist to script result buffer |
| `rmm.script.result.{agent_id}` | Agent -> Server | Agent | ScriptEngine | Process exits | Transition ScriptResultStateMachine: running -> success/error/timeout; emit completion event |
| `rmm.script.output.request` | Server -> Agent | Frontend (via WS) | Agent | User opens live terminal | Begin streaming output for a specific script run ID |

### 3.4 Patch (WinUpdate) Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.cmd.{agent_id}.winupdate.scan` | Server -> Agent | PatchEngine | Agent | Scan schedule or manual trigger | Enumerate pending updates; publish results |
| `rmm.cmd.{agent_id}.winupdate.install` | Server -> Agent | PatchEngine | Agent | Approved batch deploy | Install patches sequentially; track per-patch state |
| `rmm.winupdate.scan.{agent_id}` | Agent -> Server | Agent | PatchEngine | Scan complete | Update WinUpdate records; transition state machine: -> pending_approval |
| `rmm.winupdate.install.{agent_id}` | Agent -> Server | Agent | PatchEngine | Per-patch install result | Transition: installing -> installed/failed/reboot_required; emit saga step result |
| `rmm.winupdate.batch.approved` | Internal | API Server | PatchEngine | Admin approves batch | Initiate saga: mark approved -> dispatch install commands |
| `rmm.winupdate.reboot.coordinate` | Internal | PatchEngine | Agent (deferred) | Reboot-required patches accumulated | Schedule reboot window; notify user; or force reboot per policy |

### 3.5 Alert Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.alert.created` | Internal | AlertEngine | NotificationDispatcher, A2A Bridge | New alert from failing check | Dispatch notifications (email/Slack/webhook); create A2A task for LLM triage |
| `rmm.alert.updated` | Internal | AlertEngine | Dashboard, WebSocket | Alert state transition (ack/resolve/snooze) | Push updated alert to subscribed Frontend clients |
| `rmm.alert.escalated` | Internal | AlertEngine | A2A Bridge | Alert unresolved past SLA | Create high-priority A2A task; notify on-call |
| `rmm.alert.resolved` | Internal | AlertEngine | Audit, Dashboard | Alert state -> resolved | Write audit log entry; push resolution to Frontend |

### 3.6 A2A (Agent-to-Agent) Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.a2a.task.created` | Internal | A2A Gateway | API Server, Dashboard | New A2A task from RMM event or external client | Persist Task; push to Frontend; dispatch to Agent Adapter |
| `rmm.a2a.task.state.changed` | Internal | A2A Gateway | API Server, Dashboard | Task transitions state (working -> input-required -> completed/failed/cancelled) | Update DB; push state to Frontend WebSocket |
| `rmm.a2a.task.artifact.added` | Internal | A2A Gateway | API Server, Dashboard | Agent produces an Artifact (file, structured data, log) | Persist artifact; make available for download or further task chaining |
| `rmm.a2a.agent.card.registered` | Internal | A2A Gateway | Service Registry | New agent framework registered | Update Agent Card registry; emit discovery event |
| `rmm.a2a.agent.card.updated` | Internal | Agent Adapter | A2A Gateway | Agent capabilities changed | Update registry; notify interested subscribers |

### 3.7 Secret Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.secret.accessed` | Internal | Secret Service | AuditLog | Secret resolved and returned | Write audit entry: who, what ref, when, source IP |
| `rmm.secret.rotated` | Secret Service -> Agent | Secret Service | Agent (gRPC watch), API Server | Vault/Infisical rotation triggered | Agent refreshes in-memory credential; invalidates old lease |
| `rmm.secret.lease.expiring` | Internal | Secret Service | API Server | Lease TTL < 5 min remaining | Proactive renewal or script abort signal |
| `rmm.secret.revoked` | Internal | Secret Service | API Server, Agent | Admin or automatic revocation | Immediate lease invalidation (<100ms propagation); active scripts using revoked creds get auth failure |

### 3.8 Policy and System Events

| Subject | Direction | Publisher | Subscriber | Trigger | Subscriber Action |
|---------|-----------|-----------|------------|---------|-------------------|
| `rmm.policy.changed` | Internal | API Server | Propagation service | Policy CRUD | Compute delta; push affected agents via `rmm.cmd.{agent_id}.policy.push` |
| `rmm.cmd.{agent_id}.policy.push` | Server -> Agent | Propagation | Agent | Policy delta for agent | Apply policy locally; emit ack |
| `rmm.cmd.{agent_id}.sync` | Server -> Agent | API Server | Agent | Full resync request (post-recovery) | Re-download all checks, policies, scripts |
| `rmm.cmd.{agent_id}.agent.update` | Server -> Agent | API Server | Agent | New agent binary available | Download and self-update; restart |
| `rmm.remote.session.event.{agent_id}` | Agent -> Server | Agent | RemoteAccess | Remote session lifecycle event | Update session state machine; relay recording metadata |
| `rmm.audit.log` | Internal | All services | AuditLog collector | Any auditable action | Append-only write to PostgreSQL audit table (serial PK) |
| `rmm.system.health` | Internal | All services | HealthAggregator | Periodic self-report | Aggregate health for load balancer and dashboard |

---

## 4. Critical Data Flow Diagrams

### 4.1 Agent Check-In -> Check Failure -> Alert -> A2A Task -> LLM Agent Triage -> Remediation Script

This is the canonical "something broke, an LLM agent figured out how to fix it" flow.

```
  +----------+     +----------+     +----------+     +----------+
  |  Agent   |     |  Check   |     |  Alert   |     |  A2A     |
  |  (Go)    |     |  Engine  |     |  Engine  |     |  Gateway |
  +----+-----+     +----+-----+     +----+-----+     +----+-----+
       |                |                |                |
  (1)  | NATS msgpack  |                |                |
       | rmm.check.    |                |                |
       | result.       |                |                |
       | {agent_id}    |                |                |
       |-------------->|                |                |
       |               | (2)            |                |
       |               | Evaluate       |                |
       |               | against        |                |
       |               | thresholds     |                |
       |               |                |                |
       |               | (3)            |                |
       |               | FAIL detected  |                |
       |               | Generate dedup |                |
       |               | key            |                |
       |               |--------------->|                |
       |               |                | (4)            |
       |               |                | Create Alert   |
       |               |                | state=new      |
       |               |                |                |
       |               |                | (5)            |
       |               |                | Emit           |
       |               |                | rmm.alert.     |
       |               |                | created        |
       |               |                |--+             |
       |               |                |  | (6)         |
       |               |                |  | A2A Bridge  |
       |               |                |  | creates     |
       |               |                |  | Task from   |
       |               |                |  | alert       |
       |               |                |  v             |
       |               |                |        +-------+
       |               |                |        |       |
       |               |                |        v       |
       |               |                |  +-----+------+|
       |               |                |  | A2A        ||
       |               |                |  | Gateway    ||
       |               |                |  | (Go)       ||
       |               |                |  +-----+------+|
       |               |                |        |       |
       |               |                | (7)    |       |
       |               |                |        | JSON-RPC 2.0
       |               |                |        | SendMessage
       |               |                |        v       |
       |               |                |  +-----+------+|
       |               |                |  | Agent      ||
       |               |                |  | Adapter    ||
       |               |                |  | (Python)   ||
       |               |                |  +-----+------+|
       |               |                |        |       |
       |               |                | (8)    |       |
       |               |                |        | LangGraph.astream()
       |               |                |        | "Disk 92% on agent-4471,
       |               |                |        |  check disk usage,
       |               |                |        |  identify large logs,
       |               |                |        |  generate cleanup script"
       |               |                |        v       |
       |               |                |  +-----+------+|
       |               |                |  | LLM        ||
       |               |                |  | Provider   ||
       |               |                |  | (Anthropic)||
       |               |                |  +-----+------+|
       |               |                |        |       |
       |               |                | (9)    |       |
       |               |                |        | Streaming
       |               |                |        | response +
       |               |                |        | Artifact:
       |               |                |        | { kind: "data",
       |               |                |        |   data: { script_id: 89,
       |               |                |        |            action: "cleanup" } }
       |               |                |        v       |
       |               |                |  +-----+------+|
       |               |                |  | A2A        ||
       |               |                |  | Gateway    ||
       |               |                |  | persists   ||
       |               |                |  | Task+      ||
       |               |                |  | Artifact   ||
       |               |                |  +-----+------+|
       |               |                |        |       |
       |               |                | (10)   |       |
       |               |                |        | SSE: task
       |               |                |        | state -> input-required
       |               |                |        | (HITL approval needed)
       |               |                |        v       |
       |               |                |  +-----+------+|     +----------+
       |               |                |  | Frontend   ||     | Operator |
       |               |                |  | Dashboard  ||<--->| (human)  |
       |               |                |  +------------+|     +----------+
       |               |                |        |       |
       |               |                | (11)   |       |
       |               |                |        | Operator approves
       |               |                |        v       |
       |               |                |  +-----+------+
       |               |                |  | A2A Gateway|
       |               |                |  | resumes    |
       |               |                |  | task       |
       |               |                |  +-----+------+
       |               |                |        |
       |               |                | (12)   | Internal channel
       |               |                |        | to API Server:
       |               |                |        | "Execute script 89
       |               |                |        |  on agent-4471"
       |               |                |        v
       |               |                |  +-----+------+
       |               |                |  | Script     |
       |               |                |  | Engine     |
       |               |                |  +-----+------+
       |               |                |        |
       |               |                | (13)   | NATS JSON
       |               |                |        | rmm.cmd.{agent_id}
       |               |                |        | .script.run
       |               |                |        v
       |               |                |  +-----+------+
       |               |                |  |  Agent     |
       |               |                |  |  (Go)      |
       |               |                |  +-----+------+
       |               |                |        |
       |               |                | (14)   | Resolve secret refs
       |               |                |        | via Secret Service
       |               |                |        | Spawn process
       |               |                |        | Stream output
       |               |                |        v
       |               |                |  +-----+------+
       |               |                |  | Script     |
       |               |                |  | Result     |
       |               |                |  | success    |
       |               |                |  +-----+------+
       |               |                |        |
       |               |                | (15)   | rmm.script.result
       |               |                |        | .{agent_id}
       |               |                |        v
       |               |                |  +-----+------+
       |               |                |  | Alert      |
       |               |                |  | Engine     |
       |               |                |  | resolve    |
       |               |                |  | alert      |
       |               |                |  +-----+------+
       |               |                |        |
       |               |                | (16)   | rmm.alert.resolved
       |               |                |        v
       |               |                |  [DONE]
```

**Step-by-step annotations:**

1. Agent completes a disk space check and publishes the result on `rmm.check.result.{agent_id}`.
2. CheckEngine evaluates the result against configured thresholds (e.g., warning at 80%, failure at 90%).
3. Threshold exceeded -> CheckEngine generates a dedup key (`{tenant}:{agent}:{check_type}:fingerprint`) and dispatches to AlertEngine.
4. AlertEngine creates a new Alert record with state `new`, applies deduplication, and persists to PostgreSQL.
5. AlertEngine emits `rmm.alert.created` on the internal NATS bus.
6. The A2A bridge (subscribed to `rmm.alert.created`) creates an A2A Task with the alert context as the initial message.
7. A2A Gateway routes the task to the Agent Adapter via JSON-RPC 2.0 `SendMessage`.
8. Agent Adapter invokes the LangGraph agent with the alert context. The LLM analyzes the situation.
9. LLM streams back a response and produces an Artifact: a remediation script proposal.
10. A2A Gateway persists the Task (state: `input-required`) and Artifact, then pushes state via SSE to Frontend.
11. Human operator reviews and approves the proposed remediation in the Frontend.
12. A2A Gateway resumes the task; the resume action is sent to the API Server via internal channel.
13. ScriptEngine dispatches the approved script to the agent via `rmm.cmd.{agent_id}.script.run`.
14. Agent resolves any SecretReferences via Secret Service, spawns the process, and streams output.
15. Agent publishes final result on `rmm.script.result.{agent_id}`. ScriptEngine persists it.
16. ScriptEngine signals AlertEngine that the remediation succeeded; AlertEngine transitions alert to `resolved` and emits `rmm.alert.resolved`.

### 4.2 Patch Scan -> Patch Available -> Approval -> A2A Task -> Agent Evaluates Risk -> Approve/Install

```
  +----------+     +----------+     +----------+     +----------+
  |  Agent   |     |  Patch   |     |  A2A     |     |  Agent   |
  |  (Go)    |     |  Engine  |     |  Gateway |     |  Adapter |
  +----+-----+     +----+-----+     +----+-----+     +----+-----+
       |                |                |                |
  (1)  | NATS           |                |                |
       | rmm.cmd.       |                |                |
       | {agent_id}     |                |                |
       | .winupdate     |                |                |
       | .scan          |                |                |
       |<---------------|                |                |
       |                |                |                |
  (2)  | Enumerate      |                |                |
       | pending        |                |                |
       | updates        |                |                |
       | locally        |                |                |
       |                |                |                |
  (3)  | rmm.winupdate  |                |                |
       | .scan.         |                |                |
       | {agent_id}     |                |                |
       |-------------->|                |                |
       |               | (4)            |                |
       |               | Persist        |                |
       |               | WinUpdate      |                |
       |               | records        |                |
       |               | state=scanned  |                |
       |               |                |                |
       |               | (5)            |                |
       |               | If new patches |                |
       |               | found: emit    |                |
       |               | internal event |                |
       |               | "patches_      |                |
       |               |  available"    |                |
       |               |--+             |                |
       |               |  | (6)         |                |
       |               |  | A2A Bridge  |                |
       |               |  | creates     |                |
       |               |  | Task:       |                |
       |               |  | "Evaluate   |                |
       |               |  |  risk of    |                |
       |               |  |  KB5034123  |                |
       |               |  |  on         |                |
       |               |  |  agent-4471"|                |
       |               |  v             |                |
       |               |        +-------+-------+        |
       |               |        | A2A Gateway   |        |
       |               |        | (Go)          |        |
       |               |        +-------+-------+        |
       |               |                |                |
       |               | (7)    JSON-RPC 2.0 SendMessage|
       |               |        |        |                |
       |               |        |        v                |
       |               |        |  +-----+------+         |
       |               |        |  | Agent      |         |
       |               |        |  | Adapter    |         |
       |               |        |  | (Python)   |         |
       |               |        |  +-----+------+         |
       |               |        |        |                |
       |               |        | (8)    |                |
       |               |        |        | CrewAI.kickoff()
       |               |        |        | Risk analyst agent
       |               |        |        | evaluates CVSS,
       |               |        |        | compat, changelog
       |               |        |        |                |
       |               |        |        v                |
       |               |        |  +-----+------+         |
       |               |        |  | LLM        |         |
       |               |        |  | Provider   |         |
       |               |        |  +-----+------+         |
       |               |        |        |                |
       |               |        | (9)    |                |
       |               |        |        | Artifact:     |
       |               |        |        | { risk: "low",|
       |               |        |        |   recommend:  |
       |               |        |        |   "approve", |
       |               |        |        |   batch: 12,  |
       |               |        |        |   reboot:    |
       |               |        |        |   needed }   |
       |               |        |        v                |
       |               |        |  +-----+------+         |
       |               |        |  | A2A        |         |
       |               |        |  | Gateway    |         |
       |               |        |  | state:     |         |
       |               |        |  | input-     |         |
       |               |        |  | required   |         |
       |               |        |  +-----+------+         |
       |               |        |        |                |
       |               |        | (10)   | SSE to Frontend|
       |               |        v        v                |
       |               |  +-----+------+         |        |
       |               |  | Frontend   |         |        |
       |               |  | Approval   |<------->| Operator|
       |               |  | Dialog     |         | (human)|
       |               |  +-----+------+         |        |
       |               |        |                |        |
       |               | (11)   | Operator approves      |
       |               |        v                |        |
       |               |  +-----+------+         |        |
       |               |  | Patch      |         |        |
       |               |  | Engine     |         |        |
       |               |  | saga       |         |        |
       |               |  | begins     |         |        |
       |               |  +-----+------+         |        |
       |               |        |                |        |
       |               | (12)   | NATS: rmm.cmd. {agent_id}
       |               |        | .winupdate. install
       |               |        v                |        |
       |               |  +-----+------+         |        |
       |               |  |  Agent     |         |        |
       |               |  |  (Go)      |         |        |
       |               |  +-----+------+         |        |
       |               |        |                |        |
       |               | (13)   | Install patches sequentially
       |               |        | Per-patch: rmm.winupdate.install.{agent_id}
       |               |        v                |        |
       |               |  +-----+------+         |        |
       |               |  | Patch      |         |        |
       |               |  | Engine     |         |        |
       |               |  | records    |         |        |
       |               |  | per-patch  |         |        |
       |               |  | result     |         |        |
       |               |  +-----+------+         |        |
       |               |        |                |        |
       |               | (14)   | If reboot_required:
       |               |        | coordinate window
       |               |        v                |        |
       |               |  [COMPLETE or REBOOT_PENDING]
```

**Step-by-step annotations:**

1. PatchEngine dispatches a scan command to the agent via `rmm.cmd.{agent_id}.winupdate.scan`.
2. Agent enumerates pending Windows updates using WMI/PowerShell.
3. Agent publishes scan results on `rmm.winupdate.scan.{agent_id}`.
4. PatchEngine persists WinUpdate records with state `scanned` -> `pending_approval`.
5. If new patches are found that match a configured policy, PatchEngine emits an internal "patches_available" event.
6. The A2A bridge creates a Task to evaluate the risk of these patches for the specific agent.
7. A2A Gateway routes the task to the Agent Adapter (CrewAI risk analyst agent).
8. CrewAI kicks off the risk evaluation workflow. The LLM analyzes CVSS scores, compatibility notes, and changelogs.
9. LLM returns an Artifact containing risk assessment and install recommendation. Task state transitions to `input-required`.
10. Frontend displays the approval dialog via SSE push. Human operator reviews.
11. Operator approves. A2A Gateway resumes the task. PatchEngine initiates the deployment saga.
12. PatchEngine dispatches install commands via `rmm.cmd.{agent_id}.winupdate.install`.
13. Agent installs patches sequentially. Each patch completion is reported via `rmm.winupdate.install.{agent_id}`.
14. If reboot is required, PatchEngine coordinates the reboot window. Saga completes with state `installed` or `reboot_required`.

### 4.3 Secret Reference -> Credential Injection -> Script Execution -> Credential Revocation

```
  +----------+     +----------+     +----------+     +----------+     +----------+
  | Frontend/|     |  API     |     |  Secret  |     |  Agent   |     | Secret   |
  | A2A Task |     |  Server  |     |  Service |     |  (Go)    |     | Service  |
  |  Client  |     | (Python) |     |  (Go)    |     |          |     | (gRPC)   |
  +----+-----+     +----+-----+     +----+-----+     +----+-----+     +----+-----+
       |                |                |                |                |
  (1)  | Script         |                |                |                |
       | definition     |                |                |                |
       | includes:      |                |                |                |
       | env_vars: [    |                |                |                |
       |   "DB_PASS":   |                |                |                |
       |   "ref:oap://  |                |                |                |
       |    cred/eng/   |                |                |                |
       |    db?version  |                |                |                |
       |    =latest"]   |                |                |                |
       |                |                |                |                |
  (2)  | Trigger        |                |                |                |
       | script run     |                |                |                |
       |--------------->|                |                |                |
       |                | (3)            |                |                |
       |                | Parse env_vars, |                |                |
       |                | extract Secret- |                |                |
       |                | References      |                |                |
       |                |                |                |                |
       |                | (4)            |                |                |
       |                | REST GET       |                |                |
       |                | /v1/secret/    |                |                |
       |                | ref:oap://...  |                |                |
       |                | Bearer: <JWT>  |                |                |
       |                |--------------->|                |                |
       |                |                | (5)            |                |
       |                |                | Validate JWT,  |                |
       |                |                | check scope    |                |
       |                |                | against        |                |
       |                |                | SecretRef ACL  |                |
       |                |                |                |                |
       |                |                | (6)            |                |
       |                |                | Vault Transit  |                |
       |                |                | read path:     |                |
       |                |                | secret/data/   |                |
       |                |                | eng/db         |                |
       |                |                |                |                |
       |                |                | (7)            |                |
       |                |                | Return:        |                |
       |                |                | { value: "s3cr",                |
       |                |                |   lease_id: "abc",              |
       |                |                |   ttl: 3600 }   |                |
       |                |<---------------|                |                |
       |                |                |                |                |
       |                | (8)            |                |                |
       |                | NATS:          |                |                |
       |                | rmm.cmd.       |                |                |
       |                | {agent_id}     |                |                |
       |                | .script.run    |                |                |
       |                | + injected     |                |                |
       |                | env: DB_PASS=  |                |                |
       |                | "s3cr"         |                |                |
       |                | + lease_id:    |                |                |
       |                |   "abc"        |                |                |
       |                |------------------------------->|                |
       |                |                |                |                |
       |                |                |                | (9)            |
       |                |                |                | Subscribe to    |
       |                |                |                | gRPC stream    |
       |                |                |                | secret.Watch   |
       |                |                |                | (lease_id=abc) |
       |                |                |                |<---------------|
       |                |                |                |                |
       |                |                |                | (10)           |
       |                |                |                | Spawn process  |
       |                |                |                | with env       |
       |                |                |                | DB_PASS="s3cr" |
       |                |                |                |                |
       |                |                |                | (11)           |
       |                |                |                | Script executes|
       |                |                |                | stdout/stderr  |
       |                |                |                | via rmm.script |
       |                |                |                | .chunk.{id}    |
       |                |                |                |                |
       |                |                |  (12)          |                |
       |                |                |  Admin rotates |                |
       |                |                |  DB password   |                |
       |                |                |  in Vault      |                |
       |                |                |--+             |                |
       |                |                |  | (13)       |                |
       |                |                |  | Vault      |                |
       |                |                |  | sends      |                |
       |                |                |  | rotation   |                |
       |                |                |  | event to   |                |
       |                |                |  | Secret     |                |
       |                |                |  | Service    |                |
       |                |                |  v             |                |
       |                |                | (14)           |                |
       |                |                | Secret Service |                |
       |                |                | revokes old   |                |
       |                |                | lease (abc)   |                |
       |                |                | gRPC push:    |                |
       |                |                | SecretRotation|                |
       |                |                | Event         |                |
       |                |                | {lease: abc,  |                |
       |                |                |  action:      |                |
       |                |                |  "revoked"}   |                |
       |                |                |------------------------------->|
       |                |                |                |                |
       |                |                |                | (15)           |
       |                |                |                | Agent receives |
       |                |                |                | revocation     |
       |                |                |                | <100ms latency |
       |                |                |                |                |
       |                |                |                | (16)           |
       |                |                |                | If script still|
       |                |                |                | running and    |
       |                |                |                | needs creds:   |
       |                |                |                | -> auth failure|
       |                |                |                | -> log error   |
       |                |                |                | -> rmm.script. |
       |                |                |                |   result with  |
       |                |                |                |   status=error |
       |                |                |                |   "secret_     |
       |                |                |                |    revoked"    |
       |                |                |                |                |
       |                | (17)           |                |                |
       |                | rmm.script.    |                |                |
       |                | result         |                |                |
       |                |<-------------------------------|                |
       |                |                |                |                |
       |                | (18)           |                |                |
       |                | API Server     |                |                |
       |                | marks script   |                |                |
       |                | as FAILED      |                |                |
       |                | (error=secret_ |                |                |
       |                |  revoked)      |                |                |
       |                |                |                |                |
       |                | (19)           |                |                |
       |                | Audit log      |                |                |
       |                | entry written  |                |                |
       |                | rmm.secret.    |                |                |
       |                | revoked event  |                |                |
       |                v                |                |                |
       |          [AUDIT COMPLETE]       |                |                |
```

**Step-by-step annotations:**

1. Script definition includes a SecretReference in `env_vars`: `ref:oap://cred/eng/db?version=latest`.
2. Frontend or A2A Task triggers the script run.
3. API Server parses the script's env_vars, extracting all `ref:oap://...` references.
4. API Server calls Secret Service: `GET /v1/secret/ref:oap://cred/eng/db?version=latest` with Bearer JWT.
5. Secret Service validates the JWT and checks that the caller's tenant/scopes permit access to this SecretReference.
6. Secret Service reads the encrypted value from Vault at path `secret/data/eng/db`.
7. Secret Service returns the resolved value along with a lease_id and TTL.
8. API Server dispatches the script to the agent via NATS, injecting `DB_PASS=s3cr` into the command envelope and including the lease_id.
9. Agent subscribes to the Secret Service gRPC stream for rotation events on this lease.
10. Agent spawns the script process with the injected environment variable.
11. Script executes; stdout/stderr streams via `rmm.script.chunk.{agent_id}`.
12. Admin rotates the database password in Vault (out-of-band).
13. Vault sends a rotation event to Secret Service.
14. Secret Service revokes the old lease and pushes a `SecretRotationEvent` via gRPC to subscribed agents.
15. Agent receives the revocation event with <100ms latency.
16. If the script is still running and attempts to use the revoked credential, it receives an auth failure. The agent logs the error and publishes `rmm.script.result` with `status=error, error=secret_revoked`.
17. Agent publishes the script result to API Server.
18. API Server marks the script run as FAILED with the revocation error.
19. Audit log entry is written for the revocation event.

### 4.4 A2A Task from LangGraph -> Delegates to CrewAI -> Result Artifact -> Back to LangGraph

This flow demonstrates multi-framework agent collaboration orchestrated by the A2A Gateway.

```
  +----------+     +----------+     +----------+     +----------+     +----------+
  | LangGraph|     |  A2A     |     |  Agent   |     |  CrewAI  |     |  A2A     |
  |  Agent   |     |  Gateway |     |  Adapter |     |  Agent   |     |  Gateway |
  | (Caller) |     |  (Go)    |     | (Python) |     | (Callee) |     | (resume) |
  +----+-----+     +----+-----+     +----+-----+     +----+-----+     +----+-----+
       |                |                |                |                |
  (1)  | SendMessage    |                |                |                |
       | task A:        |                |                |                |
       | "Analyze disk  |                |                |                |
       |  usage and     |                |                |                |
       |  recommend     |                |                |                |
       |  cleanup"      |                |                |                |
       |-------------->|                |                |                |
       |               | (2)            |                |                |
       |               | Create Task A  |                |                |
       |               | context_id=ctx1|                |                |
       |               | state=submitted                |                |
       |               | Route to       |                |                |
       |               | LangGraph      |                |                |
       |               | adapter        |                |                |
       |               |                |                |                |
       |               | (3)            |                |                |
       |               | JSON-RPC       |                |                |
       |               | SendMessage    |                |                |
       |               |--------------->|                |                |
       |               |                | (4)            |                |
       |               |                | LangGraph      |                |
       |               |                | .astream()     |                |
       |               |                | begins         |                |
       |               |                |                |                |
       |               | (5)            |                |                |
       |               |                | LangGraph      |                |
       |               |                | decides:       |                |
       |               |                | "I need a      |                |
       |               |                |  cleanup plan. |                |
       |               |                |  Delegate to   |                |
       |               |                |  CrewAI        |                |
       |               |                |  cleanup crew" |                |
       |               |                |                |                |
       |               | (6)            |                |                |
       |               |                | SendMessage    |                |
       |               |                | task B:        |                |
       |               |                | "Generate      |                |
       |               |                |  cleanup plan  |                |
       |               |                |  for these     |                |
       |               |                |  directories:  |                |
       |               |                |  [...]"        |                |
       |               |<---------------|                |                |
       |               |                |                |                |
       |               | (7)            |                |                |
       |               | Create Task B  |                |                |
       |               | context_id=ctx1|                |                |
       |               | (same context, |                |                |
       |               |  different     |                |                |
       |               |  task)         |                |                |
       |               | Route to       |                |                |
       |               | CrewAI adapter |                |                |
       |               |                |                |                |
       |               | (8)            |                |                |
       |               | JSON-RPC       |                |                |
       |               | SendMessage    |                |                |
       |               |------------------------------->|                |
       |               |                |                | (9)            |
       |               |                |                | CrewAI         |
       |               |                |                | .kickoff()     |
       |               |                |                | crew: [        |
       |               |                |                |   planner,     |
       |               |                |                |   safety,      |
       |               |                |                |   executor     |
       |               |                |                | ]              |
       |               |                |                |                |
       |               |                |                | (10)           |
       |               |                |                | Crew executes  |
       |               |                |                | Produces:      |
       |               |                |                | Artifact {     |
       |               |                |                |   name:        |
       |               |                |                |   "cleanup-    |
       |               |                |                |    plan",      |
       |               |                |                |   parts: [{    |
       |               |                |                |     kind:data, |
       |               |                |                |     data: {    |
       |               |                |                |       steps:   |
       |               |                |                |       [...]    |
       |               |                |                |     }          |
       |               |                |                |   }]           |
       |               |                |                | }              |
       |               |                |                |                |
       |               |                |                | (11)           |
       |               |                |                | Task B         |
       |               |                |                | state=         |
       |               |                |                | completed      |
       |               |                |                | Artifact saved |
       |               |<-------------------------------|                |
       |               |                |                |                |
       |               | (12)           |                |                |
       |               | Task B complete|                |                |
       |               | Resume Task A  |                |                |
       |               | with Artifact  |                |                |
       |               | as input       |                |                |
       |               |                |                |                |
       |               | (13)           |                |                |
       |               | JSON-RPC       |                |                |
       |               | SendMessage    |                |                |
       |               | (resume Task A)|                |                |
       |               |--------------->|                |                |
       |               |                | (14)           |                |
       |               |                | LangGraph      |                |
       |               |                | .astream()     |                |
       |               |                | continues with |                |
       |               |                | CrewAI's       |                |
       |               |                | cleanup plan   |                |
       |               |                | as context     |                |
       |               |                |                |                |
       |               |                | (15)           |                |
       |               |                | LangGraph      |                |
       |               |                | produces final |                |
       |               |                | Artifact:      |                |
       |               |                | { kind:data,   |                |
       |               |                |   data: {      |                |
       |               |                |     script: 89,|                |
       |               |                |     action:    |                |
       |               |                |     "cleanup", |                |
       |               |                |     steps: [...]|               |
       |               |                |   } }          |                |
       |               |                |                |                |
       |               | (16)           |                |                |
       |               | Task A         |                |                |
       |               | state=         |                |                |
       |               | completed      |                |                |
       |               | Artifact saved |                |                |
       |<--------------|                |                |                |
       |               |                |                |                |
       | (17)          |                |                |                |
       | LangGraph     |                |                |                |
       | caller        |                |                |                |
       | receives      |                |                |                |
       | final result  |                |                |                |
       |               |                |                |                |
       | (18)          |                |                |                |
       | rmm.a2a.task. |                |                |                |
       | state.changed |                |                |                |
       | emitted for   |                |                |                |
       | both tasks    |                |                |                |
       | A and B       |                |                |                |
       v               |                |                |                |
  [FRONTEND SSE      |                |                |                |
   PUSH]            |                |                |                |
```

**Step-by-step annotations:**

1. LangGraph agent initiates an A2A task via `SendMessage`: "Analyze disk usage and recommend cleanup."
2. A2A Gateway creates Task A with a `context_id` and routes it to the LangGraph adapter.
3. Gateway dispatches the task via JSON-RPC 2.0 `SendMessage` to the Agent Adapter.
4. Agent Adapter invokes `LangGraph.astream()`. Execution begins.
5. LangGraph agent reasons that it needs a detailed cleanup plan and decides to delegate to a CrewAI cleanup crew agent.
6. LangGraph adapter sends a new A2A message (Task B) to the Gateway, requesting CrewAI to generate a cleanup plan.
7. A2A Gateway creates Task B under the same `context_id` (shared context) and routes it to the CrewAI adapter.
8. Gateway dispatches Task B via JSON-RPC 2.0 to the Agent Adapter, which loads the CrewAI crew.
9. Agent Adapter invokes `CrewAI.kickoff()` with a crew of three agents: planner, safety reviewer, and executor.
10. CrewAI agents collaborate. The final Artifact produced is a structured cleanup plan with ordered steps.
11. Task B transitions to state `completed`. Artifact is persisted to PostgreSQL.
12. A2A Gateway receives Task B completion and automatically resumes Task A with the CrewAI Artifact as additional context.
13. Gateway sends the resume message to the LangGraph adapter via JSON-RPC 2.0.
14. LangGraph continues execution with the CrewAI cleanup plan now in its context.
15. LangGraph produces the final Artifact: a concrete, executable cleanup script with specific steps.
16. Task A transitions to state `completed`. Final Artifact is persisted.
17. LangGraph caller receives the final result with the cleanup script.
18. A2A Gateway emits `rmm.a2a.task.state.changed` for both Task A and Task B, pushing updates to Frontend via WebSocket.

---

## 5. Shared Schemas

All schemas that cross service boundaries. These are the contracts that services agree on; any change requires a versioned protocol update.

### 5.1 AgentEvent (protobuf)

**Purpose:** Universal event envelope used by Agent -> API Server, and propagated to Frontend via WebSocket.

```protobuf
syntax = "proto3";
package oap.events;

message AgentEvent {
  string event_id = 1;          // UUID v4, unique per event
  string agent_id = 2;          // Agent identifier (UUID)
  string event_type = 3;        // Enum: HEARTBEAT, CHECK_RESULT, SCRIPT_RESULT,
                                //       INVENTORY_UPDATE, ALERT_STATE_CHANGED,
                                //       PATCH_SCAN_RESULT, PATCH_INSTALL_RESULT
  Severity severity = 4;        // INFO, WARNING, ERROR, CRITICAL
  int64 timestamp = 5;          // Unix epoch milliseconds
  bytes payload = 6;            // Event-type-specific payload (msgpack or JSON)
  string tenant_id = 7;         // Organization/tenant identifier
  string correlation_id = 8;    // For tracing across services (same as OTel trace_id)
  map<string, string> labels = 9; // Arbitrary key-value labels for filtering
}

enum Severity {
  SEVERITY_UNSPECIFIED = 0;
  INFO = 1;
  WARNING = 2;
  ERROR = 3;
  CRITICAL = 4;
}
```

**Wire format:** msgpack when transported over NATS (Agent -> Server); protobuf when transported over WebSocket (Server -> Frontend); JSON when logged.

**Size budget:** payload max 1 MB; events exceeding this are split into chunks with a shared `event_id`.

### 5.2 Task (protobuf + JSON)

**Purpose:** A2A protocol task representation, used by A2A Gateway, Agent Adapters, and Frontend.

```protobuf
syntax = "proto3";
package oap.a2a;

message Task {
  string id = 1;                // Task UUID
  string context_id = 2;        // Shared context for multi-task workflows
  TaskState state = 3;          // Current state
  Message message = 4;          // Input message (user request or upstream task output)
  repeated Artifact artifacts = 5; // Produced artifacts (results)
  int32 version = 6;            // Optimistic concurrency control version
  string agent_id = 7;          // Target agent (framework-specific)
  string tenant_id = 8;         // Organization scope
  int64 created_at = 9;         // Unix epoch milliseconds
  int64 updated_at = 10;        // Unix epoch milliseconds
  string correlation_id = 11;   // Trace correlation
  map<string, string> metadata = 12; // Framework-specific metadata
}

enum TaskState {
  TASK_STATE_UNSPECIFIED = 0;
  SUBMITTED = 1;    // Task accepted, not yet started
  WORKING = 2;      // Agent actively processing
  INPUT_REQUIRED = 3; // Waiting for human input (HITL)
  COMPLETED = 4;    // Successfully finished
  FAILED = 5;       // Unrecoverable error
  CANCELLED = 6;    // Cancelled by user or timeout
}

message Message {
  string role = 1;              // "user" or "agent"
  repeated Part parts = 2;      // Message content parts
}

message Part {
  oneof content {
    string text = 1;            // Plain text
    DataPart data = 2;          // Structured data (JSON)
    FilePart file = 3;          // File reference
  }
}

message DataPart {
  bytes data = 1;               // JSON-encoded structured data
  string mime_type = 2;         // Always "application/json"
}

message FilePart {
  string uri = 1;               // File location (oap:// or https://)
  string name = 2;
  string mime_type = 3;
}
```

**Dual format:** Protobuf for in-process and NATS transport (efficiency); JSON for HTTP/JSON-RPC 2.0 transport and REST API responses (interoperability with web clients).

**Versioning:** The `version` field is incremented on every state transition. Optimistic concurrency: clients must include the expected `version` when sending updates; conflicts return 409 Conflict.

### 5.3 Artifact (protobuf)

**Purpose:** Result output from a completed (or in-progress) A2A task.

```protobuf
syntax = "proto3";
package oap.a2a;

message Artifact {
  string artifact_id = 1;       // UUID
  string name = 2;              // Human-readable name
  string description = 3;       // Optional description
  repeated Part parts = 4;      // Content parts (text, data, file)
  string task_id = 5;           // Parent task
  int64 created_at = 6;         // Unix epoch milliseconds
  map<string, string> metadata = 7;
}
```

**Part kinds:**
- `text`: plain text (e.g., agent explanation)
- `data`: structured JSON (e.g., `{ "script_id": 89, "action": "cleanup" }`)
- `file`: file URI with MIME type (e.g., `oap://artifacts/task-abc/log.txt`)

### 5.4 SecretReference (JSON URI)

**Purpose:** Indirect reference to a secret stored in Vault/Infisical, embedded in script/check/agent configurations so that the raw secret value never appears in the database.

**Format:** `ref:oap://<type>/<workspace>/<path>?version=<v>&field=<f>`

**Components:**
- `ref:oap://` — mandatory scheme prefix
- `<type>` — secret type: `cred` (credential), `cert` (certificate), `key` (encryption key), `token` (API token)
- `<workspace>` — logical grouping (e.g., `eng`, `finance`, `infra`)
- `<path>` — path within the workspace (e.g., `db`, `api-keys/stripe`)
- `?version=<v>` — optional: `latest` (default), or a specific version number
- `&field=<f>` — optional: extract a specific field from a key-value secret (e.g., `password` from `{username, password}`)

**Examples:**
```
ref:oap://cred/eng/db?version=latest
ref:oap://cred/eng/db?version=3&field=password
ref:oap://cert/infra/web-tls?version=latest
ref:oap://key/eng/encryption?version=2
ref:oap://token/finance/stripe-api?version=latest
```

**Resolution flow:** The Secret Service parses the URI, validates the caller's access scope against the SecretReference ACL, reads from Vault/Infisical, and returns the raw value along with a lease_id for tracking and revocation.

**Security:** SecretReferences are safe to store in plaintext in the database. Only the Secret Service can resolve them to actual values, and it enforces tenant isolation and audit logging.

### 5.5 AgentCard (protobuf + JSON)

**Purpose:** A2A discovery metadata. Each agent framework registers an AgentCard so that other agents (and the A2A Gateway) can discover its capabilities.

```protobuf
syntax = "proto3";
package oap.a2a;

message AgentCard {
  string id = 1;                // Unique agent identifier
  string name = 2;              // Human-readable name
  string framework = 3;         // "langgraph" | "crewai" | "autogen" |
                                // "semantic-kernel" | "openai-agents" |
                                // "claude-agent" | "custom"
  string endpoint = 4;          // URL for the Agent Adapter
  repeated string capabilities = 5; // ["disk-analysis", "patch-risk", ...]
  repeated string tags = 6;     // Free-form tags for filtering
  repeated Skill skills = 7;    // Structured skill descriptions
  AuthConfig auth = 8;          // Authentication requirements
  string tenant_id = 9;         // Owning organization
  int32 version = 10;           // Card schema version
  int64 updated_at = 11;
}

message Skill {
  string id = 1;                // e.g., "disk-cleanup"
  string name = 2;              // e.g., "Disk Cleanup"
  string description = 3;       // What the skill does
  repeated string input_types = 4;   // Accepted input MIME types
  repeated string output_types = 5;  // Produced output MIME types
  repeated Parameter parameters = 6; // Parameter schema
}

message Parameter {
  string name = 1;
  string type = 2;              // "string" | "number" | "boolean" | "object"
  string description = 3;
  bool required = 4;
}

message AuthConfig {
  string type = 1;              // "bearer" | "mtls" | "api-key" | "none"
  repeated string scopes = 2;   // Required OAuth scopes
}
```

**Dual format:** Protobuf for internal A2A Gateway registry; JSON for the `GET /a2a/v1/agent-card` HTTP endpoint (consumed by external agents and the Frontend agent browser).

### 5.6 Auth Token (JWT)

**Purpose:** Authentication for all REST API and WebSocket connections. Issued by the OIDC identity provider (Dex, Keycloak, or Auth0).

**Format:** JSON Web Token (JWS, RS256 signed).

**Claims:**

```json
{
  "sub": "user-uuid-or-service-account-id",
  "tenant_id": "org-uuid",
  "scopes": ["agents:read", "agents:write", "checks:execute", "scripts:run"],
  "iat": 1718438400,
  "exp": 1718442000,
  "iss": "https://oidc.openagentplatform.example",
  "aud": "oap-api",
  "roles": ["admin", "operator"],
  "session_id": "session-uuid",
  "agent_id": null
}
```

**Claim details:**

| Claim | Type | Description |
|-------|------|-------------|
| `sub` | string | Subject: user UUID or service account ID |
| `tenant_id` | string | Organization scope; all queries filtered by this |
| `scopes` | string[] | OAuth2 scopes (e.g., `agents:read`, `alerts:write`) |
| `iat` | integer | Issued-at (Unix seconds) |
| `exp` | integer | Expiration (Unix seconds); max 1 hour for user tokens, 24h for service accounts |
| `iss` | string | Issuer URL (OIDC provider) |
| `aud` | string | Audience: `oap-api` for API tokens, `oap-ws` for WebSocket tokens |
| `roles` | string[] | User roles: `admin`, `operator`, `viewer`, `auditor` |
| `session_id` | string | Session UUID; enables server-side revocation |
| `agent_id` | string\|null | If present, this is an agent token (not a user token) |

**Transport:** `Authorization: Bearer <jwt>` for REST; `?token=<jwt>` query parameter for WebSocket (since browsers cannot set headers on WS).

**Refresh:** Short-lived (1h) access tokens; refresh tokens (30d) are httpOnly cookies and never sent to the API Server.

---

## 6. Error Propagation

What happens when each infrastructure component fails. This is the operational playbook for incident response.

### 6.1 NATS JetStream Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | Connection refused, heartbeat timeout (5s), subscription errors |
| **Impact** | Agent <-> API Server real-time channel broken. Script dispatch, check results, heartbeat, and event distribution all fail. |
| **Fallback (Agent)** | Agent falls back to REST polling: `GET /api/v1/agents/{id}/commands` every 10s. API Server writes commands to the `command_queue` PostgreSQL table. |
| **Fallback (A2A)** | A2A Gateway buffers events in an in-memory ring buffer (10,000 events). On overflow, oldest events are dropped and a metric is incremented. |
| **Data Loss Risk** | Agent commands delayed up to 10s. Real-time events dropped if ring buffer overflows. Check results and heartbeats are durable (retained in PostgreSQL via REST fallback). |
| **Recovery** | On NATS reconnection, buffered A2A events are flushed. Agent detects NATS availability and switches back from REST polling to NATS subscription. |
| **Monitoring** | `nats_connection_state` metric (0=disconnected, 1=connected); alert at state=0 for >30s. |

### 6.2 A2A Gateway Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | Health check `/healthz` returns 503; Go channel send blocks (IPC timeout 2s) |
| **Impact** | RMM events cannot be converted to A2A tasks. LLM-driven remediation is paused. RMM operations (checks, patches, scripts) continue normally. |
| **Fallback** | API Server queues pending A2A tasks in the `pending_a2a_tasks` PostgreSQL table. Frontend displays a banner: "A2A orchestration temporarily unavailable." |
| **Data Loss Risk** | None. The `pending_a2a_tasks` table is a durable queue. On recovery, all queued tasks are dispatched. |
| **Recovery** | On A2A Gateway restart, API Server drains `pending_a2a_tasks` in order. Tasks older than 24 hours are marked as `expired` and surfaced for manual review. |
| **Monitoring** | `a2a_gateway_up` metric; `pending_a2a_tasks_depth` gauge; alert if depth > 1000 or gateway down > 60s. |

### 6.3 HashiCorp Vault / Infisical Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | REST call to Vault returns 503/connection refused; lease renewal fails |
| **Impact** | Secret resolution fails. Scripts that require secret injection cannot start or are aborted. Non-secret checks continue normally. |
| **Fallback** | Script executor returns `status=error, error_code="secret_unavailable"`. Agent reports the error to API Server. AlertEngine creates a `secret_unavailable` alert. |
| **Data Loss Risk** | None. Scripts abort safely without using stale or hardcoded credentials. No credential leakage risk. |
| **Recovery** | On Vault recovery, Secret Service resumes resolution. Agents re-request secrets. Pending script retries can be triggered manually. |
| **Monitoring** | `vault_up` metric; `secret_resolution_failures_total` counter; alert if Vault down > 30s or failure rate > 5%. |

### 6.4 LLM Provider Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | HTTP 5xx from provider API; connection timeout; rate limit (429) |
| **Impact** | Agent Adapter cannot invoke LLM. A2A tasks in `working` state stall or fail. |
| **Fallback** | Retry 3x with exponential backoff (1s, 2s, 4s). If all retries fail, return `InvokeError(retryable=true)`. A2A Gateway transitions the task to state `FAILED` with error details. |
| **Data Loss Risk** | Task result lost (in-progress reasoning not persisted). User must retry. Intermediate task state is preserved for resumption if the provider recovers within 24h. |
| **Recovery** | Failed tasks can be retried via `POST /a2a/v1/tasks/{id}:retry`. The A2A Gateway replays the task from the last persisted state. |
| **Monitoring** | `llm_provider_up` metric per provider; `llm_invoke_latency_seconds` histogram; `llm_invoke_errors_total` counter by error type. |

### 6.5 PostgreSQL Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | Connection refused; query timeout; replication lag > threshold |
| **Impact** | All persistence fails. API Server cannot read or write agents, checks, alerts, tasks, or audit log. |
| **Fallback (Reads)** | Read-only fallback to Redis cache for GET endpoints. Cache TTL: 5-15 minutes for dashboard data, 90s for agent status. |
| **Fallback (Writes)** | All write endpoints return HTTP 503 with `Retry-After: 30` header. Clients (Frontend, Agent) implement exponential backoff. |
| **Data Loss Risk** | Writes are lost if not buffered by the client. Agent commands queued via REST fallback to `command_queue` table also fail (since the table is in PostgreSQL). In this scenario, commands are written to a local file on the API Server and replayed on recovery. |
| **Recovery** | On PostgreSQL reconnection, the API Server flushes its local command file to `command_queue`. Cache is repopulated on first miss. |
| **Monitoring** | `pg_connection_pool_active` gauge; `pg_query_latency_seconds` histogram; `pg_replication_lag_seconds` gauge; alert if pool exhausted or lag > 10s. |

### 6.6 Redis Unavailable

| Attribute | Detail |
|-----------|--------|
| **Detection** | Connection refused; command timeout (>100ms) |
| **Impact** | Idempotency keys, rate limiting, session cache, and dashboard cache all fail. |
| **Fallback (Rate Limiting)** | In-process token bucket (per-process, not global). Less precise; may allow brief over-limit during failover. |
| **Fallback (Idempotency)** | PostgreSQL-backed idempotency table (slower, ~10ms vs ~1ms). Acceptable for non-hot-path operations. |
| **Fallback (Auth)** | JWT-only auth. Token signature and claims are validated without session lookup. No server-side session revocation during outage. |
| **Fallback (Cache)** | Cache miss; read from PostgreSQL (slower path). |
| **Data Loss Risk** | Idempotency keys stored only in Redis are lost; duplicates possible during the outage window (mitigated by PostgreSQL fallback if enabled). |
| **Recovery** | On Redis reconnection, in-process token buckets reset to Redis state. Cache repopulates on first miss. |
| **Monitoring** | `redis_up` metric; `redis_memory_used_bytes` gauge; alert if Redis down > 10s or memory > 80%. |

### 6.7 Error Propagation Summary

| Failed Component | Impact | Fallback | Data Loss Risk | Recovery Time |
|------------------|--------|----------|----------------|---------------|
| NATS | Real-time channel down | REST polling (10s interval) + command_queue table | Commands delayed 10s; events dropped on buffer overflow | Immediate (automatic) |
| A2A Gateway | LLM orchestration paused | pending_a2a_tasks durable queue | None | On restart; drain queue |
| Vault | Secret injection fails | Scripts return error, non-secret checks continue | None (safe abort) | On Vault recovery |
| LLM Provider | Agent invocations fail | Retry 3x exponential backoff, then FAILED | In-progress task result lost | On provider recovery; manual retry |
| PostgreSQL | All persistence fails | Redis cache for reads; 503 for writes | Writes lost (buffered to local file) | On PG recovery; flush local file |
| Redis | Cache/rate-limit/idempotency fail | In-process token bucket; PG-backed idempotency; JWT-only auth | Duplicate processing possible | Immediate (degraded mode) |

---

## 7. Consistency Patterns

### 7.1 Idempotency

**Purpose:** Ensure that duplicate messages (from agent retries, NATS at-least-once delivery, or client double-clicks) do not cause duplicate side effects.

**Mechanism:** Redis SETNX (SET if Not eXists) with a configurable TTL.

**Key derivation:**

| Operation Type | Key Pattern | TTL |
|----------------|-------------|-----|
| Agent registration | `idemp:agent-reg:{agent_id}` | 1 hour |
| Heartbeat | `idemp:heartbeat:{agent_id}` (MAX timestamp) | 90 seconds |
| REST mutation | `idemp:rest:{Idempotency-Key header}` | 24 hours |
| A2A task creation | `idemp:a2a-task:{task_id}` | 24 hours |
| Check result ingest | `idemp:check-result:{check_id}:{execution_id}` | 1 hour |
| Script dispatch | `idemp:script:{script_run_id}` | 1 hour |

**Natural idempotency (no Redis needed):**
- Agent registration: PostgreSQL `INSERT ... ON CONFLICT (agent_id) DO UPDATE` (upsert).
- Heartbeat: `UPDATE agents SET last_seen = NOW() WHERE agent_id = ? AND last_seen < ?` (idempotent by nature).
- A2A task transitions: `version` field with optimistic concurrency; a replayed transition with the same `version` is a no-op.

**Client responsibility:** Clients SHOULD include an `Idempotency-Key` header on all non-idempotent REST mutations (POST, PUT, DELETE). The server stores the response and returns it for duplicate requests with the same key within the TTL window.

**Redis fallback:** When Redis is unavailable, idempotency falls back to a PostgreSQL `idempotency_keys` table with a unique constraint. Performance degrades from ~1ms to ~10ms per check, but correctness is preserved.

### 7.2 Sagas

**Purpose:** Multi-step distributed transactions that must maintain consistency across services without holding long-lived locks.

#### 7.2.1 Patch Deployment Saga

The patch deployment is a 6-step saga with compensating transactions:

```
STEP 1: Validate batch
  Action:  PatchEngine validates that all patches in the batch are approved
  Compensate: N/A (read-only)

STEP 2: Mark patches as "installing"
  Action:  UPDATE win_updates SET state = 'installing' WHERE id IN (batch)
  Compensate: UPDATE win_updates SET state = 'approved' WHERE id IN (batch)

STEP 3: Dispatch install commands
  Action:  NATS publish rmm.cmd.{agent_id}.winupdate.install for each agent
  Compensate: NATS publish rmm.cmd.{agent_id}.winupdate.cancel (if cancelable)

STEP 4: Wait for install results
  Action:  Collect rmm.winupdate.install.{agent_id} responses (timeout: 30 min)
  Compensate: For each failed patch: UPDATE win_updates SET state = 'failed'

STEP 5: Verify installations
  Action:  API Server dispatches verification checks to each agent
  Compensate: N/A (verification is informational)

STEP 6: Mark saga complete
  Action:  UPDATE win_updates SET state = 'installed' for successful patches
  Compensate: N/A (terminal state)
```

**Saga state persistence:** The saga state (current step, batch ID, affected agents) is stored in a `saga_instances` table in PostgreSQL. On crash recovery, the saga orchestrator reads the table and resumes from the last completed step.

**Timeout handling:** If any step times out, the saga transitions to `compensating` state and runs the compensating transactions in reverse order. If compensation also fails, the saga transitions to `manual_intervention` and an alert is created.

#### 7.2.2 A2A Human-in-the-Loop (HITL) Saga

For A2A tasks that require human approval (e.g., LLM-generated remediation scripts):

```
STEP 1: Agent produces Artifact
  Action:  A2A task state = 'input-required'; Artifact persisted
  Compensate: N/A (task paused, not failed)

STEP 2: Notify operator
  Action:  WebSocket push to Frontend; email/SMS fallback
  Compensate: N/A

STEP 3: Wait for human response (24h timeout)
  Action:  Await POST /a2a/v1/tasks/{id}:resume with human decision
  Compensate: On timeout: task state = 'cancelled'; alert "approval_timeout"

STEP 4: Resume task with human input
  Action:  Forward human decision to Agent Adapter; task state = 'working'
  Compensate: N/A
```

### 7.3 Eventual Consistency Boundaries

These are data that is allowed to be temporarily inconsistent across the platform. The system tolerates divergence within the specified window.

| Data | Consistency Window | Mechanism | Acceptable Staleness |
|------|-------------------|-----------|---------------------|
| Agent online/offline status | 90 seconds | Heartbeat TTL; if no heartbeat in 90s, agent marked offline | Up to 90s of "ghost online" status |
| Check results aggregation | Latest wins | Newer result overwrites older in `check_results` table | Only the most recent result matters |
| Dashboard metrics (CPU, memory, disk) | 5-15 seconds | Redis cache with TTL | 15s of stale dashboard data |
| A2A task state (Frontend view) | <1 second | WebSocket push on state change | Near real-time; acceptable for UI |
| Inventory data | 5-15 minutes | Agent reports on check-in; diff applied on ingest | Inventory may lag by one check-in cycle |
| Alert notification delivery | 30 seconds | Async notification queue (Celery) | Brief delay acceptable for notifications |
| Policy propagation to agent | 5 minutes | Delta push on `rmm.cmd.{agent_id}.policy.push`; agent confirms | Policy changes take effect within one push cycle |

### 7.4 Strong Consistency Boundaries

These data points require strong consistency. The system blocks on the write completing across all replicas.

| Data | Consistency Mechanism | Latency Budget |
|------|----------------------|----------------|
| Secret lease revocation | Synchronous write to Vault; gRPC push to all subscribed agents; in-memory cache invalidation in API Server | <100ms end-to-end |
| Agent registration token | One-time use; consumed on first `POST /api/v1/agents/register`; PostgreSQL `UPDATE ... WHERE consumed = false` (atomic) | <50ms |
| Audit log | Append-only PostgreSQL table with serial PK; synchronous write before API response | <10ms |
| Task state transitions (version check) | Optimistic concurrency: `UPDATE tasks SET state=?, version=version+1 WHERE id=? AND version=?`; if 0 rows affected, 409 Conflict | <5ms |
| Secret ACL changes | Synchronous cache invalidation in Secret Service; agents re-validate on next lease renewal | <200ms |

### 7.5 Ordering Guarantees

| Stream | Ordering Guarantee | Mechanism |
|--------|-------------------|-----------|
| Script output (stdout/stderr) | Strict per-stream | NATS single consumer per script run; chunks carry a monotonic `seq` number; consumer ignores out-of-order chunks |
| A2A task transitions | Strict per-task | `version` field with optimistic concurrency; concurrent transitions for the same task are serialized via version check |
| Check results | Best-effort | Each result carries a `started_at` timestamp; consumer sorts by `started_at` and discards results older than the last processed |
| Heartbeat messages | Best-effort | Only the latest heartbeat matters (MAX timestamp); older heartbeats are discarded |
| Event distribution (NATS JetStream) | Per-subject, per-consumer | JetStream consumer groups process messages in publish order; at-least-once delivery means consumer must handle duplicates (see idempotency) |
| Audit log | Strict | Serial PK; INSERT order is the event order; no reordering |
| Agent command dispatch | Per-agent strict | Each agent has its own NATS inbox (`rmm.cmd.{agent_id}.*`); commands for the same agent are processed in order by the agent's single subscriber |

---

**End of Document**
