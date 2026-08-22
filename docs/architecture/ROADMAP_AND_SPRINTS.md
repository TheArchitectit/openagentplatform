# Roadmap & Sprint Plan

> **Version:** 2.0.0 | **Last Updated:** 2026-08-22 | **Status:** Authoritative Blueprint

---

## 1. Overview

7-phase, 46-week implementation plan to build OpenAgentPlatform from scratch to commercial launch.

```
Phase 0  Phase 1    Phase 2  Phase 3  Phase 4  Phase 5       Phase 6
[4w]     [10w]      [6w]     [4w]     [6w]     [8w]          [8w]
├────┤├──────────┤├──────┤├──────┤├──────┤├────────────┤├────────────┤
Found.   Core RMM    A2A+    Secret   React     Production    Commercial
                      Agents   Mgmt     UI        Hardening     Tiering

v0.1α ──→ v0.1α ──→ v0.3α ──→ v0.4β ──→ v0.5β ──→ v1.0 GA ──→ v1.1
```

---

## 2. Phase Definitions

| Phase | Focus | Duration | Exit Criteria | Release |
|-------|-------|----------|---------------|---------|
| 0 | Foundation | 4 weeks | Agent heartbeats visible in UI | `v0.1.0-alpha` |
| 1 | Core RMM | 10 weeks | Checks, alerts, policies, patches, scripts, remote working | `v0.1.0-alpha` |
| 2 | A2A + Agents | 6 weeks | Gateway + 6 framework adapters + process pool + event bridge | `v0.3.0-alpha` |
| 3 | Secret Management | 4 weeks | Vault + Infisical + references + credential injection | `v0.4.0-beta` |
| 4 | Frontend | 6 weeks | Full React UI with all dashboards + real-time updates | `v0.5.0-beta` |
| 5 | Production | 8 weeks | Observability + load testing + security audit + docs + CI/CD | `v1.0.0` (GA) |
| 6 | Commercial | 8 weeks | Feature gating + multi-tenancy + billing + enterprise features | `v1.1.0` |

---

## 3. Phase 0 Sprint Breakdown — COMPLETE

### Sprint 0.1 (Week 1-2) ✓

| Story | Description | Status |
|-------|-------------|--------|
| 0.1.1 | Monorepo scaffold (Go workspace, Python venv, TypeScript workspace) | ✓ |
| 0.1.2 | CI pipelines for Go, Python, TypeScript | ✓ |
| 0.1.3 | PostgreSQL schema with migrations 01-09 | ✓ |
| 0.1.4 | NATS with mTLS and SPIFFE mappings | ✓ |
| 0.1.5 | OIDC auth with Dex test IdP | ✓ |
| 0.1.6 | OpenAPI 3.1 spec generation | ✓ |
| 0.1.7 | React shell with TanStack Router and Query | ✓ |

### Sprint 0.2 (Week 3-4) ✓

| Story | Description | Status |
|-------|-------------|--------|
| 0.2.1 | Agent CLI binary (Go, cross-compiled) | ✓ |
| 0.2.2 | Agent registration and heartbeat flow | ✓ |
| 0.2.3 | Endpoint list page with real-time updates | ✓ |
| 0.2.4 | Audit log infrastructure | ✓ |
| 0.2.5 | 5-minute setup guide | ✓ |

---

## 4. Phase 1 Sprint Breakdown — COMPLETE

### Sprint 1.1 (Week 5-6): Checks ✓

| Story | Description | Status |
|-------|-------------|--------|
| 1.1.1 | Check CRUD API (10 check types) | ✓ |
| 1.1.2 | Agent check executor | ✓ |
| 1.1.3 | Built-in check library (ping, CPU, memory, disk, service) | ✓ |
| 1.1.4 | Check result ingest pipeline with threshold evaluation | ✓ |
| 1.1.5 | Checks dashboard with live status | ✓ |

### Sprint 1.2 (Week 7-8): Alerts ✓

| Story | Description | Status |
|-------|-------------|--------|
| 1.2.1 | Alert rule engine with state machine | ✓ |
| 1.2.2 | Notification channels (email, Slack, webhook) | ✓ |
| 1.2.3 | Alert inbox and detail page | ✓ |
| 1.2.4 | Alert preferences and routing | ✓ |

### Sprint 1.3 (Week 9-10): Policies ✓

| Story | Description | Status |
|-------|-------------|--------|
| 1.3.1 | OPA integration for policy evaluation | ✓ |
| 1.3.2 | Compliance collectors | ✓ |
| 1.3.3 | Policy library and editor UI | ✓ |
| 1.3.4 | Policy violation alerts | ✓ |

### Sprint 1.4 (Week 11-12): Patches ✓

| Story | Description | Status |
|-------|-------------|--------|
| 1.4.1 | Patch inventory and scan | ✓ |
| 1.4.2 | Patch approval workflow | ✓ |
| 1.4.3 | Patch deployment engine | ✓ |
| 1.4.4 | Patch status UI with reboot coordination | ✓ |

### Sprint 1.5 (Week 13-14): Scripts/Remote ✓

| Story | Description | Status |
|-------|-------------|--------|
| 1.5.1 | Script library CRUD | ✓ |
| 1.5.2 | 4-runtime script executor (PowerShell, Python, Bash, Node) | ✓ |
| 1.5.3 | Script UI with Monaco editor | ✓ |
| 1.5.4 | SSH/WinRM remote shell (xterm.js + noVNC) | ✓ |
| 1.5.5 | Remote session audit and recording playback | ✓ |

---

## 5. Phase 2 Sprint Breakdown — IN PROGRESS

### Sprint 2.1: A2A Gateway REST + JSON-RPC ✓

**Branch:** `main` (merged)
**Spec:** `openspec/specs/a2a-gateway/spec.md`

| Story | Description | Status |
|-------|-------------|--------|
| 2.1.1 | Canonical data model (Task, Message, AgentCard, Part, Artifact) | ✓ |
| 2.1.2 | REST binding (`/a2a/v1/tasks`, `/a2a/v1/agents`) | ✓ |
| 2.1.3 | JSON-RPC binding (`tasks/send`, `tasks/get`, `tasks/cancel`) | ✓ |
| 2.1.4 | Auth middleware (bearer token, API key, scope-based) | ✓ |
| 2.1.5 | SSE task status streaming (`/a2a/v1/tasks/{id}/subscribe`) | ✓ |

### Sprint 2.2: A2A Gateway gRPC ✓

**Branch:** `sprint/2.2-a2a-grpc` (pushed, PR pending)
**Spec:** `openspec/specs/a2a-gateway/spec.md`

| Story | Description | Status |
|-------|-------------|--------|
| 2.2.1 | Protobuf schema (`a2a/spec/a2a.proto` — 8 RPCs, all message types) | ✓ |
| 2.2.2 | Generated Go bindings (`a2a.pb.go`, `a2a_grpc.pb.go`) | ✓ |
| 2.2.3 | gRPC adapter (`a2a/gateway/grpc_adapter.go` — 454 lines, 8 RPC methods) | ✓ |
| 2.2.4 | Server wiring (auth/logging/recovery interceptors, shared authenticator) | ✓ |
| 2.2.5 | Config (GRPCPort default 9090, start/shutdown lifecycle) | ✓ |
| 2.2.6 | bufconn tests (6 tests: SendTask, GetTask, ListTasks, CancelTask, ListAgents, SubscribeTask) | ✓ |

### Sprint 2.3: A2A Translation Proxy + Frontend Bridge ✓

**Branch:** `main` (merged)
**Spec:** `openspec/specs/a2a-gateway/spec.md`

| Story | Description | Status |
|-------|-------------|--------|
| 2.3.1 | Go reverse-proxy (`internal/api/a2a_proxy.go`) forwarding frontend→gateway | ✓ |
| 2.3.2 | SSE proxy (`internal/api/a2a_proxy_sse.go`) for task event streaming | ✓ |
| 2.3.3 | Python adapter field aliases (cost fields, auth_schemes type alignment) | ✓ |
| 2.3.4 | Frontend envelope contract locked (`web/src/lib/a2a.test.ts`) | ✓ |
| 2.3.5 | SSE error surfacing in UI (reconnecting indicator) | ✓ |
| 2.3.6 | RPC bridge error handling fix + dead code removal | ✓ |

### Sprint 2.4: Task Manager + Agent Registry ✓

**Branch:** `main` (merged)
**Spec:** `openspec/specs/a2a-task-manager/spec.md`, `openspec/specs/a2a-agent-registry/spec.md`

| Story | Description | Status |
|-------|-------------|--------|
| 2.4.1 | Task lifecycle state machine (submitted→working→input-required→completed/failed) | ✓ |
| 2.4.2 | Task store (in-memory, with artifact + message storage) | ✓ |
| 2.4.3 | Agent card registry (store + validation) | ✓ |
| 2.4.4 | NATS JetStream-backed task persistence (planned, in-memory currently) | planned |

### Sprint 2.5: ProcessPool (Warm Agent Instances)

**Branch:** `sprint/2.5-process-pool` (to be created)
**Spec:** `openspec/specs/process-pool/spec.md`
**Phase:** 2 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 2.5.1 | ProcessPool manager (spawn, health-check, recycle warm instances) | planned |
| 2.5.2 | Resource limits (CPU, memory, process count per adapter) | planned |
| 2.5.3 | Instance lifecycle (warm-up, idle timeout, crash recovery) | planned |
| 2.5.4 | Pool metrics (active/idle/count, latency histograms) | planned |

### Sprint 2.6: Event-to-Task Bridge

**Branch:** `sprint/2.6-event-task-bridge` (to be created)
**Spec:** `openspec/specs/event-task-bridge/spec.md`
**Phase:** 2 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 2.6.1 | Event consumer (8 RMM event types: check-fail, alert-fire, patch-ready, policy-violation, agent-offline, script-complete, heartbeat-miss, threshold-breach) | planned |
| 2.6.2 | Event→Task mapper (event type + payload → A2A Task with parts) | planned |
| 2.6.3 | Auto-dispatch to appropriate agent adapter | planned |
| 2.6.4 | Task→Event feedback loop (task completion generates RMM event) | planned |

### Sprint 2.7: Human-in-the-Loop Approval

**Branch:** `sprint/2.7-hitl-approval` (to be created)
**Spec:** `openspec/specs/hitl-approval/spec.md`
**Phase:** 2 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 2.7.1 | Approval request API (create/approve/reject with reason) | planned |
| 2.7.2 | HITL notification (email/Slack/webhook for pending approvals) | planned |
| 2.7.3 | Timeout + escalation (auto-reject or auto-escalate after N hours) | planned |
| 2.7.4 | Audit trail (who approved what, when, with what justification) | planned |
| 2.7.5 | Frontend approval queue UI | planned |

### Sprint 2.8: Framework Adapters (6 Adapters)

**Branch:** `sprint/2.8-framework-adapters` (to be created)
**Spec:** `openspec/specs/a2a-framework-adapters/spec.md`
**Phase:** 2 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 2.8.1 | LangGraph adapter (Python, LangChain ecosystem) | planned |
| 2.8.2 | CrewAI adapter (Python, multi-agent orchestration) | planned |
| 2.8.3 | AutoGen adapter (Python, Microsoft multi-agent) | planned |
| 2.8.4 | Semantic Kernel adapter (C#/.NET, Microsoft) | planned |
| 2.8.5 | OpenAI adapter (Python, Responses API) | planned |
| 2.8.6 | Anthropic adapter (Python, Messages API) | planned |
| 2.8.7 | Adapter SDK (shared adapter base, health/card/task primitives) | planned |
| 2.8.8 | Adapter conformance test suite | planned |

---

## 6. Phase 3 Sprint Breakdown — NOT STARTED

### Sprint 3.1: Secret Backend Abstraction

**Branch:** `sprint/3.1-secret-backends` (to be created)
**Spec:** `openspec/specs/secret-management/spec.md`
**Phase:** 3 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 3.1.1 | SecretBackend interface (Get, Set, Delete, List, Rotate) | planned |
| 3.1.2 | HashiCorp Vault integration (AppRole, K8s, JWT auth) | planned |
| 3.1.3 | Infisical integration (universal secret management) | planned |
| 3.1.4 | DB-backed backend (current `internal/billing/secrets.go` refactored) | planned |

### Sprint 3.2: Secret Reference Pipeline

**Branch:** `sprint/3.2-secret-references` (to be created)
**Spec:** `openspec/specs/secret-management/spec.md`
**Phase:** 3 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 3.2.1 | URI resolution (`vault://secret-name/field`, `infisical://project/path`) | planned |
| 3.2.2 | Credential injection (env vars, file mounts, stdin) | planned |
| 3.2.3 | Secret caching with TTL + invalidation | planned |
| 3.2.4 | A2A auth token management (EdDSA JWTs) | planned |
| 3.2.5 | MCP OAuth 2.1 server for tool authentication | planned |

---

## 7. Phase 4 Sprint Breakdown — PARTIAL

### Sprint 4.1: Dashboard + Agent Management

**Branch:** `sprint/4.1-dashboard` (to be created)
**Spec:** `openspec/specs/frontend-react/spec.md`
**Phase:** 4 | **Status:** PARTIAL (shell + A2A tasks dashboard exist)

| Story | Description | Status |
|-------|-------------|--------|
| 4.1.1 | Dashboard home (KPI cards, recent alerts, agent status) | partial |
| 4.1.2 | Agent management (list, detail, health, capabilities) | partial |
| 4.1.3 | Settings (Users, RBAC, SSO, API Keys) | partial |
| 4.1.4 | Real-time updates via WebSocket | planned |

### Sprint 4.2: Monitoring + Policy UI

**Branch:** `sprint/4.2-monitoring-ui` (to be created)
**Spec:** `openspec/specs/frontend-react/spec.md`
**Phase:** 4 | **Status:** PLANNED

| Story | Description | Status |
|-------|-------------|--------|
| 4.2.1 | Checks dashboard (live status, run history, assignments) | planned |
| 4.2.2 | Alerts inbox (list, acknowledge, resolve, snooze) | planned |
| 4.2.3 | Policy editor (OPA rules, scope assignment, violation history) | planned |
| 4.2.4 | Patch status (compliance scorecard, per-agent status) | planned |

### Sprint 4.3: A2A + Remote Access UI

**Branch:** `sprint/4.3-a2a-remote-ui` (to be created)
**Spec:** `openspec/specs/frontend-react/spec.md`
**Phase:** 4 | **Status:** PARTIAL (A2A tasks dashboard exists)

| Story | Description | Status |
|-------|-------------|--------|
| 4.3.1 | A2A dashboard (tasks list, agent cards, adapter health) | partial |
| 4.3.2 | Script editor (Monaco, execution, live output) | partial |
| 4.3.3 | Remote shell (xterm.js + noVNC integration) | planned |
| 4.3.4 | Session audit playback | planned |

---

## 8. Phase 5 Sprint Breakdown — NOT STARTED

### Sprint 5.1: Observability

| Story | Description | Status |
|-------|-------------|--------|
| 5.1.1 | OpenTelemetry instrumentation (Go + Python + TypeScript) | planned |
| 5.1.2 | Prometheus metrics export | planned |
| 5.1.3 | Grafana dashboards (system, A2A, agent health) | planned |
| 5.1.4 | Loki log aggregation | planned |

### Sprint 5.2: Load Testing + Security

| Story | Description | Status |
|-------|-------------|--------|
| 5.2.1 | k6 load testing (10K endpoint target) | planned |
| 5.2.2 | Locust stress testing (sustained load) | planned |
| 5.2.3 | OWASP ZAP security scan | planned |
| 5.2.4 | gitleaks + secret scan hardening | planned |

### Sprint 5.3: Resilience + Documentation

| Story | Description | Status |
|-------|-------------|--------|
| 5.3.1 | chaos-mesh resilience testing | planned |
| 5.3.2 | MkDocs Material documentation site | planned |
| 5.3.3 | API reference generation (OpenAPI → docs) | planned |
| 5.3.4 | Contributor guide + architecture decision records | planned |

### Sprint 5.4: CI/CD Hardening

| Story | Description | Status |
|-------|-------------|--------|
| 5.4.1 | 12 GitHub Actions workflows (lint, test, build, scan, deploy) | partial (8 exist) |
| 5.4.2 | Branch protection rules (require reviews, status checks) | planned |
| 5.4.3 | Release automation (changelog, tag, Docker publish) | partial (deploy.sh exists) |
| 5.4.4 | Dependency auto-update (Dependabot / Renovate) | planned |

---

## 9. Phase 6 Sprint Breakdown — NOT STARTED

### Sprint 6.1: Feature Gating + Licensing

| Story | Description | Status |
|-------|-------------|--------|
| 6.1.1 | BSL 1.1 license file + contributor agreement | planned |
| 6.1.2 | Feature gating (Ed25519 license validation) | planned |
| 6.1.3 | License tiers (Community, Pro, Enterprise) | planned |

### Sprint 6.2: Multi-Tenancy

| Story | Description | Status |
|-------|-------------|--------|
| 6.2.1 | Tenant model (PostgreSQL RLS) | planned |
| 6.2.2 | Data isolation (per-tenant secrets, configs) | planned |
| 6.2.3 | Tenant admin UI | planned |

### Sprint 6.3: Billing + Enterprise

| Story | Description | Status |
|-------|-------------|--------|
| 6.3.1 | Stripe Billing integration | planned |
| 6.3.2 | Usage tracking + metering | planned |
| 6.3.3 | Enterprise reporting (templates, scheduled delivery) | planned |
| 6.3.4 | Managed A2A relay service | planned |

---

## 10. Branch Naming Convention

All sprint branches follow: `sprint/<phase>.<sprint-number>-<short-name>`

Examples:
- `sprint/2.2-a2a-grpc` (completed)
- `sprint/2.5-process-pool` (next)
- `sprint/3.1-secret-backends` (future)

---

## 11. Spec Format

All capability specs follow OpenSpec format under `openspec/specs/<capability>/spec.md`.

Each spec contains:
- **Description** — what the capability does
- **User Story** — who uses it and why
- **Requirements** — numbered, with nested sub-requirements
- **Status** — COMPLETE | PARTIAL | PLANNED
- **Phase** — which phase this belongs to

---

## 12. Open Questions

| # | Question | Owner | Decision Date | Status |
|---|----------|-------|---------------|--------|
| O1 | Should agent binary use CGO for prlimit or pure-Go syscall? | agent-lead | Phase 1 Sprint 1 | CLOSED — pure-Go |
| O2 | PostgreSQL RLS vs schema-per-tenant for multi-tenancy? | backend-lead | Phase 6 Sprint 1 | OPEN |
| O3 | A2A streaming: SSE or WebSocket from gateway to frontend? | a2a-lead | Phase 2 Sprint 1 | CLOSED — SSE |
| O4 | CDN vs self-hosted for frontend static assets? | devops-lead | Phase 4 Sprint 1 | OPEN |
| O5 | Vault namespace support for enterprise multi-tenancy? | secrets-lead | Phase 3 Sprint 2 | OPEN |
| O6 | Agent binary auto-update or require explicit approval? | product | Phase 1 Sprint 5 | OPEN |
| O7 | Commercial license: online-only or offline grace period? | product | Phase 6 Sprint 1 | OPEN |
| O8 | MCP tools reference A2A skills directly or via indirection? | mcp-lead | Phase 2 Sprint 1 | OPEN |
| O9 | k6 vs Locust vs Artillery for primary load testing? | test-lead | Phase 5 Sprint 1 | OPEN |

---

## 13. Risk Register

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| R1 | Agent binary compatibility breaks on OS updates | Medium | High | Test matrix: Win10/11, Ubuntu 20/22/24, macOS 13/14; nightly CI on real VMs |
| R2 | NATS JetStream data loss on broker failure | Low | Critical | 3-node cluster with file storage; daily stream backup CronJob |
| R3 | LLM provider API changes break adapters | High | Medium | Adapter isolation; configurable model endpoints; fallback provider |
| R4 | Vault seal causes secret unavailability | Low | Critical | Auto-unseal via K8s; grace period with cached values; alert on seal event |
| R5 | A2A protocol spec divergence from implementations | Medium | Medium | Conformance test vectors; track upstream spec; version negotiation |
| R6 | React SPA bundle size exceeds performance budget | Medium | Medium | Route-based code splitting; tree-shaking; performance CI check |
| R7 | Multi-tenant data leak via query isolation failure | Low | Critical | PostgreSQL RLS; integration test for every query path; quarterly security audit |
| R8 | Agent subprocess crashes leak secrets in core dumps | Low | High | `prlimit` core size = 0; secret zeroing after use; container seccomp profiles |
| R9 | CI/CD pipeline becomes a bottleneck | Medium | Medium | Sharding, parallelization, incremental test runs; 25-min budget |
| R10 | Documentation drift from implementation | High | Low | Inline-doc lint in CI; doc changes required in same PR as code changes |
