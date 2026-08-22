# A2A Framework Adapters

> **Phase:** 2 (A2A + Agents)
> **STATUS: PLANNED**
> **Source:** `docs/architecture/A2A_PROTOCOL.md` §6; `docs/architecture/ROADMAP_AND_SPRINTS.md` §5
> **App Path:** `py/` (Python adapter service)

---

## Description

Framework adapters are the translation layer between the A2A protocol and the
six agent frameworks the platform supports: LangGraph, CrewAI, AutoGen,
Semantic Kernel, OpenAI, and Anthropic. Each adapter presents an A2A-compliant
face to the gateway and speaks its framework's native idiom on the other side.

This isolation is the whole point. The gateway, task manager, and registry never
import a framework SDK; frameworks never learn about A2A. When a vendor ships a
breaking SDK change — rated a *high-likelihood* risk in the project risk
register (R3) — the damage is contained to one adapter module.

Adapters run in the Python adapter service, reached from the Go gateway over an
HTTP bridge at the endpoint declared in each agent's card
(`http://adapter-{framework}:8090/a2a`). Python is the right host language here
because that is where every one of these frameworks actually lives.

> **Note:** Phase 2 A2A currently exhibits a three-way contract divergence
> between the Go gateway, the Python adapter service, and the React frontend.
> Adapter work MUST begin by pinning one authoritative contract — the generated
> A2A protobuf types — and conforming all three sides to it.

## User Story

**As** a platform operator with agents built on different frameworks,
**I want** each framework wrapped behind a uniform A2A interface,
**so that** I can route a task to a CrewAI crew or a LangGraph graph without
changing anything in the gateway, and so that an upstream SDK break costs me one
adapter rather than the platform.

---

## Requirements

### 1. Supported Frameworks

1.1. Six adapters MUST be implemented:

| Framework | Card `framework` value | Nature |
|-----------|------------------------|--------|
| LangGraph | `langgraph` | Graph-based agent orchestration |
| CrewAI | `crewai` | Multi-agent crew delegation |
| AutoGen | `autogen` | Conversational multi-agent |
| Semantic Kernel | `semantickernel` | Plugin/planner-based |
| OpenAI | `openai` | Direct provider integration |
| Anthropic | `anthropic` | Direct provider integration |

1.2. Each adapter MUST register an Agent Card declaring its framework,
endpoint, capabilities, and skill tags.

1.3. Adding a seventh framework MUST require no changes to the gateway, task
manager, or registry — only a new adapter module and a registered card.

### 2. Adapter Contract

2.1. Every adapter MUST implement a common interface covering: accept a task,
report progress, emit artifacts, request human input, report cost, and
terminate with a final state.

2.2. Adapters MUST accept tasks in the canonical A2A `Task` shape and MUST NOT
require framework-specific request fields at the A2A boundary.

2.3. Adapters MUST return results as **Artifacts**, never as Messages, per the
A2A semantic rule that Messages do not carry task outputs.

2.4. Adapters MUST drive task state only through legal Task Manager
transitions; an adapter MUST NOT write task state directly to the database.

2.5. Adapter-specific configuration (model endpoint, temperature, tool
allowlist) MUST be supplied as configuration, not hardcoded, so a provider
endpoint change is a config edit.

### 3. Python Bridge

3.1. The Go gateway MUST communicate with the Python adapter service over an
HTTP bridge; adapters MUST be reachable at the endpoint declared in their
Agent Card.

3.2. Bridge payloads MUST use the generated A2A types as the authoritative
contract. Hand-maintained parallel schemas on either side MUST NOT be
introduced.

3.3. The bridge MUST propagate the correlation/context ID so a task can be
traced across the Go/Python boundary in logs and traces.

3.4. Bridge calls MUST enforce a timeout. A hung adapter MUST cause the task to
transition to `FAILED` with a diagnostic message, never to hang in `WORKING`
indefinitely.

3.5. Bridge transport failures MUST be distinguishable from adapter-reported
task failures, because the former is retriable and the latter may not be.

### 4. Streaming and Progress

4.1. Adapters declaring the `streaming` capability MUST emit incremental
progress events that the gateway can relay to SSE and gRPC subscribers.

4.2. Adapters that do not declare `streaming` MUST still report a terminal
state; the gateway MUST NOT assume streaming is universal.

4.3. Streaming output MUST be chunked and flushed incrementally, not buffered
until completion.

### 5. Human-in-the-Loop Integration

5.1. An adapter needing human approval MUST trigger `require_input`, moving the
task to `INPUT_REQUIRED`, rather than blocking on a framework-internal prompt.

5.2. On approval, the adapter MUST resume execution with the human-supplied
context.

5.3. On rejection or 24-hour timeout, the adapter MUST abandon the run and
release any held resources.

### 6. Cost Reporting

6.1. Each adapter MUST report input tokens, output tokens, and model name for
every LLM call it makes.

6.2. Cost MUST be reported via the `ReportCost` operation so it is attributed
to the correct task.

6.3. Adapters MUST NOT compute USD cost themselves; pricing MUST be applied
centrally so a price change is a single configuration update.

### 7. Isolation and Failure Containment

7.1. Each adapter MUST be independently deployable and independently
versionable.

7.2. A crash or dependency failure in one adapter MUST NOT affect other
adapters or the gateway.

7.3. Model endpoints MUST be configurable, and a fallback provider MUST be
configurable, per the R3 mitigation for upstream LLM API changes.

7.4. Adapters MUST NOT share mutable in-process state with each other.

### 8. Secret Handling

8.1. Adapters MUST obtain provider API keys through the secret management
pipeline, never from committed configuration or source.

8.2. Credentials MUST NOT be written to logs, error messages, artifacts, or
task metadata.

### 9. Testing Requirements

9.1. Each adapter MUST have unit tests with the framework SDK mocked.

9.2. Each adapter MUST have an integration test performing a full round trip:
task submitted at the gateway, executed by the adapter, artifact returned,
terminal state reached.

9.3. A contract test MUST assert that all six adapters satisfy the common
adapter interface identically, so drift between adapters is caught in CI.

9.4. Tests MUST cover the timeout and transport-failure paths in §3.4 and §3.5,
not only the success path.
