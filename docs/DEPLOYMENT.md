# Deployment

Guide to deploying OpenAgentPlatform in production. Covers Docker Compose,
Kubernetes, database migrations, TLS, and monitoring.

For local development setup, see [SETUP.md](SETUP.md). For architecture,
see [ARCHITECTURE.md](ARCHITECTURE.md).

## Table of contents

1. [Docker Compose quick-start](#docker-compose-quick-start)
2. [Environment variables reference](#environment-variables-reference)
3. [Database migration guide](#database-migration-guide)
4. [TLS setup](#tls-setup)
5. [Monitoring stack setup](#monitoring-stack-setup)
6. [Kubernetes (Helm)](#kubernetes-helm)

---

## Docker Compose quick-start

### Prerequisites

- Docker 20.10+ and Docker Compose v2
- A Linux host (Ubuntu 22.04+ recommended)
- Minimum 2 vCPU, 4 GB RAM, 20 GB disk for a small deployment

### Production compose file

The repository ships `deploy/docker-compose.yml` for development. For
production, create a custom compose file or use the K8s Helm chart.

```yaml
# deploy/docker-compose.prod.yml
services:
  postgres:
    image: timescale/timescaledb:latest-pg16
    restart: always
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    volumes:
      - pgdata:/home/postgres/pgdata/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER}"]
      interval: 10s
      timeout: 5s
      retries: 5

  nats:
    image: nats:2.10-alpine
    restart: always
    command: ["--config", "/etc/nats/nats.conf"]
    volumes:
      - ./nats:/etc/nats:ro
      - natsdata:/data
    ports:
      - "4222:4222"  # Client
      - "8222:8222"  # Management (restrict via firewall)
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8222/healthz"]
      interval: 10s
      timeout: 5s
      retries: 5

  oap-server:
    image: ghcr.io/openagentplatform/oap-server:latest
    restart: always
    depends_on:
      postgres:
        condition: service_healthy
      nats:
        condition: service_healthy
    env_file:
      - .env
    ports:
      - "8080:8080"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3

  oap-web:
    image: ghcr.io/openagentplatform/oap-web:latest
    restart: always
    env_file:
      - .env
    ports:
      - "5173:5173"

volumes:
  pgdata:
  natsdata:
```

Start:

```bash
docker compose -f deploy/docker-compose.prod.yml up -d
docker compose -f deploy/docker-compose.prod.yml ps
```

### Reverse proxy

In production, always run behind a reverse proxy that handles TLS and
HTTP/2. See [TLS setup](#tls-setup) below.

---

## Environment variables reference

All variables are read from `.env` or the process environment. Variables
marked Required must be set for the service to start.

### Application

| Variable       | Default       | Required | Description                         |
|----------------|---------------|----------|-------------------------------------|
| `APP_ENV`      | `development` | No       | `development` \| `staging` \| `production` |
| `LOG_LEVEL`    | `info`        | No       | `debug` \| `info` \| `warn` \| `error` |
| `HTTP_PORT`    | `8080`        | No       | HTTP listen port                    |
| `TLS_PORT`     | (unset)       | No       | HTTPS listen port (if TLS enabled)  |
| `TLS_CERT_FILE`| (unset)       | No       | Path to TLS certificate             |
| `TLS_KEY_FILE` | (unset)       | No       | Path to TLS private key             |

### Database

| Variable          | Default          | Required | Description                |
|-------------------|------------------|----------|----------------------------|
| `POSTGRES_HOST`   | `localhost`      | Yes      | Postgres hostname          |
| `POSTGRES_PORT`   | `5432`           | No       | Postgres port              |
| `POSTGRES_DB`     | `oap`            | Yes      | Database name              |
| `POSTGRES_USER`   | `oap`            | Yes      | Database user              |
| `POSTGRES_PASSWORD`| (empty)        | Yes      | Database password          |
| `DB_SSLMODE`      | `require`        | No       | `disable` \| `require` \| `verify-full` |

### NATS

| Variable            | Default           | Required | Description              |
|---------------------|-------------------|----------|--------------------------|
| `NATS_URL`          | `nats://localhost:4222` | Yes | NATS connection URL      |
| `NATS_TLS_CERT`     | (unset)           | No       | Client cert path (mTLS)  |
| `NATS_TLS_KEY`      | (unset)           | No       | Client key path (mTLS)   |
| `NATS_TLS_CA`       | (unset)           | No       | CA cert path (mTLS)      |

### OIDC / Auth

| Variable            | Default                       | Required | Description           |
|---------------------|-------------------------------|----------|-----------------------|
| `OIDC_ISSUER_URL`   | `http://localhost:5556/dex`   | Yes      | OIDC issuer           |
| `OIDC_CLIENT_ID`    | `oap-web`                     | Yes      | OAuth client ID       |
| `OIDC_CLIENT_SECRET`| (empty)                       | Yes      | OAuth client secret   |
| `JWT_SECRET`        | (empty)                       | Yes      | Session signing key   |
| `COOKIE_DOMAIN`     | `localhost`                   | No       | Session cookie domain |
| `COOKIE_SECURE`     | `false`                       | No       | HTTPS-only cookies    |
| `COOKIE_SAMESITE`   | `lax`                         | No       | SameSite policy       |
| `SESSION_TIMEOUT`   | `24`                          | No       | Session hours         |

### Ozore AI (LLM agents)

| Variable             | Default               | Required | Description              |
|----------------------|-----------------------|----------|--------------------------|
| `OZORE_API_KEY`      | (empty)               | No       | Ozore API key            |
| `OZORE_MODEL`        | `ozore/custom`        | No       | Model identifier         |
| `OZORE_BASE_URL`     | `https://ozore.com/v1`| No       | Ozore API base URL       |
| `ENABLE_LLM_AGENTS`  | `false`               | No       | Enable LLM-powered agents|

### Stripe (optional, commercial)

| Variable                    | Default  | Required | Description             |
|-----------------------------|----------|----------|-------------------------|
| `STRIPE_SECRET_KEY`         | (empty)  | No       | Stripe secret key       |
| `STRIPE_PUBLISHABLE_KEY`    | (empty)  | No       | Stripe publishable key  |
| `STRIPE_WEBHOOK_SECRET`     | (empty)  | No       | Webhook signing secret  |
| `STRIPE_PRICE_ID_PRO`       | (empty)  | No       | Stripe price ID for Pro |
| `STRIPE_PRICE_ID_ENTERPRISE`| (empty)  | No       | Stripe price ID for Enterprise|

See [COMMERCIAL.md](COMMERCIAL.md) for tier details.

### Web UI

| Variable         | Default                  | Required | Description           |
|------------------|--------------------------|----------|-----------------------|
| `VITE_PORT`      | `5173`                   | No       | Dev server port       |
| `VITE_API_URL`   | `http://localhost:8080`  | No       | Backend API URL       |

---

## Database migration guide

Migrations are managed by Alembic (Python) and live in
`oap-data/alembic/`. Migrations run automatically on server startup in
development; in production, run them explicitly before starting the
new server version.

### Running migrations

```bash
# Local
make migrate

# Docker
docker compose exec oap-server make migrate

# Kubernetes
kubectl exec -it deploy/oap-server -- make migrate
```

### Creating a new migration

```bash
cd oap-data
uv run alembic revision --autogenerate -m "add foo table"
```

Review the generated file in `oap-data/alembic/versions/` before
committing. Migrations must be:

- Idempotent (safe to re-run)
- Backward-compatible with the previous version
- Reviewed by a second developer

### Backup before migration

```bash
# Logical backup
docker compose exec postgres pg_dump -U oap oap > backup-$(date +%F).sql

# Restore
cat backup-2026-06-17.sql | docker compose exec -T postgres psql -U oap oap
```

### TimescaleDB hypertable management

Metric tables are hypertables. To add a new hypertable:

```sql
SELECT create_hypertable('check_results', 'time');
```

To enable compression (recommended after 7 days of data):

```sql
ALTER TABLE check_results SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'agent_id'
);
SELECT add_compression_policy('check_results', INTERVAL '7 days');
```

---

## TLS setup

### Reverse proxy with Caddy (recommended)

Install Caddy:

```bash
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" | sudo tee /etc/apt/sources.list.d/caddy.list
sudo apt update && sudo apt install caddy
```

Create `/etc/caddy/Caddyfile`:

```caddyfile
your-domain.com {
    reverse_proxy localhost:8080 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }
    encode gzip zstd
}

web.your-domain.com {
    reverse_proxy localhost:5173
}

nats.your-domain.com {
    reverse_proxy localhost:4222
}
```

Reload Caddy:

```bash
sudo systemctl reload caddy
```

Caddy automatically provisions Let's Encrypt certificates.

### Direct TLS on the OAP server

Set:

```bash
TLS_CERT_FILE=/etc/oap/tls/fullchain.pem
TLS_KEY_FILE=/etc/oap/tls/privkey.pem
TLS_PORT=8443
```

Obtain certs with certbot:

```bash
sudo certbot certonly --standalone -d your-domain.com
sudo cp /etc/letsencrypt/live/your-domain.com/fullchain.pem /etc/oap/tls/
sudo cp /etc/letsencrypt/live/your-domain.com/privkey.pem /etc/oap/tls/
```

Add a cron job for renewal:

```bash
0 3 * * * certbot renew --post-hook "docker compose -f /opt/oap/deploy/docker-compose.prod.yml restart oap-server"
```

### NATS mTLS

Regenerate NATS certs for production with your internal CA:

```bash
cd deploy/nats
./scripts/gen-certs.sh --ca /path/to/your/ca.pem --days 365
```

Or use the included script for self-signed certs (dev only).

Update `deploy/nats/nats.conf` to point to the production certs and
restart the NATS container.

After enabling TLS everywhere, set in `.env`:

```bash
COOKIE_SECURE=true
```

---

## Monitoring stack setup

The platform ships Prometheus and Grafana configurations in
`deploy/prometheus/` and `deploy/grafana/`.

### Start monitoring stack

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.monitoring.yml up -d
```

This adds:

- **Prometheus** (9090) -- scrapes `/metrics` from OAP server
- **Grafana** (3000) -- dashboards and alerting
- **Node Exporter** (9100) -- host metrics
- **cAdvisor** (8080) -- container metrics

### Grafana setup

1. Open http://localhost:3000 (default: admin/admin)
2. Add Prometheus data source: URL `http://prometheus:9090`
3. Import dashboards from `deploy/grafana/dashboards/`:
   - `oap-overview.json` -- platform health
   - `oap-agents.json` -- per-agent metrics
   - `oap-api.json` -- API latency and errors
   - `oap-database.json` -- Postgres metrics

### Key metrics

| Metric                          | Description                         |
|---------------------------------|-------------------------------------|
| `oap_http_requests_total`       | HTTP request count by route/status  |
| `oap_http_request_duration_seconds` | Request latency histogram         |
| `oap_agents_online`             | Current online agent count          |
| `oap_checks_executed_total`     | Total checks executed               |
| `oap_alerts_fired_total`        | Total alerts fired by severity      |
| `oap_nats_publish_total`        | NATS messages published             |
| `oap_db_connections_active`     | Active Postgres connections         |

### Alerting

Edit `deploy/prometheus/alerts.yml` to add alert rules. Example:

```yaml
groups:
  - name: oap
    rules:
      - alert: HighErrorRate
        expr: rate(oap_http_requests_total{status=~"5.."}[5m]) > 0.1
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "OAP server error rate > 10%"
```

Reload Prometheus to apply:

```bash
curl -X POST http://localhost:9090/-/reload
```

---

## Kubernetes (Helm)

A Helm chart is provided in `deploy/helm/oap/`. Prerequisites:
Kubernetes 1.27+, Helm 3.12+, `kubectl` configured for your cluster.

Install:

```bash
helm repo add openagentplatform https://charts.openagentplatform.io
helm install oap openagentplatform/oap \
  --namespace oap --create-namespace \
  --values values.prod.yaml
```

### Key values (values.prod.yaml)

```yaml
global:
  domain: oap.your-domain.com
server:
  replicaCount: 3
  resources: { requests: {cpu: 500m, memory: 512Mi}, limits: {cpu: 2000m, memory: 2Gi} }
  env: { OIDC_ISSUER_URL: "https://auth.your-domain.com/dex" }
web:
  replicaCount: 2
  resources: { requests: {cpu: 200m, memory: 256Mi} }
postgres: { enabled: true, storage: 100Gi, storageClass: gp3 }
nats: { enabled: true, storage: 20Gi }
ingress:
  enabled: true
  className: nginx
  tls: [{ hosts: [oap.your-domain.com], secretName: oap-tls }]
monitoring: { prometheus: {enabled: true}, grafana: {enabled: true} }
```

Upgrade: `helm upgrade oap openagentplatform/oap --reuse-values -f values.prod.yaml`

Uninstall: `helm uninstall oap --namespace oap && kubectl delete namespace oap`

---

## Health checks

| Endpoint    | Purpose                   | Auth |
|-------------|---------------------------|------|
| `/health`   | Liveness probe            | No   |
| `/ready`    | Readiness probe (DB+NATS) | No   |
| `/metrics`  | Prometheus metrics        | No   |

K8s probe config:

```yaml
livenessProbe:
  httpGet: { path: /health, port: 8080 }
  initialDelaySeconds: 30
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /ready, port: 8080 }
  initialDelaySeconds: 5
  periodSeconds: 5
```

---

## Backup and disaster recovery

Schedule `pg_dump` with cron:

```bash
# /etc/cron.d/oap-backup
0 2 * * * docker compose -f /opt/oap/deploy/docker-compose.prod.yml exec -T postgres pg_dump -U oap oap | gzip > /backups/oap-$(date +\%F).sql.gz
```

Retain daily backups for 30 days, weekly for 12 weeks. To restore:

```bash
docker compose stop oap-server
cat backup.sql | docker compose exec -T postgres psql -U oap oap
docker compose start oap-server
```

---

## Next steps

- [SECURITY.md](SECURITY.md) -- production security hardening
- [COMMERCIAL.md](COMMERCIAL.md) -- commercial tier setup
- [API.md](API.md) -- REST API reference
