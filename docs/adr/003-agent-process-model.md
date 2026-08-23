# ADR 003: Agents run as supervised subprocesses

- **Status:** Accepted
- **Date:** 2026-08-22

## Context

Agent runtimes may execute different tools, consume significant resources, fail independently, or become unresponsive. Running all agents in the server process would share memory and failure domains. Requiring a container per invocation would improve isolation but impose operational overhead and slow local workflows.

The control plane needs explicit ownership of startup, cancellation, output capture, timeout handling, and cleanup.

## Decision

Run each local agent invocation as a child process supervised by the platform. The supervisor creates the process from an allow-listed executable and structured arguments, propagates cancellation, captures bounded stdout and stderr, enforces configured time and resource limits where the operating system supports them, and waits for process termination.

Use process groups or equivalent platform facilities so cancellation and shutdown terminate descendants rather than only the direct child. Do not invoke agents through a shell. Pass only explicitly allow-listed environment values and working directories.

Persist task state outside the child process. A process exit, timeout, or signal maps to an explicit terminal task result, and cleanup is idempotent.

## Consequences

- Agent crashes are isolated from the control-plane process.
- Independent cancellation and per-agent resource accounting become possible.
- Startup is lighter than mandatory per-invocation containers.
- Host subprocesses are not a complete security sandbox; untrusted agents require stronger isolation such as containers or microVMs.
- The supervisor must handle platform differences, output backpressure, orphan prevention, and graceful shutdown.

## Alternatives considered

- **In-process plugins:** Lower startup overhead, but share memory and failure domains and make cancellation unreliable.
- **Container per invocation:** Stronger isolation, but adds runtime dependencies and startup cost; retained as a deployment option for untrusted workloads.
- **Long-lived worker daemon per agent:** Can reduce startup latency, but increases lifecycle and state-recovery complexity.
