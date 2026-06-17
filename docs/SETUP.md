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

## Ozore AI configuration (LLM agents)

The platform uses [Ozore AI](https://ozore.com) as its hosted LLM agent
provider via an OpenAI-compatible API. To enable LLM-powered agents
(policy suggestions, natural-language queries, automated remediation):

1. Sign up at https://ozore.com and obtain an API key.
2. Add the following to your `.env` file:

```bash
OZORE_API_KEY=your-api-key-here
OZORE_MODEL=ozore/custom
OZORE_BASE_URL=https://ozore.com/v1
ENABLE_LLM_AGENTS=true
```

3. Restart the server:

```bash
docker compose -f deploy/docker-compose.yml restart oap-server
```

4. Verify the connection in the dashboard under Settings > AI Agents.

For air-gapped deployments, set `ENABLE_LLM_AGENTS=false` and the
platform will operate without LLM features.

## Stripe billing (optional, commercial tiers)

Commercial tiers require a Stripe account for subscription billing. This
section is only relevant if you are deploying OAP commercially.

1. Create a Stripe account at https://stripe.com.
2. In the Stripe dashboard, create a subscription product with the
   pricing matching your tier (see [COMMERCIAL.md](COMMERCIAL.md)).
3. Obtain your API keys (test mode for development, live mode for production).
4. Add the following to your `.env` file:

```bash
STRIPE_SECRET_KEY=sk_test_...
STRIPE_PUBLISHABLE_KEY=pk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_ID_PRO=price_...
STRIPE_PRICE_ID_ENTERPRISE=price_...
```

5. Set up a webhook endpoint in Stripe pointing to
   `https://your-domain.com/api/v1/billing/webhook`.
6. Restart the server.

If `STRIPE_SECRET_KEY` is not set, the billing endpoints return 503
and the platform runs in free/community mode.

## TLS setup

For production deployments, TLS must be terminated before the OAP
services or at the load balancer in front of them.

### Option A: TLS at the reverse proxy (recommended)

Use Caddy, Nginx, or Traefik as a reverse proxy with automatic cert
provisioning (e.g. Caddy + Let's Encrypt).

Example Caddyfile:

```caddyfile
your-domain.com {
    reverse_proxy localhost:8080
    reverse_proxy /api/* localhost:8080
    reverse_proxy /ws/* localhost:8080
}

web.your-domain.com {
    reverse_proxy localhost:5173
}
```

### Option B: Direct TLS on the OAP server

Set the following environment variables:

```bash
TLS_CERT_FILE=/etc/oap/tls/fullchain.pem
TLS_KEY_FILE=/etc/oap/tls/privkey.pem
TLS_PORT=8443
```

The server will listen on `TLS_PORT` with HTTPS. Obtain certs via
certbot or your preferred ACME client:

```bash
sudo certbot certonly --standalone -d your-domain.com
sudo cp /etc/letsencrypt/live/your-domain.com/fullchain.pem /etc/oap/tls/
sudo cp /etc/letsencrypt/live/your-domain.com/privkey.pem /etc/oap/tls/
```

### TLS for NATS (mTLS)

NATS mTLS certs are auto-generated by `./deploy/nats/scripts/gen-certs.sh`
on first startup. For production, replace these with certs from your
internal CA. See [DEPLOYMENT.md](DEPLOYMENT.md) for details.

After enabling TLS, update `.env`:

```bash
COOKIE_SECURE=true
```

This ensures session cookies are only sent over HTTPS.

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

### Ozore LLM errors

- Verify `OZORE_API_KEY` is set and valid: `curl -H "Authorization: Bearer $OZORE_API_KEY" $OZORE_BASE_URL/models`
- Check server logs for 4xx/5xx responses from the Ozore API
- If rate-limited, reduce concurrent LLM agent invocations

### Stripe webhook signature failures

- Ensure `STRIPE_WEBHOOK_SECRET` matches the endpoint signing secret in Stripe
- For local testing, use the Stripe CLI: `stripe listen --forward-to localhost:8080/api/v1/billing/webhook`
- Webhook events must reach the server on a publicly accessible URL (use ngrok for local dev)

### TLS certificate errors

- Verify cert files are readable by the server process
- Check cert expiry: `openssl x509 -in /etc/oap/tls/fullchain.pem -noout -dates`
- Ensure `COOKIE_SECURE=true` only when serving over HTTPS

### Reset everything

Nuclear option--destroys all data:

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
- See [DEPLOYMENT.md](DEPLOYMENT.md) for production deployment
- See [SECURITY.md](SECURITY.md) for security configuration
- See [COMMERCIAL.md](COMMERCIAL.md) for tier and licensing details
- Check [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines
- Join our [Discord](https://discord.gg/openagentplatform) for support
