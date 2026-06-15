# Roadmap & Sprint Plan

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

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

## 3. Phase 0 Sprint Breakdown

### Sprint 0.1 (Week 1-2)

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 0.1.1 | **As a developer, I want a monorepo scaffold** with Go workspace, Python venv, and TypeScript workspace so that I can develop across all languages in one repo. Acceptance: `go build ./...`, `python -m pytest`, `npm run build` all succeed. | A (Backend) | M |
| 0.1.2 | **As a developer, I want CI pipelines** for Go, Python, and TypeScript so that every PR is automatically tested. Acceptance: PR triggers lint+test matrix. | D (Infra) | M |
| 0.1.3 | **As a developer, I want PostgreSQL schema** with migrations 01-09 so that all base tables exist. Acceptance: `python manage.py migrate` succeeds; 9 tables created. | A (Backend) | L |
| 0.1.4 | **As a developer, I want NATS with mTLS** and SPIFFE mappings so that agent connections are authenticated. Acceptance: Agent connects via client cert; plaintext rejected. | D (Infra) | M |
| 0.1.5 | **As a user, I want OIDC auth** with Dex test IdP so that I can log in. Acceptance: Login via Dex redirect; JWT issued; API returns user identity. | A (Backend) | L |
| 0.1.6 | **As a developer, I want OpenAPI 3.1 spec** generation so that API is documented. Acceptance: `/docs/swagger` renders complete API spec. | A (Backend) | M |
| 0.1.7 | **As a user, I want a React shell** with TanStack Router and Query so that I can navigate the app. Acceptance: App loads, sidebar renders, login redirects. | C (Frontend) | M |

### Sprint 0.2 (Week 3-4)

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 0.2.1 | **As an admin, I want an agent CLI binary** (Go, cross-compiled) so that I can deploy to Windows/Linux/macOS. Acceptance: Binary connects to NATS, registers, sends heartbeat. | B (Agent) | XL |
| 0.2.2 | **As a user, I want agent registration and heartbeat** flow so that devices appear in the dashboard. Acceptance: Agent registers, heartbeats every 60s, status updates in UI. | A+B | L |
| 0.2.3 | **As a user, I want an endpoint list page** with real-time updates so that I can see all managed devices. Acceptance: Agent list shows hostname, status, last seen; updates live via WebSocket. | C (Frontend) | M |
| 0.2.4 | **As a compliance officer, I want audit log** infrastructure so that all actions are tracked. Acceptance: Login, API call, and agent action produce audit records. | A (Backend) | M |
| 0.2.5 | **As a developer, I want a 5-minute setup guide** so that new contributors can start quickly. Acceptance: Fresh clone → `docker compose up` → dashboard visible in <5 minutes. | D (Infra) | S |

---

## 4. Phase 1 Sprint Breakdown

### Sprint 1.1 (Week 5-6): Checks

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 1.1.1 | **As an admin, I want Check CRUD API** so that I can define monitoring checks. Acceptance: Create/read/update/delete checks via REST; all 10 check types supported. | A | L |
| 1.1.2 | **As an agent, I want a check executor** so that I can run checks locally and report results. Acceptance: Agent receives check command, executes, sends result via NATS. | B | L |
| 1.1.3 | **As an admin, I want a built-in check library** (ping, CPU, memory, disk, service) so that common checks are ready out-of-the-box. Acceptance: 5 check types work end-to-end; results visible in UI. | A | XL |
| 1.1.4 | **As a system, I want a check result ingest pipeline** so that results are stored and thresholds evaluated. Acceptance: Result posted → stored in DB → alert triggered if threshold exceeded. | A | M |
| 1.1.5 | **As a user, I want a checks dashboard** with live status so that I can see check health at a glance. Acceptance: Table of checks with status indicators; auto-refresh every 30s. | C | M |

### Sprint 1.2 (Week 7-8): Alerts

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 1.2.1 | **As a system, I want an alert rule engine** with state machine so that alerts fire on threshold breaches. Acceptance: 3 consecutive check failures → alert created; state machine transitions tested. | A | XL |
| 1.2.2 | **As an admin, I want notification channels** (email, Slack, webhook) so that alerts reach the right people. Acceptance: Alert fires → email sent, Slack message posted, webhook called. | A | L |
| 1.2.3 | **As a user, I want an alert inbox and detail page** so that I can triage alerts. Acceptance: List alerts, acknowledge, resolve, snooze; detail page shows timeline. | C | M |
| 1.2.4 | **As an admin, I want alert preferences and routing** so that alerts go to the right team. Acceptance: Configure per-severity routing; silence periods work. | A | M |

### Sprint 1.3 (Week 9-10): Policies

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 1.3.1 | **As an admin, I want OPA integration** for policy evaluation so that I can enforce compliance rules. Acceptance: Policy defined → agent actions evaluated against OPA → violations logged. | A | XL |
| 1.3.2 | **As a system, I want compliance collectors** so that agent state is checked against policies. Acceptance: Collector runs on schedule; non-compliant agents flagged. | A | L |
| 1.3.3 | **As a user, I want a policy library and editor UI** so that I can create and modify policies. Acceptance: Create policy with rules, assign scope, preview affected agents. | A+C | L |
| 1.3.4 | **As a system, I want policy violation alerts** so that non-compliance triggers notifications. Acceptance: Policy violated → alert created → notification sent. | A | M |

### Sprint 1.4 (Week 11-12): Patches

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 1.4.1 | **As an admin, I want patch inventory and scan** so that I can see available patches for each agent. Acceptance: Scan triggered → results in DB → per-agent patch list in UI. | A+B | L |
| 1.4.2 | **As an admin, I want a patch approval workflow** so that patches are reviewed before deployment. Acceptance: Patches pending_approval → approve/reject → state transitions logged. | A | L |
| 1.4.3 | **As a system, I want a patch deployment engine** so that approved patches are installed on schedule. Acceptance: Batch deploy to 10 agents; progress tracked; failures retried. | A | XL |
| 1.4.4 | **As a user, I want a patch status UI** with reboot coordination so that I can track deployment progress. Acceptance: Compliance scorecard; per-agent patch status; reboot prompts. | A+C | M |

### Sprint 1.5 (Week 13-14): Scripts/Remote

| Story | Description | Stream | Complexity |
|-------|-------------|--------|------------|
| 1.5.1 | **As an admin, I want script library CRUD** so that I can manage reusable scripts. Acceptance: Create/edit/delete scripts with metadata; 5 runtime types supported. | A | M |
| 1.5.2 | **As an agent, I want a 4-runtime script executor** so that I can run PowerShell, Python, Bash, and Node scripts. Acceptance: Script dispatched → executed → stdout/stderr streamed → result stored. | B | XL |
| 1.5.3 | **As a user, I want a script UI** with Monaco editor so that I can write and test scripts. Acceptance: Syntax highlighting, execution form, live output console. | C | L |
| 1.5.4 | **As a user, I want SSH/WinRM remote shell** (xterm.js + noVNC) so that I can access endpoints remotely. Acceptance: Terminal session opens; keystrokes transmitted; VNC desktop viewable. | B+C | XL |
| 1.5.5 | **As a compliance officer, I want remote session audit** and recording playback so that access is traceable. Acceptance: Session duration logged; recording available for replay; audit trail complete. | A+B | M |

---

## 5. Phase 2-6 Overviews

### Phase 2: A2A + Agents (6 weeks)
- A2A gateway with JSON-RPC, REST, gRPC bindings
- 6 framework adapters (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI, Anthropic)
- ProcessPool with warm agent instances
- Event-to-Task bridge (8 RMM event types → A2A Tasks)
- Human-in-the-loop approval workflow

### Phase 3: Secret Management (4 weeks)
- SecretBackend abstraction with 5 implementations
- HashiCorp Vault integration (AppRole, K8s, JWT auth)
- Infisical integration
- Secret Reference URI resolution pipeline
- Credential injection (env, file, stdin)
- A2A auth token management (EdDSA JWTs)
- MCP OAuth 2.1 server

### Phase 4: Frontend (6 weeks)
- Full React 19 SPA (20 pages in 10 feature modules)
- Dashboard, Agent Management, Monitoring, Patches, Remote Access
- Script Editor (Monaco), A2A Dashboard, Policy Editor
- Settings (Users, RBAC, SSO, API Keys)
- Real-time updates via WebSocket

### Phase 5: Production (8 weeks)
- OpenTelemetry instrumentation for all services
- Prometheus + Grafana + Loki observability stack
- k6 + Locust load testing (10K endpoint target)
- OWASP ZAP + gitleaks security testing
- chaos-mesh resilience testing
- MkDocs Material documentation site
- CI/CD hardening (12 GitHub Actions workflows)

### Phase 6: Commercial (8 weeks)
- BSL 1.1 license file and contributor agreement
- Feature gating with Ed25519 license validation
- Multi-tenancy (tenant model, data isolation)
- Managed A2A relay service
- Enterprise reporting (templates, scheduled delivery)
- Stripe Billing integration

---

## 6. Parallel Work Streams

| Stream | Focus | Languages | Sync Cadence |
|--------|-------|-----------|-------------|
| A — Backend | API, data models, NATS, business logic | Go, Python | Daily standup |
| B — Agent | Endpoint binary, OS integrations, script execution | Go | Weekly arch review |
| C — Frontend | UI, design system, real-time updates | TypeScript | Bi-weekly sprint planning/retro |
| D — Infrastructure | CI/CD, monitoring, docs, deployments | K8s, Terraform | Monthly roadmap review |

---

## 7. Release Strategy

| Release | Tag | Phase | Audience | Channels |
|---------|-----|-------|----------|----------|
| Alpha 0.1 | `v0.1.0-alpha` | End Phase 1 | Design partners | alpha |
| Alpha 0.2/0.3 | `v0.2/0.3-alpha` | Phase 2 | Design partners | alpha |
| Beta 0.4 | `v0.4.0-beta` | End Phase 3 | Public beta | beta |
| Beta 0.5 | `v0.5.0-beta` | End Phase 4 | Public beta | beta |
| **GA 1.0** | `v1.0.0` | End Phase 5 | General availability | stable |
| Commercial 1.1 | `v1.1.0` | End Phase 6 | Paying customers | stable |

**Channels:** nightly (every main push), alpha, beta, stable, LTS (quarterly with 6-month support).

**SemVer policy:** Major for breaking API changes, minor for features, patch for fixes.

**Release process:** Feature freeze → 2 RCs → smoke tests → sign-off → tag → 72h war room monitoring.

---

## 8. Open Questions

| # | Question | Owner | Decision Date |
|---|----------|-------|---------------|
| O1 | Should agent binary use CGO for prlimit or pure-Go syscall? | agent-lead | Phase 1 Sprint 1 |
| O2 | PostgreSQL RLS vs schema-per-tenant for multi-tenancy? | backend-lead | Phase 6 Sprint 1 |
| O3 | Should A2A streaming use SSE or WebSocket from gateway to frontend? | a2a-lead | Phase 2 Sprint 1 |
| O4 | CDN vs self-hosted for frontend static assets? | devops-lead | Phase 4 Sprint 1 |
| O5 | Vault namespace support for enterprise multi-tenancy? | secrets-lead | Phase 3 Sprint 2 |
| O6 | Should agent binary auto-update or require explicit approval? | product | Phase 1 Sprint 5 |
| O7 | Commercial license enforcement: online-only or offline grace period? | product | Phase 6 Sprint 1 |
| O8 | Should MCP tools reference A2A skills directly or via indirection layer? | mcp-lead | Phase 2 Sprint 1 |
| O9 | k6 vs Locust vs Artillery for primary load testing tool? | test-lead | Phase 5 Sprint 1 |
| O10 | Should Helm chart use KEDA for NATS-prometheus-based scaling? | devops-lead | Phase 5 Sprint 3 |

---

## 9. Risk Register

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
