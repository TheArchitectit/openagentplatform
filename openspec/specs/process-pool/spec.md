# ProcessPool

> **Phase:** 2 (A2A + Agents)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/ROADMAP_AND_SPRINTS.md` §5 (Phase 2)
> **App Path:** `py/` (Python adapter service)

---

## Description

The ProcessPool keeps warm agent framework instances ready to accept work.
Cold-starting a LangGraph graph or a CrewAI crew means importing a heavy SDK,
constructing the agent, and loading configuration — hundreds of milliseconds to
seconds of latency paid on every task. For an RMM platform where an agent triages
an alert, that startup cost dominates the actual work.

The pool amortizes it. Instances are created ahead of demand, held warm, checked
out for a task, and returned. A pool of ready workers is a well-understood
pattern; the difficulty here is that these workers execute LLM-driven,
operator-supplied logic, which means they leak state, hang, and crash in ways an
ordinary connection pool does not.

So the pool's real requirements are about containment rather than reuse: an
instance must never carry one task's context into the next task, a hung instance
must be reclaimable, and a crash loop must not silently drain capacity until the
platform stops accepting work.

## User Story

**As** the A2A gateway dispatching a task to a framework adapter,
**I want** a warm, isolated framework instance available immediately and
guaranteed clean of prior task state,
**so that** task latency reflects the work itself rather than SDK startup, and so
that one task's context, credentials, or memory can never leak into another's.

---

## Requirements

### 1. Pool Composition

1.1. A separate pool MUST be maintained per framework, since instances are not
interchangeable across frameworks.

1.2. Minimum warm instances, maximum instances, and target idle count MUST be
configurable per framework.

1.3. Pools MUST pre-warm to the configured minimum at service startup, before
the service reports ready.

1.4. Readiness MUST NOT be signaled until minimum warm capacity exists, so the
gateway does not route tasks into an unwarmed pool.

### 2. Checkout and Return

2.1. Task execution MUST acquire an instance from the pool and return it when
complete.

2.2. Return MUST occur on every exit path — success, failure, timeout, and
cancellation — implemented so that an early return or raised exception cannot
skip it.

2.3. An instance MUST be checked out to at most one task at a time.

2.4. If no instance is available and the pool is at maximum, the request MUST
either queue with a bounded timeout or be rejected with a clear
capacity error. It MUST NOT block indefinitely.

2.5. Checkout MUST be fair enough to avoid starvation under sustained load.

### 3. State Isolation

3.1. An instance returned to the pool MUST be reset to a clean state before
reuse.

3.2. Conversation history, task context, loaded credentials, and
framework-internal memory MUST NOT persist across checkouts.

3.3. If an instance cannot be verifiably reset, it MUST be destroyed and
replaced rather than reused. Reuse of a possibly-dirty instance is a
cross-task data leak.

3.4. Instances MUST NOT share mutable global state within the host process.

### 4. Lifecycle Management

4.1. Instances MUST be health-checked before checkout, and unhealthy instances
MUST be destroyed and replaced.

4.2. Instances MUST be recycled after a configurable maximum number of tasks, to
bound the impact of gradual memory growth in framework SDKs.

4.3. Instances MUST be recycled after a configurable maximum age.

4.4. Idle instances above the target idle count MUST be reaped, subject to the
configured minimum.

4.5. Instance creation failures MUST be retried with backoff and MUST NOT spin
in a tight loop.

### 5. Crash and Hang Handling

5.1. A crashed instance MUST be detected, removed from the pool, and replaced.

5.2. A task holding an instance that exceeds its timeout MUST have the instance
forcibly reclaimed, so a hung framework call cannot permanently consume
capacity.

5.3. Forcible reclamation MUST destroy the instance rather than return it to the
pool, since its state is unknown.

5.4. A sustained crash or replacement-failure rate MUST raise an alert. Silent
capacity drain MUST NOT be possible — the failure mode where the pool shrinks to
zero while appearing healthy is explicitly disallowed.

5.5. Repeated creation failures MUST trip a circuit breaker that fails fast with
a diagnostic, rather than making every task wait for the full timeout.

### 6. Resource Constraints

6.1. Per-instance memory limits MUST be enforceable.

6.2. Total pool resource consumption MUST be bounded by the maximum instance
count, so pool sizing is a predictable capacity decision.

6.3. Instance subprocesses MUST have core dump size set to 0, per risk R8
(subprocess crashes leaking secrets in core dumps).

### 7. Observability

7.1. The pool MUST expose per-framework metrics: warm count, in-use count, idle
count, checkout wait time, checkout failures, instance creation rate, crash
rate, and recycle rate.

7.2. Checkout wait time MUST be tracked as a latency distribution, since it
directly contributes to task latency.

7.3. Pool exhaustion events MUST be logged and counted.

7.4. Metrics MUST be exported via OpenTelemetry, consistent with platform
observability.

### 8. Shutdown

8.1. On graceful shutdown, the pool MUST stop accepting checkouts, wait a
bounded period for in-flight tasks, then destroy all instances.

8.2. Shutdown MUST NOT wait indefinitely for a hung instance.

8.3. All instance subprocesses MUST be terminated on shutdown — orphaned
processes MUST NOT survive the parent service.

### 9. Testing Requirements

9.1. Tests MUST cover: pre-warm at startup, checkout/return cycle, return on
every exit path, state reset between checkouts, exhaustion behavior at maximum,
crash detection and replacement, hang reclamation, and clean shutdown.

9.2. A test MUST assert that task context does not leak between sequential
checkouts of the same instance.

9.3. A test MUST assert the pool does not silently drain to zero capacity under
repeated instance creation failure.
