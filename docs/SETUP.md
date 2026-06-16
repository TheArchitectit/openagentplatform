# Setup

5-minute walkthrough to get OpenAgentPlatform running locally.

## Prerequisites

- Docker + Docker Compose
- Go 1.23+ (for server development)
- Python 3.12 + [uv](https://github.com/astral-sh/uv)
- Node 22 + pnpm (for web development)

## 1. Clone & configure

```bash
git clone https://github.com/openagentplatform/openagentplatform
cd openagentplatform
cp .env.example .env
```

## 2. Start the stack

```bash
make up
```

This launches:
- Postgres (5432) with TimescaleDB
- NATS (4222, 8222 mgmt)
- Dex OIDC (5556) with one mock user
- OAP server (8080)
- OAP web (5173)

## 3. Migrate the database

```bash
make migrate
```

Creates the 9 base tables: users, sites, agents, checks, alerts, policies,
patches, scripts, audit_events.

## 4. Open the UI

Visit http://localhost:5173 and sign in with the mock account:

```
[email protected] / admin
```

## 5. Run the dev stack with hot reload

```bash
make up-dev
```

This mounts the source directories into the containers for live code reload.

## Troubleshooting

- **Port already in use** — change the port in `.env` and re-run `make up`.
- **Dex login fails** — confirm `OIDC_ISSUER_URL` is reachable from the web container.
- **Migration errors** — drop the volume: `docker compose down -v` then `make up && make migrate`.
