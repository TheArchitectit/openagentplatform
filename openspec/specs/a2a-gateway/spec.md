# A2A Gateway

> **Phase:** 2 (A2A + Agents)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/A2A_PROTOCOL.md` §2, §3, §5, §7
> **App Path:** `a2a/` (Go module)

---

## Description

The A2A Gateway is the single ingress for all Agent-to-Agent protocol traffic in
OpenAgentPlatform. A2A is Google's open specification for inter-agent
communication; it lets LLM agents running on different frameworks discover each
other's capabilities, delegate tasks, and collaborate without knowledge of each
other's internals. The complementary design rule for the platform is: **MCP
connects agents to tools, A2A connects agents to agents.**

The gateway exposes the same abstract operations over three concrete protocol
bindings — JSON-RPC 2.0 over HTTP/SSE, HTTP+JSON/REST with SSE, and gRPC with
server-streaming — all backed by a single canonical data model defined
normatively in `spec/a2a.proto`. Requests are authenticated, routed to the
best-matching registered agent by skill-match score, and persisted through the
Task Manager.

The gateway owns cross-cutting concerns for the A2A subsystem: authentication
(Bearer, mTLS, OAuth 2.1), SSE subscription fan-out with backpressure, outbound
push notifications with HMAC signing, and per-task LLM cost accounting.

## User Story

**As** an operator or an external agent client,
**I want** a single authenticated endpoint that speaks JSON-RPC, REST, and gRPC
interchangeably and routes my task to the agent best able to handle it,
**so that** I can integrate with the platform using whichever transport my stack
already supports, without needing to know which framework or process will
actually execute the work.

---

## Requirements

### 1. Three-Layer Protocol Architecture

1.1. The gateway MUST implement a three-layer separation so the same data model
flows over any transport:

| Layer | Name | Contents |
|-------|------|----------|
| 1 | Canonical Data Model | Protobuf messages: `Task`, `Message`, `AgentCard`, `Part`, `Artifact`, `Extension` |
| 2 | Abstract Operations | Binding-independent: `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask`, `SubscribeToTask`, Push Notification CRUD, `GetExtendedAgentCard` |
| 3 | Protocol Bindings | JSON-RPC 2.0 over HTTP/SSE, gRPC server-streaming, HTTP+JSON/REST with SSE |

1.2. `spec/a2a.proto` MUST be the single authoritative normative definition; Go
types MUST be generated from it (`protoc --go_out=. --go-grpc_out=.`) rather
than hand-written.

1.3. Abstract operations MUST NOT contain binding-specific logic. Adding a
fourth binding MUST NOT require changes to Layer 1 or Layer 2.

### 2. JSON-RPC 2.0 Binding

2.1. The gateway MUST expose 7 JSON-RPC methods:

| Method | Params | Returns |
|--------|--------|---------|
| `a2a/sendTask` | `TaskSendParams` (id, message, configuration) | `Task` |
| `a2a/sendTaskStreaming` | `TaskSendParams` | SSE stream of `TaskDeltaEvent` |
| `a2a/getTask` | `id` | `Task` |
| `a2a/cancelTask` | `id` | `Task` (CANCELED) |
| `a2a/getArtifact` | `taskId`, `artifactId` | `Artifact` |
| `a2a/listArtifacts` | `taskId` | `Artifact[]` |
| `a2a/getAgentCard` | `agentId` | `AgentCard` |

2.2. Responses MUST conform to JSON-RPC 2.0 envelope rules, including `id`
echo and the `error` object shape with structured A2A error codes.

2.3. `a2a/sendTaskStreaming` MUST upgrade to an SSE response and emit
`TaskDeltaEvent` frames until a terminal task state is reached or the client
disconnects.

### 3. REST Binding

3.1. The gateway MUST expose 12 REST endpoints under `/a2a/v1`:

| Method | Path | Purpose | Auth |
|--------|------|---------|------|
| POST | `/a2a/v1/tasks` | Create task | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}` | Get task | Bearer/mTLS |
| PATCH | `/a2a/v1/tasks/{id}` | Update task | Bearer/mTLS |
| DELETE | `/a2a/v1/tasks/{id}` | Cancel task | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}/artifacts` | List artifacts | Bearer/mTLS |
| GET | `/a2a/v1/tasks/{id}/artifacts/{aid}` | Get artifact | Bearer/mTLS |
| GET | `/a2a/v1/agents/{id}/card` | Get AgentCard | Public |
| POST | `/a2a/v1/agents/{id}/tasks` | Send task to a named agent | Bearer/mTLS |
| GET | `/a2a/v1/agents` | List registered agents | Bearer |
| POST | `/a2a/v1/approvals/{id}/approve` | Approve HITL request | Bearer |
| POST | `/a2a/v1/approvals/{id}/reject` | Reject HITL request | Bearer |
| GET | `/a2a/v1/subscriptions/{id}` | SSE stream | Bearer |

3.2. `GET /a2a/v1/agents/{id}/card` MUST be publicly reachable without
credentials, because agent-card discovery is a precondition of authentication
negotiation.

3.3. All other endpoints MUST reject unauthenticated requests with `401`, and
authenticated-but-unauthorized requests with `403`.

### 4. gRPC Binding

4.1. The gateway MUST implement the `a2a.v1.A2AService` service with 9 RPCs:
`SendTask`, `SendTaskStreaming` (server-streaming `TaskDeltaEvent`), `GetTask`,
`CancelTask`, `GetArtifact`, `ListArtifacts`, `SubscribeTask`
(server-streaming `TaskEvent`), `GetAgentCard`, and `ReportCost`.

4.2. Streaming RPCs MUST terminate cleanly on client cancellation without
leaking goroutines or subscription registrations.

4.3. The gRPC listener MUST require mTLS for service-to-service callers.

### 5. Authentication

5.1. The gateway MUST validate credentials on every request (except the public
agent-card route) using three mechanisms:

| Method | Use Case | Implementation |
|--------|----------|----------------|
| Bearer Token | API / CLI access | JWT with RS256, validated against JWKS |
| mTLS | Service-to-service, NATS | SPIFFE ID from client certificate, trust-domain validation |
| OAuth 2.1 | Third-party integrations | Authorization code + PKCE, DPoP binding, RFC 8707 resource indicators |

5.2. Bearer token validation MUST verify signature, expiry, issuer, and
audience. Key material MUST be fetched from JWKS and cached with refresh.

5.3. mTLS validation MUST extract the SPIFFE ID from the client certificate and
verify the trust domain matches platform configuration. A valid certificate
from an untrusted domain MUST be rejected.

5.4. Auth failures MUST NOT leak whether a task or agent ID exists.

### 6. Task Routing

6.1. Incoming `SendMessage` requests without an explicit target agent MUST be
routed by the GatewayRouter using skill-match scoring:

```
score = 1.0 + (matching_tags × 0.1) − (current_load × 0.05)
```

6.2. The highest-scoring candidate MUST win. Ties MUST be broken by
`AgentCard.ID` so routing is deterministic and reproducible.

6.3. If no agent matches the required skills, the request MUST fail with a
structured "no capable agent" error rather than routing arbitrarily.

6.4. Requests that name a target agent explicitly
(`POST /a2a/v1/agents/{id}/tasks`) MUST bypass scoring and MUST fail if that
agent is unregistered.

### 7. SSE Subscription Fan-Out

7.1. The SubscriberHub MUST manage in-process SSE subscriptions and fan task
events out to all subscribers of a task.

7.2. The hub MUST apply backpressure so a slow consumer cannot cause unbounded
memory growth; a subscriber that cannot keep up MUST be dropped rather than
buffered indefinitely.

7.3. The hub MUST emit heartbeat keep-alive frames every 15 seconds so idle
connections are not closed by intermediate proxies.

### 8. Push Notifications

8.1. Agents MUST be able to register webhook URLs via push-notification CRUD
operations.

8.2. Every outbound payload MUST be signed with HMAC-SHA256 using the
per-registration shared secret, and receiving agents MUST be able to verify it.

8.3. Delivery MUST be handled by 4 worker goroutines draining a queue.

8.4. Failed deliveries MUST be retried with exponential backoff (1s, 2s, 4s, …
capped at 30s) for a maximum of 5 attempts before being recorded as
permanently failed.

### 9. Cost Tracking

9.1. The gateway MUST record input tokens, output tokens, model name, and
computed USD cost for every LLM call attributable to a task.

9.2. Cost MUST be computed as
`input_tokens × input_price + output_tokens × output_price` using per-model
pricing.

9.3. Per-task cost MUST be the aggregate of all LLM calls made within that
task.

9.4. A configurable monthly spend cap per managed endpoint MUST be enforceable.

### 10. Operational Requirements

10.1. Shutdown MUST be graceful: drain in-flight connections and persist
in-flight task state before exit.

10.2. The gateway MUST sustain 10k messages/second with p99 latency under
250 ms, verified by a k6 load test.

10.3. The service MUST be containerized, present in `docker-compose`, and
deployable to Kubernetes with an HPA targeting 70% CPU.

10.4. A2A conformance tests MUST run against upstream spec vectors to detect
divergence between the implementation and the published specification.
