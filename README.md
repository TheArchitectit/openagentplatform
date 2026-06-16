# OpenAgentPlatform

Open-source, agent-first RMM (Remote Monitoring & Management) platform.
Endpoints, agents, checks, alerts, scripts, and patches — managed from one place,
extensible through AI agents and an A2A-compatible API.

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

## Stack

- **Server**: Go 1.23 + chi + slog
- **Web**: React 19 + TanStack Router/Query + TailwindCSS
- **Services**: Python 3.12 (FastAPI, SQLAlchemy, Alembic)
- **Data**: PostgreSQL 16 + TimescaleDB
- **Messaging**: NATS with mTLS
- **Auth**: OIDC (Dex) + JWT sessions
- **Deploy**: Docker Compose

## Prerequisites

- Docker 20.10+ with Compose v2
- Go 1.23+ (for agent)
- Node 20+ (for web dev)

See [docs/SETUP.md](docs/SETUP.md) for detailed installation instructions.

## What's included

Once running, you get:

- **Dashboard** — Sites, agents, checks, alerts overview at http://localhost:5173
- **REST API** — Full CRUD for all resources at http://localhost:8080
- **Agent** — Cross-platform endpoint agent (Linux/macOS/Windows)
- **OIDC auth** — Dex with two pre-configured users:
  - `admin@oap.local` / `password` (admin role)
  - `tech@oap.local` / `password` (technician role)
- **Health checks** — All services have healthcheck endpoints
- **Sample data** — 3 sites, 10 agents, 20 checks, 5 alert rules

## Development

For active development with hot reload:

```bash
make up-dev
```

This mounts your local source into containers. Server changes trigger Air hot-reload; web changes trigger Vite HMR.

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

- [Setup](docs/SETUP.md) — 5-minute walkthrough
- [Architecture](docs/ARCHITECTURE.md) — system design
- [API](docs/API.md) — REST endpoints
- [Contributing](docs/CONTRIBUTING.md) — PR process & coding standards

## Community

- [Discord](https://discord.gg/openagentplatform) — Join for support and discussion
- [GitHub Issues](https://github.com/openagentplatform/openagentplatform/issues) — Bug reports and feature requests
- [Discussions](https://github.com/openagentplatform/openagentplatform/discussions) — Q&A and ideas

## License

Business Source License 1.1 — see [LICENSE](LICENSE).

---

**Status:** Active development | **Version:** 0.1.0-alpha | **Last updated:** 2026-06-16
