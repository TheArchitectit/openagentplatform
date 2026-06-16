export const meta = {
  name: 'write-all-architecture-docs',
  description: 'Write all 10 architecture documents in parallel',
  phases: [
    { title: 'Write Docs', detail: '10 parallel agents writing architecture docs' },
  ],
}

// All 10 docs — each agent writes one file directly, no plan mode
const docs = [
  {
    path: 'RMM_CORE.md',
    title: 'RMM Core Architecture',
    prompt: `Write a comprehensive, extremely clear architecture document to /mnt/data/git/openagentplatform/docs/architecture/RMM_CORE.md for the RMM Core subsystem of OpenAgentPlatform (an open-source, agent-first RMM platform).

Include these sections with full detail:

# RMM Core Architecture

## Overview
What the RMM Core does — device registration, monitoring (checks), policy propagation, patch management, alert lifecycle, script execution, remote access, and NATS orchestration. Why it matters as the foundation of an agent-first RMM.

## Dual-Transport Architecture
Why we use BOTH REST/HTTP AND NATS JetStream. REST for CRUD and periodic check-ins. NATS for real-time command execution, streaming output, and event distribution. Pattern validated by Tactical RMM (Django REST + NATS) and MeshCentral (WebSocket + MQTT).

## Data Models (10 models)
Document EVERY model with all fields in clear tables:

1. **Agent** (rmm_agent): agent_id (unique), hostname, client FK, site FK, platform, status, last_seen, inventory (JSONField), tags (ArrayField), mesh_token. Indexes: (org,status), (org,client,site), (org,platform), (org,last_seen), tags GIN. Constraint: UNIQUE(org, agent_id).

2. **Check** (rmm_check): name, check_type, interval_seconds, config (JSON), fail_threshold, alert_severity, last_status. Indexes: (org,check_type), (org,is_template), (org,last_status). Constraints: CHECK(interval_seconds >= 30), CHECK(timeout_seconds <= 3600).

3. **AgentCheck** (rmm_agent_check): agent FK, check FK, is_enabled, next_run_at. Indexes: (agent,is_enabled), (check,is_enabled), (next_run_at). Constraint: UNIQUE(agent, check).

4. **CheckResult** (rmm_check_result): agent_check FK, status, value (JSON), duration_ms, execution_start/end. Indexes: (agent,check,-execution_start), (org,status,-execution_start). Constraint: CHECK(duration_ms >= 0).

5. **Policy** (rmm_policy): name, enforcement_mode, priority, checks (JSON), automated_tasks (JSON), win_update_policy (JSON), alert_routing (JSON). Index: (org,priority).

6. **PolicyScope** (rmm_policy_scope): policy FK, scope_type, client FK, site FK, agent FK. Indexes: (client), (site), (agent). Constraint: CHECK(XOR — exactly one of client/site/agent set per scope_type).

7. **WinUpdate** (rmm_win_update): agent FK, kb_id, severity, state, cve_ids (JSON), approved_by FK. Indexes: (org,state), (agent,state), (org,severity). Constraint: UNIQUE(agent, kb_id).

8. **AutomatedTask** (rmm_automated_task): name, task_type, schedule_bitmask (21-bit), actions (JSON array), next_run_at. Indexes: (org,is_template), (org,next_run_at). Constraint: CHECK(bitmask in [0, 2^21)).

9. **Alert** (rmm_alert): severity, state, agent FK, check FK, dedup_key, notification_channels (JSON). Indexes: (org,state,-fired_at), (org,severity,-fired_at), (dedup_key).

10. **ScriptResult** (rmm_script_result): agent FK, script FK, runtime, state, stdout, stderr, exit_code. Indexes: (agent,-created_at), (org,state).

For each model, explain what it's for, how it's used, and include a field-by-field table with: Field Name | Type | Constraints | Default | Description.

## Enums (12)
List every enum with all values and meanings:
AgentStatus (pending/online/offline/degraded/uninstalled), AgentPlatform, CheckType (10 types: ping/cpu/memory/disk/service/script/event_log/process/wmi/custom), CheckStatus, PolicyEnforcementMode (inherit/enforce/exclude), WinUpdateState (8 states), AutomatedTaskActionType (8), AlertSeverity (5 levels), AlertState (6: new/acknowledged/in_progress/resolved/snoozed/closed), ScriptRuntime (5), RemoteSessionProtocol (5), RemoteSessionState (7).

## NATS Subject Taxonomy
Every subject with: Subject | Direction | Format | Publisher | Subscriber | Trigger | Description

Agent→Server (msgpack): rmm.agent.heartbeat (60s), rmm.agent.checkin (5-15min), rmm.check.result.{agent_id}, rmm.script.result.{agent_id}, rmm.script.chunk.{agent_id} (streaming), rmm.winupdate.scan.{agent_id}, rmm.winupdate.install.{agent_id}, rmm.agent.inventory.{agent_id}, rmm.remote.session.event.{agent_id}

Server→Agent (JSON, per-agent inbox): rmm.cmd.{agent_id}.script.run, .script.cancel, .check.run, .winupdate.install, .winupdate.scan, .sync, .agent.update, .remote.open, .remote.close, .policy.push, .inventory.refresh

Broadcast: rmm.broadcast.all, rmm.broadcast.org.{org_id}

## State Machines (5)
Draw each as ASCII state diagram with all valid transitions and triggers:
1. AlertStateMachine: new↔acknowledged→in_progress→resolved, new→snoozed, any→closed, resolved/closed→new (reopen)
2. WinUpdateStateMachine: scanned→pending_approval→approved→installing→installed→reboot_required, rejected, failed (retry)
3. AgentStateMachine: pending→online→offline, degraded→online, uninstalled
4. ScriptResultStateMachine: pending→running→success/error/timeout, cancel
5. RemoteSessionStateMachine: requested→pending_agent→active→transferring→closed, failed, timeout

## Services (10)
Table of: Service | File | Responsibility | Key Methods

CheckEngine (scheduling, dispatch, result processing, alert evaluation), PolicyEngine (hierarchical resolution, enforcement, propagation), AlertEngine (dedup, state machine, notifications, routing), PatchEngine (scan orchestration, approval, deployment, reboot, CVE), ScriptEngine (library, dispatch, streaming, results), InventoryCollector (inventory ingest, delta detection), CheckinHandler (heartbeat, full check-in, online/offline transitions), Propagation (policy push, delta computation, NATS publish), Enforcement (scope evaluation, exclusion, conflict resolution), RemoteAccess (session establishment, relay, recording, audit).

## Celery Tasks (7 packages)
check_tasks (run_due_checks, evaluate_check_result, escalate_failing_check), patch_tasks, alert_tasks, policy_tasks, inventory_tasks, script_tasks, registration (15+ beat schedule entries).

## Key Design Decisions
Why flat-table polymorphism for checks (avoids JOIN complexity at scale), why bitmask scheduling for tasks (compact, queryable, avoids cron parsing), why Client>Site>Agent hierarchy (mirrors MSP organizational model), why msgpack for agent + JSON for server (binary efficiency on constrained endpoints, human-readability server-side).

## Implementation Steps (28 ordered steps)
List each step with what it produces and what it depends on, from Django app scaffold through models, migrations, NATS, services, Celery, API, tests.

Write this as clear, professional markdown. Use tables for models and enums. Use ASCII diagrams for state machines. Be exhaustive — a developer should be able to implement from this doc alone.`,
  },
  {
    path: 'A2A_PROTOCOL.md',
    title: 'A2A Protocol Architecture',
    prompt: `Write a comprehensive, extremely clear architecture document to /mnt/data/git/openagentplatform/docs/architecture/A2A_PROTOCOL.md for the A2A (Agent-to-Agent) Protocol subsystem of OpenAgentPlatform.

Include:

# A2A Protocol Architecture

## Overview
What A2A is — Google's inter-agent communication protocol. Three-layer architecture: (1) Canonical Data Model (protobuf), (2) Abstract Operations (binding-independent), (3) Protocol Bindings (JSON-RPC/gRPC/REST). Why A2A matters for an agent-first RMM — enables agents on different frameworks to discover, delegate, and collaborate.

## Gateway Architecture
The A2A Gateway service with internal ASCII diagram showing: JSON-RPC handler (7 methods), REST handler (12 endpoints), gRPC handler (9 RPCs), TaskManager (pgx-backed persistence), AgentCardReg (in-memory + PostgreSQL snapshot), GatewayRouter (skill-match scoring), SubscriberHub (in-process, backpressure, 15s heartbeats), Auth (Bearer/mTLS/OAuth2), PushNotify (HMAC-SHA256, 4 workers, exponential backoff), EventBridge (8 RMM event types → A2A skill tags).

## Task Lifecycle State Machine
14 valid transitions, 5 terminal states. Document EVERY transition as a table: Current State | Trigger | Target State | Condition | Side Effects. States: SUBMITTED, WORKING, COMPLETED, FAILED, CANCELED, INPUT_REQUIRED, AUTH_REQUIRED, REJECTED. Semantic rule: Messages SHOULD NOT deliver task outputs — results MUST use Artifacts.

## Protocol Bindings
### JSON-RPC 2.0 Methods (7)
a2a/sendTask, a2a/sendTaskStreaming, a2a/getTask, a2a/cancelTask, a2a/getArtifact, a2a/listArtifacts, a2a/getAgentCard — with params and return types.

### REST Endpoints (12)
Full table: Method | Path | Handler | Auth | Description

### gRPC Service
The protobuf definition with all 9 RPCs: SendTask, SendTaskStreaming (server-stream), GetTask, CancelTask, GetArtifact, ListArtifacts, SubscribeTask (server-stream), GetAgentCard, ReportCost.

## Agent Card Registry
Schema: ID, Name, Description, Version, Framework, Endpoint, Capabilities, Tags, Skills, Authentication. Discovery at /.well-known/agent-card.json. Skill-match scoring formula: 1.0 + (matching_tags × 0.1) − (current_load × 0.05). Highest score wins; tie-break by AgentCard.ID.

## Authentication
Bearer tokens (JWT), mutual TLS (SPIFFE/SPIRE), OAuth 2.1 (authorization code + PKCE). When each is used.

## Event-to-Task Bridge
8 RMM event types mapped to A2A skill tags. How check_failure, alert_fired, patch_available, etc. become A2A Tasks.

## Human-in-the-Loop (HITL)
ApprovalStateMachine, NotificationDispatcher, 24h timeout, INPUT_REQUIRED state handling.

## Push Notifications
Webhook registration, HMAC-SHA256 signing, exponential backoff retry, 4 worker goroutines.

## Cost Tracking
TokenCounter per model, cost aggregation, per-task cost records, per-endpoint caps.

## Database Schema (6 tables)
a2a_tasks (id, context_id, agent_id, state enum, message jsonb, metadata jsonb, version int4, timestamps), a2a_artifacts, a2a_messages, a2a_cost_records, a2a_approval_requests, a2a_agent_cards — with columns, types, and key indexes.

## Implementation Steps (28 steps)
Ordered from Go module init through proto, models, state machine, registry, router, protocol handlers, auth, push, cost, HITL, bridge, tests, Docker, K8s, load test, security review, runbook, docs.

Write as clear, professional markdown with ASCII diagrams and tables. Include the protobuf definition.`,
  },
  {
    path: 'AGENT_FRAMEWORKS.md',
    title: 'Agent Framework Adapters Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/AGENT_FRAMEWORKS.md for the Agent Framework Adapters subsystem.

# Agent Framework Adapters Architecture

Include:

## Overview
Why adapters exist — to let LLM agents on different frameworks (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Anthropic Claude) participate as first-class citizens in the RMM via the A2A protocol.

## AgentWrapper Interface
The ABC with: invoke, stream, cancel, card, skills. What each method does, return types, error handling.

## Framework Adapters (6 detailed sections)
For EACH adapter, explain:
- How the framework communicates internally
- How A2A messages are translated to/from framework-native patterns
- Key methods and their implementations
- Code example of the translation pattern

**LangGraph**: State dict translation, compiled graph execution, MessagesState, Send/Command primitives, artifact extraction from final state, LangSmith Agent Server A2A wrapping at /a2a/{assistant_id}.

**CrewAI**: Native crewai.a2a module (a2a-sdk~=0.3.10) — we leverage it directly, NOT wrap it. Register as A2A peers. Within-crew communication = task/output pipeline. Cross-crew = A2A.

**AutoGen**: GroupChat → SendMessage mapping, round-robin message patterns, termination conditions → Task states.

**Semantic Kernel**: HandoffOrchestration wrapping, ChatCompletionAgent peers, ChatHistoryAgentThread, MAF (Microsoft Agent Framework) native A2A hosting.

**OpenAI Agents SDK**: Handoff → SendMessage delegation, tool calls → MCP tool invocations.

**Anthropic Claude Agent SDK**: Tool-use capabilities as A2A skills, streaming responses → A2A Artifact streams.

## ProcessPool
Warm pool management: max_size=10, idle_timeout=300s, LRU eviction. Methods: acquire, release, health_check, drain, kill. Health monitoring (heartbeat ping). Graceful lifecycle.

## OrchestrationService
Routing A2A tasks to available framework instances. Methods: dispatch, cancel, get_cost. Agent selection via skill-match scoring.

## Cost Management
Per-endpoint cost caps, token usage tracking (input/output per model), budget alerts, cost aggregation per task.

## Human-in-the-Loop
A2A INPUT_REQUIRED state handling. Approval UI integration. Notification delivery. 24h timeout.

## Implementation Steps (16 tasks)
Skeleton, DB engine, exceptions, ABC, 6 adapters, ProcessPool, OrchestrationService, TokenCounter, HITL, JSON-RPC router, docs.

Write as clear, professional markdown with Python code examples for the ABC and adapter patterns.`,
  },
  {
    path: 'SECRET_MANAGEMENT.md',
    title: 'Secret Management Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/SECRET_MANAGEMENT.md for the Secret Management subsystem.

# Secret Management Architecture

## Overview
Why secret management matters in an RMM. The core principle: NEVER store credentials in the primary database. Only store secret references (backend_type + path). Delegate to dedicated backends.

## Backend Abstraction (SecretBackend interface)
Go interface with 8 methods: Get, Set, Delete, List, Metadata, Rotate, Healthcheck, Close, SupportsDynamic. 5 implementations: VaultBackend, InfisicalBackend, K8sCSIBackend, EnvBackend, MemoryBackend.

## Vault Backend
4 auth methods: AppRole (machine-to-machine), Kubernetes (container deployments), JWT/OIDC (agent identity), Token. KV v2 secrets engine with versioning and rollback. Dynamic secrets (short-lived DB credentials, cloud API tokens). Lease management (renew, revoke). Audit logging. Policy-to-hierarchy mapping with template variables ({{client_id}}, {{site_id}}, {{agent_id}}). Token renewal at token_ttl * 0.7.

## Infisical Backend
Universal Auth and Kubernetes Auth. Path mapping: OAP path → Infisical folder/key. Auto token-refresh on 401. Folder inheritance maps to Client>Site>Agent hierarchy.

## Other Backends
K8sCSIBackend (file-based, read-only, via Secrets Store CSI Driver), EnvBackend (process environment, write is no-op), MemoryBackend (in-process dict, testing only).

## Secret Reference Model
URI format: ref:oap://<backend_type>/<workspace_id>/<path>?version=<v>&key=<k>. Resolution pipeline: parse URI → lookup backend → authorize (hierarchy check) → fetch secret → return. Concurrent resolution with asyncio.gather + Semaphore(max_concurrency). TTL-based LRU cache. Audit emission on every resolution.

## Credential Injection Pipeline
3 injection methods: env (OAP_INJECTED_ prefix), file (mode 0600, owned by agent UID), stdin (Unix socket). TTL sweeper every 10s. Revocation: secure deletion (zero-fill + unlink), dynamic lease revocation. Just-in-time fetching.

## A2A Auth Token Management
EdDSA Ed25519 signed JWTs. Claims: iss, sub, aud, jti, scopes, delegation_chain. Issue, exchange (down-scope, extend chain), verify (signature + exp/nbf + scope matching with wildcards + revocation check), revoke (add jti to revocation list with TTL, audit event). Max delegation depth: 3 with TTL reduction per hop.

## Script Credential Safety
Why secrets NEVER go to script arguments or env vars on the endpoint. Server-side authenticated operations model. JIT endpoint credential delivery (credential in HTTP response only). Audit logging. Risks: process listings, shell history, log files, core dumps.

## MCP OAuth 2.1 Integration
DPoP binding via cnf claim. Dynamic Client Registration (RFC 7591). Protected Resource Metadata (RFC 9728). Token validation before privileged operations.

## Hierarchy-Based Access
Client > Site > Agent scoping. Agent can access secrets at its level and below, never above. Vault policy hierarchy. Infisical folder mapping. Enforcement on every backend call.

## API Endpoints (9 route groups)
routes_secrets (CRUD per backend), routes_references (resolve, validate, batch-resolve), routes_rotation (rotate, policies), routes_injection (inject, revoke, active), routes_a2a_tokens (issue, exchange, verify, revoke), routes_mcp (OAuth2 authorize/token, well-known), routes_audit (query, detail), routes_hierarchy (tree, grant, revoke), routes_migration (migrate, status).

## K8s Integration
Secrets Store CSI Driver with Vault and Infisical providers. SecretProviderClass CRD. tmpfs mounts. Sync intervals. RBAC via ServiceAccount tokens.

## Implementation Steps (10 ordered steps)

Write as clear, professional markdown with Go code examples for the interface and code patterns.`,
  },
  {
    path: 'ENDPOINT_API.md',
    title: 'Endpoint API Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/ENDPOINT_API.md for the Endpoint API subsystem.

# Endpoint API Architecture

## Overview
The dual-transport API pattern: REST/HTTP for CRUD + NATS JetStream for real-time. Why both exist. Extends existing mcp-server Go codebase.

## REST API (22+ endpoints)
Full table for EVERY endpoint: Method | Path | Description | Request Body | Response Body | Auth | Status Codes

Groups: Agents (register, list, detail, deregister, heartbeat, commands), Checks (list, create, run, results), Scripts (list, create, execute, status), Events (query, detail), System (health, version), Patches (scan, approve, deploy), Auth (login, refresh, logout).

Include example request/response JSON for key endpoints (agent registration, check run, script execute).

## NATS Bus
4 JetStream streams with subjects, retention, and consumers:
- AGENTS (oap.endpoint.>): heartbeat, registration, events
- CHECKS (oap.check.>): dispatch, result
- SCRIPTS (oap.script.>): dispatch, output, result
- WINUPDATE (oap.winupdate.>): scan, install

5 consumer groups: api-cmd-dispatcher, check-result-ingester, script-output-relay, winupdate-processor, event-persister.

Msgpack codec for agent traffic (tagged fields for schema evolution). orjson for server-to-server.

## Agent Binary (Go)
Architecture, cross-compilation (Windows/Linux/macOS, amd64/arm64), lifecycle: NEW→REGISTERING→REGISTERED→STALE→OFFLINE→DEREGISTERED. Per-agent NATS subscription (subject = agentID, dispatch on Func field via switch). UUID persisted to disk for stable identity. Exponential backoff reconnect (1s, 2s, 4s, ..., 30s cap). Script executor with 4 runtimes (Python3, Bash, PowerShell, Node). prlimit resource constraints.

## gRPC Service
30+ message types in protobuf. Reflection + health service. Auth/logging/recovery interceptors.

## Implementation Steps (22 ordered steps)

Write as clear, professional markdown with full JSON examples for REST endpoints and subject diagrams for NATS.`,
  },
  {
    path: 'FRONTEND.md',
    title: 'Frontend Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/FRONTEND.md for the Frontend subsystem.

# Frontend Architecture

## Overview
React 19 SPA replacing existing vanilla JS UI. Why we chose this stack. What it replaces.

## Tech Stack
Vite, React 19, TypeScript, TanStack Router, TanStack Query v5, Shadcn/ui + Tailwind CSS, xterm.js, noVNC, Monaco Editor. Version for each.

## Project Structure
File-based routing, feature modules (10), shared components, hooks, types. Directory tree.

## State Management
- Server state: TanStack Query v5 (stale-while-revalidate, WebSocket invalidation)
- Client state: Zustand (sidebar, modals, form drafts)
- Form state: React Hook Form + Zod validation
- Real-time: WebSocket subscriptions auto-refetch TanStack caches

## Feature Modules (10 modules, 20 pages)
For each module, list: pages, key components, interactions, data dependencies:
Auth (Login, SSO, Password Reset), Dashboard (status grid, alerts, gauges, compliance), Agent Management (list, detail with 6 tabs), Monitoring (checks, alerts, detail pages), Patch Management (compliance, patches, policies, deployments), Remote Access (xterm.js terminal, noVNC desktop, sessions), Script Editor (Monaco editor, run form, live output), A2A Dashboard (agent cards, task lifecycle, messages, artifacts), Policies (editor with validation, import diff), Secret Management (list, detail, access log), Settings (users, roles, SSO, notifications, org, API keys).

## Shared Infrastructure (Tasks 1-9)
Vite+React scaffolding, Tailwind+Shadcn config, env config, API client, WebSocket client, auth layer, TanStack Router + shell, Shadcn primitives (30+), shared visualization components.

## API Client
Fetch wrapper, JWT interceptors, retry logic, error handling, base URL config.

## WebSocket Client
Reconnect strategy, heartbeat, message type routing, TanStack Query cache invalidation.

## Auth Layer
JWT decode, token refresh, RBAC context, permission hooks (usePermission, useRole).

## Implementation Steps (20 tasks)
Tasks 1-9 infrastructure, Tasks 10-20 feature modules. Each is one PR.

Write as clear, professional markdown with component tree diagrams and data flow examples.`,
  },
  {
    path: 'INFRASTRUCTURE.md',
    title: 'Infrastructure Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/INFRASTRUCTURE.md for the Infrastructure subsystem.

# Infrastructure Architecture

## Overview
Dev stack (Docker Compose), production stack (Kubernetes + Helm), CI/CD (GitHub Actions), observability (OTel + Prometheus + Grafana + Loki).

## Docker Compose Dev Stack (13 services)
Table: Service | Image | Port | Health Check | Purpose
Services: postgres:16-alpine (5432), nats:2.10-alpine (4222/8222), redis:7-alpine (6379), api (8080), web (3000), agent, otel-collector (4317/4318), prometheus (9090), grafana (3001), loki (3100), promtail, mailhog (8025), vault:1.16 (8200).

## Helm Chart (charts/oap/)
60+ templates: Deployments, StatefulSets, DaemonSets, Services, Ingress, HPA, PDB, ConfigMaps, Secrets, NetworkPolicies, ServiceMonitors, PrometheusRules, Dashboard ConfigMaps, CronJobs. values.yaml 200+ keys.

## CI/CD (12 GitHub Actions workflows)
Table: Workflow | Trigger | Purpose
ci-unit, ci-integration, ci-e2e, ci-load, ci-security, ci-chaos, ci-coverage-gate, build-images, migrations-check, release, deploy-staging, deploy-prod.

## Observability Stack
- Traces: OTel SDK → Collector → Tempo (tail-based sampling)
- Metrics: Prometheus with 24 oap_* business metrics, 4 SLO burn-rate alerts
- Logs: JSON → Promtail → Loki
- Dashboards: 5 Grafana dashboards (overview 10 panels, RMM 8, A2A 6, infra 8, cost 6)

## Security
TLS everywhere, cert-manager ClusterIssuer, default-deny NetworkPolicy + 7 allow-policies, PodSecurity, RBAC.

## Backup and DR
PostgreSQL pg_dump CronJob, NATS stream backup CronJob, Velero, RPO 5min/RTO 15min.

## Implementation Steps (7 phases, 7 weeks)

Write as clear, professional markdown with YAML config examples.`,
  },
  {
    path: 'AUTH_AND_RBAC.md',
    title: 'Auth & RBAC Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/AUTH_AND_RBAC.md for the Auth & RBAC subsystem.

# Auth & RBAC Architecture

## Overview
6 auth methods and when each is used.

## JWT Bearer Auth
RS256 signing, 15-min access tokens, 30-day refresh tokens with rotation. Full token structure (header + payload claims).

## mTLS/SPIFFE
Service-to-service identity, NATS connections, trust domain validation. X.509 SVID extraction.

## OAuth 2.1
Authorization code + PKCE, DPoP binding, RFC 8707 resource indicators.

## SAML 2.0
SP-initiated SSO, JIT provisioning, group-to-role mapping.

## OIDC
RP implementation with PKCE and nonce validation.

## API Keys
SHA-256 hashed storage, scoped permissions, rotation, last-used tracking.

## RBAC Model
5 roles: super_user (all), manager (org-wide, no super_user assignment), operator (site-level, agent/script/alert mgmt), technician (site-scoped, read+execute), read_only (site-scoped, read-only). Scoping hierarchy: Tenant>Organization>Client>Site>Agent. Policy Decision Point evaluation flow.

## MFA
TOTP enrollment with backup codes (10 single-use). Enforcement policies.

## Session Management
Max 5 concurrent sessions. Idle timeout (8h default). Absolute timeout (30d). Refresh token rotation with family revocation on reuse detection.

## Audit Log
Hash-chained Merkle-style append-only log. 13 AuditAction values. PII redaction. Monthly partitioning. Hourly integrity verification.

## SCIM 2.0
RFC 7644: Users/Groups CRUD, ServiceProviderConfig, ResourceTypes, Schemas. Filter parsing. Bearer token auth.

## Database Schema (15 tables)
migrations 01-15: tenants, users, organizations, clients, sites, roles, permissions, role_permissions, user_roles, api_keys, sessions, mfa_credentials, sso_connections, scim_endpoints, audit_events.

## Implementation Steps

Write as clear, professional markdown with JWT payload examples and evaluation flow diagrams.`,
  },
  {
    path: 'INTEGRATION_AND_EVENTS.md',
    title: 'Integration & Event Flow Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/INTEGRATION_AND_EVENTS.md for integration and event flow across all OpenAgentPlatform subsystems.

# Integration & Event Flow Architecture

## Overview
How all subsystems fit together. The big picture.

## Service Communication Map
Table: From | To | Protocol | Data Contract | Purpose
Agent↔API Server (NATS msgpack), API↔Agent (NATS JSON), API↔A2A Gateway (Go channel + JSON-RPC), A2A↔Agent Adapter (JSON-RPC 2.0 + SSE), Adapter↔LLM Provider (framework-native), API↔Secret Service (REST + Bearer), Frontend↔API (REST/JSON + WebSocket), All→OTel Collector (OTLP).

## Complete NATS Event Flow Map
Organize ALL subjects by category with: Subject | Publisher | Subscriber | Trigger | Action Format
Categories: agent (heartbeats, checkins, inventory), checks (dispatch, results), scripts (dispatch, output, result), patches (scan, install), alerts (fire, state changes), A2A (task lifecycle), secrets (access, revocation), system (policy push, broadcast).

## Critical Data Flow Diagrams (4 detailed flows)
Step-by-step ASCII diagrams:
1. Agent check-in → check failure → alert → A2A task → LLM triage → remediation
2. Patch scan → available → approval → A2A risk evaluation → install
3. Secret reference → credential injection → script execution → revocation
4. A2A task: LangGraph → CrewAI delegation → result artifact → back to LangGraph

## Shared Schemas (6)
Every schema that crosses service boundaries with format and which services use it:
AgentEvent (protobuf), Task (protobuf+JSON), Artifact (protobuf), SecretReference (JSON URI), AgentCard (protobuf+JSON), Auth Token (JWT).

## Error Propagation
What happens when each component fails: NATS down (REST fallback + agent polling every 10s), A2A Gateway down (rmm-core queues in pending_a2a_tasks table), Vault down (scripts return error, non-secret checks continue), LLM Provider down (retry 3x exponential backoff then FAILED), PostgreSQL down (read from Redis cache, writes return 503 with Retry-After), Redis down (in-process token bucket, JWT-only auth, cache misses hit PostgreSQL).

## Consistency Patterns
Idempotency (Redis SETNX with operation+UUID keys), Sagas (patch deployment with compensating transactions), Eventually consistent boundaries (agent status 90s TTL, check results latest-wins, metrics 5-15s Redis TTL, A2A task state <1s via WebSocket), Strongly consistent (secret lease revocation <100ms, registration tokens one-time, audit log serial PK), Ordering guarantees (script output strict per-stream, A2A task transitions strict per-task via version+optimistic concurrency, check results best-effort).

Write as clear, professional markdown with ASCII diagrams for every data flow.`,
  },
  {
    path: 'COMMERCIAL_AND_LICENSING.md',
    title: 'Commercial & Licensing Architecture',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/COMMERCIAL_AND_LICENSING.md for the commercial tiering and licensing system.

# Commercial & Licensing Architecture

## Overview
BSL 1.1 rationale for open-core model. Why not AGPL (too restrictive for enterprise), Apache 2.0 + CLA (administratively burdensome), Elastic License (more complex), SSPL (not OSI-recognized). BSL strikes the right balance.

## BSL 1.1 License Terms
Full terms explained in plain language. Change date: 2030-06-15 (auto-converts to Apache 2.0). Additional Use Grant: caps non-paying use at 250 endpoints. What users CAN do (self-host, modify, distribute). What users CANNOT do (offer as multi-tenant SaaS, managed hosting) without license.

## Code Boundary
Open-source code in main repository. Proprietary code under internal/commercial/ with //go:build commercial build tags. Separate binaries: oap-server (community) and oap-server-enterprise. How the build system produces each.

## Feature Gating Architecture
Feature flag system. Runtime license validation using EdDSA Ed25519 (license key + signature verification). Graceful degradation: gated features return HTTP 402 with structured {reason, tier_required, feature_name}. API responses include tier information for gated endpoints.

## Tier Definitions
### Community (BSL 1.1, free)
Single-tenant, 250 endpoints, 3 LLM agent processes, local A2A only, HashiCorp Vault integration, basic alerting/policy, community support.

### Professional (paid)
Multi-tenant MSP mode, unlimited endpoints, 25 LLM agents with framework mixing, managed A2A relay (cross-network discovery), enterprise reporting (PDF/HTML, scheduled delivery), A2A task audit + compliance, Vault + Infisical, RBAC + MFA + SSO (SAML/OIDC), priority support with SLA.

### Enterprise (premium)
All Professional features, unlimited LLM agents, custom A2A relay with VPC peering, agent-assisted change management + CAB, MCP marketplace, custom framework adapters, dedicated CSM + onboarding, air-gapped deployment mode.

## Multi-Tenancy
Tenant model and isolation. PostgreSQL RLS vs schema-per-tenant (open question). Tenant provisioning API. NATS subject isolation with tenant prefix. Stripe billing integration.

## Managed A2A Relay
Cloud relay service. Cross-network agent discovery federation. Relay auth and metering. Usage-based pricing (task count, bandwidth).

## Enterprise Reporting
Template engine. Scheduled delivery (email, webhook, S3). Cross-tenant aggregation. PDF/HTML export.

## Billing
License key generation (EdDSA key pair, signed payload). Per-endpoint subscription tracking. Per-agent-process pricing metering. Stripe Billing integration (checkout sessions, webhooks, customer portal). Usage reporting dashboard.

## Contributor Agreement
What contributors agree to. Patent license grant to the project. DCO (Developer Certificate of Origin) sign-off.

## Implementation Steps

Write as clear, professional markdown with Go code examples for feature gating and license validation.`,
  },
  {
    path: 'ROADMAP_AND_SPRINTS.md',
    title: 'Roadmap & Sprint Plan',
    prompt: `Write a comprehensive architecture document to /mnt/data/git/openagentplatform/docs/architecture/ROADMAP_AND_SPRINTS.md for the phased roadmap and sprint plan.

# Roadmap & Sprint Plan

## Overview
7-phase, 46-week implementation plan. Visual ASCII timeline showing all phases with their durations.

## Phase Definitions
Table: Phase | Focus | Duration | Exit Criteria | Release Tag
- Phase 0 (Foundation, 4 weeks): scaffold, DB, NATS, auth, agent MVP → Agent heartbeats visible in UI → v0.1.0-alpha
- Phase 1 (Core RMM, 10 weeks): checks, alerts, policies, patches, scripts, remote → Alpha 0.1
- Phase 2 (A2A + Agents, 6 weeks): gateway, adapters, process pool, bridge → Alpha 0.3
- Phase 3 (Secret Mgmt, 4 weeks): Vault, Infisical, references, injection → Beta 0.4
- Phase 4 (Frontend, 6 weeks): full React UI, dashboards, real-time → Beta 0.5
- Phase 5 (Production, 8 weeks): observability, load testing, security, docs → GA 1.0
- Phase 6 (Commercial, 8 weeks): gating, multi-tenancy, relay, reporting, billing → Commercial 1.1

## Phase 0 Sprint Breakdown
**Sprint 0.1 (Week 1-2)** — for each story, write as proper user story with acceptance criteria:
- 0.1.1: Monorepo scaffold with Go workspace, Python venv, TypeScript workspace [Stream A, M]
- 0.1.2: CI pipeline for all 3 languages [Stream D, M]
- 0.1.3: PostgreSQL schema + migrations 01-09 [Stream A, L]
- 0.1.4: NATS server with mTLS, SPIFFE mappings [Stream D, M]
- 0.1.5: OIDC auth with Dex test IdP [Stream A, L]
- 0.1.6: OpenAPI 3.1 spec generation [Stream A, M]
- 0.1.7: React shell with TanStack Router + Query [Stream C, M]

**Sprint 0.2 (Week 3-4):**
- 0.2.1: Agent CLI binary (Go cross-compile) [Stream B, XL]
- 0.2.2: Agent registration + heartbeat flow [Stream A+B, L]
- 0.2.3: Endpoint list page with real-time updates [Stream C, M]
- 0.2.4: Audit log infrastructure [Stream A, M]
- 0.2.5: Developer docs (5-minute setup guide) [Stream D, S]

## Phase 1 Sprint Breakdown
**Sprint 1.1 (Week 5-6): Checks** — 5 stories
**Sprint 1.2 (Week 7-8): Alerts** — 4 stories
**Sprint 1.3 (Week 9-10): Policies** — 4 stories
**Sprint 1.4 (Week 11-12): Patches** — 4 stories
**Sprint 1.5 (Week 13-14): Scripts/Remote** — 5 stories

(Write each story as: As a [role], I want [feature], so that [benefit]. Acceptance criteria: [list]. Complexity: [S/M/L/XL]. Stream: [A/B/C/D].)

## Phase 2-6 Overviews
Component lists and key deliverables for each remaining phase.

## Parallel Work Streams
4 streams: A=Backend (Go/Python), B=Agent (Go), C=Frontend (TypeScript), D=Infrastructure (K8s). Sync cadence: daily standup, weekly architecture review, bi-weekly sprint planning/retro.

## Release Strategy
6 releases with channels (nightly/alpha/beta/stable/LTS). SemVer policy. Release process: feature freeze → 2 RCs → smoke tests → sign-off → tag → 72h war room.

## Open Questions (10)
O1: CGO vs pure-Go prlimit, O2: RLS vs schema-per-tenant, O3: SSE vs WebSocket for A2A, O4: CDN vs self-hosted, O5: Vault namespace support, O6: Agent auto-update, O7: Online-only license, O8: MCP-to-A2A indirection, O9: k6 vs Locust, O10: KEDA for NATS scaling. Each with owner and decision date.

## Risk Register (10 risks)
R1-R10 with likelihood, impact, and mitigation.

Write as clear, professional markdown with an ASCII timeline and sprint story tables.`,
  },
]

phase('Write Docs')

const results = await parallel(docs.map(doc => () =>
  agent(doc.prompt, {
    label: `write:${doc.path}`,
    phase: 'Write Docs',
    model: 'sonnet',
  })
))

log(`Docs written: ${results.filter(Boolean).length}/${docs.length}`)

// Now save each result to its file
const saved = results.filter(Boolean).map((content, i) => {
  // Each agent returned the full markdown content as text
  return { path: docs[i].path, length: content.length }
})

log(`Saved: ${JSON.stringify(saved)}`)

return saved