# Architecture

OpenAgentPlatform is an agent-first RMM platform built around three core
principles: agents are first-class citizens, communication is event-driven,
and every action is auditable.

## High-level

```
┌────────────┐       OIDC        ┌──────────────┐
│   Web UI   │ ───────────────▶  │  OAP Server  │ ──┐
└────────────┘                   │   (Go API)   │   │
                                 └──────┬───────┘   │
                                        │           │
                            pgxpool     │           │  publish/subscribe
                                        ▼           ▼
                                 ┌──────────┐  ┌──────────┐
                                 │ Postgres │  │   NATS   │
                                 │ +TSDB    │  │  (mTLS)  │
                                 └──────────┘  └────┬─────┘
                                                   │
                                                   ▼
                                          ┌────────────────┐
                                          │   Agents       │
                                          │ (Go / Python)  │
                                          └────────────────┘
```

## Component diagram (all phases)

```
┌─────────────────────────────────────────────────────────────────────┐
│                         PRESENTATION LAYER                          │
│  ┌─────────────────┐  ┌──────────────────┐  ┌────────────────────┐  │
│  │  Web UI         │  │  MCP Server      │  │  A2A Dashboard     │  │
│  │  React 19       │  │  (Go, stdio/HTTP)│  │  (React routes)    │  │
│  │  TanStack       │  │                  │  │                    │  │
│  └────────┬────────┘  └────────┬─────────┘  └──────────┬─────────┘  │
└───────────┼────────────────────┼───────────────────────┼────────────┘
            │ OIDC + JWT         │ JSON-RPC              │ REST
            ▼                    ▼                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           API LAYER (Go)                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  │ /agents  │ │ /checks  │ │ /alerts  │ │ /scripts │ │ /patches │  │
│  │ /sites   │ │ /policies│ │ /secrets │ │ /remote  │ │ /a2a/*   │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────────┐   │
│  │ /audit   │ │ /webhook │ │ /ws      │ │  Auth Middleware     │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────────┘   │
└──────┬──────────────────────────────────────────────────┬───────────┘
       │                                                  │
       │  pgxpool                                         │  pub/sub
       ▼                                                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────────────────────┐
│  Postgres 16 │  │  NATS 2.10   │  │  Go–Python RPC Bridge       │
│  +TimescaleDB│  │  (mTLS)      │  │  (a2a/bridge/)              │
│  9+ tables   │  │  Subjects:   │  │  Adapters: Anthropic,       │
│              │  │  oap.events.*│  │  OpenAI, AutoGen, CrewAI,   │
│              │  │  oap.commands│  │  LangGraph, Semantic Kernel │
│              │  │  .*          │  │                              │
└──────────────┘  └──────────────┘  └──────────────────────────────┘
       ▲                                        ▲
       │                                        │
┌──────┴────────────────────────────────────────┴───────────────────────┐
│                         AGENT LAYER                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────────┐ │
│  │  oap-agent   │  │  Checkers    │  │  Script Runtime              │ │
│  │  (Go daemon) │  │  ping,http,  │  │  bash, python, powershell,   │ │
│  │  mTLS client │  │  tcp,dns,    │  │  node                        │ │
│  │              │  │  cpu,mem,    │  │                              │ │
│  │              │  │  disk,svc    │  │                              │ │
│  └──────────────┘  └──────────────┘  └──────────────────────────────┘ │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────────────┐ │
│  │  Patch Mgr   │  │  Remote      │  │  Policy Enforcer             │ │
│  │  scan/apply  │  │  Shell       │  │  OPA rego evaluation         │ │
│  │              │  │  WebSocket   │  │                              │ │
│  └──────────────┘  └──────────────┘  └──────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
```

### Component table (all phases)

| Component         | Tech                        | Responsibility                                | Phase |
|-------------------|-----------------------------|-----------------------------------------------|-------|
| Server API        | Go + chi + slog             | REST + WebSocket, auth, orchestration        | 0.1+  |
| Web Console       | React 19 + TanStack         | Operator dashboard, Monaco script editor      | 0.1+  |
| MCP Server        | Go (separate module)        | Model Context Protocol tool surface           | 1.0   |
| A2A Backend       | Go (a2a/ submodule)         | Agent-to-Agent protocol, task routing         | 2.x   |
| A2A Adapters      | Python + FastAPI            | LLM framework bridges (6 frameworks)          | 2.x   |
| Secret Vault      | Go (a2a/bridge/vault)       | Encrypted secret storage, rotation             | 3.x   |
| Database          | Postgres 16 + TimescaleDB   | System of record + time-series metrics        | 0.1+  |
| Messaging         | NATS 2.10 + mTLS            | Event bus, command dispatch, agent comms      | 0.1+  |
| Auth              | Dex (OIDC) + JWT            | Federated identity, SSO, session cookies     | 0.1+  |
| Agent Daemon      | Go binary                   | mTLS client, heartbeat, check executor        | 0.1+  |
| Script Runtime    | Docker exec / host          | Multi-language script execution (4 runtimes)  | 1.5   |
| Patch Engine      | Go (internal/patches)       | OS package management, scan/approve/apply     | 1.4   |
| Policy Engine     | Go + OPA (rego)             | Declarative compliance rules, evaluation      | 1.3   |
| Alert Engine      | Go (internal/alerts)        | Rule evaluation, notification dispatch        | 1.2   |
| Remote Shell      | WebSocket (xterm.js)        | Interactive terminal sessions, recording      | 1.5   |
| Session Recorder  | Go (internal/remote)        | Terminal session playback, audit              | 1.5   |
| Monitoring        | Prometheus + Grafana        | Metrics, dashboards, alerting                 | 0.1+  |

## Data flow diagram

```
┌────────────────────────────────────────────────────────────────────────┐
│                        DATA FLOW — CHECK LIFECYCLE                      │
└────────────────────────────────────────────────────────────────────────┘

  ┌──────────┐   schedule    ┌──────────────┐   NATS publish    ┌──────┐
  │ Scheduler│──────────────▶│  Check Queue │──────────────────▶│ NATS │
  │ (cron)   │               │  (internal)  │                   │      │
  └──────────┘               └──────────────┘                   └──┬───┘
                                                                │
                                                   oap.commands.<agent_id>
                                                                │
                                                                ▼
  ┌──────────────┐  execute   ┌──────────────┐  NATS publish   ┌──────┐
  │ Check Result │◀───────────│   Agent      │────────────────▶│ NATS │
  │ (TimescaleDB)│            │  (Go daemon) │                 │      │
  └──────┬───────┘            └──────────────┘                 └──┬───┘
         │                                                         │
         │ persist              oap.agents.<id>.results            │
         ▼                                                         │
  ┌──────────────┐  evaluate  ┌──────────────┐  fire  ┌────────────┐│
  │ Alert Engine │───────────▶│  Alert Store │───────▶│ Notify     ││
  │ (rules)      │            │              │        │ (email,    ││
  └──────────────┘            └──────────────┘        │  webhook)  ││
                                                      └────────────┘│
                                                                   │
  ┌──────────────┐  WebSocket  ┌──────────────┐                    │
  │ Web UI       │◀──────────│  WS Hub       │◀───────────────────┘
  │ (TanStack)   │  push      │  (server)     │  alert events
  └──────────────┘            └──────────────┘
```

### Event flow (detailed)

1. Agent connects to NATS with a per-agent mTLS cert (`oap.agents.<id>`).
2. Server publishes commands to `oap.commands.<site_id>.<agent_id>`.
3. Agent subscribes, executes check/script/patch, publishes results to
   `oap.agents.<id>.results`.
4. Server subscribes to `oap.agents.*.results`, persists to TimescaleDB,
   evaluates alert rules, and fans out via WebSocket.
5. A2A bridge subscribes to `oap.events.*` for policy violations, agent
   online/offline, and shell session events.
6. Web UI queries REST for CRUD; subscribes to WebSocket for live updates.

## Data model

Base tables (Phase 1):

- `users` — operators with role/org_id
- `sites` — logical grouping of agents
- `agents` — registered endpoints (hostname, os, version, status, tags)
- `checks` — scheduled probes (ping, http, disk, custom)
- `alerts` — fired alerts, severity, lifecycle
- `policies` — declarative rules (patches, configs)
- `patches` — patch application records
- `scripts` — reusable runnable scripts (bash/python)
- `audit_events` — append-only audit log

Extended tables (Phase 2-6):

- `a2a_tasks` — agent-to-agent task records
- `a2a_adapters` — registered LLM framework adapters
- `a2a_costs` — token usage and cost tracking
- `secrets` — encrypted secret vault entries
- `compliance_results` — OPA policy evaluation results
- `patch_jobs` — patch approval workflow records
- `script_runs` — multi-runtime script execution history
- `shell_sessions` — remote terminal session metadata + recordings
- `alert_rules` — declarative alert rule definitions
- `notification_channels` — email, webhook, Slack configs

## Security model

- OIDC for user authn; short-lived JWTs for the SPA
- mTLS for agent-to-server messaging (per-agent certs)
- Append-only audit log for every mutating action
- Role-based authorization (admin / operator / viewer)
- Encrypted secret vault with envelope encryption (AES-256-GCM)
- Rate limiting on all public endpoints

---

## Architecture Decision Records (ADRs)

### ADR-001: Go as primary server language

**Status:** Accepted
**Date:** 2026-01-15

**Context:** The server needs low-latency HTTP handling, mTLS client
management, and strong concurrency for WebSocket fan-out.

**Decision:** Use Go 1.23+ with chi router, pgx for Postgres, and
log/slog for structured logging.

**Consequences:**
- Single binary deployment, fast startup
- Strong typing catches integration errors at compile time
- Smaller talent pool vs Python/Node, but excellent performance profile
- Standard library covers mTLS, HTTP/2, and WebSocket natively

---

### ADR-002: NATS as message broker

**Status:** Accepted
**Date:** 2026-01-15

**Context:** Agents need bidirectional communication with the server.
The system also needs pub/sub for event fan-out to the A2A bridge
and WebSocket clients.

**Decision:** Use NATS 2.10 with mTLS and subject-based routing.

**Consequences:**
- Lightweight, no ZooKeeper/etcd dependency
- Subject hierarchies map naturally to agent/site routing
- mTLS provides per-agent identity without a separate CA
- No built-in message persistence (use JetStream if needed later)

---

### ADR-003: Postgres + TimescaleDB for storage

**Status:** Accepted
**Date:** 2026-01-15

**Context:** The platform needs OLTP for CRUD, time-series for metrics,
and a single backup story.

**Decision:** Use PostgreSQL 16 with the TimescaleDB extension. All
data lives in one database; time-series tables use hypertables.

**Consequences:**
- One backup target, one connection pool
- TimescaleDB compression reduces metric storage by ~90%
- Migrations via Alembic (Python) keep schema changes auditable
- No need for a separate InfluxDB/Prometheus TSDB for application data

---

### ADR-004: React 19 + TanStack for frontend

**Status:** Accepted
**Date:** 2026-02-01

**Context:** The operator console needs real-time updates, complex
data tables, and a script editor (Monaco).

**Decision:** Use React 19 with TanStack Router (file-based routing)
and TanStack Query (server state with caching).

**Consequences:**
- Type-safe routes via generated `routeTree.gen.ts`
- 30s stale time on queries reduces API load
- Monaco editor integrates via `@monaco-editor/react`
- Bundle size is larger than Svelte/Solid, but ecosystem is mature

---

### ADR-005: A2A protocol for inter-agent communication

**Status:** Accepted
**Date:** 2026-04-01

**Context:** Phase 2 introduces agent-to-agent task delegation
across LLM framework adapters (Anthropic, OpenAI, AutoGen, CrewAI,
LangGraph, Semantic Kernel).

**Decision:** Implement the A2A protocol spec with a Go gateway and
Python adapter layer. The Go server brokers tasks; Python adapters
translate to framework-specific calls.

**Consequences:**
- Adapters are independently deployable (separate FastAPI process)
- Go–Python bridge uses HTTP/RPC with Pydantic validation
- Adapter health and cost tracking flow back through NATS events
- 6 framework adapters shipped; new frameworks require Python only

---

### ADR-006: Encrypted secret vault with envelope encryption

**Status:** Accepted
**Date:** 2026-05-01

**Context:** Phase 3 adds secret management for agent credentials,
API keys, and database passwords. Secrets must be encrypted at rest
and accessible only to authorized agents.

**Decision:** Store secrets in Postgres with envelope encryption
(AES-256-GCM). The master key is derived from a KMS or local secret.
Per-secret data keys are wrapped by the master key.

**Consequences:**
- No plaintext secrets in DB or logs
- Rotation is a single UPDATE with a new data key
- Agents receive secrets via secure NATS message (mTLS channel)
- Key management can move to AWS KMS / HashiCorp Vault later

---

### ADR-007: Business Source License 1.1

**Status:** Accepted
**Date:** 2026-01-15

**Context:** The platform is open source but the project needs
sustainable commercial funding.

**Decision:** Use BSL 1.1: free for non-production use, with a
4-year change date to Apache 2.0. Commercial licenses available
for production deployments above the free tier limits.

**Consequences:**
- See [COMMERCIAL.md](COMMERCIAL.md) for tier details
- Community can read, modify, and self-host for testing
- Production deployments require a commercial agreement
- BSL 1.1 is OSI-approved as a source-available license
