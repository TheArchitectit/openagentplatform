# OpenAgentPlatform

Open-source, agent-first RMM (Remote Monitoring & Management) platform.
Endpoints, agents, checks, alerts, scripts, and patches — managed from one place,
extensible through AI agents and an A2A-compatible API.

## Stack

- **Server**: Go 1.23 + chi + slog
- **Web**: React 19 + TanStack Router/Query + TailwindCSS
- **Services**: Python 3.12 (FastAPI, SQLAlchemy, Alembic)
- **Data**: PostgreSQL 16 + TimescaleDB
- **Messaging**: NATS with mTLS
- **Auth**: OIDC (Dex) + JWT sessions
- **Deploy**: Docker Compose

## Quick start

```bash
# 1. Copy env
cp .env.example .env

# 2. Start the stack
make up

# 3. Run database migrations
make migrate

# 4. Open the web UI
open http://localhost:5173
```

## Repository layout

```
cmd/server        Go HTTP API server
internal/         server-only Go packages (api, auth, config, db, events, schema)
pkg/              reusable Go packages (logger, models)
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

## License

Business Source License 1.1 — see [LICENSE](LICENSE).
