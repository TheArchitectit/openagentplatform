# Gap Analysis — OpenAgentPlatform (OAP) RMM/A2A Platform

**Analysis Date:** 2026-03-15
**Analyst:** Agent-GDUI-2026
**Scope:** Full codebase review — Go server, Go agent sidecar, Python adapter service, React web SPA, A2A gateway, docs
**Method:** Structural code inspection (routes, packages, models, collectors), keyword sweeps for target domains, cross-reference with existing sprint/STATUS docs

---

## 0. TL;DR — The Big Surprise

**`STATUS.md` is describing the wrong project.** It claims this repo is a "Guardrail MCP Server" with sprints for document ingestion and MCP gap tools. The **actual codebase** is a multitenant **Remote Monitoring & Management (RMM) platform** with an **A2A/LLM agent orchestrator** bolted on — built in Go + Python + React/TypeScript. The STATUS.md, sprint docs (001/002/004), and the `mcp-server/` subtree describe an older or fork-divergent project that does **not** reflect the code in `cmd/`, `internal/`, `pkg/`, `py/oap/`, `a2a/`, or `web/`.

**This is good news.** The real platform is far further along than the stale docs suggest. Below is what actually exists, what's missing, and a remediation plan grounded in the real code.

---

## 1. What Actually Exists (Inventory)

### 1.1 Go Management Plane (`cmd/server`, `internal/`)

A fully-wired multitenant RMM control plane:

| Subsystem | Packages | Maturity |
| --- | --- | --- |
| **HTTP API** | `internal/api/` (42 handler files, chi router) | High — routes registered, RBAC enforced |
| **Auth** | `internal/auth/` (OIDC/Dex, session minting, RBAC roles: Admin/Technician/...) | High |
| **Tenancy** | `internal/tenancy/` (isolation middleware, quotas per tier, retention purger) | High |
| **Licensing** | `internal/license/` (Ed25519-signed offline keys, feature gating, HTTP 402 middleware) | High |
| **Billing** | `internal/billing/` (Stripe subscriptions, metering service, hourly usage → Stripe meter events) | High — nil when `STRIPE_SECRET_KEY` unset |
| **Checks** | `internal/checks/` (definitions, assignments, result ingestor, dispatching via NATS) | High |
| **Alerts** | `internal/alerts/` (alert engine, state machine: ack/snooze/resolve/close, rules, channels, prefs) | High |
| **Patches** | `internal/patches/` (catalog, scheduler, per-agent patch state, stats) | High |
| **Scripts** | `internal/api/scripts.go` + `script_store.go` (run, list runs, results) | Medium |
| **Policy** | `internal/policy/` (OPA engine, violations, collectors: firewall/AV/encryption/screen-lock/USB/... ) | Medium-High |
| **Audit** | `internal/audit/` (hash-chained audit events, chain verification endpoint) | High |
| **Remote Control** | `internal/remote/` (SSH/WinRM-via-NATS, session mgmt, AES-encrypted credential store, recordings) | Medium-High |
| **Events** | `internal/events/` (NATS client, heartbeat handler, check dispatcher) | High |
| **Telemetry** | `internal/telemetry/` (OTel tracing, otelpgx, Prometheus metrics, `/metrics`) | High |
| **Resilience** | `internal/resilience/` (rate limiter, circuit breaker for adapters) | Medium |
| **Secrets** | `secrets/` (Vault, Infisical, OAuth, resolver/injector/sweeper, JWT revocation) | High |
| **A2A Gateway** | `a2a/` (registry, manager, router, bridge, RPC bridge) | High |

### 1.2 Go Agent Sidecar (`cmd/agent`, `pkg/agent/`)

Long-lived daemon (Windows/Linux/macOS) connecting to NATS with mTLS:

- **Heartbeats** (`pkg/agent/heartbeats.go`) → `oap.agents.<id>.heartbeat`
- **Check execution** (`pkg/agent/checks.go` + `checkers/`) → subscribes `oap.agents.<id>.checks`, publishes results
- **Patch execution** (`pkg/agent/patcher/`)
- **Script execution** (`pkg/agent/executor/`)
- **Remote shell** (`pkg/agent/shell/`) — SSH/WinRM subprocess bridged over NATS, base64 I/O, idle timeout, max concurrent sessions
- **Compliance collectors** (`pkg/agent/compliance.go`) — posture data fed to policy engine
- **CLI**: `-register`, `-version`, `-list-checkers`

### 1.3 Python Adapter Service (`py/oap/`)

A **separate FastAPI app** — a multi-framework LLM agent orchestrator:

- Adapters for: **OpenAI, Anthropic, CrewAI, LangGraph, AutoGen, Semantic Kernel, Ozore**
- Process pool (`pool.py`/`pool_worker.py`), cost manager, human-in-the-loop, orchestrator service
- This is the "A2A agent" backend that the Go `a2a/` gateway routes to.

### 1.4 Web SPA (`web/`)

TanStack Router + TypeScript SPA with real feature routes:

- Dashboard, Agents (list + detail + **shell**), Checks, Alerts, Patches, Scripts, Policies, Audit Log
- Settings: Users, Roles, SSO, API Keys, Audit Log
- A2A: agents, tasks, costs
- Shell Recordings (playback/export)
- Lib hooks: `useAgents`, `useChecks`, `useAlerts`, `usePatches`, `usePolicies`, `useScripts`, `useA2A`, `useSettings`
- a11y utilities, theme system, permission gating (`require-permission.tsx`)

### 1.5 Data Model (`pkg/models/`, alembic migrations)

Real RMM schema: **organizations → clients → sites → agents**, with `check_definitions`, `check_assignments`, `check_results`, alert state machines, policy engine + violations, patch catalog + jobs + targets, script runs, hash-chained audit log. Alembic migrations present.

### 1.6 Cyber-Defense Foothold (partial)

`internal/policy/collectors/` already has **read-only posture collectors**:
`antivirus.go`, `firewall.go`, `encryption.go`, `password_policy.go`, `screen_lock.go`, `usb_storage.go`, `remote_access.go`, `patching.go`, `browser_extensions.go`

These **detect** posture (is AV installed? is firewall on?) and feed the policy/violations engine. What's **missing** is active defense (see §2.5).

---

## 2. Gap Analysis by Domain

### 2.1 RMM Core — ✅ LARGELY COMPLETE

**Status:** Checks, patches, scripts, policies, alerts, remote shell, audit chain — all wired with API + storage + NATS dispatch.
**Gaps:**

- **G-RMM-001 (Med):** No scheduled-report / email-digest cadence endpoint (reports exist but unclear if they emit on schedule).
- **G-RMM-002 (Low):** No maintenance-window / silence-window concept to suppress alerts during planned work. — **CLOSED** (RMM-02, implemented and verified in-tree, uncommitted): org/client/site-scoped windows, IANA timezone recurring and overnight semantics, paid-tier gate, RBAC, audit, tenant isolation, fail-open delivery on store read errors, migration 0012.
- **G-RMM-003 (Med):** No agent auto-update mechanism (agent version is reported; no channel for pushing agent binary updates).
- **G-RMM-004 (Low):** No offline-agent SLA/alerting ("agent hasn't reported in 24h"). — **CLOSED** (RMM-01, shipped commit `6ff3a17`).

### 2.2 Multitenancy / Billing / Licensing — ✅ COMPLETE

**Status:** Org → Client → Site → Agent hierarchy, per-tier quotas, Stripe billing + metering, offline Ed25519 licenses, feature gating (HTTP 402), retention purger.
**Gaps:**

- **G-TEN-001 (Low):** Self-serve signup flow not evident (Stripe webhook → org provisioning may need an end-to-end test).
- **G-TEN-002 (Low):** No reseller/MSP-of-MSP tiering (likely out of scope).

### 2.3 A2A / LLM Orchestration — ✅ HIGH

**Status:** A2A gateway (registry/manager/router/bridge), Python adapter service with 7 frameworks, cost management, web UI for tasks/costs.
**Gaps:**

- **G-A2A-001 (Med):** Adapter process-pool failure modes / backpressure not documented.
- **G-A2A-002 (Med):** No per-adapter circuit-breaker visibility in web UI (breaker exists in `internal/resilience/` but not surfaced).
- **G-A2A-003 (Low):** No streaming-cancel UX confirmation in web.

### 2.4 Cloud Control — ❌ ABSENT  ← **BIGGEST GAP**

**Status:** **Zero cloud-provider integration code.** Keyword sweeps for `aws-sdk`, `azure-sdk`, `google.cloud`, `@google-cloud/compute`, Terraform/Pulumi found **only documentation mentions** (`docs/standards/INFRASTRUCTURE_STANDARDS.md`, `docs/advisors/COST_ADVISOR.md`) — no actual SDK imports, no cloud API clients, no IaC reconciliation.
**What's needed:**

- **G-CLD-001 (Critical):** AWS integration — EC2/RDS/S3/Lambda inventory, cost telemetry, IAM drift detection.
- **G-CLD-002 (Critical):** Azure integration — VM/AVM/Blob/Functions inventory, cost, RBAC drift.
- **G-CLD-003 (High):** GCP integration — Compute/GCS/Cloud Functions inventory, cost.
- **G-CLD-004 (High):** Unified cloud-resource model reconciled against site/agent inventory (agent reports "I'm an EC2 i-abc"; cloud API confirms the instance exists and tags match).
- **G-CLD-005 (Med):** IaC drift detection (Terraform state vs. live) as a check type.
- **G-CLD-006 (Med):** Cost-anomaly alerts as a first-class alert-rule condition.

### 2.5 Cyber Defense / Active Security — ⚠️ PARTIAL (detection only)

**Status:** 10 posture collectors detect AV/firewall/encryption presence → policy violations. No active IDS/EDR/SIEM/threat-intel.
**Gaps:**

- **G-SEC-001 (High):** No EDR telemetry ingest (CrowdStrike/SentinelOne/Defender-for-Endpoint APIs). Detection of "CrowdStrike installed" exists; ingestion of its alerts does **not**.
- **G-SEC-002 (High):** No IDS/Network detection (Suricata/Zeek/Wazuh/CrowdSec) as check or alert sources.
- **G-SEC-003 (High):** No threat-intel feed ingestion (IOC feeds, Sigma rule matching).
- **G-SEC-004 (Med):** No firewall-rule-change alerting (firewall *state* is collected; diff-over-time is not).
- **G-SEC-005 (Med):** No SIEM forwarding (OAP audit events → external Splunk/ELK via syslog/HTTPS).
- **G-SEC-006 (Low):** No vulnerability-scan integration (Nessus/OpenVAS/Trivy results as check_results).

### 2.6 EVE / Hypervisor Monitoring — ❌ ABSENT

**Status:** **Zero code.** No `libvirt`, `proxmox`, `vsphere`, `xenapi`, `kubevirt`, `govc`, `veeam` references anywhere in source. A keyword sweep returned empty.
**What's needed:**

- **G-EVE-001 (High):** Proxmox VE integration (nodes/VMs/CTs, cluster health, snapshot status, migration).
- **G-EVE-002 (High):** libvirt integration (VM list, CPU/mem/disk, lifecycle).
- **G-EVE-003 (Med):** ESXi/vSphere integration (via govmomi).
- **G-EVE-004 (Med):** Hypervisor as a first-class "agent type" — a hypervisor is a site+agent that also reports child-VM telemetry.
- **G-EVE-005 (Low):** Backup status (Veeam/Proxmox backup jobs) as check_results.

### 2.7 Endpoint Protection (AV) — ⚠️ POSTURE-ONLY

**Status:** `antivirus.go` collector detects Defender/ClamAV/Sophos/CrowdStrike presence; no active scan scheduling, no definition-update monitoring, no quarantine telemetry.
**Gaps:**

- **G-AV-001 (High):** AV definition staleness as a check (last signature date).
- **G-AV-002 (High):** On-demand scan trigger via the script/executor pipeline.
- **G-AV-003 (Med):** Quarantine/remediation outcome ingestion from EDR APIs.

### 2.8 Power / UPS Monitoring — ❌ ABSENT

**Status:** No UPS/power/battery monitoring. (Only match was unrelated game-design docs.)
**What's needed:**

- **G-PWR-001 (Med):** SNMP UPS monitoring (apcupsd/Network UPS Tools) as a check type.
- **G-PWR-002 (Med):** Battery health for laptops/edge devices via agent telemetry.
- **G-PWR-003 (Low):** Power-event → alert rule (on-battery, low-battery, on-line).

### 2.9 Documentation Parity — ❌ STALE/MISMATCHED  ← **PROCESS GAP**

- **G-DOC-001 (Critical):** `STATUS.md` describes a different project ("Guardrail MCP Server"). Misleads every new contributor.
- **G-DOC-002 (Critical):** Sprint docs 001/002/004 reference "document ingestion system", "26 API endpoints in api.js", "6 pages" — none match the real Go API (200+ routes) or the React SPA (30+ routes).
- **G-DOC-003 (High):** No architecture diagram tying Go server ↔ NATS ↔ agent ↔ Python adapter ↔ A2A gateway ↔ web.
- **G-DOC-004 (High):** `py/oap` is entirely undocumented in the docs tree.
- **G-DOC-005 (Med):** No per-subsystem README (e.g. `internal/policy/README.md` explaining collectors → OPA → violations flow).
- **G-DOC-006 (Med):** API.md needs to be regenerated from the real `internal/api/routes.go` (current API.md likely reflects the old project).

### 2.10 Test Coverage — ⚠️ UNEVEN

- **47 `*_test.go` files** across Go — good baseline.
- `internal/api/` has tests: `rbac_test.go`, `health_test.go`, `routes_test.go`, `websocket_test.go`, `remote_ws_auth_test.go`, `billing_wiring_test.go`.
- **Gaps:**
  - **G-TST-001 (High):** No evidence of cloud/EVE/power integration tests (because the code doesn't exist).
  - **G-TST-002 (Med):** No end-to-end test wiring Go server ↔ NATS ↔ agent ↔ Python adapter.
  - **G-TST-003 (Med):** `py/oap` test presence not verified (need `pytest --collect-only`).

---

## 3. Prioritized Remediation Plan

### P0 — Foundation & Truth (before any new features)

| ID | Gap | Action | Effort |
| ---- | ----- | -------- | -------- |
| P0-1 | G-DOC-001 | Rewrite `STATUS.md` to describe the real platform (Go server, agent, A2A, web). Archive old sprints to `docs/sprints/archive/`. | S |
| P0-2 | G-DOC-006 | Regenerate `docs/API.md` from `internal/api/routes.go` (openapi spec export or script). | M |
| P0-3 | G-DOC-003 | Author `docs/architecture/OAP_TOPOLOGY.md` — one diagram + one page per subsystem. | M |

### P1 — Cloud Control (biggest product gap)

| ID | Gap | Action | Effort |
| ---- | ----- | -------- | -------- |
| P1-1 | G-CLD-001 | AWS integration: new `internal/cloud/aws/` package, EC2/RDS inventory as `cloud_resources` table, reconciled against agents. | L |
| P1-2 | G-CLD-002 | Azure integration: `internal/cloud/azure/`. | L |
| P1-3 | G-CLD-004 | Unified `cloud_resource` model + reconciliation worker. | M |
| P1-4 | G-CLD-006 | Cost-anomaly alert condition. | S |

### P2 — Cyber Defense (extend existing foothold)

| ID | Gap | Action | Effort |
| ---- | ----- | -------- | -------- |
| P2-1 | G-SEC-001 | EDR telemetry ingest: Defender-for-Endpoint + Crowdstrike Falcon APIs → `security_events` table → alerts. | L |
| P2-2 | G-SEC-002 | IDS ingest: Wazuh/CrowdSec agent-forwarder as a check type. | M |
| P2-3 | G-SEC-004 | Firewall-rule-change diff collector (store last-seen rules, alert on delta). | S |
| P2-4 | G-AV-001 | AV definition-staleness check. | S |

### P3 — EVE / Hypervisor Monitoring

| ID | Gap | Action | Effort |
| ---- | ----- | -------- | -------- |
| P3-1 | G-EVE-001 | Proxmox VE client (`internal/eve/proxmox/`). | M |
| P3-2 | G-EVE-002 | libvirt client (`internal/eve/libvirt/`). | M |
| P3-3 | G-EVE-004 | Hypervisor as agent subtype (extends `pkg/models.Agent`). | S |

### P4 — Power & Polish

| ID | Gap | Action | Effort |
| ---- | ----- | -------- | -------- |
| P4-1 | G-PWR-001 | SNMP UPS check type (`pkg/agent/checkers/ups.go`). | S |
| P4-2 | G-RMM-001 | Scheduled report digests. | S |
| P4-3 | G-RMM-003 | Agent auto-update channel. | M |
| P4-4 | G-A2A-002 | Surface circuit-breaker state in web UI. | S |

**Effort key:** S = 1-3 days, M = ~1 week, L = 2+ weeks

---

## 4. Recommended Next Sprint

**Sprint 005 (proposed): "Truth & Topology"** — execute P0 entirely:

1. Rewrite `STATUS.md` against the real codebase.
2. Archive mismatched sprint docs.
3. Regenerate `API.md`.
4. Author the architecture/topology doc.

This unblocks every subsequent contributor and costs roughly 2-3 days. It should land **before** P1 cloud work so cloud design decisions are made against an accurate map.

---

## 5. Appendix — Evidence

### A. Real API route count (from `internal/api/routes.go`)

200+ routes registered across: `/auth`, `/api/v1/agents`, `/checks`, `/alerts`, `/alert-rules`, `/alert-preferences`, `/notification-channels`, `/policies`, `/violations`, `/audit`, `/billing`, `/scripts`, `/patches`, `/shell`, `/shell/recordings`, `/a2a`, `/diagnostics`.

### B. Real policy collectors (from `internal/policy/collectors/`)

`antivirus.go`, `browser_extensions.go`, `encryption.go`, `firewall.go`, `password_policy.go`, `patching.go`, `remote_access.go`, `screen_lock.go`, `usb_storage.go` — 9 read-only posture collectors feeding the OPA engine.

### C. Absent domains (zero source matches)

Cloud SDKs (AWS/Azure/GCP), hypervisor APIs (libvirt/Proxmox/vSphere), UPS/SNMP-power, IDS/EDR/SIEM, threat-intel feeds. All confirmed absent via `rg` sweeps. Cyber-defense detection exists only via the posture collectors in Appendix B.

### D. Repo structure summary

```
cmd/server/   Go management plane entrypoint
cmd/agent/     Go agent sidecar entrypoint
internal/      20+ Go packages (api, alerts, billing, checks, policy, remote, tenancy, ...)
pkg/agent/     agent-side: checks, shell, patcher, executor, checkers
pkg/models/    shared data model
a2a/           A2A gateway (registry, manager, router, bridge)
py/oap/        Python FastAPI LLM adapter orchestrator (7 frameworks)
web/           React/TS TanStack SPA
secrets/       Vault + Infisical + OAuth secret management
alembic/       DB migrations
mcp-server/    legacy/stale MCP server subtree (matches old STATUS.md)
```
