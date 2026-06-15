# A2A Protocol Architecture

> **Version:** 1.0.0 | **Last Updated:** 2026-06-15 | **Status:** Authoritative Blueprint

---

## 1. Overview

The A2A (Agent-to-Agent) protocol is Google's open specification for inter-agent communication. It enables LLM agents running on different frameworks (LangGraph, CrewAI, AutoGen, etc.) to discover each other's capabilities, delegate tasks, and collaborate through a standardized protocol — without needing to know each other's internal implementation.

In OpenAgentPlatform, A2A is the **inter-agent backbone**. Every cross-framework agent communication flows through A2A. The complementary design: **MCP connects agents to tools, A2A connects agents to agents**.

**App Path:** `a2a/` (Go module)

---

## 2. Three-Layer Protocol Architecture

A2A is defined in three layers, allowing the same data model to flow over any transport:

| Layer | Name | Contents |
|-------|------|----------|
| **Layer 1** | Canonical Data Model | Protocol Buffer messages: `Task`, `Message`, `AgentCard`, `Part`, `Artifact`, `Extension` |
| **Layer 2** | Abstract Operations | Binding-independent: `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask`, `SubscribeToTask`, Push Notification CRUD, `GetExtendedAgentCard` |
| **Layer 3** | Protocol Bindings | Concrete transports: JSON-RPC 2.0 over HTTP/SSE, gRPC with server-streaming, HTTP+JSON/REST with SSE |

The proto file (`spec/a2a.proto`) is the single authoritative normative definition.

---

## 3. Gateway Architecture

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
│  │ mTLS, OAuth2  │  │ HMAC-SHA256  │  │ RMM -> A2A  │ │
│  └───────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### Key Components

| Component | Responsibility |
|-----------|---------------|
| **TaskManager** | Persists Tasks, Artifacts, Messages, CostRecords to PostgreSQL via pgx. Uses optimistic concurrency (version column) for safe concurrent updates. |
| **AgentCardReg** | In-memory registry of Agent Cards with periodic PostgreSQL snapshot. Supports CRUD and skill-based lookup. |
| **GatewayRouter** | Routes incoming `SendMessage` to the best-matching agent using skill-match scoring (see §6). |
| **SubscriberHub** | Manages in-process SSE subscriptions with backpressure. Sends 15s heartbeat keep-alive frames. |
| **EventBridge** | Converts 8 RMM event types (check_failure, alert_fired, patch_available, etc.) into A2A Tasks with appropriate skill tags. |
| **PushNotify** | Sends webhook notifications with HMAC-SHA256 signing. 4 worker goroutines with exponential backoff retry. |
| **Auth** | Validates Bearer tokens, mTLS certificates, and OAuth 2.1 tokens on every request. |

---

## 4. Task Lifecycle State Machine

### States

| State | Category | Description |
|-------|----------|-------------|
| `SUBMITTED` | Initial | Task created, awaiting agent pickup |
| `WORKING` | Active | Agent is processing |
| `COMPLETED` | ✅ Terminal | All artifacts finalized |
| `FAILED` | ❌ Terminal | Unrecoverable error |
| `CANCELED` | ❌ Terminal | User or system canceled |
| `INPUT_REQUIRED` | Interrupted | LLM requests human approval |
| `AUTH_REQUIRED` | Interrupted | Additional auth needed |
| `REJECTED` | ❌ Terminal | Agent declined the task |

### Transitions (14 valid)

| Current State | Trigger | Target State | Condition |
|---------------|---------|-------------|-----------|
| `SUBMITTED` | agent_accept | `WORKING` | Agent acknowledges task |
| `WORKING` | complete_task | `COMPLETED` | All artifacts finalized |
| `WORKING` | require_input | `INPUT_REQUIRED` | LLM requests human approval |
| `WORKING` | require_auth | `AUTH_REQUIRED` | Additional auth required |
| `WORKING` | fail_task | `FAILED` | Unrecoverable error |
| `WORKING` | cancel_task | `CANCELED` | User cancels |
| `INPUT_REQUIRED` | resume_task | `WORKING` | Human provides input |
| `INPUT_REQUIRED` | cancel_task | `CANCELED` | Human rejects |
| `INPUT_REQUIRED` | timeout (24h) | `CANCELED` | Input not provided in time |
| `AUTH_REQUIRED` | provide_auth | `WORKING` | Auth credentials supplied |
| `AUTH_REQUIRED` | cancel_task | `CANCELED` | User cancels |
| `FAILED` | retry_task | `WORKING` | Retriable error, retry |
| `SUBMITTED` | reject_task | `REJECTED` | Agent declines |

**Key semantic rule:** Messages SHOULD NOT deliver task outputs — results MUST be returned using Artifacts.

---

## 5. Protocol Bindings

### 5.1 JSON-RPC 2.0 Methods (7)

| Method | Direction | Params | Returns |
|--------|-----------|--------|---------|
| `a2a/sendTask` | Client → Gateway | `TaskSendParams` (id, message, configuration) | `Task` |
| `a2a/sendTaskStreaming` | Client → Gateway | `TaskSendParams` | SSE stream of `TaskDeltaEvent` |
| `a2a/getTask` | Client → Gateway | `id` | `Task` |
| `a2a/cancelTask` | Client → Gateway | `id` | `Task` (CANCELED) |
| `a2a/getArtifact` | Client → Gateway | `taskId`, `artifactId` | `Artifact` |
| `a2a/listArtifacts` | Client → Gateway | `taskId` | `Artifact[]` |
| `a2a/getAgentCard` | Client → Gateway | `agentId` | `AgentCard` |

### 5.2 REST Endpoints (12)

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

### 5.3 gRPC Service

```protobuf
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
```

---

## 6. Agent Card Registry

### Schema

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

### Discovery

Published at `/.well-known/agent-card.json` for each agent. The gateway maintains a registry with in-memory cache and periodic PostgreSQL snapshot.

### Skill-Match Scoring

When a task arrives with required skills, the GatewayRouter scores each candidate agent:

```
score = 1.0 + (matching_tags × 0.1) − (current_load × 0.05)
```

Highest score wins. Tie-break by `AgentCard.ID` (deterministic). This balances capability matching against current load.

---

## 7. Authentication

| Method | Use Case | Implementation |
|--------|----------|----------------|
| **Bearer Token** | API/CLI access | JWT with RS256, validated against JWKS |
| **mTLS** | Service-to-service, NATS | SPIFFE ID from client certificate, trust domain validation |
| **OAuth 2.1** | Third-party integrations | Authorization code + PKCE, DPoP binding, RFC 8707 resource indicators |

---

## 8. Event-to-Task Bridge

Converts RMM events into A2A Tasks. The bridge maps RMM event types to A2A skill tags:

| RMM Event Type | A2A Skill Tag | Trigger |
|----------------|---------------|---------|
| `check_failure` | `alert.triage` | Check result crosses failure threshold |
| `alert_fired` | `alert.correlation` | New alert created |
| `patch_available` | `patch.planning` | New patches detected by scan |
| `patch_approval_needed` | `patch.risk_assessment` | Patch awaiting approval |
| `script_error` | `script.debugging` | Script execution failed |
| `agent_offline` | `agent.recovery` | Agent heartbeat TTL exceeded |
| `compliance_violation` | `compliance.remediation` | Policy violation detected |
| `security_event` | `security.investigation` | Security-related alert |

---

## 9. Human-in-the-Loop (HITL)

When an LLM agent encounters `INPUT_REQUIRED`, the A2A gateway:

1. Transitions task to `INPUT_REQUIRED` state
2. Creates an `ApprovalRequest` record with 24h expiry
3. Dispatches notification to configured channels (WebSocket, email, Slack)
4. Blocks the task until approval or rejection (or 24h timeout → auto-cancel)
5. On approval: resumes task to `WORKING` with human-provided context
6. On rejection: transitions to `CANCELED`

---

## 10. Push Notifications

- Registration: agents register webhook URLs via `createPushNotification`
- Signing: all payloads signed with HMAC-SHA256 using shared secret
- Delivery: 4 worker goroutines process the queue
- Retry: exponential backoff (1s, 2s, 4s, ..., 30s cap), max 5 retries
- Verification: receiving agent validates HMAC signature

---

## 11. Cost Tracking

| Metric | How |
|--------|-----|
| **Input tokens** | Per-model token counter from LLM provider responses |
| **Output tokens** | Same |
| **Cost (USD)** | `input_tokens × input_price + output_tokens × output_price` per model |
| **Per-task** | Aggregated from all LLM calls within a single A2A task |
| **Per-endpoint cap** | Monthly spend limit per managed endpoint (configurable) |

---

## 12. Database Schema (6 Tables)

| Table | Key Columns | Indexes |
|-------|-------------|---------|
| `a2a_tasks` | id, context_id, agent_id, state (enum), message (jsonb), metadata (jsonb), version (int4), created_at, updated_at | (agent_id, state), (context_id) |
| `a2a_artifacts` | id, task_id (FK), name, description, parts (jsonb), mime_type, created_at | (task_id) |
| `a2a_messages` | id, task_id (FK), role, parts (jsonb), created_at | (task_id, created_at) |
| `a2a_cost_records` | id, task_id (FK), model, input_tokens, output_tokens, cost_usd, created_at | (task_id), (created_at) |
| `a2a_approval_requests` | id, task_id (FK), state, requested_by, responded_by, response, expires_at | (state, expires_at) |
| `a2a_agent_cards` | id, name, framework, endpoint, capabilities (jsonb), tags (jsonb), skills (jsonb), auth_config (jsonb) | GIN on tags, GIN on capabilities |

---

## 13. Implementation Steps (28 Ordered)

| Step | Produces |
|------|----------|
| 1. Initialize Go module `a2a/` with directory structure | Module scaffold |
| 2. Write `a2a.proto` with all message types and service definition | Proto definition |
| 3. Generate Go code with `protoc --go_out=. --go-grpc_out=.` | Generated code |
| 4. Implement data models in `internal/models/` (6 SQLModel + pgx repo) | Data layer |
| 5. Write SQL migrations 001-006 | Database tables |
| 6. Implement `internal/states/task_state.go` (14 transitions) | State machine |
| 7. Implement `internal/registry/agent_card.go` (in-memory + PG) | Card registry |
| 8. Implement `internal/router/gateway.go` (skill-match scoring) | Task router |
| 9. Implement `internal/protocol/jsonrpc.go` (7 methods) | JSON-RPC binding |
| 10. Implement `internal/protocol/rest.go` (12 endpoints) | REST binding |
| 11. Implement `internal/protocol/grpc.go` (9 RPCs) | gRPC binding |
| 12. Implement `internal/sse/subscriber_hub.go` (backpressure, heartbeats) | SSE streaming |
| 13. Implement auth handlers: bearer, mTLS, OAuth2 | Authentication |
| 14. Implement `internal/push/notify.go` (HMAC-SHA256, retry) | Push notifications |
| 15. Implement `internal/cost/tracker.go` | Cost tracking |
| 16. Implement `internal/hitl/service.go` (ApprovalStateMachine) | Human-in-the-loop |
| 17. Implement `internal/bridge/event.go` (8 RMM event mappings) | Event bridge |
| 18. Write `internal/errors/a2aerr.go` | Error types |
| 19. Write unit tests for all packages | Tests |
| 20. Write integration tests: JSON-RPC, REST, gRPC roundtrips | Integration tests |
| 21. Write A2A conformance tests against spec vectors | Conformance tests |
| 22. Implement graceful shutdown: drain connections, persist in-flight | Graceful shutdown |
| 23. Build Docker image, add to docker-compose | Containerization |
| 24. Deploy to K8s with HPA (target 70% CPU) | Kubernetes deployment |
| 25. Write k6 load test (10k msg/s, p99 < 250ms) | Load testing |
| 26. Security review: auth bypass, token revocation, cert validation | Security audit |
| 27. Write runbook: A2A Task stuck in WORKING state | Operations doc |
| 28. Write A2A protocol reference, adapter contract, cost model | User documentation |
