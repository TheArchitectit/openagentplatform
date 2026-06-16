# Setup

5-minute walkthrough to get OpenAgentPlatform running locally with Docker Compose.

## Prerequisites

Install these before starting:

- **Docker** 20.10+ ([download](https://www.docker.com/get-started))
- **Docker Compose** v2 (included with Docker Desktop; verify with `docker compose version`)
- **Go** 1.23+ (for agent development; [install](https://golang.org/dl/))
- **Node** 20+ (for web development; [install](https://nodejs.org/))

Optional for advanced workflows:

- **Python 3.12** + [uv](https://github.com/astral-sh/uv) (for database migrations and seed scripts)
- **pnpm** (for web package management)

## Step 1: Clone & configure

```bash
git clone https://github.com/openagentplatform/openagentplatform
cd openagentplatform
cp .env.example .env
```

Review `.env` and adjust defaults if needed. The defaults work for local development.

## Step 2: Start the stack

```bash
docker compose -f deploy/docker-compose.yml up -d
```

This launches:
- **Postgres** (5432) with TimescaleDB extension
- **NATS** (4222, 8222 mgmt) with mTLS
- **Dex OIDC** (5556) with two mock users
- **OAP server** (8080) — Go API
- **OAP web** (5173) — React UI

Wait ~10 seconds for all services to become healthy. Check status:

```bash
docker compose -f deploy/docker-compose.yml ps
```

## Step 3: Verify health

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```

## Step 4: Login to the dashboard

Open http://localhost:5173 in your browser and sign in with the default admin account:

```
Email:    [email protected]
Password: password
```

You'll see the dashboard with Sites, Agents, Checks, and Alerts panels.

## Step 5: Build and register the agent

In a new terminal:

```bash
make build-agent
./bin/oap-agent -register
```

This performs a one-shot registration: the agent generates an mTLS cert, connects to NATS, and registers with the platform. It exits after successful registration.

## Step 6: Start the agent daemon

```bash
./bin/oap-agent
```

The agent runs as a long-lived process, publishing heartbeats every 30 seconds and responding to check/script commands.

## Step 7: See your agent in the dashboard

Open http://localhost:5173/agents. Your newly registered agent should appear in the list with status "Online" and a green pulse indicator.

## Development mode (hot reload)

For active development with live code reload:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml up
```

This mounts your local source directories into the containers. Server changes trigger Air hot-reload; web changes trigger Vite HMR.

## Useful commands

```bash
make up              # Start stack in background
make down            # Stop stack
make logs            # Tail logs from all services
make migrate         # Run database migrations
make seed            # Load sample data
make reset           # Destroy volumes and start fresh
make test            # Run all tests
```

## Troubleshooting

### Port already in use

Edit `.env` and change the conflicting port, then restart:

```bash
docker compose -f deploy/docker-compose.yml down
docker compose -f deploy/docker-compose.yml up -d
```

Common ports: `5432` (Postgres), `4222` (NATS), `5556` (Dex), `8080` (Server), `5173` (Web).

### Healthcheck fails

Check service logs:

```bash
docker compose -f deploy/docker-compose.yml logs [service-name]
```

Common issues:
- **postgres**: Wait 15-20 seconds for first-time initialization
- **nats**: Verify `./deploy/nats/certs` contains generated certs (run `./deploy/nats/scripts/gen-certs.sh` if missing)
- **dex**: Check that `./deploy/dex/config.yaml` and `static-users.yaml` exist

### Migration errors

Reset the database:

```bash
make reset
make migrate
```

### Dex login fails

Verify `OIDC_ISSUER_URL` in `.env` matches the Dex container URL:

```
OIDC_ISSUER_URL=http://localhost:5556/dex
```

If running in a remote Docker host, replace `localhost` with the host's IP or hostname.

### Agent registration fails

- Ensure the agent binary has execute permissions: `chmod +x ./bin/oap-agent`
- Check that NATS is healthy: `curl http://localhost:8222/healthz`
- Verify agent logs: `./bin/oap-agent -register -v` (with debug logging)

### Web UI shows "Network Error"

Confirm the server is reachable:

```bash
curl http://localhost:8080/health
```

If using a custom domain, update `VITE_API_URL` in `web/.env.local`.

### Reset everything

Nuclear option—destroys all data:

```bash
make clean
docker compose -f deploy/docker-compose.yml down -v
rm -rf .env bin/
cp .env.example .env
make setup
```

## Next steps

- Read [ARCHITECTURE.md](ARCHITECTURE.md) to understand the system design
- Explore [API.md](API.md) for REST endpoints
- Check [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines
- Join our [Discord](https://discord.gg/openagentplatform) for support
