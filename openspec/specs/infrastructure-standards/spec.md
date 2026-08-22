# Infrastructure Standards

> **Phase:** Infrastructure
> **STATUS: COMPLETE**
> **Source:** `deploy/docker-compose.yml`, `deploy/Dockerfile.server`, `deploy/Dockerfile.web`, `.dockerignore`, `deploy/{nats,postgres,dex,prometheus,grafana}/`
> **App Path:** `deploy/`

---

## Description

Infrastructure standards define how OpenAgentPlatform is packaged, deployed,
configured and observed. The deployment unit is a Docker Compose stack rooted at
`deploy/docker-compose.yml` with six services:

| Service | Image / Build | Role |
|---------|---------------|------|
| `postgres` | `timescale/timescaledb:2.17.2-pg16` | Primary datastore + time-series |
| `nats` | `nats:2.10.22-alpine` | Messaging / JetStream |
| `dex` | `ghcr.io/dexidp/dex:v2.40.0` | OIDC identity provider |
| `server` | `deploy/Dockerfile.server` | Go API server (`cmd/server`) |
| `web` | `deploy/Dockerfile.web` | React frontend behind nginx |
| — | `deploy/{prometheus,grafana}/` | Metrics + dashboards (config present) |

Three principles run through the configuration. **Images carry no
credentials** — every secret arrives at runtime via `${VAR}` interpolation, which
is what allows `deploy/docker-compose.yml` onto the secret-scanner allowlist.
**Startup order is health-gated**, not timing-gated: `depends_on` uses
`condition: service_healthy` so the server never races an unready database.
**Every service is resource-bounded**, so one runaway container cannot starve the
host.

## User Story

**As** an operator deploying the platform,
**I want** a single health-gated, resource-bounded Compose stack that takes all
secrets from the environment,
**so that** I can bring up a reproducible deployment with one command and no
credential ever lands in an image layer or the repository.

---

## Requirements

### 1. Container Build Standards

1.1. Application images MUST use multi-stage builds separating `build` from
`runtime`, so no compiler or source ships in the final image.

1.2. Go builds MUST set `CGO_ENABLED=0` to produce a static binary runnable on a
minimal base.

1.3. The runtime stage MUST use a minimal base (`alpine:3.20`) and install only
what the runtime needs (`ca-certificates`, `wget` for the healthcheck).

1.4. Dependency resolution MUST be layered ahead of source copy so module
downloads cache independently of code changes:

```dockerfile
COPY go.mod go.sum ./
COPY go.work ./
COPY a2a/go.mod a2a/go.sum ./a2a/
COPY secrets/go.mod ./secrets/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/oap-server ./cmd/server
```

1.5. The build context MUST be the repository root (`context: ..`) because the
Go workspace spans multiple modules.

1.6. Images MUST declare `EXPOSE`, a default `ENV` port, a `HEALTHCHECK`, and an
`ENTRYPOINT` (not a bare `CMD`) for the application process.

1.7. Compose builds MUST specify `network: host` for the build phase.

### 2. `.dockerignore` Hygiene

2.1. `.dockerignore` MUST exclude, at minimum:

| Category | Entries |
|----------|---------|
| Host build artifacts | `server`, `agent`, `*.exe`, `dist/`, `web/dist/` |
| Dependency trees | `node_modules/`, `web/node_modules/` |
| Python caches | `__pycache__/`, `*.pyc` |
| Tooling caches | `.pi/`, `.cache/`, `.turbo/`, `coverage/`, `*.log` |
| VCS / CI | `.git/`, `.github/` |
| Local env | `.env`, `.env.local` |

2.2. Excluding `.env` and `.env.local` is a **security requirement**, not an
optimisation: without it a local credential file would be baked into a
distributable image layer.

2.3. Host-built binaries MUST be excluded so a stale local build cannot shadow
the in-image build.

### 3. Service Configuration

3.1. Every service MUST declare `restart: unless-stopped`.

3.2. Every service MUST declare a `healthcheck` with
`interval: 10s`, `timeout: 5s`, `retries: 5`:

| Service | Probe |
|---------|-------|
| `postgres` | `pg_isready -U oap -d oap` |
| `nats` | `wget --spider http://localhost:8222/healthz` |
| `dex` | `wget --spider http://localhost:5556/dex/healthz` |
| `server` | `wget --spider http://localhost:8080/healthz` |
| `web` | `wget --spider http://localhost:80` |

3.3. Startup ordering MUST be health-gated via `depends_on` with
`condition: service_healthy`. `server` MUST wait on `postgres`, `nats` and
`dex`; `web` MUST wait on `server`.

3.4. Every service MUST declare resource limits:

| Service | Memory | CPUs |
|---------|--------|------|
| `postgres` | 1G | 1.0 |
| `nats` | 512M | 0.5 |
| `server` | 512M | 0.5 |
| `dex` | 256M | 0.25 |
| `web` | 256M | 0.25 |

3.5. Stateful data MUST use named volumes (`pg_data`, `nats_jetstream`), never
bind mounts into the container's writable layer.

3.6. Configuration files MUST be mounted read-only (`:ro`).

### 4. Configuration and Secrets

4.1. All credentials MUST be supplied by environment interpolation
(`${POSTGRES_PASSWORD}`, `${JWT_SECRET}`, `${OIDC_CLIENT_SECRET}`) and MUST NOT
appear literally in any tracked file.

4.2. Because it contains only `${VAR}` references,
`deploy/docker-compose.yml` is allowlisted in the secret scanner
(`secret-scanning` §3.1). That allowlist entry is valid **only** while the file
holds no literal credentials — introducing one both breaks this requirement and
silently evades the scanner.

4.3. Variables with a safe non-secret default MAY use
`${VAR:-default}` (e.g. `NATS_URL`); secrets MUST NOT have defaults.

4.4. The server MUST be configured entirely by environment:
`HTTP_PORT`, `APP_ENV`, `LOG_LEVEL`, `POSTGRES_DSN`, `NATS_URL`,
`OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `JWT_SECRET`.

4.5. `.env.example` MUST document every required variable using the documented
placeholder values (`dev-secret-change-me`, `oap-web-secret`), and the real
`.env` MUST be git-ignored.

### 5. Observability

5.1. Every service MUST expose an HTTP health endpoint suitable for an
unauthenticated container probe.

5.2. The platform MUST standardise on `/healthz` for application services, as
implemented in both the Dockerfile `HEALTHCHECK` and the compose healthcheck.

5.3. Log level MUST be runtime-configurable via `LOG_LEVEL` (default `info`).

5.4. Application logging MUST use structured logging (`slog`), never
`fmt.Print*` — enforced by `pattern-scanning` PREVENT-010.

5.5. NATS MUST expose its monitoring port (`8222`) for health and metrics
scraping.

5.6. Prometheus and Grafana configuration MUST live under `deploy/prometheus/`
and `deploy/grafana/`. These are **not** yet declared as services in
`deploy/docker-compose.yml` — see Known Divergences.

### 6. Port Allocation

6.1. Host port mappings MUST be explicit and non-overlapping:

| Port(s) | Service |
|---------|---------|
| 5432 | postgres |
| 4222, 6222, 7422, 8222 | nats (client, cluster, gateway, monitoring) |
| 5556 | dex |
| 8080 | server |
| 5173 → 80 | web |

### 7. Deployment Operations

7.1. The runtime upgrade path MUST be pull → rebuild → recreate → verify:

```bash
git pull && docker compose -f deploy/docker-compose.yml build
docker compose -f deploy/docker-compose.yml up -d
curl -sS http://localhost:8080/healthz
```

7.2. Images MUST be proven buildable in the release gate before tagging
(`deploy-pipeline` §7).

7.3. A separate `deploy/docker-compose.dev.yml` MUST provide the local
development topology, keeping dev-only conveniences out of the production file.

---

## Known Divergences

| # | Divergence | Impact |
|---|-----------|--------|
| 1 | `deploy.sh` post-release text tells operators to verify `/health`; every healthcheck uses `/healthz` | Documented verification step probes a path the server may not serve |
| 2 | `Dockerfile.server` builds on `golang:1.26.3-alpine`; CI tests Go 1.22/1.23 | Released binary is built on a toolchain no CI job exercises |
| 3 | `prometheus/` and `grafana/` config exist but neither is a service in `docker-compose.yml` | Metrics stack is configured but not deployed by the stack |
| 4 | `postgres` healthcheck hardcodes `-U oap -d oap` while the service uses `${POSTGRES_USER}`/`${POSTGRES_DB}` | Healthcheck fails permanently if either variable is set to anything but `oap` |
| 5 | `version: "3.9"` is retained though Compose V2 ignores it | Cosmetic; emits a deprecation warning |
| 6 | All service ports are published to the host, including `postgres` and `nats` | Datastore and broker are host-reachable; should be internal-only in production |
| 7 | `APP_ENV: development` is hardcoded in the production compose file | A production deploy runs with development semantics unless overridden |

---

## Verification

```bash
# Validate compose syntax and resolved config
docker compose -f deploy/docker-compose.yml config >/dev/null && echo "compose OK"

# Confirm no literal credentials (only ${VAR} interpolation)
grep -nE '(PASSWORD|SECRET|DSN):' deploy/docker-compose.yml

# Confirm health-gating and resource limits on every service
grep -c 'condition: service_healthy' deploy/docker-compose.yml
grep -c 'memory:' deploy/docker-compose.yml

# Confirm .env exclusion from images
grep -nE '^\.env' .dockerignore
```

---

## Related Specifications

- `deploy-pipeline` — release gate that builds these images
- `secret-scanning` — allowlist contract for the compose file
- `schema-health` — migration validation for the Postgres schema
- `ci-pipeline` — toolchain versions used to test what these images build
