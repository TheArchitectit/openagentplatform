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

## Components

| Component  | Tech                          | Responsibility                          |
|------------|-------------------------------|-----------------------------------------|
| Server     | Go + chi + slog               | REST API, auth, orchestration           |
| Web        | React 19 + TanStack           | Operator console                        |
| Services   | Python + FastAPI              | Agents, scripts, ML/AI workloads        |
| Database   | Postgres 16 + TimescaleDB     | System of record + time-series metrics  |
| Messaging  | NATS 2.10 + mTLS              | Event bus for check results & commands  |
| Auth       | Dex (OIDC) + JWT              | Federated identity, session cookies     |

## Data model

9 base tables:

- `users` — operators with role/org_id
- `sites` — logical grouping of agents
- `agents` — registered endpoints (hostname, os, version, status, tags)
- `checks` — scheduled probes (ping, http, disk, custom)
- `alerts` — fired alerts, severity, lifecycle
- `policies` — declarative rules (patches, configs)
- `patches` — patch application records
- `scripts` — reusable runnable scripts (bash/python)
- `audit_events` — append-only audit log

## Event flow

1. Agent connects to NATS with a per-agent mTLS cert.
2. Agent subscribes to `oap.commands.{site_id}.{agent_id}` and publishes results to `oap.events.*`.
3. Server subscribes to `oap.events.*`, persists check results to Postgres, and fires alerts.
4. Web UI queries the server via REST; long-lived state uses TanStack Query with 30s stale time.

## Security

- OIDC for user authn; short-lived JWTs for the SPA
- mTLS for agent-to-server messaging
- Append-only audit log for every mutating action
- Role-based authorization (admin / operator / viewer)
