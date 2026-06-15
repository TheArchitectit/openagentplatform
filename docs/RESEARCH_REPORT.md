# Architectural Design Report: OpenAgentPlatform — An Agent-First Open-Source RMM

> **Research methodology:** 96 parallel agents, 5 search angles, 15 source fetches, 3-vote adversarial verification on 25 claims (15 survived). Full citation traceability throughout.

---

## Executive Summary

OpenAgentPlatform is an agent-first Remote Monitoring and Management (RMM) platform designed around the principle that LLM-based agents are first-class citizens, not bolted-on features. Unlike conventional RMMs that treat automation as rigid policy engines, OpenAgentPlatform natively embeds multi-framework LLM agent orchestration (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Anthropic Claude Agent SDK) alongside traditional device management. The platform adopts the A2A (Agent-to-Agent) protocol as its inter-agent communication backbone, enabling agents running on different frameworks to discover, delegate to, and collaborate with each other through a standardized protocol. Device management follows the proven dual-transport pattern observed in production RMMs like Tactical RMM and MeshCentral: REST/HTTP for CRUD and periodic check-ins, and a persistent message bus (NATS) for real-time command execution and event streaming. The platform integrates HashiCorp Vault and Infisical for secret management, and is released under a Business Source License (BSL 1.1) with an open-core commercial tier that reserves multi-tenant SaaS, enterprise reporting, and managed A2A relay for paying customers.

---

## Core Architecture (Agent-First Design Principles)

### Design Principles

1. **Agent-First, Not Agent-Added:** LLM agents participate in every operational layer — triage, remediation, patch approval, alert correlation, and change management. Traditional RMM automation (checks, tasks, policies) coexists but is delegatable to agent workflows.

2. **Dual-Transport Core:** REST/HTTP for CRUD operations and periodic check-ins; NATS for real-time command execution, streaming output, and event distribution. This pattern is validated by Tactical RMM (Django REST + NATS) and MeshCentral (WebSocket relay + optional MQTT) [source: Tactical RMM `agent/rpc.go`, MeshCentral `meshrelay.js`].

3. **A2A as the Inter-Agent Backbone:** All cross-framework agent communication flows through the A2A protocol. Each agent framework is wrapped as an A2A server exposing an Agent Card, enabling discovery and task delegation regardless of the underlying runtime.

4. **MCP for Tool Access:** Agents use MCP (Model Context Protocol) to access external tools, APIs, and data sources. MCP uses a client-host-server architecture with Streamable HTTP transport and OAuth 2.1 authorization [source: https://modelcontextprotocol.io/specification/2025-11-25/architecture]. The complementary design: **MCP connects agents to tools, A2A connects agents to agents.**

5. **Policy-Propagated Automation:** RMM automation follows a Client > Site > Agent hierarchy with enforcement and exclusion semantics, as validated by Tactical RMM's Policy model [source: Tactical RMM `automation/models.py`]. Agent workflows can be triggered by policy events (check failures, alert thresholds) or invoked directly.

### System Topology

```
                        +-----------------------+
                        |   API Gateway / LB    |
                        +----------+------------+
                                   |
                    +--------------+--------------+
                    |                             |
           +--------v--------+          +---------v---------+
           |  REST API Layer  |          |  NATS JetStream   |
           |  (CRUD, Auth,   |          |  (Real-time cmd,  |
           |   Queries)      |          |   Events, Agent   |
           +--------+--------+          |   Check-ins)      |
                    |                   +----+----------+---+
                    |                        |          |
           +--------v--------+      +--------v---+  +---v--------+
           |  Agent Orchest- |      |  Agent     |  | Event      |
           |  ration Service |      |  Gateway   |  | Processor  |
           |  (A2A Server,   |      |  (NATS <-> |  | (Checks,   |
           |   Framework     |      |   REST)    |  |  Alerts,   |
           |   Wrappers)     |      +------------+  |  Tasks)    |
           +--------+--------+                     +------------+
                    |
        +-----------+-----------+-----------+
        |           |           |           |
   +----v---+ +----v---+ +----v---+ +------v-----+
   |LangGraph| |CrewAI | |AutoGen | |SemanticKrnl|
   | Wrapper | |Wrapper | |Wrapper | |  Wrapper   |
   +---------+ +--------+ +--------+ +------------+
```

### Core Data Models

**Agent (Device) Model:** Carries inventory as structured JSON fields on a single row — `operating_system`, `hostname`, `goarch`, `total_ram`, `disks` (JSON), `services` (JSON), `wmi_detail` (JSON), `public_ip`, `boot_time`, `logged_in_username`, `needs_reboot`. Software inventory and patch status live in separate related models (`InstalledSoftware`, `WinUpdate`) [source: Tactical RMM `agents/models.py`].

**Check Model:** The core monitoring primitive uses flat-table polymorphism with a `check_type` discriminator (`DISK_SPACE`, `PING`, `CPU_LOAD`, `MEMORY`, `WINSVC`, `SCRIPT`, `EVENT_LOG`) and type-specific nullable fields consolidated on the same table. This avoids JOIN complexity at scale [source: Tactical RMM `checks/models.py`].

**Policy Model:** Defines checks and automated tasks propagated through Client > Site > Agent hierarchy. The `enforced` flag discards agent-level overrides. `block_policy_inheritance` on Agent/Site/Client stops propagation. Excluded sites/clients/agents are tracked via M2M fields [source: Tactical RMM `automation/models.py`].

**WinUpdate Model:** Per-agent, per-update records with `guid`, `kb`, `title`, `severity`, `installed`/`downloaded` booleans, `action` (inherit/approve/ignore/nothing), `result` string, `date_installed`. `WinUpdatePolicy` controls approval behavior per severity level with scheduling, reboot policy, and run-time windows [source: Tactical RMM `winupdate/models.py`].

**AutomatedTask Model:** Actions stored as a JSON array of script/cmd objects (each with its own timeout, args, env_vars). Scheduling uses bitmask-based fields (`task_type`: DAILY/WEEKLY/MONTHLY/MONTHLY_DOW/ONBOARDING/RUN_ONCE/CHECK_FAILURE, with `run_time_bit_weekdays`, `monthly_days_of_month` etc.) — NOT crontab expressions. `assigned_check` FK triggers tasks on check failure. Supported platforms controlled via ArrayField [source: Tactical RMM `autotasks/models.py`].

---

## RMM Essential Capabilities Matrix

| RMM Capability | Traditional Approach | Agent-First Approach |
|---|---|---|
| **Monitoring (Checks)** | Flat-table polymorphic Check model with check_type discriminator, run_interval, fails_b4_alert, error/warning thresholds. Results in separate CheckResult/CheckHistory tables [source: Tactical RMM `checks/models.py`] | Same Check model, but check failures can trigger A2A task delegation to an LLM agent for intelligent triage instead of just firing alerts |
| **Patch Management** | Per-agent WinUpdate records with approval workflows (inherit/approve/ignore/nothing per severity). WinUpdatePolicy controls auto-approval, scheduling (daily/weekly/monthly), and reboot-after-install [source: Tactical RMM `winupdate/models.py`] | Same approval workflow, but an LLM agent can evaluate patch risk contextually (e.g., "this critical patch caused boot failures on similar hardware last quarter, recommend delay"), overriding or supplementing the policy |
| **Script Execution** | Multiple runtimes (PowerShell, CMD, Python, Shell, NuShell, Deno) with per-action timeout, environment variables, streaming stdout/stderr via NATS [source: Tactical RMM `scripts/models.py`, `agent/agent.go`] | Scripts execute as SYSTEM (no sandboxing in existing RMMs). Agent can generate scripts on-the-fly, review script output, and decide on follow-up actions autonomously |
| **Remote Access** | Server-mediated relay architecture: browser and agent maintain persistent connections to server, which bridges them. WebRTC as optional P2P optimization. Endpoint never needs inbound reachability [source: MeshCentral `meshrelay.js`, `meshdesktopmultiplex.js`] | Relay unchanged. Agent can initiate remote sessions on alert conditions, supervise session activity, and log session context for audit |
| **Asset Inventory** | Agent publishes structured data on NATS topics (`agent-hello`, `agent-agentinfo`, `agent-disks`, `agent-winsvc`, `agent-publicip`, `agent-wmi`), stored as JSONFields on Agent model + separate InstalledSoftware model [source: Tactical RMM `agents/models.py`, `natsapi/svc.go`] | Same collection. Agent can interpret inventory changes ("3 new services installed in last 24h on server X, none match approved software baseline") and surface actionable alerts |
| **Automation / Policies** | Policy-propagation model: Client > Site > Agent hierarchy with enforced/block_policy_inheritance semantics. Tasks triggered by CHECK_FAILURE, schedules, or onboarding [source: Tactical RMM `automation/models.py`] | Policies remain the deterministic backbone. Agent workflows layer on top: an agent can create/modify policies based on observed patterns, but policy enforcement remains the source of truth |
| **Alerting** | Alert records with `resolved_on` timestamps, separate pruning cycles (`resolved_alerts_prune_days`). Alert templates FK to Policy [source: Tactical RMM CoreSettings, `agents/models.py`] | Agent triages alerts: correlates across checks, suppresses duplicates, escalates based on business impact assessment, and creates remediation tasks |
| **Reporting** | Cross-cutting aggregation from CheckResult/CheckHistory, Alert, AgentHistory, WinUpdate with time-bounded pruning and separate retention policies per data type [source: Tactical RMM `core/models.py`, `ee/reporting/`] | Agent generates natural-language report summaries, identifies trends, and proactively distributes insights to stakeholders |

---

## A2A Protocol Integration

### A2A Protocol Architecture

A2A (Agent-to-Agent) is a three-layer protocol [source: https://a2a-protocol.org/latest/specification/]:

- **Layer 1 (Canonical Data Model):** Protocol-agnostic data structures defined as Protocol Buffer messages — `Task`, `Message`, `AgentCard`, `Part`, `Artifact`, `Extension`. The proto file (`spec/a2a.proto`) is the single authoritative normative definition.
- **Layer 2 (Abstract Operations):** Binding-independent operations: `SendMessage`, `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask`, `SubscribeToTask`, Push Notification CRUD (Create/Get/List/Delete), `GetExtendedAgentCard`.
- **Layer 3 (Protocol Bindings):** Concrete transports — JSON-RPC 2.0 over HTTP with SSE streaming, gRPC with server-streaming RPCs, HTTP+JSON/REST with SSE. The canonical schema is transport-agnostic, allowing the same Task/Message/Artifact structures to flow over any binding.

### Task Lifecycle

The A2A TaskState enum has 10 values: `TASK_STATE_UNSPECIFIED`, `TASK_STATE_SUBMITTED`, `TASK_STATE_WORKING`, `TASK_STATE_COMPLETED`, `TASK_STATE_FAILED`, `TASK_STATE_CANCELED`, `TASK_STATE_INPUT_REQUIRED`, `TASK_STATE_REJECTED`, `TASK_STATE_AUTH_REQUIRED`. Four terminal states (COMPLETED, FAILED, CANCELED, REJECTED) cannot accept further messages. Two interrupted states (INPUT_REQUIRED, AUTH_REQUIRED) require client response before the task can resume. Task IDs are always server-generated [source: A2A specification `a2a.proto`].

### Messages and Artifacts

Messages carry multi-modal Parts using a protobuf OneOf pattern: at most one of `text` (string), `raw` (bytes), `url` (string), or `data` (arbitrary JSON via `google.protobuf.Value`), plus optional metadata, filename, mediaType. Messages convey conversation turns (role: USER or AGENT). Artifacts convey task results (`artifactId`, `parts[]`, `name`, `description`, `metadata`, `extensions[]`). **Key semantic rule: Messages SHOULD NOT deliver task outputs — results MUST be returned using Artifacts** [source: A2A specification, `a2a.proto`].

### Implementation Strategy

1. **Agent Card Server:** Each RMM agent framework wrapper exposes an Agent Card at `/.well-known/agent-card.json` describing its capabilities (monitoring, remediation, patch management) as A2A skills. The card advertises supported protocol bindings (JSON-RPC, gRPC, REST).

2. **A2A Gateway Service:** A dedicated service acts as the central A2A broker. It maintains the registry of Agent Cards, routes `SendMessage` requests to the appropriate framework wrapper, and tracks Task lifecycle state transitions. The gateway persists Tasks and Artifacts to PostgreSQL for durability and audit.

3. **Framework Wrappers:** Each supported agent framework (LangGraph, CrewAI, AutoGen, Semantic Kernel, OpenAI Agents SDK, Anthropic Claude Agent SDK) has a thin adapter that:
   - Translates A2A `SendMessage` requests into framework-native invocations
   - Translates framework outputs (state mutations, task results) into A2A Artifacts
   - Maps A2A interrupted states to framework-appropriate human-in-the-loop mechanisms

4. **MCP Integration:** Agents access external tools via MCP clients. The platform hosts an MCP server that exposes RMM operations (run script, check status, patch approve) as MCP Tools. MCP uses Streamable HTTP transport with OAuth 2.1 authorization [source: https://modelcontextprotocol.io/specification/2025-11-25/architecture]. MCP servers cannot see the full conversation or other servers, preserving isolation.

5. **Event-to-Task Bridge:** RMM events (check failures, alert thresholds, patch available) are mapped to A2A Tasks. A monitoring agent receives the event as an A2A Message, processes it, and either resolves the task directly or delegates to a specialist agent (e.g., a patch-analysis agent) via `SendMessage`.

---

## Multi-Agent Platform Support

### Framework Abstraction Layer

The platform defines a universal `AgentWrapper` interface that each framework adapter implements:

```python
interface AgentWrapper {
  agentCard(): AgentCard                    # A2A discovery metadata
  invoke(message: A2AMessage, context): TaskResult  # Execute a task
  stream(message: A2AMessage, context): AsyncIterable<PartialResult>  # Stream results
  cancel(taskId: string): void              # Cancel running task
  interrupt(taskId: string, response: A2AMessage): void  # Resume from INPUT_REQUIRED
}
```

### Framework-Specific Adapters

#### LangGraph
LangGraph agents communicate through shared state (`StateGraph` + `MessagesState`), not direct messaging [source: https://langchain-ai.github.io/langgraph/]. Multi-agent patterns include supervisor (central agent delegates to workers) and swarm (agents hand off via tool calls producing `Command` objects that route to other agents). LangGraph recently added `Send` and `Command` primitives for targeted state updates between nodes. Agents are compiled into a runnable graph with durable execution (checkpointer), streaming, and human-in-the-loop (`interrupt_before`/`interrupt_after`). No native A2A in the core library, but LangSmith's Agent Server (>= 0.4.21) provides A2A endpoint wrapping at `/a2a/{assistant_id}`. **The OpenAgentPlatform adapter:** translates `A2AMessage` into a LangGraph state input dict, runs the compiled graph, and extracts artifacts from the final state.

#### CrewAI
CrewAI agents communicate through Sequential process (task output pipelines) or Hierarchical process (manager agent delegation), plus event-driven Flows with `@start`/`@listen`/`@router` decorators [source: https://github.com/crewAIInc/crewAI]. Critically, **CrewAI has NATIVE A2A support** via its built-in `crewai.a2a` module (installable via the 'a2a' pip extra including `a2a-sdk~=0.3.10`), which provides A2A client/server configuration, agent card generation and signing, authentication schemes, delegation, and streaming/polling/push notification handlers. **The OpenAgentPlatform adapter** leverages CrewAI's native A2A module directly rather than wrapping it, registering CrewAI agents as peers on the A2A network. Within-crew communication remains the task/output pipeline; cross-crew/inter-agent communication uses A2A.

#### AutoGen
AutoGen supports conversational agent patterns where agents exchange messages in round-robin or group chat topologies. The `GroupChat` manager selects the next speaker via an LLM-based selector. AutoGen's message-passing model aligns closely with A2A's Message/Task abstraction. **The OpenAgentPlatform adapter:** maps each AutoGen agent to an A2A Agent Card; AutoGen group messages become A2A `SendMessage` calls; AutoGen's termination conditions map to A2A terminal Task states.

#### Semantic Kernel / Microsoft Agent Framework
Semantic Kernel's current agent communication uses `HandoffOrchestration` where specialist agents (e.g., `RefundAgent`, `OrderStatusAgent`) are standalone `ChatCompletionAgent` peers, not plugins. Plugins are plain code classes with `KernelFunction` methods that provide tool capabilities to individual agents. `ChatHistoryAgentThread` maintains per-agent conversation state. The Process Framework models event-driven business process steps [source: https://github.com/microsoft/semantic-kernel]. Semantic Kernel's successor, Microsoft Agent Framework (MAF), adds A2A hosting support (confirmed in MAF Python and .NET hosting samples). **The OpenAgentPlatform adapter:** for SK, wraps `HandoffOrchestration` agents as A2A servers; for MAF, uses its native A2A hosting.

#### OpenAI Agents SDK
The OpenAI Agents SDK (formerly Swarm) uses a handoff pattern where agents transfer control via tool calls. Each agent has instructions, tools, and handoff definitions. The SDK is lightweight and Python-native. **The adapter:** maps agent handoffs to A2A `SendMessage` delegates, and tool calls to MCP tool invocations.

#### Anthropic Claude Agent SDK
Claude's agent SDK uses a tool-use loop where the model decides which tools to invoke. **The adapter:** exposes Claude's tool-use capabilities as A2A skills in the Agent Card, and translates Claude's streaming responses into A2A Artifact streams.

### Persistent Agent Processes

For cost and latency efficiency, the platform maintains warm agent processes using a process pool pattern:
- Each framework adapter manages a pool of pre-initialized agent instances
- Agent instances are kept warm with their context/memory loaded
- A2A Task requests are routed to available instances via the A2A gateway
- Instance lifecycle (spawn, health-check, drain, kill) is managed by the orchestration service

---

## Endpoint API Design

### Transport Architecture

The platform follows the dual-transport pattern validated by Tactical RMM and MeshCentral [source: Tactical RMM `agent/rpc.go`, MeshCentral `meshrelay.js`]:

- **REST/HTTP (JSON):** All CRUD operations on agents, checks, tasks, scripts, policies, alerts. Agent periodic check-ins. Query and reporting endpoints.
- **NATS JetStream:** Real-time command execution (ping, runscript, terminal, registry operations, service control, agent update, uninstall). Streaming stdout/stderr. Agent inventory updates on dedicated subjects (`agent-hello`, `agent-agentinfo`, `agent-disks`, `agent-winsvc`, `agent-publicip`, `agent-wmi`). Event distribution for alerts and check results.

The NATS transport uses msgpack serialization for agent check-ins (following Tactical RMM's pattern) and JSON for server-to-server communication. Agent subscriptions follow the per-agent-ID subject pattern: each agent subscribes to its own NATS subject (its `agentID`) and dispatches on a `Func` field via a switch statement [source: Tactical RMM `agent/rpc.go`].

### REST API Schemas

#### Agent Registration and Check-in
```
POST   /api/v1/agents/                    # Register new agent
GET    /api/v1/agents/                     # List agents (filter by client, site, platform, status)
GET    /api/v1/agents/{agent_id}/          # Get agent detail
PATCH  /api/v1/agents/{agent_id}/          # Update agent metadata
DELETE /api/v1/agents/{agent_id}/          # Uninstall/decommission agent
POST   /api/v1/agents/{agent_id}/checkin/  # REST check-in (supplement to NATS)
```

Agent payload includes: `operating_system`, `hostname`, `goarch`, `total_ram`, `disks` (JSON array), `services` (JSON array), `wmi_detail` (JSON object), `public_ip`, `boot_time`, `logged_in_username`, `needs_reboot`.

#### Monitoring (Checks)
```
POST   /api/v1/checks/                     # Create check
GET    /api/v1/checks/                      # List checks (filter by agent, policy, type, status)
GET    /api/v1/checks/{check_id}/           # Get check detail
PATCH  /api/v1/checks/{check_id}/           # Update check
DELETE /api/v1/checks/{check_id}/           # Delete check
GET    /api/v1/checks/{check_id}/results/   # Get check results (paginated, time-bounded)
GET    /api/v1/checks/{check_id}/history/   # Get check history time-series
```

Check types: `DISK_SPACE`, `PING`, `CPU_LOAD`, `MEMORY`, `WINSVC`, `SCRIPT`, `EVENT_LOG`. Each check has: agent FK, policy FK, `check_type`, `error_threshold`, `warning_threshold`, `fails_b4_alert`, `run_interval`, `alert_severity`, plus type-specific nullable fields (`disk`, `ip`, script FK, `svc_name`, `log_name`, `event_id`, etc.) [source: Tactical RMM `checks/models.py`].

#### Patch Management
```
POST   /api/v1/agents/{agent_id}/patches/scan/    # Trigger patch scan
GET    /api/v1/agents/{agent_id}/patches/          # List patches for agent
PATCH  /api/v1/agents/{agent_id}/patches/{patch_id}/  # Approve/ignore patch
POST   /api/v1/agents/{agent_id}/patches/install/ # Install approved patches
GET    /api/v1/policies/{policy_id}/patch-policy/  # Get patch approval policy
PATCH  /api/v1/policies/{policy_id}/patch-policy/  # Update patch approval policy
```

WinUpdate records: `guid`, `kb`, `title`, `severity`, `installed`, `downloaded`, `action` (inherit/approve/ignore/nothing), `result`, `date_installed`. WinUpdatePolicy per severity: critical/important/moderate/low/other with manual/approve/ignore/inherit options, scheduling (daily/weekly/monthly), reboot policy (never/required/always/inherit), and run-time windows [source: Tactical RMM `winupdate/models.py`].

#### Remote Access
```
POST   /api/v1/agents/{agent_id}/remote/desktop/   # Initiate desktop session (returns relay token)
POST   /api/v1/agents/{agent_id}/remote/shell/     # Initiate shell session
WS     /ws/relay/{relay_token}/                     # WebSocket relay for desktop/shell traffic
```

The relay architecture mediates between technician browser and endpoint agent, never requiring direct endpoint reachability. Both browser and agent connect inbound to the server, which pairs them by relay token and forwards traffic. WebRTC is an optional P2P optimization attempted after the relay session is established [source: MeshCentral `meshrelay.js`].

#### Script Execution
```
POST   /api/v1/scripts/                        # Create script
GET    /api/v1/scripts/                         # List scripts
POST   /api/v1/agents/{agent_id}/execute/       # Execute script on agent
GET    /api/v1/agents/{agent_id}/history/       # Get execution history
```

Script model: `shell` (powershell/cmd/python/shell/nushell/deno), `script_body`, `args` (array), `env_vars` (array), `default_timeout`, `supported_platforms` (array), `run_as_user`. Execution returns: `retcode`, `stdout`, `stderr`, `execution_time`. Real-time script output streams over NATS [source: Tactical RMM `scripts/models.py`, `agent/agent.go`].

#### A2A Endpoints
```
GET    /.well-known/agent-card.json             # Agent Card discovery
POST   /a2a/jsonrpc                            # JSON-RPC 2.0 endpoint (SendMessage, GetTask, ListTasks, CancelTask, SubscribeToTask)
POST   /a2a/grpc                               # gRPC endpoint
GET    /a2a/rest/tasks                          # REST: list tasks
GET    /a2a/rest/tasks/{task_id}                # REST: get task
POST   /a2a/rest/tasks                          # REST: send message (create task)
```

#### Automation Policies
```
POST   /api/v1/policies/                       # Create policy
GET    /api/v1/policies/                        # List policies
PATCH  /api/v1/policies/{policy_id}/            # Update policy
GET    /api/v1/policies/{policy_id}/agents/     # Get agents affected by policy
POST   /api/v1/policies/{policy_id}/tasks/      # Create automated task under policy
```

### Real-Time Event Model

Events flow through NATS JetStream subjects with consumer groups:
- `agents.{agent_id}.checks` — check result events
- `agents.{agent_id}.alerts` — alert state transitions
- `agents.{agent_id}.patches` — patch status changes
- `agents.{agent_id}.tasks` — task execution events
- `system.policy.updated` — policy change notifications (fan-out to affected agents)

The event processor consumes these subjects, updates materialized views for the REST API, and bridges to the A2A gateway for agent-triggered workflows. Time-bounded data retention follows per-type pruning: `check_history_prune_days` (30), `resolved_alerts_prune_days`, `agent_history_prune_days` (60), `report_history_prune_days` [source: Tactical RMM `core/models.py`, `core/tasks.py`].

---

## Secret Management Integration

### Architecture

Secret management follows a provider-abstracted pattern where the RMM platform **never stores credentials in its primary database**. Instead, it delegates to dedicated secret management backends via a pluggable interface.

### Supported Backends

#### HashiCorp Vault
- Integration via Vault's KV Secrets Engine (v2) with versioning and rollback
- Authentication: AppRole (machine-to-machine), Kubernetes auth (container deployments), JWT auth (agent identity)
- Secret paths: `openagentplatform/{client_id}/{site_id}/{agent_id}/credentials`
- Dynamic secrets: Vault generates short-lived database credentials and cloud API tokens on demand
- Audit: Vault's audit logging captures all secret access with request IDs traceable to RMM operations
- Policies: Vault policies map to RMM Client/Site/Agent hierarchy, ensuring agents can only access secrets within their scope

#### Infisical
- Integration via Infisical SDK (Python/Node)
- Authentication: Machine identity via Client ID/Secret with scoped access
- Secret paths: `projects/{environment}/credentials/{scope}`
- Key rotation: Infisical's automatic secret rotation for supported services
- Folders and inheritance map naturally to the Client > Site > Agent hierarchy
- Advantages: Developer-friendly UI, native GitOps integration, simpler setup than Vault

### Implementation Patterns

1. **Secret Reference Pattern:** The RMM database stores only a secret reference (`backend_type` + `path`), never the secret value. Example: `{"backend": "vault", "path": "openagentplatform/acme/ny/agent-042/ssh_key", "version": 5}`. At runtime, the agent orchestration service resolves the reference by calling the backend.

2. **Agent Credential Injection:** When an LLM agent needs credentials to execute a remote operation (SSH, API call), the orchestration service fetches the secret from the backend and injects it as an environment variable or file mount into the agent's execution context. The credential is never logged, never stored in task output, and is revoked (if dynamic) after task completion.

3. **A2A Authentication:** A2A agents authenticate using the protocol's built-in auth schemes (bearer tokens, mutual TLS, or OAuth 2.1). Tokens and certificates are managed by the secret backend and rotated automatically. The A2A gateway validates credentials on every request.

4. **Script Credential Safety:** Scripts execute as SYSTEM on endpoints [source: Tactical RMM `agent/agent.go` — confirmed no sandboxing]. To mitigate this risk, secrets are **never** passed as script arguments or environment variables on the endpoint. Instead, the server-side orchestration handles authenticated operations and streams results to the endpoint. When endpoint-local credentials are unavoidable (e.g., domain admin for patch installation), they are fetched just-in-time and the credential reference is audited.

5. **MCP Authorization:** MCP tools requiring authentication use OAuth 2.1 with Protected Resource Metadata (RFC 9728), dynamic client registration (RFC 7591), and PKCE [source: https://modelcontextprotocol.io/specification/2025-11-25/architecture]. The platform's MCP server validates tokens before executing privileged operations.

6. **Hierarchy-Based Access:** Secret access follows the same Client > Site > Agent hierarchy as policy propagation. An agent can access secrets at its level and below, but never above. This mirrors Vault/Infisical's own scoping mechanisms.

---

## Open-Core Commercial Strategy

### License Choice: Business Source License (BSL 1.1)

BSL 1.1 grants production use rights but reserves specific use cases (multi-tenant SaaS, managed hosting) for the copyright holder. After a change date (typically 3-4 years), the code automatically converts to Apache 2.0. This model is used by MariaDB, CockroachDB, and TimescaleDB.

**Rationale over alternatives:**
- **AGPL:** Too restrictive for enterprise adoption; requires contributing back even for SaaS use, which deters adoption.
- **Apache 2.0 + CLA:** Requires a Contributor License Agreement, which is administratively burdensome and community-hostile.
- **Elastic License:** Similar to BSL but more complex; BSL is simpler and has broader industry acceptance.
- **SSPL:** Not OSI-recognized; creates legal ambiguity.
- **BSL 1.1** strikes the right balance: open for self-hosted use, protected against cloud providers offering it as a managed service without contributing.

### Tiering

#### Community Edition (BSL 1.1, free)
- Single-tenant deployment (one RMM instance per installation)
- Up to 250 managed endpoints
- Core RMM: checks, patch management, script execution, remote access, asset inventory
- Up to 3 LLM agent processes (any framework)
- A2A protocol support (local network only, no relay)
- Basic alerting and policy automation
- HashiCorp Vault integration (self-hosted)
- Community forum support

#### Professional Edition (commercial license, paid)
- Multi-tenant deployment (MSP mode)
- Unlimited managed endpoints
- Up to 25 LLM agent processes with framework mixing
- Managed A2A relay service (cross-network agent discovery and delegation)
- Enterprise reporting with scheduled PDF/HTML exports and cross-data aggregation
- A2A task audit trail and compliance reporting
- Both Vault and Infisical integration
- RBAC with MFA and SSO (SAML, OIDC)
- Priority support with SLA

#### Enterprise Edition (commercial license, premium)
- All Professional features
- Unlimited LLM agent processes
- Custom A2A relay with private networking (VPC peering, dedicated relay)
- Agent-assisted change management and CAB workflows
- MCP marketplace with pre-built tool integrations
- Custom framework adapters (bring your own agent framework)
- Dedicated CSM and onboarding
- Air-gapped deployment mode

### Revenue Model

1. **Subscription:** Per-endpoint pricing (tiers at 250/1000/5000/unlimited) + per-agent-process pricing for LLM orchestration.
2. **Managed A2A Relay:** Usage-based pricing for cross-network agent communication (relay bandwidth, task count).
3. **Enterprise Reporting:** Per-report-pack or included in Professional+.
4. **Professional Services:** Custom framework integration, onboarding, and training.

### Open-Core Boundaries

Code that remains proprietary (not in the BSL-licensed repository):
- Multi-tenant SaaS layer (tenant isolation, billing, provisioning)
- Managed A2A relay service (routing, discovery, cross-network federation)
- Enterprise reporting engine (template renderer, scheduled delivery, cross-tenant aggregation)
- SSO/RBAC extensions beyond basic auth

All agent framework wrappers, core RMM functionality, A2A client/server implementation, and single-tenant deployment are open-source under BSL 1.1.

---

## Technology Stack Recommendations

### Backend
- **Language:** Python 3.12+ (primary API and orchestration) + Go (high-performance agent binary, NATS microservices)
- **Web Framework:** Django + Django REST Framework (proven by Tactical RMM; rich admin, ORM, and ecosystem)
- **Async Layer:** FastAPI for A2A gateway and streaming endpoints (native async/await, WebSocket support)
- **ORM:** Django ORM for the RMM data model; SQLAlchemy for reporting engine (complex cross-model queries)
- **Task Queue:** Celery with Redis broker for background jobs (pruning, scheduled reports, patch scan orchestration)

### Messaging
- **NATS JetStream:** Primary message bus for agent communication. Chosen for:
  - Lightweight Go client (ideal for endpoint agent binary)
  - Per-agent subject subscription pattern (agent subscribes to its `agentID`)
  - Built-in persistence and replay (JetStream)
  - Lower operational overhead than Kafka at RMM scale
- **Protocol:** msgpack for agent-server communication (following Tactical RMM's pattern), JSON for server-to-server

### Database
- **PostgreSQL 16:** Primary data store (JSONFields for disks/services/wmi_detail, ArrayFields for supported platforms/categories, rich querying for reporting)
- **Redis 7:** Celery broker, session cache, API rate limiting, agent process pool state
- **TimescaleDB (extension):** Time-series storage for CheckHistory, AlertHistory, and metric aggregation

### LLM Agent Frameworks
- **LangGraph:** `langgraph` + `langgraph-supervisor` + `langgraph-swarm` Python packages
- **CrewAI:** `crewai[all]` package with `a2a` optional extra (native A2A support)
- **AutoGen:** `autogen-agent-chat` Python package
- **Semantic Kernel:** `semantic-kernel` Python + .NET SDK (for MAF A2A hosting)
- **OpenAI Agents SDK:** `openai-agents` Python package
- **Anthropic Claude Agent SDK:** `anthropic` Python SDK with tool-use agent pattern

### Protocol Support
- **A2A:** Custom Python implementation of A2A spec (JSON-RPC 2.0 over HTTP/SSE, gRPC via `grpcio`, REST+JSON). Proto compilation from official `spec/a2a.proto`.
- **MCP:** `mcp` Python SDK (Streamable HTTP transport, OAuth 2.1 auth)

### Secret Management
- **HashiCorp Vault:** `hvac` Python client library, AppRole/Kubernetes auth
- **Infisical:** `infisical-sdk` Python package

### Frontend
- **React 19 + TypeScript:** Single-page application
- **TanStack Query:** Server state management
- **TanStack Router:** File-based routing
- **Shadcn/ui + Tailwind CSS:** Component library
- **xterm.js:** Terminal emulator for remote shell
- **noVNC:** Remote desktop viewer

### Infrastructure
- **Container Runtime:** Docker + Kubernetes (with Helm chart)
- **CI/CD:** GitHub Actions
- **Observability:** OpenTelemetry (traces, metrics, logs) + Grafana/Prometheus/Loki stack
- **Secret Injection:** Kubernetes Secrets Store CSI Driver with Vault/Infisical provider

---

## Risks and Mitigations

| Risk | Severity | Description | Mitigation |
|---|---|---|---|
| **LLM Agent Hallucination in Production Operations** | Critical | An LLM agent could generate incorrect remediation scripts, approve risky patches, or take destructive actions on endpoints. Unlike a chatbot, RMM agents control real infrastructure. | Defense-in-depth: (1) All agent-proposed actions go through a human approval gate before execution (A2A `INPUT_REQUIRED` state); (2) Agent actions are scoped by RBAC policies that limit which operations an agent can perform; (3) Dry-run mode: agents propose changes but do not execute them until explicitly approved; (4) Audit trail: every agent action is logged with full context (prompt, response, decision reasoning, artifacts). |
| **A2A Protocol Maturity** | High | A2A is a relatively new protocol (v1.0.0 released March 2026). Breaking changes are possible. Interoperability between framework implementations is unproven at scale. | (1) Thin abstraction: the A2A integration is isolated in a gateway service, so protocol changes require updates in only one place; (2) Pin to specific A2A spec version per release; (3) Integration tests against the official A2A test suite; (4) Support multiple protocol bindings (JSON-RPC, gRPC, REST) to avoid dependency on any single transport. |
| **Agent Process Cost and Latency** | High | Maintaining warm LLM agent processes is expensive (GPU memory, API token costs). Cold starts add 5-15s latency. At RMM scale (thousands of endpoints), the cost of agent-assisted triage for every alert is prohibitive. | (1) Tiered response: simple checks use deterministic policies; agents are invoked only for complex triage (multi-signal alerts, novel failure patterns); (2) Process pooling with LRU eviction; (3) Cost caps per endpoint per month; (4) Hybrid model: small local models for classification, cloud LLMs for complex reasoning. |
| **No Script Sandboxing on Endpoints** | High | Tactical RMM confirms scripts run as SYSTEM with no sandboxing. An LLM-generated script executed with SYSTEM privileges on an endpoint is a significant attack surface. | (1) Server-side review: agent-proposed scripts are stored as draft and require human approval before execution; (2) Runtime restrictions: deny list of dangerous commands (`Format`, `Remove-Item -Recurse` on system paths, etc.); (3) Future: explore Deno runtime with restrictive `--allow-none` default permissions (Deno's permission model is the only capability-based sandbox among supported runtimes); (4) Network isolation: endpoint agents run in a restricted network segment. |
| **Policy-Agent Conflict** | Medium | LLM agents may make decisions that conflict with enforced RMM policies (e.g., an agent ignores a patch approval policy). | (1) Enforced policies always take precedence over agent decisions (mirroring Tactical RMM's enforcement model where policy overrides are discarded); (2) Agents operate in an advisory capacity for enforced policies; (3) Agent actions that would modify enforced policies are blocked at the API level. |
| **Multi-Framework Complexity** | Medium | Supporting 6+ agent frameworks creates significant maintenance burden. Framework APIs change frequently. | (1) Thin adapter pattern: each wrapper is <500 lines of translation code; (2) Common test suite across all adapters; (3) Semantic versioning aligned with framework releases; (4) Community-contributed adapters for niche frameworks. |
| **Relay Architecture Latency** | Medium | Server-mediated relay adds latency for remote access. Cross-region relay is particularly slow. | (1) WebRTC P2P optimization for latency-sensitive sessions (following MeshCentral's pattern of progressive enhancement); (2) Regional relay server deployment; (3) Relay connection pooling. |
| **BSL License Adoption Resistance** | Low-Medium | Some organizations and Linux distributions reject BSL-licensed software. | (1) Clear change date (3-4 years) with automatic Apache 2.0 conversion; (2) Transparent governance: public roadmap, community advisory board; (3) Contributor license agreement that grants patent license to contributors; (4) Active marketing of the time-limited nature of restrictions. |

---

## References

| # | Title | URL | Relevance |
|---|---|---|---|
| 1 | Tactical RMM Source Code | https://github.com/amidaware/tacticalrmm | Primary reference for RMM data models (Check, Agent, Policy, WinUpdate, AutomatedTask, Script), dual-transport architecture (Django REST + NATS), and agent check-in protocol |
| 2 | Tactical RMM Agent Source | https://github.com/amidaware/rmmagent | Agent-side NATS subscription pattern, Func dispatch table, multi-runtime script execution, CmdV2 streaming, MeshCentral integration |
| 3 | MeshCentral Source Code | https://github.com/Ylianst/MeshCentral | WebSocket relay architecture (`meshrelay.js`), desktop multiplexing (`meshdesktopmultiplex.js`), MQTT broker (`mqttbroker.js`), optional WebRTC P2P |
| 4 | A2A Protocol Specification | https://a2a-protocol.org/latest/specification/ | Three-layer protocol architecture, TaskState lifecycle, Message/Artifact/Part data model, JSON-RPC/gRPC/REST bindings |
| 5 | MCP (Model Context Protocol) Specification | https://modelcontextprotocol.io/specification/2025-11-25/architecture | Client-host-server architecture, Streamable HTTP transport, OAuth 2.1 authorization, tool/resource/prompt primitives |
| 6 | LangGraph Documentation | https://langchain-ai.github.io/langgraph/ | StateGraph + MessagesState shared state model, supervisor/swarm patterns, compilation with checkpointer and HITL, LangSmith Agent Server A2A endpoint |
| 7 | CrewAI Source Code | https://github.com/crewAIInc/crewAI | Sequential/Hierarchical processes, Flows with @listen/@router, native A2A support via `crewai.a2a` module with `a2a-sdk` |
| 8 | Semantic Kernel Source Code | https://github.com/microsoft/semantic-kernel | ChatCompletionAgent, HandoffOrchestration, Process Framework, ChatHistoryAgentThread, Microsoft Agent Framework successor with A2A hosting |
| 9 | HashiCorp Vault Documentation | https://developer.hashicorp.com/vault/docs | KV v2 secrets engine, AppRole authentication, dynamic secrets, audit logging, policy-based access control |
| 10 | Infisical Documentation | https://infisical.com/docs | Machine identity authentication, secret rotation, folder-based scoping, SDK integration patterns |
