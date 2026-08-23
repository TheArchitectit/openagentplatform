# A2A Task Manager

> **Phase:** 2 (A2A + Agents)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/A2A_PROTOCOL.md` §3, §4, §12
> **App Path:** `a2a/manager/statemachine.go`, `a2a/models/models.go`

---

## Description

The Task Manager is the persistence and lifecycle authority for A2A Tasks. Every
task that enters the gateway — whether from an external client, an internal
service, or the Event-to-Task Bridge — is created, transitioned, and terminated
here.

It owns four record types (`Task`, `Artifact`, `Message`, `CostRecord`) stored
in PostgreSQL via pgx, and enforces a strict 8-state lifecycle state machine
with 14 valid transitions. Invalid transitions are rejected rather than
silently coerced, so a task's history is always a legal path through the state
graph.

Concurrency is handled with optimistic locking on a `version` column: two
agents racing to transition the same task cannot both win, and the loser
receives a conflict error instead of clobbering state. This matters because
tasks are touched concurrently by protocol handlers, the subscriber hub, the
HITL service, and timeout sweepers.

A key semantic rule the Task Manager enforces: **Messages SHOULD NOT deliver
task outputs — results MUST be returned using Artifacts.** Messages are
conversational; Artifacts are the deliverable.

## User Story

**As** an agent framework adapter or an operator tracking work,
**I want** task state to be durable, auditable, and governed by a state machine
that rejects illegal transitions,
**so that** I can trust that a task marked COMPLETED really finished, that a
canceled task cannot resurrect itself, and that concurrent updates never
corrupt the record.

---

## Requirements

### 1. Task States

1.1. The Task Manager MUST implement exactly 8 states:

| State | Category | Meaning |
|-------|----------|---------|
| `SUBMITTED` | Initial | Task created, awaiting agent pickup |
| `WORKING` | Active | Agent is processing |
| `COMPLETED` | Terminal (success) | All artifacts finalized |
| `FAILED` | Terminal | Unrecoverable error |
| `CANCELED` | Terminal | User or system canceled |
| `INPUT_REQUIRED` | Interrupted | LLM requests human approval |
| `AUTH_REQUIRED` | Interrupted | Additional auth needed |
| `REJECTED` | Terminal | Agent declined the task |

1.2. Newly created tasks MUST begin in `SUBMITTED`.

1.3. Terminal states MUST be final, with the single documented exception of
`FAILED → WORKING` via `retry_task` for retriable errors.

### 2. State Transitions

2.1. The Task Manager MUST permit exactly these transitions and reject all
others:

| From | Trigger | To | Condition |
|------|---------|----|-----------|
| `SUBMITTED` | `agent_accept` | `WORKING` | Agent acknowledges task |
| `SUBMITTED` | `reject_task` | `REJECTED` | Agent declines |
| `WORKING` | `complete_task` | `COMPLETED` | All artifacts finalized |
| `WORKING` | `require_input` | `INPUT_REQUIRED` | LLM requests human approval |
| `WORKING` | `require_auth` | `AUTH_REQUIRED` | Additional auth required |
| `WORKING` | `fail_task` | `FAILED` | Unrecoverable error |
| `WORKING` | `cancel_task` | `CANCELED` | User cancels |
| `INPUT_REQUIRED` | `resume_task` | `WORKING` | Human provides input |
| `INPUT_REQUIRED` | `cancel_task` | `CANCELED` | Human rejects |
| `INPUT_REQUIRED` | `timeout` (24h) | `CANCELED` | Input not provided in time |
| `AUTH_REQUIRED` | `provide_auth` | `WORKING` | Auth credentials supplied |
| `AUTH_REQUIRED` | `cancel_task` | `CANCELED` | User cancels |
| `FAILED` | `retry_task` | `WORKING` | Retriable error |

2.2. An attempted invalid transition MUST return a structured error naming the
current state, the attempted trigger, and the set of legal triggers. It MUST
NOT mutate the task.

2.3. Every transition MUST be covered by unit tests, including explicit tests
that invalid transitions are rejected.

2.4. Transitions MUST emit an event to the SubscriberHub so SSE and gRPC
streaming subscribers observe state changes without polling.

### 3. Timeout Handling

3.1. Tasks in `INPUT_REQUIRED` MUST auto-cancel after 24 hours without a
response.

3.2. Timeout sweeping MUST be driven by the persisted `expires_at` value, not
by an in-memory timer, so a gateway restart does not lose or duplicate
expiries.

### 4. Persistence Model

4.1. The Task Manager MUST persist to PostgreSQL via pgx across these tables:

| Table | Key Columns | Indexes |
|-------|-------------|---------|
| `a2a_tasks` | id, context_id, agent_id, state (enum), message (jsonb), metadata (jsonb), version (int4), created_at, updated_at | `(agent_id, state)`, `(context_id)` |
| `a2a_artifacts` | id, task_id (FK), name, description, parts (jsonb), mime_type, created_at | `(task_id)` |
| `a2a_messages` | id, task_id (FK), role, parts (jsonb), created_at | `(task_id, created_at)` |
| `a2a_cost_records` | id, task_id (FK), model, input_tokens, output_tokens, cost_usd, created_at | `(task_id)`, `(created_at)` |

4.2. Artifacts and Messages MUST cascade from their parent task so orphan rows
cannot accumulate.

4.3. Schema changes MUST ship as ordered SQL migrations, never as
ad-hoc alterations.

### 5. Optimistic Concurrency

5.1. `a2a_tasks` MUST carry an integer `version` column, incremented on every
update.

5.2. Updates MUST include the expected version in the WHERE clause. A zero-row
update MUST be surfaced as a conflict error, never treated as success.

5.3. Callers receiving a conflict MUST be able to re-read and retry; the Task
Manager MUST NOT silently retry on their behalf, because the correct retry
policy depends on the trigger.

### 6. Artifacts vs Messages Semantics

6.1. Task outputs MUST be delivered as Artifacts.

6.2. Messages MUST be treated as conversational history and MUST NOT be relied
upon to carry deliverable results.

6.3. `complete_task` MUST require that the task's artifacts are finalized.

### 7. Subscribe and Cancel Operations

7.1. `SubscribeToTask` MUST stream all subsequent state transitions, artifact
additions, and message additions for a task until a terminal state is reached
or the subscriber disconnects.

7.2. A subscriber attaching to an already-terminal task MUST receive the
terminal state immediately and then a clean stream close, not an indefinite
hang.

7.3. `CancelTask` MUST be idempotent: canceling an already-`CANCELED` task MUST
succeed and return the existing task.

7.4. `CancelTask` on a task in a non-cancelable terminal state (`COMPLETED`,
`FAILED`, `REJECTED`) MUST fail with a structured error.

### 8. Listing and Query

8.1. `ListTasks` MUST support filtering by `agent_id` and `state`, served by the
`(agent_id, state)` index.

8.2. Tasks sharing a `context_id` MUST be retrievable as a group, served by the
`(context_id)` index, so multi-turn conversations can be reconstructed.

8.3. List operations MUST be paginated. Unbounded result sets MUST NOT be
returned.

### 9. Operational Requirements

9.1. On graceful shutdown, in-flight task state MUST be persisted before exit.

9.2. A runbook MUST exist for the "A2A Task stuck in WORKING state" failure
mode, covering diagnosis and safe forced transition.
