# Endpoint Agent

> **Phase:** 0 (Foundation) / 1 (Core RMM)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/ENDPOINT_API.md` §4, §5
> **App Path:** `agents/oap-agent/` (Go module)

---

## Description

The `oap-agent` is a single static Go binary installed on every managed device.
It is the platform's presence on the endpoint: it registers itself, heartbeats,
receives commands, executes checks and scripts locally, and reports results.

Its security posture is defined by what it does *not* do. It has no inbound HTTP
port — all communication is outbound. It has no database access. It talks to the
control plane exclusively over NATS JetStream, which gives it reconnect
tolerance and durable command delivery across the flaky networks real endpoints
live on.

The binary is cross-compiled for six OS/arch targets with CGO disabled, so a
single static artifact runs anywhere without runtime dependencies. Child
processes running operator-supplied scripts are constrained by `prlimit` on
Linux and job objects on Windows — a runaway script must not be able to take
down the host it was meant to monitor.

## User Story

**As** an administrator deploying to a mixed Windows/Linux/macOS fleet,
**I want** a single dependency-free binary that registers itself, survives
network interruptions, and executes checks and scripts under strict resource
limits,
**so that** I can manage any endpoint without opening inbound firewall ports and
without a misbehaving script destabilizing a production host.

---

## Requirements

### 1. Cross-Compilation Targets

1.1. The binary MUST be built for six targets:

| OS | Arch | Output |
|----|------|--------|
| windows | amd64 | `oap-agent-windows-amd64.exe` |
| windows | arm64 | `oap-agent-windows-arm64.exe` |
| linux | amd64 | `oap-agent-linux-amd64` |
| linux | arm64 | `oap-agent-linux-arm64` |
| darwin | amd64 | `oap-agent-darwin-amd64` |
| darwin | arm64 | `oap-agent-darwin-arm64` |

1.2. All builds MUST be static with CGO disabled, for maximum portability.

1.3. Windows builds MUST use `-H windowsgui` to run as a tray application.

1.4. A CI test matrix MUST cover Windows 10/11, Ubuntu 20/22/24, and macOS
13/14 on real VMs nightly, per risk R1 (OS updates breaking agent
compatibility).

### 2. Lifecycle State Machine

2.1. Six states MUST be implemented: `NEW`, `REGISTERING`, `REGISTERED`,
`STALE`, `OFFLINE`, `DEREGISTERED`.

2.2. Transitions MUST be exactly:

| From | To | Trigger | Action |
|------|----|---------|--------|
| NEW | REGISTERING | First start | Read UUID from disk, or generate |
| REGISTERING | REGISTERED | Server accepts (201) | Save JWT, start heartbeat, subscribe to commands |
| REGISTERING | STALE | Registration fails (3 retries) | Enter backoff, log error |
| REGISTERED | STALE | 2 consecutive missed heartbeats (60s gap) | Pause command subscription, re-register |
| STALE | REGISTERED | Re-registration succeeds | Resume command subscription |
| STALE | OFFLINE | Re-registration fails 5 times | Long backoff (30s cap), log critical |
| OFFLINE | REGISTERED | Backoff fires, re-registration succeeds | Resume normal operation |
| Any | DEREGISTERED | DELETE endpoint or deregister message | Unsubscribe, close NATS, exit |

2.3. Invalid transitions MUST be ignored rather than applied, and the state
machine MUST be guarded by a mutex for concurrent access.

2.4. Transitions MUST invoke an `onTransition` callback so lifecycle changes can
be logged and reported.

### 3. Identity Persistence

3.1. The agent MUST generate a UUID v4 on first launch and persist it, so
identity survives restarts and upgrades.

3.2. Storage paths MUST be:

| OS | Path |
|----|------|
| Linux | `/var/lib/oap-agent/agent.uuid` |
| macOS | `/var/lib/oap-agent/agent.uuid` |
| Windows | `%PROGRAMDATA%\oap-agent\agent.uuid` |

3.3. The UUID file MUST be written with `0600` permissions (owner read/write
only).

3.4. If the file is absent, a new UUID MUST be generated; the agent MUST NOT
refuse to start.

### 4. Registration

4.1. Registration MUST be performed via `POST /api/v1/agents/register` using an
enrollment token.

4.2. The registration payload MUST report: hostname, FQDN, OS, OS version, arch,
agent version, IP addresses, MAC addresses, tags, and capabilities.

4.3. The response MUST supply the assigned `agent_id`, the per-agent NATS
subject, heartbeat interval, heartbeat stale threshold, JWT expiry, and runtime
config.

4.4. Registration MUST be rate-limited server-side (10/min, burst 5) to prevent
enrollment storms.

### 5. Heartbeat and Liveness

5.1. The agent MUST heartbeat on a server-supplied interval (default 30s over
REST/NATS per binding; 60s for the RMM NATS heartbeat).

5.2. Missing 2 consecutive heartbeats MUST move the agent to `STALE`.

5.3. Heartbeat responses MUST be able to carry a refreshed JWT, so credentials
can rotate without re-enrollment.

### 6. NATS Subscription

6.1. Each agent MUST subscribe to its own dedicated subject:
`oap.endpoint.{agent_id}.commands`.

6.2. Subscription MUST be pull-based with a 1-second batch timeout.

6.3. No queue group MUST be used — each agent is its own consumer and MUST NOT
share command delivery with another agent.

6.4. `MaxAckPending` MUST be 100, providing backpressure if the agent falls
behind.

6.5. `AckWait` MUST be 30 seconds, so unacked commands are redelivered.

6.6. Commands MUST be dispatched by a switch on the message type: `run_check`,
`exec_script`, `reboot`, `patch_scan`, `deregister`.

### 7. Reconnect Strategy

7.1. Reconnection MUST use exponential backoff: 1s, 2s, 4s, 8s, 16s, then 30s
capped.

7.2. Jitter of ±20% MUST be applied to each delay to prevent thundering-herd
reconnects after a broker restart.

7.3. `MaxReconnects` MUST be unlimited (-1) — an agent MUST NOT permanently give
up on the control plane.

7.4. On reconnect the agent MUST: open a new connection with the stored JWT;
request a new JWT if the stored one is expired; re-subscribe to its command
subject; and transition back through `REGISTERING` to `REGISTERED`.

### 8. Check Execution

8.1. Check execution MUST proceed through four phases with these budgets:

| Phase | Work | Budget |
|-------|------|--------|
| Receive | Decode msgpack `CheckDispatch` | < 10 ms |
| Validate | Verify check type, parameters, capability, timeout bounds | < 50 ms |
| Run | Execute check logic, capture output | up to `timeout_sec` |
| Report | Encode `CheckResult`, publish, ack | < 100 ms |

8.2. Results MUST be published to `oap.check.results.{checkID}.{agentID}` with
status `PASS`, `FAIL`, or `ERROR`, plus exit code, output, duration, timestamp,
tags, and correlation ID.

8.3. The agent MUST validate that it has the capability the check requires, and
MUST report `ERROR` rather than attempting an unsupported check.

8.4. The original NATS message MUST be acked only after the result is published,
so a crash mid-check results in redelivery rather than a silently lost check.

### 9. Script Execution

9.1. The agent MUST support these runtimes with these exact invocations:

| Runtime | Command |
|---------|---------|
| Python3 (Unix) | `python3 -I -E -s {script} {args}` |
| Python3 (Windows) | `python -I -E -s {script} {args}` |
| Bash | `/bin/bash --noprofile --norc -e -o pipefail {script} {args}` |
| PowerShell | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File {script} {args}` |
| Node | `node --no-warnings --experimental-vm-modules {script} {args}` |

9.2. Interpreter isolation flags MUST NOT be omitted — `-I -E -s` for Python,
`--noprofile --norc` for Bash, and `-NoProfile -NonInteractive` for PowerShell
prevent host profile and environment contamination.

9.3. Scripts MUST be written to a temp directory with `0600` permissions, and
the temp directory MUST be cleaned up after execution.

9.4. Script content MUST be validated: no null bytes, maximum size 1 MB.

9.5. Arguments MUST be passed as a separate argv slice and MUST NOT be
shell-interpolated, preventing command injection.

9.6. Environment variables MUST be filtered to an allowlist.

9.7. stdout and stderr MUST stream incrementally to the server rather than only
being delivered at completion.

### 10. Resource Constraints

10.1. On Linux, child processes MUST be constrained via `prlimit`
(`SYS_prlimit64`); macOS MUST use `setrlimit` equivalents:

| Resource | Limit | Constant |
|----------|-------|----------|
| CPU time | 300s or script timeout | `RLIMIT_CPU` |
| File size | 100 MB | `RLIMIT_FSIZE` |
| Open files | 256 | `RLIMIT_NOFILE` |
| Address space | 2 GB | `RLIMIT_AS` |
| File locks | 256 | `RLIMIT_LOCKS` |
| Pending signals | 128 | `RLIMIT_SIGPENDING` |
| Message queue | 1 MB | `RLIMIT_MSGQUEUE` |
| Nice priority | +19 (lowest) | `RLIMIT_NICE` |
| Realtime priority | 0 (disabled) | `RLIMIT_RTPRIO` |
| Resident set | 512 MB | `RLIMIT_RSS` |

10.2. Wall-clock timeout MUST be enforced independently via
`context.WithTimeout`, because CPU-time limits do not catch a sleeping process.

10.3. On Windows, equivalent constraints MUST be applied via job objects:
`JOB_OBJECT_LIMIT_PROCESS_MEMORY`, `JOB_OBJECT_LIMIT_JOB_MEMORY`,
`JOB_OBJECT_LIMIT_ACTIVE_PROCESS`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`.

10.4. Core dump size MUST be set to 0 and secrets MUST be zeroed after use, per
risk R8 (subprocess crashes leaking secrets in core dumps).

### 11. Security Posture

11.1. The agent MUST NOT open any inbound network port; all communication MUST
be outbound-initiated.

11.2. The agent MUST NOT have direct database access.

11.3. The agent MUST authenticate to NATS with its issued JWT, and MUST be
rejected if presenting an expired or invalid credential.

11.4. The agent MUST NOT log credentials, enrollment tokens, or script contents
containing secrets.

### 12. Inventory Reporting

12.1. The agent MUST report OS details, architecture, total RAM, disks,
services, public IP, boot time, logged-in username, and pending-reboot status.

12.2. Windows agents MUST additionally report WMI detail.

12.3. Inventory MUST be refreshable on demand via the
`rmm.cmd.{agent_id}.inventory.refresh` command.
