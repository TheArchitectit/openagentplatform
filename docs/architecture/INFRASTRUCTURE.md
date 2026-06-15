# Infrastructure Architecture

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

---

## 1. Overview

OpenAgentPlatform's infrastructure spans three environments: **local development** (Docker Compose), **production** (Kubernetes + Helm), and **CI/CD** (GitHub Actions). The observability stack (OpenTelemetry + Prometheus + Grafana + Loki) provides full visibility across all environments.

**App Path:** `deploy/`

---

## 2. Docker Compose Dev Stack (13 Services)

| # | Service | Image | Port | Health Check | Purpose |
|---|---------|-------|------|-------------|---------|
| 1 | postgres | `postgres:16-alpine` | 5432 | `pg_isready` | Primary data store |
| 2 | nats | `nats:2.10-alpine` | 4222/8222 | HTTP `/healthz` | Message bus (JetStream) |
| 3 | redis | `redis:7-alpine` | 6379 | `redis-cli ping` | Cache, sessions, rate limiting |
| 4 | api | `oap/api:latest` | 8080 | HTTP `/health/live` | REST + gRPC API server |
| 5 | web | `oap/web:latest` | 3000 | HTTP `/` | React SPA |
| 6 | agent | `oap/agent:latest` | — | depends on NATS | Endpoint agent binary |
| 7 | otel-collector | `otel/opentelemetry-collector-contrib:latest` | 4317/4318 | HTTP `/` | Trace/metric/log collection |
| 8 | prometheus | `prom/prometheus:latest` | 9090 | HTTP `/-/healthy` | Metric storage + alerting |
| 9 | grafana | `grafana/grafana:latest` | 3001 | HTTP `/api/health` | Dashboards + visualization |
| 10 | loki | `grafana/loki:latest` | 3100 | HTTP `/ready` | Log aggregation |
| 11 | promtail | `grafana/promtail:latest` | — | depends on loki | Log shipper |
| 12 | mailhog | `mailhog/mailhog:latest` | 8025 | SMTP | Email testing (notification channel) |
| 13 | vault | `hashicorp/vault:1.16` | 8200 | HTTP `/v1/sys/health` | Secret management (dev mode) |

### Quick Start

```bash
cd deploy/
cp .env.example .env     # Edit DB_PASSWORD, NATS_TOKEN, etc.
docker compose up -d      # All 13 services boot
open http://localhost:3000  # React dashboard
```

---

## 3. Helm Chart (`charts/oap/`)

### 3.1 Template Count

60+ templates covering all platform services:

| Category | Templates | Purpose |
|----------|-----------|---------|
| API Server | Deployment + HPA + PDB + Service + Ingress | Core REST/gRPC API |
| Web UI | Deployment + Service | React SPA |
| Agent DaemonSet | DaemonSet + Service | Endpoint agent (per-node) |
| PostgreSQL | StatefulSet + Service | Primary database |
| NATS | StatefulSet + ConfigMap | Message bus cluster |
| Redis | StatefulSet + Service | Cache + sessions |
| A2A Gateway | Deployment + HPA + Service | A2A protocol gateway |
| Agent Adapter | Deployment + Service | Framework adapter service |
| Secret Service | Deployment + Service | Secret management |
| MCP Server | Deployment + Service | MCP guardrail tools |
| OTel Collector | Deployment + ConfigMap | Observability pipeline |
| Prometheus | Deployment + ServiceMonitor + PrometheusRule + AlertmanagerConfig | Metrics + alerts |
| Grafana | Deployment + Dashboard ConfigMaps | Dashboards |
| Loki | Deployment | Log storage |
| Promtail | DaemonSet | Log shipping |
| cert-manager | ClusterIssuer | TLS certificate automation |
| NetworkPolicies | Default-deny + 7 allow-policies | Network segmentation |
| RBAC | Role + RoleBinding + ServiceAccount | Access control |
| Migrations | K8s CronJob | Database schema management |

### 3.2 values.yaml

200+ keys organized by service with sensible defaults:

```yaml
api:
  replicaCount: 2
  image: oap/api:latest
  resources: { requests: { cpu: 250m, memory: 256Mi } }
  hpa: { minReplicas: 2, maxReplicas: 10, targetCPU: 70 }
nats:
  clusterSize: 3
  jetStream: { maxMem: 1Gi, maxFile: 10Gi }
postgresql:
  primary: { persistence: { size: 50Gi } }
  readReplicas: 1
```

---

## 4. CI/CD (12 GitHub Actions Workflows)

| # | Workflow | Trigger | Purpose |
|---|----------|---------|---------|
| 1 | `ci-unit.yml` | PR | Run unit tests for all services (matrix: Go 1.22/1.23, Python 3.12/3.13, Node 20/22) |
| 2 | `ci-integration.yml` | PR (paths filter) | Integration tests with Testcontainers |
| 3 | `ci-e2e.yml` | Push to main, workflow_dispatch | Playwright E2E tests |
| 4 | `ci-load.yml` | Nightly, workflow_dispatch | k6 + Locust load tests |
| 5 | `ci-security.yml` | PR + nightly | ZAP scanning, gitleaks, trufflehog, RBAC fuzz, Trivy image scan |
| 6 | `ci-chaos.yml` | Nightly | chaos-mesh experiments (NATS down, DB failover, agent disconnect) |
| 7 | `ci-coverage-gate.yml` | PR | Merge lcov, enforce ≥90% lines, ≥85% branches |
| 8 | `build-images.yml` | Tag push | Buildx multi-arch, cosign signing, push to registry |
| 9 | `migrations-check.yml` | PR | Verify migrations are reversible (`migrate` + `migrate rollback`) |
| 10 | `release.yml` | Tag push `v*` | Generate release notes, publish binaries (Go cross-compile) |
| 11 | `deploy-staging.yml` | Push to `release/**` | Helm upgrade staging namespace |
| 12 | `deploy-prod.yml` | Manual approval | Helm upgrade production with canary (10% → 50% → 100%) |

---

## 5. Observability Stack

### 5.1 Traces

```
OTel SDK (instrumented services) → OTel Collector → Tempo
                                                     ↓
                                             Tail-based sampling:
                                             • Keep ALL errors
                                             • Keep ALL slow (>5s)
                                             • 10% probabilistic
```

### 5.2 Metrics (24 custom `oap_*` business metrics)

| Category | Metric | Type | Labels |
|----------|--------|------|--------|
| Endpoint/RMM | `oap_agent_checkins_total` | Counter | agent_id, status |
| Endpoint/RMM | `oap_check_results_total` | Counter | check_type, status |
| Endpoint/RMM | `oap_script_duration_seconds` | Histogram | runtime, exit_code |
| Endpoint/RMM | `oap_alerts_fired_total` | Counter | severity, channel |
| A2A | `oap_a2a_tasks_created_total` | Counter | agent_framework, task_type |
| A2A | `oap_a2a_task_duration_seconds` | Histogram | framework, state |
| Secrets | `oap_secret_injection_duration_seconds` | Histogram | method, backend |
| LLM | `oap_llm_tokens_total` | Counter | provider, model, direction |
| LLM | `oap_llm_cost_dollars` | Counter | provider, model |

### 5.3 SLOs (4)

| SLO | Target | Burn-Rate Alert |
|-----|--------|-----------------|
| API availability | 99.9% | 14.4x (critical), 6x (warning) |
| Check-in p99 | <5s | 6x over 5m window |
| A2A task p95 | <30s | 6x over 5m window |
| Alert delivery p99 | <60s | 14.4x over 1h window |

### 5.4 Grafana Dashboards (5)

| Dashboard | Panels | Key Visualizations |
|-----------|--------|-------------------|
| Overview | 10 | Endpoint count, alert count, system health |
| RMM | 8 | Check pass/fail rates, patch compliance, script execution trends |
| A2A | 6 | Task throughput by framework, latency percentiles, error rates |
| Infrastructure | 8 | Resource utilization, DB connections, NATS stream stats |
| Cost | 6 | LLM token usage by framework/agent, per-endpoint cost |

### 5.5 Logs

Structured JSON → Promtail → Loki. PII redaction via regex (password, token, SSN patterns). Correlation IDs propagated from traces.

---

## 6. Security

| Control | Implementation |
|---------|---------------|
| **TLS everywhere** | cert-manager ClusterIssuer (Let's Encrypt or internal CA) |
| **Network segmentation** | Default-deny NetworkPolicy + 7 allow-policies (API↔DB, API↔NATS, Frontend↔API, etc.) |
| **PodSecurity** | Restricted pod security standards (no privileged, no hostPID, read-only root FS) |
| **RBAC** | Per-service ServiceAccounts with minimal permissions |
| **Secret rotation** | Vault sidecar/injector for automatic credential rotation |

---

## 7. Backup and Disaster Recovery

| What | Method | Schedule | RPO | RTO |
|------|--------|----------|-----|-----|
| PostgreSQL | pg_dump CronJob | Every 5 min | 5 min | 15 min |
| NATS streams | NATS backup CronJob | Every 5 min | 5 min | 15 min |
| K8s resources | Velero | Hourly | 1 hour | 30 min |
| Vault | Vault Raft snapshot | Every 5 min | 5 min | 15 min |

Recovery procedure: Velero restore → PostgreSQL restore → NATS stream replay → Vault unseal.

---

## 8. Implementation Steps (7 Phases, 7 Weeks)

| Phase | Duration | Focus |
|-------|----------|-------|
| 1 | 2 days | Docker Compose dev stack (13 services, .env, health checks) |
| 2 | 3 days | Helm chart scaffolding (60+ templates, values.yaml, secrets) |
| 3 | 5 days | CI/CD workflows (12 GitHub Actions, matrix builds, coverage gates) |
| 4 | 2 days | Database migration infrastructure (Alembic/Django, rollback procedures) |
| 5 | 5 days | Observability stack (OTel instrumentation, Prometheus rules, Grafana dashboards, Loki logs) |
| 6 | 3 days | Security hardening (cert-manager, NetworkPolicies, PodSecurity, RBAC) |
| 7 | 5 days | Multi-region + backup + DR (Velero, pg_dump CronJobs, NATS backup) |
