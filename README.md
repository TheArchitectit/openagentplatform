# OpenAgentPlatform

Open-source, agent-first RMM (Remote Monitoring & Management) platform.
Endpoints, agents, checks, alerts, scripts, and patches -- managed from
one place, extensible through AI agents and an A2A-compatible API.

[![License: BSL 1.1](https://img.shields.io/badge/License-BSL_1.1-blue.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-%230db7ed.svg?logo=docker&logoColor=white)](https://www.docker.com/)
[![Go 1.23+](https://img.shields.io/badge/Go-1.23+-00ADD8.svg?logo=go)](https://golang.org/)
[![React 19](https://img.shields.io/badge/React-19-61DAFB.svg?logo=react)](https://react.dev/)
[![PostgreSQL 16](https://img.shields.io/badge/PostgreSQL-16-336791.svg?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![NATS](https://img.shields.io/badge/NATS-2.10-27AAE1.svg?logo=nats)](https://nats.io/)

## Quick Start (5 minutes)

Get the platform running locally with three commands:

```bash
cp .env.example .env && make setup
```

Then in a new terminal:

```bash
make build-agent && ./bin/oap-agent -register && ./bin/oap-agent
```

Open http://localhost:5173 and login with `admin@oap.local` / `password`.

**What just happened:**
- Docker Compose started Postgres, NATS, Dex, server, and web
- Database migrations created 9 base tables
- Sample data was seeded
- The agent registered itself and is now publishing heartbeats

For the full walkthrough see [docs/SETUP.md](docs/SETUP.md).

## Architecture

```
┌────────────┐       OIDC        ┌──────────────┐
│   Web UI   │ ───────────────▶  │  OAP Server  │ ──┐
│  (React)   │                   │   (Go API)   │   │
└────────────┘                   └──────┬───────┘   │
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

For the full component diagram (all phases) and ADRs, see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Built With

- **Server**: Go 1.23 + chi + slog
- **Web**: React 19 + TanStack Router/Query + TailwindCSS + Monaco
- **MCP Server**: Go (separate module, stdio/HTTP)
- **A2A Adapters**: Python 3.12 (FastAPI, Pydantic) -- Anthropic, OpenAI,
  AutoGen, CrewAI, LangGraph, Semantic Kernel
- **LLM Provider**: Ozore AI (OpenAI-compatible)
- **Data**: PostgreSQL 16 + TimescaleDB + Alembic
- **Messaging**: NATS 2.10 with mTLS
- **Auth**: OIDC (Dex) + JWT sessions
- **Policy**: OPA (Open Policy Agent) with rego
- **Observability**: OpenTelemetry, Prometheus, Grafana
- **Secrets**: Envelope encryption (AES-256-GCM) with 5 backends
- **CI**: GitHub Actions
- **Deploy**: Docker Compose, Kubernetes (Helm)

## Phase status

| Phase | Sprint | Status      | Description                         |
|-------|--------|-------------|-------------------------------------|
| 0     | 0.1    | Complete    | Foundation: scaffold, CI, schema, NATS, OIDC |
| 0     | 0.2    | Complete    | Agent CLI, registration, heartbeat  |
| 1     | 1.1    | Complete    | Check CRUD, built-in library, ingest |
| 1     | 1.2    | Complete    | Alert rules, notifications, inbox   |
| 1     | 1.3    | Complete    | OPA policy engine, compliance scans |
| 1     | 1.4    | Complete    | Patch approval, inventory, deploy   |
| 1     | 1.5    | Complete    | Scripts, 4-runtime executor, shell  |
| 2     | 2.1    | Complete    | A2A Gateway, JSON-RPC, EventBridge  |
| 2     | 2.2    | Complete    | 6 framework adapters, orchestration |
| 2     | 2.3    | Complete    | Python-Go bridge, end-to-end wiring |
| 3     | 3.0    | Complete    | Secret vault, A2A auth, OAuth       |
| 4     | 4.0    | Complete    | Settings, Monaco, dark mode, a11y   |
| 5     | 5.0    | Complete    | OTel, Prometheus, resilience, tests |
| 5     | 5.1    | Complete    | Ozore AI integration                |
| 6     | 6.0    | Complete    | Live dashboard, multi-tenant, polish|

## Prerequisites

- Docker 20.10+ with Compose v2
- Go 1.23+ (for agent)
- Node 20+ (for web dev)
- Python 3.12 + uv (for migrations)

See [docs/SETUP.md](docs/SETUP.md) for detailed installation instructions.

## What's included

Once running, you get:

- **Dashboard** -- Sites, agents, checks, alerts overview at http://localhost:5173
- **REST API** -- Full CRUD for all resources at http://localhost:8080
- **Agent** -- Cross-platform endpoint agent (Linux/macOS/Windows)
- **OIDC auth** -- Dex with two pre-configured users:
  - `admin@oap.local` / `password` (admin role)
  - `tech@oap.local` / `password` (technician role)
- **Health checks** -- All services have healthcheck endpoints
- **Sample data** -- 3 sites, 10 agents, 20 checks, 5 alert rules

## Development

For active development with hot reload:

```bash
make up-dev
```

This mounts your local source into containers. Server changes trigger
Air hot-reload; web changes trigger Vite HMR.

## Useful commands

```bash
make help           # Show all available targets
make up             # Start stack in background
make down           # Stop stack
make logs           # Tail logs from all services
make migrate        # Run database migrations
make seed           # Load sample data
make reset          # Destroy volumes and start fresh
make test           # Run all tests
make lint           # Run linters
make build          # Build server and web
make build-agent    # Build the endpoint agent
make clean          # Remove build artifacts
```

## Repository layout

```
cmd/server        Go HTTP API server
cmd/agent         Endpoint agent (daemon + registration)
internal/         server-only Go packages (api, auth, config, db, events, schema)
pkg/              reusable Go packages (logger, agent, models)
py/               Python services, agents, and Alembic migrations
web/              React + Vite frontend
deploy/           docker-compose, NATS config, Dex config, postgres init
docs/             documentation
```

## Documentation

- [Setup](docs/SETUP.md) -- 5-minute walkthrough
- [Architecture](docs/ARCHITECTURE.md) -- system design, ADRs
- [API](docs/API.md) -- REST endpoints
- [Deployment](docs/DEPLOYMENT.md) -- production deployment guide
- [Security](docs/SECURITY.md) -- auth, RBAC, secrets, audit
- [Commercial](docs/COMMERCIAL.md) -- licensing tiers, billing, SSO
- [Changelog](docs/CHANGELOG.md) -- sprint-by-sprint history
- [Contributing](docs/CONTRIBUTING.md) -- PR process and coding standards

## Community

- [Discord](https://discord.gg/openagentplatform) -- Join for support
- [GitHub Issues](https://github.com/openagentplatform/openagentplatform/issues) -- Bug reports
- [Discussions](https://github.com/openagentplatform/openagentplatform/discussions) -- Q&A

## License

Business Source License 1.1 -- see [LICENSE](LICENSE). Free for
non-production use; commercial licenses available for production.
See [docs/COMMERCIAL.md](docs/COMMERCIAL.md) for tier details.

---

**Status:** All sprints 0.1 -- 6.0 complete | **Version:** 6.0.0 | **Last updated:** 2026-06-17
