# OpenAgentPlatform -- Master Implementation Plan

**Version:** 1.0.0
**Status:** Authoritative Blueprint
**Date:** 2026-06-15
**Source Repository:** `/mnt/data/git/openagentplatform/`

---

## 1. Project Overview

### 1.1 Mission

OpenAgentPlatform (OAP) transforms passive AI-agent guardrails into an actively enforced governance and operations platform. It sits at the intersection of remote monitoring and management (RMM), agent-to-agent (A2A) orchestration, and secret lifecycle management -- providing a single control plane where human operators manage endpoints, delegate tasks to LLM-based agents, and enforce security policies across the entire stack.

### 1.2 Scope

| In Scope | Out of Scope |
|----------|-------------|
| Device agent (Go binary) for Windows/Linux/macOS | Mobile native apps (iOS/Android) |
| RMM core: checks, alerts, policies, patches, scripts, remote access | Network device monitoring (SNMP switches/routers) |
| A2A protocol gateway with 6 framework adapters | Training or fine-tuning LLM models |
| Secret management: Vault, Infisical, K8s CSI backends | Custom PKI CA deployment |
| React SPA dashboard (Vite + TanStack) | Desktop Electron application |
| Observability: OTel + Prometheus + Grafana + Loki | Splunk/ELK integration (export only) |
| Auth: JWT, mTLS/SPIFFE, OAuth 2.1, SAML, OIDC, SCIM, RBAC | Biometric hardware tokens |
| Multi-tenant commercial tiering (BSL 1.1) | On-prem appliance distribution |
| CI/CD: GitHub Actions, Docker, Helm, K8s | Terraform cloud modules |

### 1.3 Success Metrics

| Metric | Target | Measurement |
|--------|--------|-------------|
| Agent check-in latency (p99) | < 5 seconds | k6 load test, 10k agents |
| Alert delivery latency (p99) | < 60 seconds | Synthetic alert test |
| Check execution throughput | 100k checks/minute | Locust NATS benchmark |
| A2A task completion (p95) | < 30 seconds | A2A gateway smoke test |
| API availability (per tenant) | 99.9% | SLO burn-rate alert |
| Unit test coverage | >= 90% lines, >= 85% branches | CI coverage gate |
| Security findings per release | 0 high/critical | ZAP + gitleaks + trufflehog |
| Chaos recovery MTTR | < 60 seconds | chaos-mesh experiments |
| Secret injection latency (p99) | < 500 ms | Integration test |
| Commercial feature gate enforcement | 100% bypass-free | Adversarial test suite |

---

## 2. Architecture Overview

### 2.1 System Topology

```
                              ┌─────────────────────────────────────────────┐
                              │              OPERATOR BROWSER                │
                              │  React SPA (Vite, TanStack Router/Query)    │
                              │  WebSocket for real-time push                │
                              └──────────────────┬──────────────────────────┘
                                                 │ HTTPS / WSS
                                                 ▼
┌────────────────────────────────────────────────────────────────────────────────┐
│                           KUBERNETES CLUSTER                                   │
│                                                                                │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────────────┐ │
│  │  API Server      │  │  A2A Gateway     │  │  Secret Service             │ │
│  │  (Go :8080)      │  │  (Go :8082)      │  │  (Go :8200)                │ │
│  │  REST + gRPC     │  │  JSON-RPC/SSE    │  │  REST + gRPC               │ │
│  │  NATS publisher  │  │  Task router     │  │  Vault/Infisical backends  │ │
│  │  RMM event emit  │  │  AgentCard reg   │  │  Credential injection      │ │
│  └──────┬───────────┘  └──────┬───────────┘  └──────────┬───────────────────┘ │
│         │                     │                          │                     │
│         │  ┌──────────────────┴──────────────────┐      │                     │
│         │  │  Agent Framework Adapter (Py :8090)  │      │                     │
│         │  │  LangGraph | CrewAI | AutoGen |     │      │                     │
│         │  │  SemanticKernel | OpenAI | Anthropic │      │                     │
│         │  └─────────────────────────────────────┘      │                     │
│         │                                              │                     │
│  ┌──────┴──────────────────────────────────────────────┴─────────────────┐   │
│  │                        NATS JetStream Cluster (:4222)                  │   │
│  │  Streams: AGENTS, CHECKS, SCRIPTS, WINUPDATE, INVENTORY, A2A        │   │
│  │  Accounts: OAP (services), AGENTS (device binaries)                  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│         │                     │                          │                     │
│  ┌──────┴─────┐  ┌───────────┴──────────┐  ┌───────────┴──────────────┐     │
│  │ PostgreSQL │  │  Redis Cluster       │  │  OTel Collector         │     │
│  │ Primary +  │  │  Cache, rate-limit, │  │  :4317 (gRPC)           │     │
│  │ Replicas   │  │  session state       │  │  :4318 (HTTP)           │     │
│  └────────────┘  └─────────────────────┘  └──────────────────────────┘     │
│                                                                            │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────────────┐    │
│  │  Prometheus      │  │  Grafana         │  │  Loki + Promtail       │    │
│  │  + Alertmanager  │  │  (dashboards)    │  │  (log aggregation)     │    │
│  └──────────────────┘  └──────────────────┘  └───────────────────────┘    │
│                                                                            │
│  ┌──────────────────┐  ┌──────────────────────────────────────────────┐    │
│  │  MCP Server      │  │  Helm Chart: charts/oap/                     │    │
│  │  (Go :8094)      │  │  60+ templates, values.yaml                  │    │
│  │  Guardrail tools  │  │  CI: GitHub Actions (12 workflows)          │    │
│  └──────────────────┘  └──────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────────────────│
                    │
                    │ NATS leafnode / mTLS (SPIFFE)
                    ▼
         ┌────────────────────┐
         │  MANAGED DEVICES   │
         │  Agent Binary (Go) │
         │  Windows/Linux/macOS│
         │  Check executor    │
         │  Script runner     │
         │  Inventory reporter│
         └────────────────────┘
```

### 2.2 Service Inventory

| # | Service | Language | Port | Domain | Status |
|---|---------|----------|------|--------|--------|
| S1 | API Server | Go 1.23 | :8080 (HTTP/gRPC) | endpoint-api | Existing (mcp-server) |
| S2 | A2A Gateway | Go | :8082 | a2a | New |
| S3 | Agent Adapter | Python | :8090 | agent-framework | New |
| S4 | Secret Service | Go | :8200 | secret-management | New |
| S5 | MCP Guardrail Server | Go | :8094 | cross-cutting | Existing |
| S6 | Agent Binary | Go | outbound | endpoint-api | New |
| S7 | Frontend SPA | TypeScript | :3000 (dev) | frontend | Partial (vanilla JS exists) |
| S8 | NATS JetStream | - | :4222 | infrastructure | New |
| S9 | PostgreSQL | - | :5432 | infrastructure | Existing |
| S10 | Redis | - | :6379 | infrastructure | Existing |
| S11 | Prometheus | - | :9090 | observability | New |
| S12 | Grafana | - | :3001 | observability | New |
| S13 | Loki | - | :3100 | observability | New |
| S14 | OTel Collector | - | :4317/:4318 | observability | New |
| S15 | Vault | Go | :8200 | secret-management | New (external) |
| S16 | Tempo | Go | :3200 | observability | New |

### 2.3 Data Flow Summary

1. **Agent heartbeat loop**: Agent --(NATS msgpack, 30s)--> API Server --(PostgreSQL upsert)--> DB --(WebSocket push)--> Frontend
2. **Check execution**: API Server --(NATS command)--> Agent --(local exec)--> Agent --(NATS result)--> API Server --(rmm-core evaluation)--> alert if threshold exceeded --(NATS event)--> A2A bridge
3. **A2A task lifecycle**: RMM event --(NATS bridge)--> A2A Gateway --(JSON-RPC)--> Agent Adapter --(LLM invoke)--> LLM provider --(streaming response)--> A2A Gateway --(SSE)--> Frontend
4. **Secret injection**: API Server --(REST)--> Secret Service --(Vault Transit)--> Vault --(lease returned)--> API Server --(env injection)--> Agent
5. **Script execution**: Frontend --(REST)--> API Server --(NATS dispatch)--> Agent --(spawn process)--> Agent --(streaming output via NATS)--> API Server --(WebSocket)--> Frontend
6. **Patch management**: API Server --(scan command)--> Agent --(OS package query)--> Agent --(result)--> API Server --(policy evaluation)--> approval workflow --(install command)--> Agent

---

## 3. Domain Implementation Specifications

### 3.1 RMM Core

**App Path:** `/mnt/data/git/openagentplatform/backend/apps/rmm/`

The RMM core implements device registration, check execution, policy propagation, patch management, automated task scheduling, alert lifecycle, remote access relay, and NATS JetStream message orchestration as a Django app within the backend service.

#### 3.1.1 Data Models (10 models)

| Model | Table | Key Fields | Indexes | Constraints |
|-------|-------|------------|---------|-------------|
| `Agent` | `rmm_agent` | `agent_id` (unique), `hostname`, `client` FK, `site` FK, `platform`, `status`, `last_seen`, `inventory` (JSON), `tags` (ArrayField), `mesh_token` | `(org, status)`, `(org, client, site)`, `(org, platform)`, `(org, last_seen)`, `(tags)` GIN | `UNIQUE(org, agent_id)` |
| `Check` | `rmm_check` | `name`, `check_type`, `interval_seconds`, `config` (JSON), `fail_threshold`, `alert_severity`, `last_status` | `(org, check_type)`, `(org, is_template)`, `(org, last_status)` | `CHECK(interval_seconds >= 30)`, `CHECK(timeout_seconds <= 3600)` |
| `AgentCheck` | `rmm_agent_check` | `agent` FK, `check` FK, `is_enabled`, `next_run_at` | `(agent, is_enabled)`, `(check, is_enabled)`, `(next_run_at)` | `UNIQUE(agent, check)` |
| `CheckResult` | `rmm_check_result` | `agent_check` FK, `status`, `value` (JSON), `duration_ms`, `execution_start/end` | `(agent, check, -execution_start)`, `(org, status, -execution_start)` | `CHECK(duration_ms >= 0)` |
| `Policy` | `rmm_policy` | `name`, `enforcement_mode`, `priority`, `checks` (JSON), `automated_tasks` (JSON), `win_update_policy` (JSON), `alert_routing` (JSON) | `(org, priority)` | - |
| `PolicyScope` | `rmm_policy_scope` | `policy` FK, `scope_type`, `client` FK, `site` FK, `agent` FK | `(client)`, `(site)`, `(agent)` | `CHECK(XOR: exactly one of client/site/agent set per scope_type)` |
| `WinUpdate` | `rmm_win_update` | `agent` FK, `kb_id`, `severity`, `state`, `cve_ids` (JSON), `approved_by` FK | `(org, state)`, `(agent, state)`, `(org, severity)` | `UNIQUE(agent, kb_id)` |
| `AutomatedTask` | `rmm_automated_task` | `name`, `task_type`, `schedule_bitmask` (21-bit), `actions` (JSON array), `next_run_at` | `(org, is_template)`, `(org, next_run_at)` | `CHECK(bitmask in [0, 2^21))` |
| `Alert` | `rmm_alert` | `severity`, `state`, `agent` FK, `check` FK, `dedup_key`, `notification_channels` (JSON) | `(org, state, -fired_at)`, `(org, severity, -fired_at)`, `(dedup_key)` | - |
| `ScriptResult` | `rmm_script_result` | `agent` FK, `script` FK, `runtime`, `state`, `stdout`, `stderr`, `exit_code` | `(agent, -created_at)`, `(org, state)` | - |

#### 3.1.2 Enums (12)

`AgentStatus` (pending/online/offline/degraded/uninstalled), `AgentPlatform` (windows/linux/macos/unknown), `CheckType` (10 types: ping/cpu/memory/disk/service/script/event_log/process/wmi/custom), `CheckStatus` (passing/failing/warning/pending/paused), `PolicyEnforcementMode` (inherit/enforce/exclude), `WinUpdateState` (8 states: scanned through reboot_required), `AutomatedTaskActionType` (8 types), `AlertSeverity` (5 levels), `AlertState` (6 states: new through closed), `ScriptRuntime` (5 types), `RemoteSessionProtocol` (5 types), `RemoteSessionState` (7 states).

#### 3.1.3 NATS Subject Taxonomy

**Agent-to-Server (msgpack):**
- `rmm.agent.heartbeat` -- lightweight, every 60s
- `rmm.agent.checkin` -- full state, every 5-15 min
- `rmm.check.result.{agent_id}` -- check execution outcomes
- `rmm.script.result.{agent_id}` -- script execution outcomes
- `rmm.script.chunk.{agent_id}` -- streaming stdout/stderr
- `rmm.winupdate.scan.{agent_id}` -- patch scan results
- `rmm.winupdate.install.{agent_id}` -- patch install results
- `rmm.agent.inventory.{agent_id}` -- hardware/software inventory
- `rmm.remote.session.event.{agent_id}` -- remote session events

**Server-to-Agent (JSON, per-agent inbox):**
- `rmm.cmd.{agent_id}.script.run`
- `rmm.cmd.{agent_id}.script.cancel`
- `rmm.cmd.{agent_id}.check.run`
- `rmm.cmd.{agent_id}.winupdate.install`
- `rmm.cmd.{agent_id}.winupdate.scan`
- `rmm.cmd.{agent_id}.sync`
- `rmm.cmd.{agent_id}.agent.update`
- `rmm.cmd.{agent_id}.remote.open`
- `rmm.cmd.{agent_id}.remote.close`
- `rmm.cmd.{agent_id}.policy.push`
- `rmm.cmd.{agent_id}.inventory.refresh`

**Broadcast:**
- `rmm.broadcast.all` -- global broadcast
- `rmm.broadcast.org.{org_id}` -- tenant-scoped

#### 3.1.4 State Machines (5)

| SM | States | Key Transitions |
|----|--------|-----------------|
| `AlertStateMachine` | new, acknowledged, in_progress, resolved, snoozed, closed | acknowledge (new/snoozed->acknowledged), resolve (new/ack/in_progress->resolved), snooze (new/ack/in_progress->snoozed), close (any->closed), reopen (resolved/closed->new) |
| `WinUpdateStateMachine` | scanned, pending_approval, approved, rejected, installing, installed, failed, reboot_required | approve, reject, auto_approve, start_install, complete_install, fail_install, mark_reboot_required, retry |
| `AgentStateMachine` | pending, online, offline, degraded, uninstalled | check_in (pending/offline/degraded->online), mark_offline, mark_degraded, recover, uninstall |
| `ScriptResultStateMachine` | pending, running, success, error, timeout, cancelled | start (pending->running), complete (running->success), fail (running->error), timeout (running->timeout), cancel (pending/running->cancelled) |
| `RemoteSessionStateMachine` | requested, pending_agent, active, transferring, closed, failed, timeout | agent_ack (pending_agent->active), close, fail, timeout, transfer (active->transferring) |

#### 3.1.5 Services (10)

| Service | File | Responsibility |
|---------|------|----------------|
| `CheckEngine` | `services/check_engine.py` | Scheduling, dispatch, result processing; `get_due_checks()`, `dispatch_check()`, `process_result()`, alert evaluation |
| `PolicyEngine` | `services/policy_engine.py` | Hierarchical resolution (Client>Site>Agent), enforcement/exclusion, propagation via NATS |
| `AlertEngine` | `services/alert_engine.py` | Dedup key generation, state machine driving, notification channel dispatch, routing |
| `PatchEngine` | `services/patch_engine.py` | Scan orchestration, approval workflow, batch deployment, reboot coordination, CVE correlation |
| `ScriptEngine` | `services/script_engine.py` | Script library management, dispatch, streaming output relay, result storage |
| `InventoryCollector` | `services/inventory_collector.py` | Agent inventory ingest, software catalog, delta detection, change event emission |
| `CheckinHandler` | `services/checkin_handler.py` | Heartbeat processing, full check-in merges, online/offline transitions |
| `Propagation` | `services/propagation.py` | Policy-to-agent push, delta computation, NATS publish |
| `Enforcement` | `services/enforcement.py` | Policy scope evaluation, exclusion enforcement, conflict resolution |
| `RemoteAccess` | `services/remote_access.py` | Session establishment, relay coordination, recording, audit events |

#### 3.1.6 Celery Tasks (7 packages)

| Package | Tasks |
|---------|-------|
| `check_tasks` | `run_due_checks`, `evaluate_check_result`, `escalate_failing_check` |
| `patch_tasks` | `scan_agent_patches`, `approve_patch_batch`, `deploy_patches`, `verify_patch_install` |
| `alert_tasks` | `process_alert`, `send_alert_notifications`, `expire_snoozed_alerts`, `escalate_unresolved` |
| `policy_tasks` | `propagate_policy_changes`, `enforce_policy_exclusions`, `sync_agent_policies` |
| `inventory_tasks` | `collect_agent_inventory`, `detect_software_changes`, `purge_stale_inventory` |
| `script_tasks` | `dispatch_script_run`, `process_script_output`, `handle_script_timeout` |
| `celery` (registration) | `register_periodic_tasks`, 15+ beat schedule entries |

#### 3.1.7 Implementation Steps (28 steps, each verifiable)

1. Create Django app `apps/rmm/` with `apps.py`, `__init__.py`, `enums.py`, `constants.py`
2. Implement `models/base.py`: `UUIDPrimaryKeyMixin`, `TimestampedMixin`, `OrgScopedMixin`, `SoftDeleteMixin`
3. Implement `models/agent.py`: full Agent model with all fields, indexes, constraints; write `test_models_agent.py`
4. Implement `models/check.py`: Check, AgentCheck, CheckResult; write `test_models_check.py`
5. Implement `models/policy.py`: Policy, PolicyScope, PolicyExclusion; write `test_models_policy.py`
6. Implement `models/win_update.py`: WinUpdate, WinUpdateBatch; write `test_models_win_update.py`
7. Implement `models/automated_task.py`: AutomatedTask, AutomatedTaskAssignment, Script; write `test_models_automated_task.py`
8. Implement `models/installed_software.py`, `models/alert.py`, `models/script_result.py`, `models/remote_session.py`
9. Create migrations 0001-0007 and run `python manage.py migrate`
10. Implement state machines in `state_machines/`: alert, win_update, agent, script_result, remote_session
11. Write `test_state_machines.py` covering all transitions and invalid transition rejections
12. Implement `nats/subjects.py` and `nats/client.py` (async singleton, stream creation)
13. Implement `nats/publishers.py` (CommandPublisher with all send_* methods)
14. Implement `nats/serializers.py` (msgpack for agent traffic, orjson for server traffic)
15. Implement `nats/consumers.py`: 8 consumer coroutines registered in `apps.ready()`
16. Write `test_nats_publishers.py` and `test_nats_consumers.py` with mock NATS
17. Implement `services/checkin_handler.py`: heartbeat and full check-in processing
18. Implement `services/check_engine.py`: scheduling, dispatch, result processing
19. Implement `services/policy_engine.py`: hierarchical resolution, propagation
20. Implement `services/alert_engine.py`: dedup, state machine driving, notification
21. Implement `services/patch_engine.py`: scan, approval, deployment, CVE correlation
22. Implement `services/script_engine.py`: library, dispatch, streaming, results
23. Implement `services/inventory_collector.py`, `services/propagation.py`, `services/enforcement.py`, `services/remote_access.py`
24. Write service tests: `test_services_check_engine.py`, `test_services_policy_engine.py`, etc.
25. Implement Celery tasks in `tasks/`: all 7 packages with registration
26. Write task tests: `test_tasks_check.py`, `test_tasks_patch.py`, `test_tasks_alert.py`
27. Implement API serializers, viewsets, URLs, permissions, pagination, filters
28. Write API tests: `test_api_agent.py` through `test_api_automated_task.py`

---

### 3.2 A2A Protocol

**App Path:** `/mnt/data/git/openagentplatform/a2a/` (Go module)

The A2A gateway implements the Agent-to-Agent protocol specification, providing task lifecycle management, agent discovery via AgentCards, skill-based routing, JSON-RPC 2.0 and REST bindings, streaming via SSE, human-in-the-loop approval, and cost tracking.

#### 3.2.1 Gateway Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     A2A Gateway                         │
│                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │ JSON-RPC 2.0 │  │ REST + JSON  │  │ gRPC stream  │  │
│  │ 7 methods    │  │ 12 endpoints │  │ server-stream │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  │
│         └─────────────────┼─────────────────┘          │
│                           │                             │
│  ┌────────────────────────┴────────────────────────┐   │
│  │            TaskManager (pgx-backed)             │   │
│  │  Tasks, Artifacts, Messages, CostRecords        │   │
│  │  Optimistic concurrency (version column)        │   │
│  └────────────────────────┬───────────────────────┘   │
│                           │                             │
│  ┌───────────────┐  ┌────┴────────┐  ┌──────────────┐ │
│  │ AgentCardReg  │  │ GatewayRtr  │  │ SubscriberHub│ │
│  │ In-memory +   │  │ Skill-match │  │ In-process   │ │
│  │ PostgreSQL    │  │ scoring     │  │ backpressure  │ │
│  └───────────────┘  └─────────────┘  └──────────────┘ │
│                                                         │
│  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │ Auth: Bearer, │  │ PushNotify  │  │ EventBridge  │ │
│  │ mTLS, OAuth2  │  │ HMAC-SHA256  │  │ RMM -> A2A   │ │
│  └───────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
```

#### 3.2.2 Task Lifecycle State Machine

14 valid transitions, 5 terminal states:

| Current State | Trigger | Target State | Condition |
|---------------|---------|-------------|-----------|
| WORKING | complete_task | COMPLETED | All artifacts finalized |
| WORKING | require_input | INPUT_REQUIRED | LLM requests human approval |
| WORKING | fail_task | FAILED | Unrecoverable error |
| WORKING | cancel_task | CANCELED | User cancels |
| INPUT_REQUIRED | resume_task | WORKING | Human provides input |
| INPUT_REQUIRED | cancel_task | CANCELED | Human rejects |
| INPUT_REQUIRED | timeout | CANCELED | 24h timeout |
| COMPLETED | (terminal) | - | Final state |
| FAILED | retry_task | WORKING | Retriable error |
| FAILED | (terminal) | - | Final state |
| CANCELED | (terminal) | - | Final state |

#### 3.2.3 JSON-RPC 2.0 Methods (7)

| Method | Direction | Params | Returns |
|--------|-----------|--------|---------|
| `a2a/sendTask` | Client -> Gateway | `TaskSendParams` (id, message, configuration) | `Task` |
| `a2a/sendTaskStreaming` | Client -> Gateway | `TaskSendParams` | SSE stream of `TaskDeltaEvent` |
| `a2a/getTask` | Client -> Gateway | `id` | `Task` |
| `a2a/cancelTask` | Client -> Gateway | `id` | `Task` (CANCELED) |
| `a2a/getArtifact` | Client -> Gateway | `taskId`, `artifactId` | `Artifact` |
| `a2a/listArtifacts` | Client -> Gateway | `taskId` | `Artifact[]` |
| `a2a/getAgentCard` | Client -> Gateway | `agentId` | `AgentCard` |

#### 3.2.4 REST Endpoints (12)

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| POST | `/a2a/v1/tasks` | Create task | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}` | Get task | Bearer/mTLS |
| PATCH | `/a2a/v1/tasks/{id}` | Update task | Bearer/mTLS |
| DELETE | `/a2a/v1/tasks/{id}` | Cancel task | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}/artifacts` | List artifacts | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}/artifacts/{aid}` | Get artifact | Bearer/mTLS |
| GET | `/a2a/v1/agents/{id}/card` | Get AgentCard | Public |
| POST | `/a2a/v1/agents/{id}/tasks` | Send task to agent | Bearer/mTLS |
| GET | `/a2a/v1/agents` | List registered agents | Bearer |
| POST | `/a2a/v1/approvals/{id}/approve` | Approve HITL request | Bearer |
| POST | `/a2a/v1/approvals/{id}/reject` | Reject HITL request | Bearer |
| GET | `/a2a/v1/subscriptions/{id}` | SSE stream | Bearer |

#### 3.2.5 AgentCard Registry

```go
type AgentCard struct {
    ID            string
    Name          string
    Description   string
    Version       string
    Framework     string   // langgraph|crewai|autogen|semantickernel|openai|anthropic
    Endpoint      string   // http://adapter-{framework}:8090/a2a
    Capabilities  []string // streaming, pushNotifications, stateTransitionHistory
    Tags          []string // alert.triage, patch.planning, security-scanning
    Skills        []AgentSkill
    Authentication AgentAuth
}
```

Skill-match scoring: `1.0 + (matching_tags * 0.1) - (current_load * 0.05)`; highest score wins; tie-break by `AgentCard.ID`.

#### 3.2.6 Database Schema (6 tables)

| Table | Columns | Key Indexes |
|-------|---------|------------|
| `a2a_tasks` | id, context_id, agent_id, state (enum), message (jsonb), metadata (jsonb), version (int4), created_at, updated_at | `(agent_id, state)`, `(context_id)` |
| `a2a_artifacts` | id, task_id (FK), name, description, parts (jsonb), mime_type, created_at | `(task_id)` |
| `a2a_messages` | id, task_id (FK), role, parts (jsonb), created_at | `(task_id, created_at)` |
| `a2a_cost_records` | id, task_id (FK), model, input_tokens, output_tokens, cost_usd, created_at | `(task_id)`, `(created_at)` |
| `a2a_approval_requests` | id, task_id (FK), state, requested_by, responded_by, response, expires_at | `(state, expires_at)` |
| `a2a_agent_cards` | id, name, framework, endpoint, capabilities (jsonb), tags (jsonb), skills (jsonb), auth_config (jsonb) | GIN on tags, GIN on capabilities |

#### 3.2.7 Protobuf Definition

```protobuf
// a2a/proto/a2a.proto
syntax = "proto3";
package a2a.v1;

service A2AService {
  rpc SendTask(SendTaskRequest) returns (Task);
  rpc SendTaskStreaming(SendTaskRequest) returns (stream TaskDeltaEvent);
  rpc GetTask(GetTaskRequest) returns (Task);
  rpc CancelTask(CancelTaskRequest) returns (Task);
  rpc GetArtifact(GetArtifactRequest) returns (Artifact);
  rpc ListArtifacts(ListArtifactsRequest) returns (ArtifactList);
  rpc SubscribeTask(SubscribeTaskRequest) returns (stream TaskEvent);
  rpc GetAgentCard(GetAgentCardRequest) returns (AgentCard);
  rpc ReportCost(ReportCostRequest) returns (CostReport);
}
// 15 message types, 9 RPCs (fully defined in a2a.proto)
```

#### 3.2.8 Implementation Steps (28 steps)

1. Initialize Go module `a2a/` with `go.mod`, directory structure
2. Write `a2a.proto` with all message types and service definition
3. Generate Go code with `protoc --go_out=. --go-grpc_out=.`
4. Implement data models in `internal/models/` (6 SQLModel classes + pgx repo)
5. Write SQL migrations 001-006 for all A2A tables
6. Implement `internal/states/task_state.go` (TaskStateMachine with 14 transitions)
7. Implement `internal/registry/agent_card.go` (in-memory + PostgreSQL Snapshot())
8. Implement `internal/router/gateway.go` (skill-match scoring, load penalty)
9. Implement `internal/protocol/jsonrpc.go` (7 methods, request/response dispatch)
10. Implement `internal/protocol/rest.go` (12 endpoints, Chi router)
11. Implement `internal/protocol/grpc.go` (9 RPCs, server-streaming)
12. Implement `internal/sse/subscriber_hub.go` (in-process, backpressure, 15s heartbeats)
13. Implement `internal/auth/bearer.go`, `internal/auth/mtls.go`, `internal/auth/oauth2.go`
14. Implement `internal/push/notify.go` (HMAC-SHA256 signing, exponential backoff, 4 workers)
15. Implement `internal/cost/tracker.go` (TokenCounter per model, cost aggregation)
16. Implement `internal/hitl/service.go` (ApprovalStateMachine, NotificationDispatcher)
17. Implement `internal/bridge/event.go` (8 RMM event types -> A2A skill tags)
18. Write `internal/errors/a2aerr.go` (structured errors, code mapping)
19. Write unit tests for all packages
20. Write integration tests: JSON-RPC roundtrip, REST CRUD, gRPC streaming
21. Write A2A conformance tests against spec vectors
22. Implement graceful shutdown: drain connections, persist in-flight tasks
23. Build Docker image, add to docker-compose
24. Deploy to K8s with HPA (target 70% CPU)
25. Write k6 load test (10k msg/s, p99 < 250ms)
26. Security review: test auth bypass, token revocation, mTLS cert validation
27. Write runbook: A2A Task stuck in WORKING state
28. Documentation: A2A protocol reference, adapter contract, cost model

---

### 3.3 Agent Framework Adapters

**App Path:** `/mnt/data/git/openagentplatform/adapters/` (Python package)

Six adapters wrap popular LLM agent frameworks, exposing a uniform `AgentWrapper` ABC that translates between A2A `SendMessage` calls and framework-native invocation patterns. A shared `ProcessPool` manages subprocess lifecycle.

#### 3.3.1 AgentWrapper ABC

```python
class AgentWrapper(ABC):
    @abstractmethod
    async def invoke(self, message: A2AMessage, config: dict) -> A2ATaskResult: ...

    @abstractmethod
    async def stream(self, message: A2AMessage, config: dict) -> AsyncIterator[A2ADeltaEvent]: ...

    @abstractmethod
    async def cancel(self, task_id: str) -> None: ...

    @abstractmethod
    def card(self) -> AgentCard: ...

    @abstractmethod
    def skills(self) -> list[AgentSkill]: ...
```

#### 3.3.2 Adapter Summary (6 adapters)

| Adapter | Framework | Key Translation | File |
|---------|-----------|-----------------|------|
| `LangGraphAdapter` | LangGraph | A2A message -> state dict; `astream()` -> A2A deltas; artifact extraction from state | `adapters/langgraph/` |
| `CrewAIAdapter` | CrewAI | A2A message -> crew inputs; `crew.kickoff_async()` -> A2A task result; native `crewai.a2a` peer registration | `adapters/crewai/` |
| `AutoGenAdapter` | AutoGen | A2A message -> group chat message; `GroupChat` -> `SendMessage`; termination condition as StateMachine | `adapters/autogen/` |
| `SemanticKernelAdapter` | Semantic Kernel | `HandoffOrchestration` -> A2A `SendMessage`; MAF A2A protocol support | `adapters/semantic_kernel/` |
| `OpenAIAdapter` | OpenAI Agents SDK | Handoff -> `SendMessage`; tools -> MCP tool bridge; streaming -> A2A artifacts | `adapters/openai/` |
| `AnthropicAdapter` | Claude | Tool-use as A2A skills; streaming -> A2A artifacts; message translation | `adapters/anthropic/` |

#### 3.3.3 ProcessPool

Warm pool of pre-initialized agent subprocesses. LRU eviction. Health monitoring (heartbeat ping). Graceful spawn/drain/kill.

```python
class ProcessPool:
    def __init__(self, max_size: int = 10, idle_timeout: int = 300): ...
    async def acquire(self, adapter_type: str, config: dict) -> SubprocessHandle: ...
    async def release(self, handle: SubprocessHandle): ...
    async def health_check(self) -> dict[str, bool]: ...
    async def drain(self, timeout: int = 30): ...
    async def kill(self, handle_id: str): ...
```

#### 3.3.4 OrchestrationService

Orchestrates multi-agent tasks: routing, cancellation, cost tracking.

```python
class OrchestrationService:
    async def dispatch(self, task: A2ATask, target_card: AgentCard) -> A2ATaskResult: ...
    async def cancel(self, task_id: str): ...
    async def get_cost(self, task_id: str) -> CostRecord: ...
```

#### 3.3.5 Implementation Steps (16 tasks, ~140 atomic steps)

| Task | Component | Files |
|------|-----------|-------|
| 1 | Project skeleton, config, conftest | 5 |
| 2 | Database engine + 6 SQLModel classes | 11 |
| 3 | Adapter exception taxonomy (10 classes) + TaskStateMachine | 5 |
| 4 | AgentWrapper ABC + FrameworkAdapterRegistry | 4 |
| 5 | LangGraph adapter | 5 |
| 6 | CrewAI adapter | 4 |
| 7 | AutoGen adapter | 4 |
| 8 | Semantic Kernel adapter | 4 |
| 9 | OpenAI Agents SDK adapter | 4 |
| 10 | Anthropic Claude adapter | 4 |
| 11 | ProcessPool (warm pool, LRU, health) | 8 |
| 12 | OrchestrationService, AgentRouter, CancellationRegistry, CostManager | 9 |
| 13 | TokenCounter + full cost test coverage | 4 |
| 14 | HITLService, ApprovalStateMachine, NotificationDispatcher | 8 |
| 15 | A2A JSON-RPC router, SSE streaming, FastAPI factory | 11 |
| 16 | Documentation + self-review | 4 |

---

### 3.4 Secret Management

**App Path:** `/mnt/data/git/openagentplatform/secrets/` (Go module)

Vault/Infisical-backed secret CRUD, reference resolution, credential injection, A2A token management, MCP OAuth 2.1, and script credential safety.

#### 3.4.1 Backend Abstraction

```go
type SecretBackend interface {
    Get(ctx context.Context, path string, version *int) (*SecretValue, error)
    Set(ctx context.Context, path string, data MapStr, opts SetOptions) (*SecretVersion, error)
    Delete(ctx context.Context, path string, opts DeleteOptions) error
    List(ctx context.Context, prefix string, opts ListOptions) ([]string, error)
    Metadata(ctx context.Context, path string) (*SecretMetadata, error)
    Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error)
    Healthcheck(ctx context.Context) bool
    Close(ctx context.Context) error
    SupportsDynamic() bool
}
```

5 implementations: `VaultBackend`, `InfisicalBackend`, `K8sCSIBackend`, `EnvBackend`, `MemoryBackend`.

#### 3.4.2 VaultBackend

- Auth: AppRole, Kubernetes, JWT/OIDC, Token
- KV v2 read/write with versioning and `check_version` optimistic concurrency
- Dynamic secrets: `read_dynamic()`, `revoke_dynamic()`, `renew_lease()`
- Token renewal at `token_ttl * 0.7` in background goroutine
- Audit log query via Vault audit device

#### 3.4.3 InfisicalBackend

- Auth: Universal Auth, Kubernetes
- Path mapping: OAP path -> Infisical folder/key
- Configurable folder prefix via `OAP_INFISICAL_FOLDER_PREFIX`
- Auto token-refresh on 401

#### 3.4.4 Secret Reference Model

URI format: `ref:oap://<backend_type>/<workspace_id>/<path>?version=<v>&key=<k>`

Resolution: group by backend_type, concurrent `asyncio.gather` with `Semaphore(max_concurrency)`, audit every resolution.

#### 3.4.5 Credential Injection Pipeline

3 injection methods:
- `env`: write to agent's env namespace with prefix
- `file`: write to path with mode 0600, owned by agent process UID
- `stdin`: pipe to agent stdin via Unix socket

TTL sweeper runs every 10s; revocation securely deletes files (overwrite + unlink), revokes dynamic leases.

#### 3.4.6 A2A Auth Token Management

- Issue: EdDSA (Ed25519) signed JWT with claims (iss, sub, aud, jti, scopes, delegation_chain)
- Exchange: verify presented token, down-scope, extend delegation chain (max depth 3)
- Verify: signature check, exp/nbf, scope matching (with wildcards), revocation list check
- Revoke: add jti to revocation list with TTL, audit event

#### 3.4.7 API Endpoints (9 route groups, ~25 endpoints)

| Route Group | Endpoints |
|-------------|-----------|
| `routes_secrets` | GET/PUT/DELETE per backend, GET metadata |
| `routes_references` | POST /resolve, POST /validate, POST /batch-resolve |
| `routes_rotation` | POST /{path}/rotate, GET /policies |
| `routes_injection` | POST /inject, POST /{id}/revoke, GET /active |
| `routes_a2a_tokens` | POST /issue, POST /exchange, POST /verify, POST /{id}/revoke, GET / |
| `routes_mcp` | POST /oauth2/authorize, POST /oauth2/token, GET /.well-known/*
| `routes_audit` | GET /audit, GET /audit/{id} |
| `routes_hierarchy` | GET /tree, POST /grant, DELETE /revoke |
| `routes_migration` | POST /migrate, GET /status |

#### 3.4.8 Implementation Steps (10 steps per backend + integration)

1. Backend ABC + exceptions + data classes + tests
2. VaultBackend: init, auth flows, get/set/delete/list/metadata
3. VaultBackend: rotate, dynamic secrets, lease management, audit query
4. InfisicalBackend: full CRUD with Universal Auth and K8s auth
5. K8sCSIBackend, EnvBackend, MemoryBackend
6. Secret Reference model: URI format, resolution, caching
7. Credential Injection Pipeline: env, file, stdin methods + TTL sweeper
8. A2A Token Manager: issue, exchange, verify, revoke
9. MCP OAuth 2.1: DPoP, dynamic client registration
10. API routes, integration tests, E2E with Vault

---

### 3.5 Endpoint API

**App Path:** `/mnt/data/git/openagentplatform/mcp-server/` (existing Go codebase, expanded)

The core REST + gRPC API server, NATS bus, and agent binary. Extends the existing MCP guardrail server with agent management, check execution, script dispatch, event streaming, and patch management endpoints.

#### 3.5.1 REST API Endpoints (22)

| Group | Method | Path | Description |
|-------|--------|------|-------------|
| Agents | POST | `/api/v1/agents` | Register new agent |
| Agents | GET | `/api/v1/agents` | List agents (paginated, filtered) |
| Agents | GET | `/api/v1/agents/{id}` | Get agent detail |
| Agents | DELETE | `/api/v1/agents/{id}` | Deregister agent |
| Agents | POST | `/api/v1/agents/{id}/heartbeat` | Agent heartbeat |
| Agents | POST | `/api/v1/agents/{id}/commands` | Send command to agent |
| Checks | GET | `/api/v1/checks` | List check definitions |
| Checks | POST | `/api/v1/checks` | Create check |
| Checks | POST | `/api/v1/checks/{id}/run` | Execute check on assigned agents |
| Checks | GET | `/api/v1/checks/{id}/results` | Get check result history |
| Scripts | GET | `/api/v1/scripts` | List script library |
| Scripts | POST | `/api/v1/scripts` | Upload script |
| Scripts | POST | `/api/v1/agents/{id}/scripts` | Execute script on agent |
| Scripts | GET | `/api/v1/scripts/runs/{id}` | Get script run status + output |
| Events | GET | `/api/v1/events` | Query event stream |
| Events | GET | `/api/v1/events/{id}` | Get single event |
| System | GET | `/api/v1/system/health` | Health check |
| System | GET | `/api/v1/system/version` | Version info |
| Patches | POST | `/api/v1/patches/scan` | Scan target agents |
| Patches | POST | `/api/v1/patches/approve` | Approve patches |
| Patches | POST | `/api/v1/patches/deploy` | Deploy patches |
| Auth | POST | `/api/v1/auth/login` | Login |
| Auth | POST | `/api/v1/auth/refresh` | Refresh token |
| Auth | POST | `/api/v1/auth/logout` | Logout |

#### 3.5.2 NATS Bus

4 JetStream streams:
- `AGENTS`: `oap.endpoint.>` (heartbeat, registration, events)
- `CHECKS`: `oap.check.>` (dispatch, result)
- `SCRIPTS`: `oap.script.>` (dispatch, output, result)
- `WINUPDATE`: `oap.winupdate.>` (scan, install)

5 consumer groups: `api-cmd-dispatcher`, `check-result-ingester`, `script-output-relay`, `winupdate-processor`, `event-persister`.

Msgpack codec for agent traffic; orjson for server traffic. Tagged fields in msgpack for schema evolution.

#### 3.5.3 Agent Binary (Go)

Cross-compiled for Windows (amd64/arm64), Linux (amd64/arm64), macOS (amd64/arm64). Lifecycle: NEW -> REGISTERING -> REGISTERED -> STALE -> OFFLINE -> DEREGISTERED. Per-agent NATS subscription. UUID persisted to disk for stable identity across restarts. Exponential backoff reconnect (1s, 2s, 4s, ..., 30s cap). Script executor with 4 runtimes (Python3, Bash, PowerShell, Node) with `prlimit` resource constraints.

#### 3.5.4 gRPC Service

30+ message types in protobuf. Reflection + health service. Auth/logging/recovery interceptors.

#### 3.5.5 Implementation Steps (22 steps)

1. Add agent management endpoints to existing API server (register, list, heartbeat, deregister)
2. Implement agent state machine (NEW -> REGISTERING -> REGISTERED -> STALE -> OFFLINE)
3. Create NATS JetStream provisioning logic (4 streams, 5 consumer groups)
4. Implement check endpoints (CRUD, run)
5. Implement script endpoints (CRUD, dispatch)
6. Implement event endpoints (query, detail)
7. Implement script execution in agent binary (4 runtimes, prlimit)
8. Implement streaming script output via NATS chunks -> WebSocket bridge
9. Implement agent binary registration and heartbeat flows
10. Implement check execution dispatch and result ingest
11. Implement patch scan/approve/deploy endpoints
12. Write gRPC proto definition and generate code
13. Implement gRPC service with interceptors
14. Implement rate limiting (token bucket per JWT subject)
15. Implement API versioning (URL-path, deprecation headers)
16. Implement OpenAPI 3.1 spec generation
17. Generate SDK clients (Python, Go, TypeScript)
18. Write integration tests for all endpoints
19. Write k6 load tests
20. Build agent binary for all platforms
21. Deploy agent binary as Docker image + native installers
22. Security review and hardening

---

### 3.6 Frontend

**App Path:** `/mnt/data/git/openagentplatform/web/` (existing, to be rewritten)

Full React 19 SPA replacing the existing vanilla JS UI. Vite + TanStack Router + TanStack Query + Shadcn UI + Tailwind CSS.

#### 3.6.1 Pages (20 pages in 10 feature modules)

| Module | Pages | Key Components |
|--------|-------|----------------|
| Auth (Task 10) | Login, SSO redirect, Password reset | LoginForm, SSOButton, PasswordResetForm |
| Dashboard (Task 11) | Dashboard | AgentStatusGrid, AlertFeed, CheckHealthGauge, PatchComplianceBar, LiveMetricLine |
| Agent Management (Task 12) | AgentList, AgentDetail (6 tabs) | AgentTable, AgentDetailTabs, AgentActionsDropdown, TagEditor |
| Monitoring (Task 13) | Checks, CheckDetail, Alerts, AlertDetail | CheckTimeSeries, AlertTable, AcknowledgeButton, SilenceModal, ResolveButton |
| Patch Management (Task 14) | Compliance, Patches, Policies, Deployments | ComplianceScorecard, PatchTable, PolicyEditor, DeploymentProgressBar |
| Remote Access (Task 15) | Terminal, Desktop, Sessions | XtermTerminal, NoVNCViewer, SessionList, RecordingPlayer |
| Script Editor (Task 16) | ScriptEditor, ScriptRun | MonacoEditor, RunForm, LiveOutputConsole |
| A2A Dashboard (Task 17) | AgentCards, TaskLifecycle, Messages, Artifacts | CardViewer, TaskTimeline, MessageStream, ArtifactPreview |
| Policies (Task 18) | PolicyEditor | PolicyRulesEditor, ValidationPanel, ImportDiffViewer |
| Secret Management (Task 19) | SecretList, SecretDetail, AccessLog | SecretTable, RevealModal, RotateButton, AccessTimeline |
| Settings (Task 20) | Users, Roles, SSO, Notifications, Org, APIKeys | UserTable, RoleEditor, SSOConfigForm, NotificationPreferences, OrgSettings, APIKeyManager |

#### 3.6.2 Shared Infrastructure (Tasks 1-9)

| Task | Deliverable |
|------|------------|
| 1 | Vite + React 19 + TypeScript scaffolding, `tsconfig.json`, `vite.config.ts` |
| 2 | Tailwind CSS + Shadcn UI config, `tailwind.config.ts`, `components.json` |
| 3 | Environment config + Zod schemas, `src/config/env.ts`, `src/schemas/` |
| 4 | API client (fetch wrapper, interceptors, retry), `src/lib/api-client.ts` |
| 5 | WebSocket client with reconnect, heartbeat, message type routing, `src/lib/ws-client.ts` |
| 6 | Auth layer: JWT decode, refresh, RBAC context, permission hooks |
| 7 | TanStack Router + app shell (sidebar, header, breadcrumbs) |
| 8 | Shadcn UI primitives (30+ components) |
| 9 | Shared visualization components (line chart, gauge, sparkline, status badge) |

#### 3.6.3 State Management

- Server state: TanStack Query v5 with stale-while-revalidate, WebSocket invalidation
- Client state: Zustand for UI-only state (sidebar open, modals, form drafts)
- Form state: React Hook Form + Zod validation
- Real-time: WebSocket subscriptions auto-refetch TanStack Query caches

#### 3.6.4 Implementation Steps (20 tasks)

Tasks 1-9 build infrastructure; Tasks 10-20 build feature modules. Each task is one PR with verifiable increment.

---

### 3.7 Infrastructure

**App Path:** `/mnt/data/git/openagentplatform/deploy/`

Docker Compose for development (13 services), Helm chart for production (60+ templates), GitHub Actions CI/CD (12 workflows), and full observability stack.

#### 3.7.1 Docker Compose Dev Stack (13 services)

| Service | Image | Port | Health Check |
|---------|-------|------|-------------|
| postgres | postgres:16-alpine | 5432 | `pg_isready` |
| nats | nats:2.10-alpine | 4222/8222 | HTTP `/healthz` |
| redis | redis:7-alpine | 6379 | `redis-cli ping` |
| api | oap/api:latest | 8080 | HTTP `/health/live` |
| web | oap/web:latest | 3000 | HTTP `/` |
| agent | oap/agent:latest | - | depends on NATS |
| otel-collector | otel/opentelemetry-collector-contrib:latest | 4317/4318 | HTTP `/` |
| prometheus | prom/prometheus:latest | 9090 | HTTP `/-/healthy` |
| grafana | grafana/grafana:latest | 3001 | HTTP `/api/health` |
| loki | grafana/loki:latest | 3100 | HTTP `/ready` |
| promtail | grafana/promtail:latest | - | depends on loki |
| mailhog | mailhog/mailhog:latest | 8025 | SMTP |
| vault | hashicorp/vault:1.16 | 8200 | HTTP `/v1/sys/health` |

#### 3.7.2 Helm Chart (`charts/oap/`)

60+ templates covering: API server (Deployment + HPA + PDB + Service + Ingress), web (Deployment + Service), agent DaemonSet, PostgreSQL (StatefulSet + Service), NATS (StatefulSet), Redis (StatefulSet), OTel Collector (Deployment), Prometheus (Deployment + ServiceMonitor + PrometheusRule + AlertmanagerConfig), Grafana (Deployment + Dashboard ConfigMaps), Loki (Deployment), Promtail (DaemonSet), cert-manager ClusterIssuer, NetworkPolicies (default-deny + 7 allow-policies), RBAC, K8s CronJob for migrations.

`values.yaml` schema: 200+ keys organized by service with sensible defaults.

#### 3.7.3 GitHub Actions (12 workflows)

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci-unit.yml` | PR | Run unit tests for all services (matrix) |
| `ci-integration.yml` | PR (paths filter) | Run integration test suites |
| `ci-e2e.yml` | Push to main, workflow_dispatch | Playwright E2E tests |
| `ci-load.yml` | Nightly, workflow_dispatch | k6 + Locust load tests |
| `ci-security.yml` | PR, nightly | ZAP, gitleaks, trufflehog, RBAC fuzz, Trivy |
| `ci-chaos.yml` | Nightly | chaos-mesh experiments |
| `ci-coverage-gate.yml` | PR | Merge lcov, enforce >=90% lines, >=85% branches |
| `build-images.yml` | Tag push | Buildx multi-arch, cosign, push to registry |
| `migrations-check.yml` | PR | Verify migrations are reversible |
| `release.yml` | Tag push `v*` | Generate release notes, publish binaries |
| `deploy-staging.yml` | Push to `release/**` | Helm upgrade staging |
| `deploy-prod.yml` | Manual approval | Helm upgrade production with canary |

#### 3.7.4 Observability Stack

- Traces: OTel SDK -> OTel Collector -> Tempo (tail-based sampling: keep all errors + slow + 10% probabilistic)
- Metrics: Prometheus with 24 custom `oap_*` business metrics, recording rules, 4 SLO burn-rate alerts
- Logs: Structured JSON -> Promtail -> Loki, with PII redaction
- Dashboards: 5 Grafana dashboards (overview, RMM, A2A, infrastructure, cost)
- Alerts: 5 Prometheus alert rule files (platform, rmm, a2a, infra, cost)
- Runbooks: 6 runbooks (agent failure, NATS down, A2A stuck, DB rollback, secret rotation fail, hallucination rollback)

#### 3.7.5 Implementation Steps (7 phases, 7 weeks)

Phase 1: Docker Compose dev stack (2 days)
Phase 2: Helm chart scaffolding (3 days)
Phase 3: CI/CD workflows (5 days)
Phase 4: Database migrations infrastructure (2 days)
Phase 5: Observability stack (5 days)
Phase 6: Security (cert-manager, NetworkPolicies, RBAC, PodSecurity) (3 days)
Phase 7: Multi-region + backup + DR (5 days)

---

## 4. Cross-Cutting Specifications

### 4.1 Auth and RBAC

#### 4.1.1 Authentication Methods (6)

| Method | Use Case | Implementation |
|--------|----------|----------------|
| JWT Bearer | API access for users and service accounts | RS256 signing, 15-min access, 30-day refresh with rotation |
| mTLS/SPIFFE | Service-to-service, NATS, agent connections | SPIFFE ID extraction from client certs, trust domain validation |
| OAuth 2.1 | Third-party app access | Authorization code + PKCE, DPoP binding, RFC 8707 resource indicators |
| SAML 2.0 | Enterprise SSO | SP-initiated, `samael` library, JIT provisioning with group-to-role mapping |
| OIDC | Enterprise SSO | RP with `coreos/go-oidc`, PKCE, nonce validation |
| API Keys | Programmatic access | SHA-256 hashed, scoped, rotatable, last-used tracking |

#### 4.1.2 RBAC Model

5 built-in roles:

| Role | Scope | Key Permissions |
|------|-------|-----------------|
| `super_user` | Tenant-wide | All operations |
| `manager` | Organization | User management, policy, deployment; cannot assign super_user |
| `operator` | Site | Agent management, script execution, alert handling |
| `technician` | Site (scoped) | Read + execute; no policy or user management |
| `read_only` | Site (scoped) | Read-only access to all resources |

Scoping hierarchy: Tenant > Organization > Client > Site > Agent. Policy Decision Point (PDP) evaluates: subject roles, action, resource scope. Decision: Allow (with matched rules) or Deny (with reason: tenant_mismatch, no_role_grants, insufficient_scope, cannot_elevate_above_self).

#### 4.1.3 MFA

TOTP enrollment with backup codes (10 single-use). Strict single-use enforcement (last_used tracking). Enrollment required before access if org policy mandates.

#### 4.1.4 Session Management

Max 5 concurrent sessions per user (oldest evicted). Idle timeout: configurable (default 8h). Absolute timeout: 30 days. Refresh token rotation with family revocation on reuse detection. Single logout (SLO) via SAML if IdP supports it.

#### 4.1.5 Audit Log

Hash-chained (Merkle-style) append-only log. 13 predefined AuditAction values. PII redaction via regex (password, token, SSN patterns). Monthly partitioning. Periodic integrity verification (every 1h for last 24h).

#### 4.1.6 SCIM 2.0

Full RFC 7644 compliance: Users CRUD, Groups CRUD, ServiceProviderConfig, ResourceTypes, Schemas. Filter parsing: `userName eq "alice@example.com"`. Bearer token auth for SCIM clients (Okta, Azure AD, etc.).

#### 4.1.7 Database Schema (15 tables)

Migrations 01-15 covering: tenants, users, organizations, clients, sites, roles, permissions, role_permissions, user_roles, api_keys, sessions, mfa_credentials, sso_connections, scim_endpoints, audit_events.

---

### 4.2 Testing Strategy

#### 4.2.1 Testing Pyramid

| Tier | Coverage Target | Tooling | Scope |
|------|----------------|---------|-------|
| Unit | >=90% lines, >=85% branches | cargo test, go test, vitest, pytest | No network, no filesystem, no real clocks |
| Integration | 100% pass on main | Testcontainers (postgres, nats, vault, redis, minio) | Service boundaries |
| E2E | 100% pass on release | Playwright + custom harness | Full user workflows |
| Load | SLO adherence | k6, Locust | Throughput and latency |
| Security | 0 high/critical | ZAP, gitleaks, RBAC fuzz, Trivy | Vulnerability surface |
| Chaos | MTTR <60s | chaos-mesh | Fault tolerance |

#### 4.2.2 Test Environment

Testcontainers harness in `packages/testkit/`: programmable boot of postgres, nats, vault, redis, minio. Agent simulator for NATS roundtrip tests. Fake Identity Provider (Dex) for OIDC/SAML. Shared fixtures, golden files, deterministic clocks.

#### 4.2.3 E2E Scenarios (5)

1. Agent deploy -> check-in -> monitor -> alert -> remediate
2. A2A cross-framework (LangGraph <-> CrewAI delegation)
3. Remote access session (terminal + file transfer + audit)
4. Patch management (scan -> approve -> deploy -> rollback on failure)
5. Multi-tenant isolation

#### 4.2.4 Quality Gates

| Gate | Source | Threshold | Blocking |
|------|--------|-----------|----------|
| Unit coverage lines | merged lcov | >=90% | Yes |
| Unit coverage branches | merged lcov | >=85% | Yes |
| Diff coverage | new code only | >=90% | Yes |
| Mutation score (auth, secrets, rbac) | cargo-mutants / mutmut | >=70% | Yes |
| Integration suite | junit | 100% pass | Yes |
| E2E critical paths | junit | 100% pass on release | Yes |
| Load SLOs | k6 thresholds | p99<500ms ingest | Yes on release |
| Security findings | ZAP, gitleaks, fuzz | 0 high/critical | Yes |
| Chaos recovery | experiments | MTTR<60s | Yes on release |

#### 4.2.5 Implementation Sequence (14 phases, 60 working days)

Phase 0: Bootstrap tooling (2 days)
Phase 1: Testkit foundation (3 days)
Phase 2: Unit tests per service (9 days)
Phase 3: Coverage gate (1 day)
Phases 4-8: Integration suites (15 days)
Phase 9-10: E2E harness + scenarios (10 days)
Phase 11: Load testing (6 days)
Phase 12: Security testing (6 days)
Phase 13: Chaos testing (6 days)
Phase 14: Hardening (2 days)

---

### 4.3 Observability

#### 4.3.1 Custom Business Metrics (24)

| Category | Metric | Type | Labels |
|----------|--------|------|--------|
| Endpoint/RMM | `oap_agent_checkins_total` | Counter | agent_id, status |
| Endpoint/RMM | `oap_check_results_total` | Counter | check_type, status |
| Endpoint/RMM | `oap_script_duration_seconds` | Histogram | runtime, exit_code |
| Endpoint/RMM | `oap_alerts_fired_total` | Counter | severity, channel |
| A2A | `oap_a2a_tasks_created_total` | Counter | agent_framework, task_type |
| A2A | `oap_a2a_task_duration_seconds` | Histogram | framework, state |
| DB Pool | `oap_db_pool_connections` | Gauge | pool_name, state |
| NATS | `oap_nats_publish_errors_total` | Counter | stream, subject |
| Cache | `oap_cache_hit_total` | Counter | backend, key_prefix |
| Secrets | `oap_secret_injection_duration_seconds` | Histogram | method, backend |
| LLM | `oap_llm_tokens_total` | Counter | provider, model, direction |
| LLM | `oap_llm_cost_dollars` | Counter | provider, model |
| Agent | `oap_agent_uptime_seconds` | Gauge | platform, version |

#### 4.3.2 SLOs (4)

| SLO | Target | Alert | Window |
|-----|--------|-------|--------|
| API availability | 99.9% | Burn rate 14.4x (critical), 6x (warning) | 1h/5m |
| Check-in p99 | <5s for 99% of requests | Burn rate 6x | 5m/1m |
| A2A task p95 | <30s for 95% of tasks | Burn rate 6x | 5m |
| Alert delivery p99 | <60s for 99% of alerts | Burn rate 14.4x | 1h |

#### 4.3.3 Grafana Dashboards (5)

Overview (10 panels), RMM (8 panels), A2A (6 panels), Infrastructure (8 panels), Cost (6 panels). All with PromQL queries, variables for tenant/agent/time range.

---

### 4.4 Documentation

MkDocs Material site at `https://docs.openagentplatform.io/`. Three audience sites (developer, operator, user). Generated API reference from swaggo + typedoc + gomarkdoc. Inline-doc lint for Python, Go, TypeScript. Versioned deployment via `mike`. 8-sprint implementation plan.

---

### 4.5 Commercial Tiering

BSL 1.1 license with change date 2030-06-15 and Additional Use Grant capping at 250 endpoints for non-paying users. Commercial code under `internal/commercial/` with `//go:build commercial` build tags. Separate binaries: `oap-server` (community) and `oap-server-enterprise`. EdDSA (Ed25519) runtime license validation. Feature flags (tier-based + metered). Graceful degradation (HTTP 402 with structured reason). Multi-tenancy (PostgreSQL RLS or schema-per-tenant). Enterprise SSO (SAML, OIDC), SCIM 2.0. Managed A2A relay (gRPC, cross-network). Enterprise reporting (chronedp, HTML/PDF delivery via email/S3/webhook). Stripe billing with per-agent metered usage.

---

## 5. Integration Specifications

### 5.1 Service Communication Matrix

| From | To | Protocol | Data Contract |
|------|----|----------|--------------|
| Agent | API Server | NATS msgpack | `oap.endpoint.{agent_id}.event` |
| API Server | Agent | NATS JSON | `rmm.cmd.{agent_id}.*` |
| API Server | A2A Gateway | Go channel + JSON-RPC | Event bridge: RMM event -> A2A task |
| A2A Gateway | Agent Adapter | JSON-RPC 2.0 + SSE | `SendMessage` / `SendStreamingMessage` |
| Agent Adapter | LLM Provider | Framework-native | LangGraph.astream(), CrewAI.kickoff(), etc. |
| API Server | Secret Service | REST + Bearer token | `GET /v1/secret/{ref}` -> injected value |
| Agent | Secret Service | gRPC streaming | `secret.Watch` for credential rotation |
| Frontend | API Server | REST/JSON + WebSocket | TanStack Query + WS `ws://api:8080/ws?token=...` |
| All services | OTel Collector | OTLP gRPC/HTTP | Traces, metrics, logs |

### 5.2 Event Flow Map (NATS Subject Taxonomy)

Complete 40+ subject hierarchy covering: agent heartbeats (30s), check dispatch/results, script dispatch/output/result, RMM alert/ticket/deploy/scan/patch events, secret access/revocation, A2A task lifecycle events, frontend WebSocket push channels.

### 5.3 Shared Schemas

| Schema | Used By | Format |
|--------|---------|--------|
| `AgentEvent` | Agent -> API -> rmm-core -> Frontend | protobuf (event_id, agent_id, event_type, severity, timestamp, payload, tenant_id, correlation_id) |
| `Task` | A2A Gateway <-> Agent Adapters <-> Frontend | protobuf + JSON (id, context_id, state, message, artifacts, version, agent_id) |
| `Artifact` | A2A task results | protobuf (artifact_id, name, description, parts[{kind, text/data/uri}]) |
| `SecretReference` | Checks, scripts, deployments | JSON URI `ref:oap://<type>/<ws>/<path>?version=<v>` |
| `AgentCard` | A2A discovery | protobuf + JSON (id, name, framework, endpoint, capabilities, tags, skills, auth) |
| `Auth Token (JWT)` | All authenticated requests | JSON (sub, tenant_id, scopes, iat, exp, iss, roles, session_id) |

### 5.4 Error Propagation

| Failed Component | Impact | Fallback | Data Loss Risk |
|------------------|--------|----------|----------------|
| NATS down | Agent<->API delivery fails | REST fallback: API queues in PostgreSQL `command_queue`, Agent polls `/agents/{id}/commands` every 10s; A2A buffers in memory (10k ring) | Commands delayed up to 10s; events dropped if buffer overflows |
| A2A Gateway down | RMM events can't become A2A tasks | rmm-core queues in `pending_a2a_tasks` table; dashboard shows "A2A unavailable" banner | None (durable queue) |
| Vault down | Secret injection fails | Script executor returns `status=error, "secret_unavailable"`; non-secret checks continue | None (scripts abort safely) |
| LLM Provider down | Agent adapter invoke fails | Retry 3x exponential backoff; then `InvokeError(retryable=true)`; task transitions to FAILED | Task result lost; user retries |
| PostgreSQL down | All persistence fails | Read-only from Redis cache for GETs; writes return 503 with Retry-After | Writes lost; clients retry |
| Redis down | Rate limiting, sessions, cache fail | In-process token bucket; JWT-only auth; cache misses hit PostgreSQL | Rate limits less precise; no session revocation |

### 5.5 Consistency Patterns

**Idempotency**: Duplicate detection via Redis SETNX with keys derived from operation type + UUID. Naturals: agent registration (upsert on `agent_id`), heartbeat (MAX timestamp), REST mutations (`Idempotency-Key` header).

**Sagas**: Patch deployment uses multi-step saga with compensating transactions (rollback on failure). A2A HITL uses persistent state in DB with 24h auto-cancel timeout.

**Eventually consistent boundaries**: Agent online/offline (90s heartbeat TTL), check results aggregation (latest wins), dashboard metrics (5-15s Redis TTL), A2A task state (<1s via WebSocket). Strongly consistent: secret lease revocation (<100ms), agent registration token (one-time), audit log (append-only serial PK).

**Ordering**: Script output (strict per-stream via NATS single consumer), A2A task transitions (strict per-task via version field + optimistic concurrency), check results (best-effort, client sorts by started_at).

---

## 6. Phased Roadmap

### 6.1 Phase Overview (7 phases, 46 weeks total)

| Phase | Focus | Duration | Exit Criteria |
|-------|-------|----------|---------------|
| 0 | Foundation: scaffold, DB, NATS, auth, agent MVP | 4 weeks | Agent heartbeats visible in UI |
| 1 | Core RMM: checks, alerts, policies, patches, scripts, remote | 10 weeks | Alpha 0.1 release |
| 2 | A2A + Agents: gateway, adapters, process pool, bridge | 6 weeks | Alpha 0.3 release |
| 3 | Secret Management: Vault, Infisical, reference resolver | 4 weeks | Beta 0.4 release |
| 4 | Frontend: full React UI, dashboards, real-time, terminal | 6 weeks | Beta 0.5 release |
| 5 | Production: observability, load test, security, docs, CI/CD | 8 weeks | GA 1.0 release |
| 6 | Commercial: gating, multi-tenancy, relay, reporting, billing | 8 weeks | Commercial 1.1 release |

### 6.2 Phase 0 Sprint Breakdown (2 sprints, 4 weeks)

**Sprint 0.1 (Week 1-2):**
- Story 0.1.1: Monorepo scaffold with Go workspace, Python venv, TypeScript workspace [Stream A, M]
- Story 0.1.2: CI pipeline for all 3 languages [Stream D, M]
- Story 0.1.3: PostgreSQL schema + migrations 01-09 [Stream A, L]
- Story 0.1.4: NATS server with mTLS, SPIFFE mappings [Stream D, M]
- Story 0.1.5: OIDC auth with Dex test IdP [Stream A, L]
- Story 0.1.6: OpenAPI 3.1 spec generation [Stream A, M]
- Story 0.1.7: React shell with TanStack Router + Query [Stream C, M]

**Sprint 0.2 (Week 3-4):**
- Story 0.2.1: Agent CLI binary (Go cross-compile) [Stream B, XL]
- Story 0.2.2: Agent registration + heartbeat flow [Stream A+B, L]
- Story 0.2.3: Endpoint list page with real-time updates [Stream C, M]
- Story 0.2.4: Audit log infrastructure [Stream A, M]
- Story 0.2.5: Developer docs (5-minute setup guide) [Stream D, S]

### 6.3 Phase 1 Sprint Breakdown (5 sprints, 10 weeks)

**Sprint 1.1 (Week 5-6): Checks**
- Story 1.1.1: Check definition CRUD API [Stream A, L]
- Story 1.1.2: Agent check executor [Stream B, L]
- Story 1.1.3: Built-in check library (ping, CPU, memory, disk, service) [Stream A, XL]
- Story 1.1.4: Check result ingest pipeline [Stream A, M]
- Story 1.1.5: Checks dashboard with live status [Stream C, M]

**Sprint 1.2 (Week 7-8): Alerts**
- Story 1.2.1: Alert rule engine + state machine [Stream A, XL]
- Story 1.2.2: Notification channels (email, Slack, webhook) [Stream A, L]
- Story 1.2.3: Alert inbox + detail page [Stream C, M]
- Story 1.2.4: Alert preferences and routing [Stream A, M]

**Sprint 1.3 (Week 9-10): Policies**
- Story 1.3.1: OPA integration for policy evaluation [Stream A, XL]
- Story 1.3.2: Compliance collectors [Stream A, L]
- Story 1.3.3: Policy library + editor UI [Stream A+C, L]
- Story 1.3.4: Policy violation alerts [Stream A, M]

**Sprint 1.4 (Week 11-12): Patches**
- Story 1.4.1: Patch inventory + scan workflow [Stream A+B, L]
- Story 1.4.2: Patch approval workflow [Stream A, L]
- Story 1.4.3: Patch deployment engine [Stream A, XL]
- Story 1.4.4: Patch status UI + reboot coordination [Stream A+C, M]

**Sprint 1.5 (Week 13-14): Scripts/Remote**
- Story 1.5.1: Script library CRUD [Stream A, M]
- Story 1.5.2: Script execution engine (4 runtimes) [Stream B, XL]
- Story 1.5.3: Script UI with Monaco editor [Stream C, L]
- Story 1.5.4: SSH/WinRM remote shell (xterm.js + noVNC) [Stream B+C, XL]
- Story 1.5.5: Remote session audit + recording playback [Stream A+B, M]

### 6.4 Parallel Work Streams (4 streams)

| Stream | Focus | Languages | Lead |
|--------|-------|-----------|------|
| A | Backend (API, data, NATS, business logic) | Go, Python | backend-lead |
| B | Agent (endpoint binary, OS integrations) | Go | agent-lead |
| C | Frontend (UI, design system, real-time) | TypeScript | frontend-lead |
| D | Infrastructure (CI/CD, monitoring, docs) | K8s, Terraform | devops-lead |

Cross-stream sync: daily standup, weekly architecture review, bi-weekly sprint planning/retro, monthly roadmap review.

### 6.5 Release Strategy

| Release | Tag | Phase | Audience | Channels |
|---------|-----|-------|----------|----------|
| Alpha 0.1 | `v0.1.0-alpha` | End Phase 1 | Design partners | alpha |
| Alpha 0.2/0.3 | `v0.2/0.3-alpha` | Phase 2 | Design partners | alpha |
| Beta 0.4 | `v0.4.0-beta` | End Phase 3 | Public beta | beta |
| Beta 0.5 | `v0.5.0-beta` | End Phase 4 | Public beta | beta |
| **GA 1.0** | `v1.0.0` | End Phase 5 | General availability | stable |
| Commercial 1.1 | `v1.1.0` | End Phase 6 | Paying customers | stable |

Release channels: nightly (every main push), alpha, beta, stable, LTS ( quarterly with 6-month support). SemVer policy: major for breaking API changes, minor for features, patch for fixes. Release process: feature freeze -> 2 RCs -> smoke tests -> sign-off -> tag -> post-release monitoring (72h war room).

---

## 7. Risk Register

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| R1 | Agent binary compatibility breaks on OS updates | Medium | High | Test matrix: Win10/11, Ubuntu 20/22/24, macOS 13/14; nightly CI on real VMs |
| R2 | NATS JetStream data loss on broker failure | Low | Critical | 3-node cluster with file storage; daily stream backup CronJob |
| R3 | LLM provider API changes break adapters | High | Medium | Adapter isolation; configurable model endpoints; fallback provider |
| R4 | Vault seal causes secret unavailability | Low | Critical | Auto-unseal via K8s; grace period with cached values; alert on seal event |
| R5 | A2A protocol spec divergence from implementations | Medium | Medium | Conformance test vectors; track upstream spec; version negotiation |
| R6 | React frontend SPA bundle size exceeds performance budget | Medium | Medium | Route-based code splitting; tree-shaking; performance CI check |
| R7 | Multi-tenant data leak via query isolation failure | Low | Critical | PostgreSQL RLS; integration test for every query path; quarterly security audit |
| R8 | Agent subprocess crashes leak secrets in core dumps | Low | High | prlimit core size = 0; secret zeroing after use; container seccomp profiles |
| R9 | CI/CD pipeline becomes a bottleneck | Medium | Medium | Sharding, parallelization, incremental test runs; 25-min budget |
| R10 | Documentation drift from implementation | High | Low | Inline-doc lint in CI; doc changes required in same PR as code changes |

---

## 8. Open Questions

| # | Question | Owner | Decision Date |
|---|----------|-------|---------------|
| O1 | Should the agent binary use CGO for prlimit or pure-Go syscall? | agent-lead | Phase 1 Sprint 1 |
| O2 | PostgreSQL RLS vs schema-per-tenant for multi-tenancy? | backend-lead | Phase 6 Sprint 1 |
| O3 | Should A2A streaming use SSE or WebSocket from gateway to frontend? | a2a-lead | Phase 2 Sprint 1 |
| O4 | CDN vs self-hosted for frontend static assets? | devops-lead | Phase 4 Sprint 1 |
| O5 | Vault namespace support for enterprise multi-tenancy? | secrets-lead | Phase 3 Sprint 2 |
| O6 | Should the agent binary auto-update or require explicit approval? | product | Phase 1 Sprint 5 |
| O7 | Commercial license enforcement: online-only or offline grace period? | product | Phase 6 Sprint 1 |
| O8 | Should MCP tools reference A2A skills directly or via indirection layer? | mcp-lead | Phase 2 Sprint 1 |
| O9 | k6 vs Locust vs Artillery for primary load testing tool? | test-lead | Phase 5 Sprint 1 |
| O10 | Should the Helm chart use KEDA for NATS-prometheus-based scaling? | devops-lead | Phase 5 Sprint 3 |

---

## 9. Appendices

### Appendix A: File Tree (Complete)

```
/mnt/data/git/openagentplatform/
├── .github/workflows/           # 12 CI/CD workflows
├── a2a/                         # A2A Gateway (Go module)
│   ├── cmd/gateway/main.go
│   ├── internal/
│   │   ├── auth/                # Bearer, mTLS, OAuth2
│   │   ├── bridge/              # RMM event -> A2A task
│   │   ├── cost/                # Token counting, cost tracking
│   │   ├── errors/              # Structured A2A errors
│   │   ├── hitl/                # Human-in-the-loop
│   │   ├── models/              # SQLModel (6 tables)
│   │   ├── protocol/            # JSON-RPC, REST, gRPC
│   │   ├── push/                # Webhook notifications
│   │   ├── registry/            # AgentCard registry
│   │   ├── router/              # Skill-match routing
│   │   ├── sse/                 # Subscriber hub
│   │   └── states/              # Task state machine
│   ├── migrate/                 # 6 SQL migration pairs
│   ├── proto/a2a.proto
│   └── go.mod
├── adapters/                    # Agent Framework Adapters (Python)
│   ├── src/oap/adapters/
│   │   ├── langgraph/
│   │   ├── crewai/
│   │   ├── autogen/
│   │   ├── semantic_kernel/
│   │   ├── openai/
│   │   └── anthropic/
│   ├── src/oap/pool/            # ProcessPool
│   ├── src/oap/orchestration/  # OrchestrationService
│   └── pyproject.toml
├── backend/                     # Django RMM Core
│   └── apps/rmm/
│       ├── models/              # 10 model files
│       ├── api/                 # Serializers, viewsets, URLs
│       ├── services/           # 10 service files
│       ├── tasks/               # 7 Celery task packages
│       ├── nats/                # Subjects, client, publishers, consumers
│       ├── state_machines/     # 5 state machines
│       ├── consumers/          # WebSocket consumers
│       └── tests/               # 19 test files
├── secrets/                    # Secret Management (Go module)
│   ├── cmd/secret-service/main.go
│   ├── internal/
│   │   ├── backends/           # 5 backends (Vault, Infisical, K8s CSI, Env, Memory)
│   │   ├── models/             # Reference, audit, rotation, injection, hierarchy
│   │   ├── services/           # Manager, injection, rotation, migration, audit
│   │   ├── a2a/               # Token manager, models, service
│   │   ├── mcp/               # OAuth 2.1, discovery, dynamic client
│   │   └── script_safety/     # Safe runner, audit, delivery
│   ├── api/                    # 9 route groups
│   └── go.mod
├── mcp-server/                  # Existing: API Server + MCP Guardrails
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/                # Vision server
│   │   ├── audit/              # Audit logger
│   │   ├── cache/              # Redis client
│   │   ├── circuitbreaker/     # Circuit breaker
│   │   ├── config/             # 400-line config
│   │   ├── database/           # Postgres stores
│   │   ├── guardrails/         # Rule registry
│   │   ├── ingest/             # Document ingestion
│   │   ├── mcp/                # MCP handlers + tools
│   │   ├── metrics/            # Prometheus metrics
│   │   ├── middleware/         # HTTP middleware
│   │   ├── security/          # Secret scanner
│   │   ├── team/               # Team management
│   │   ├── validation/         # Validation engine
│   │   ├── vision/             # Vision tools
│   │   └── web/                # Web server
│   ├── web/                     # Existing vanilla JS UI (to be replaced)
│   ├── migrations/
│   └── go.mod
├── web/                         # New: React SPA (Vite)
│   ├── src/
│   │   ├── config/
│   │   ├── lib/                # API client, WS client, auth
│   │   ├── routes/             # TanStack Router routes
│   │   ├── features/           # 10 feature modules (20 pages)
│   │   ├── components/         # Shadcn UI primitives
│   │   ├── hooks/              # Custom React hooks
│   │   └── types/              # TypeScript type definitions
│   ├── package.json
│   └── vite.config.ts
├── deploy/
│   ├── docker-compose.yml      # 13-service dev stack
│   ├── docker-compose.test.yml
│   ├── docker-compose.loadtest.yml
│   ├── k8s/                    # K8s configs, NATS conf
│   ├── vault/                  # Vault dev server + policies
│   └── observability/          # OTel, Prometheus, Grafana, Loki configs
├── charts/oap/                 # Helm chart (60+ templates)
├── tests/
│   ├── load/                   # k6 + Locust scripts
│   ├── security/               # ZAP, fuzz, injection suites
│   ├── chaos/                  # chaos-mesh experiments
│   ├── e2e/                    # Playwright scenarios
│   ├── conformance/            # A2A protocol vectors
│   └── fixtures/               # Golden files
├── packages/
│   ├── proto/                  # Shared protobuf/gRPC defs
│   ├── sdk-go/                 # Go agent SDK
│   ├── sdk-py/                 # Python agent SDK
│   ├── sdk-ts/                 # TypeScript web SDK
│   └── testkit/                # Reusable test helpers
├── docs/
│   ├── plans/                  # Implementation plans
│   └── runbooks/               # 6 operational runbooks
└── scripts/                    # Build, test, migration scripts
```

### Appendix B: Package Manifest

| Package | Version | Language | Purpose |
|---------|---------|----------|---------|
| `github.com/nats-io/nats.go` | 1.34+ | Go | NATS JetStream client |
| `github.com/jackc/pgx/v5` | 5.7+ | Go | PostgreSQL driver |
| `github.com/go-redis/redis/v8` | 8.11+ | Go | Redis client |
| `github.com/labstack/echo/v4` | 4.13+ | Go | HTTP framework |
| `github.com/prometheus/client_golang` | 1.20+ | Go | Prometheus metrics |
| `github.com/sony/gobreaker` | 1.0+ | Go | Circuit breaker |
| `github.com/hvac/hvac` | 2.3+ (async) | Python | Vault client |
| `infisical-sdk` | 2.0+ | Python | Infisical client |
| `django` | 5.0+ | Python | RMM core framework |
| `celery` | 5.4+ | Python | Async task execution |
| `nats-py` | 2.7+ | Python | NATS Python client |
| `langgraph` | 0.2+ | Python | LangGraph framework |
| `crewai` | 0.80+ | Python | CrewAI framework |
| `autogen-agentchat` | 0.4+ | Python | AutoGen framework |
| `semantic-kernel` | 1.0+ | Python | Semantic Kernel |
| `openai-agents` | 0.1+ | Python | OpenAI Agents SDK |
| `anthropic` | 0.40+ | Python | Anthropic SDK |
| `react` | 19.0+ | TypeScript | UI framework |
| `@tanstack/react-router` | 1.x | TypeScript | Routing |
| `@tanstack/react-query` | 5.x | TypeScript | Server state |
| `tailwindcss` | 4.x | TypeScript | Styling |
| `shadcn/ui` | latest | TypeScript | Component library |
| `xterm.js` | 5.x | TypeScript | Terminal emulator |
| `@novnc/novnc` | 1.x | TypeScript | VNC viewer |
| `@monaco-editor/react` | 4.x | TypeScript | Code editor |
| `k6` | 0.50+ | JS | Load testing |
| `playwright` | 1.45+ | TypeScript | E2E testing |
| `testcontainers` | 10.x | TypeScript | Integration testing |
| `chaos-mesh` | 2.6+ | YAML | Chaos engineering |

### Appendix C: Glossary

| Term | Definition |
|------|-----------|
| A2A | Agent-to-Agent protocol; JSON-RPC/gRPC specification for inter-agent communication |
| AgentCard | Discovery document published by each A2A agent listing capabilities, skills, and auth schemes |
| Agent (RMM) | Managed device running the OAP endpoint binary reporting to the control plane |
| Agent (A2A) | LLM-backed software agent (e.g., LangGraph graph, CrewAI crew) registered with the A2A gateway |
| Check | A monitoring test (ping, CPU, disk, service, script, etc.) assigned to one or more agents |
| HITL | Human-in-the-Loop; A2A task state where an LLM requests human approval before proceeding |
| JetStream | NATS persistence layer providing exactly-once, replayable message streams |
| MCP | Model Context Protocol; tool gateway for AI agent guardrails |
| NATS | High-performance messaging system used as the primary event bus |
| OAP | OpenAgentPlatform; the overall system described in this document |
| Policy | Hierarchical rule set defining checks, tasks, and patch behavior for Client/Site/Agent scopes |
| RMM | Remote Monitoring and Management; the core operational domain |
| RLS | Row-Level Security; PostgreSQL feature for multi-tenant data isolation |
| SecretReference | URI-format pointer to a secret in a backend (`ref:oap://vault/ws/path?version=7`) |
| SPIFFE | Secure Production Identity Framework for Everyone; standard for service identity via X.509 SVIDs |
| WinUpdate | Windows patch management record tracking scan -> approval -> install -> reboot lifecycle |
