# Event-to-Task Bridge

> **Phase:** 2 (A2A + Agents)
> **STATUS: PLANNED**
> **Source:** `docs/architecture/A2A_PROTOCOL.md` §3, §8
> **App Path:** `a2a/bridge/event_bridge.go`, `a2a/bridge/dedup.go`, `a2a/bridge/ratelimit.go`

---

## Description

The Event-to-Task Bridge is the seam between deterministic RMM automation and
LLM-driven agent work. It listens for RMM events on NATS and converts eight
event types into A2A Tasks tagged with the skill required to handle them — a
failing check becomes a task tagged `alert.triage`, a newly available patch
becomes `patch.planning`.

This is what makes the platform "agent-first" rather than an RMM with a chatbot
attached. The RMM continues to detect problems deterministically; the bridge
decides which of those detections are worth an agent's attention and hands them
over in the A2A protocol's own vocabulary, letting the gateway router pick
whichever registered agent is best equipped.

The bridge's hard constraint is that it must not amplify. RMM events arrive in
bursts — a network partition can fail thousands of checks in seconds — and each
task it creates costs LLM tokens and real money. Deduplication, rate limiting,
and spend awareness are therefore core requirements, not enhancements.

## User Story

**As** an operator whose fleet generates more alerts than a human can triage,
**I want** RMM events automatically converted into agent tasks tagged with the
right skill,
**so that** an LLM agent triages the alert, assesses the patch risk, or debugs
the script failure before a human is involved — without a single incident storm
generating thousands of duplicate agent tasks and an unbounded bill.

---

## Requirements

### 1. Event Type Mappings

1.1. The bridge MUST convert these 8 RMM event types to A2A skill tags:

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

1.2. Mappings MUST be configurable, so an operator can disable agent delegation
for an event type without a code change.

1.3. An unrecognized event type MUST be logged and ignored, never converted to
an untagged task that no agent can handle.

1.4. Adding a ninth event type MUST require only a new mapping entry, not
changes to the gateway or task manager.

### 2. Event Consumption

2.1. The bridge MUST consume RMM events from NATS.

2.2. Consumers MUST be durable, so events published while the bridge is down are
processed on recovery rather than lost.

2.3. Acknowledgement MUST occur only after the resulting task is durably
persisted, so a crash mid-conversion results in redelivery rather than a
dropped event.

2.4. Because delivery is at-least-once, conversion MUST be idempotent — the same
event delivered twice MUST NOT produce two tasks.

### 3. Task Construction

3.1. Each created task MUST carry the skill tag from its event mapping.

3.2. The task message MUST include sufficient context for an agent to act:
event type, source agent, affected resource, severity, timestamps, and the
event payload.

3.3. The originating event ID and correlation ID MUST be recorded in task
metadata, so a task is traceable back to the event that caused it.

3.4. Tasks MUST be created via the Task Manager's normal creation path, entering
in `SUBMITTED` state. The bridge MUST NOT write task rows directly.

3.5. The bridge MUST NOT select a target agent itself; routing MUST be delegated
to the GatewayRouter's skill-match scoring.

3.6. Secret values MUST NOT be copied into task messages or metadata; references
MUST be used instead.

### 4. Deduplication

4.1. The bridge MUST deduplicate events so a single underlying condition does
not produce repeated tasks.

4.2. Deduplication MUST reuse the RMM alert `dedup_key` where available, so
bridge and alert-engine deduplication agree.

4.3. A configurable suppression window MUST prevent re-creating a task for the
same dedup key while a prior task for it is still active.

4.4. When a task for a condition is already open, a recurring event MUST update
or annotate the existing task rather than creating a new one.

### 5. Rate Limiting and Storm Protection

5.1. Task creation MUST be rate-limited per event type and in aggregate.

5.2. An event storm MUST NOT produce unbounded task creation. Excess events MUST
be shed or aggregated, and the shedding MUST be logged and counted so the gap is
visible rather than invisible.

5.3. Where an event type permits it, a burst affecting many endpoints SHOULD be
aggregated into one task covering the affected set rather than one task per
endpoint.

5.4. A circuit breaker MUST halt task creation when the downstream A2A gateway
is failing, rather than queueing unboundedly.

### 6. Cost Control

6.1. The bridge MUST respect configured per-endpoint monthly spend caps before
creating a task.

6.2. When a spend cap is reached, task creation for that endpoint MUST stop and
MUST raise an operator-visible alert. Silent suppression MUST NOT occur.

6.3. Estimated cost impact MUST be observable per event type, so operators can
see which mappings drive spend.

### 7. Filtering and Scoping

7.1. Delegation MUST be filterable by severity, so low-severity events need not
create agent tasks.

7.2. Delegation MUST be scopable by organization, client, site, agent, and tag.

7.3. Filters MUST fail closed: a misconfigured filter MUST result in no
delegation rather than delegating everything.

### 8. Observability

8.1. The bridge MUST expose metrics: events consumed, tasks created, events
deduplicated, events rate-limited, events filtered, and conversion failures —
broken down by event type.

8.2. Conversion failures MUST be logged with the event ID and reason.

8.3. Every created task MUST be traceable to its source event, and every
suppressed event MUST be attributable to a specific suppression reason
(dedup, rate limit, filter, or spend cap).

### 9. Failure Handling

9.1. A task-creation failure MUST NOT ack the event; the event MUST be retried.

9.2. Retries MUST use bounded exponential backoff.

9.3. Events failing repeatedly MUST be routed to a dead-letter destination after
a maximum delivery count, and MUST NOT block the consumer indefinitely.

9.4. Dead-lettered events MUST be inspectable and replayable by an operator.

### 10. Testing Requirements

10.1. Each of the 8 mappings MUST have a test asserting the correct skill tag
and task payload.

10.2. Tests MUST cover idempotency under duplicate delivery, deduplication
within the suppression window, rate limiting under a simulated storm, spend-cap
enforcement, filter fail-closed behavior, and dead-lettering.

10.3. An integration test MUST assert the full path: RMM event published → task
created → routed to a capable agent.
