# Endpoint Agent

> **Phase:** 0 (Foundation) / 1 (Core RMM)
> **STATUS: COMPLETE**
> **App Path:** `cmd/agent` + `pkg/agent/` (Go module, single static binary)

---

## Description

The `oap-agent` is a single Go binary installed on every managed endpoint. It
is the platform's presence on the device: it connects outward to the platform's
NATS server, registers itself with the platform API, publishes heartbeats, and
executes operator commands (checks, scripts, compliance collections, patch
operations, and remote shell sessions) delivered on its per-agent NATS
subjects.

Communication is entirely outbound-initiated over **core NATS** (pub/sub, not
JetStream). Command delivery is fire-and-forget publish/subscribe on topics
scoped to a single agent; durability and redelivery are provided by NATS
client reconnect semantics rather than streams, durable consumers, or
acknowledgements.

The agent has no inbound HTTP port and no direct database access. Its network
authentication is mTLS client certificates against the NATS server plus a
Bearer token for the REST registration API. It is configured from a local
YAML/JSON file (with environment overrides) and persists its assigned
`agent_id` and `auth_token` back to that file after registration, so identity
survives restarts.

The executable is a command-line daemon (`cmd/agent/main.go`) that also exposes
`-register` for one-shot registration, `-version`, and `-list-checkers`.

## User Story

**As** an administrator deploying to a mixed Windows/Linux/macOS fleet,
**I want** a single dependency-free binary that registers itself, survives
NATS interruptions via automatic reconnect, and executes checks and scripts
with output isolation and timeout enforcement,
**so that** I can manage any endpoint without opening inbound firewall ports
and without a misbehaving script destabilizing a production host.

---

## Requirements

### 1. Build and Packaging

1.1. The agent MUST be buildable via the Makefile target `make build-agent`
(`go build -o bin/oap-agent ./cmd/agent`) and `make build-agent-all`, which
cross-compiles three OS artifacts:

| OS | Output (`bin/`) |
|----|-----------------|
| linux | `oap-agent-linux` |
| darwin | `oap-agent-darwin` |
| windows | `oap-agent-windows.exe` |

(`Makefile`, targets `build-agent` / `build-agent-all`.)

1.2. `pkg/agent` MUST compile for windows, darwin, and linux using the same
source; platform differences MUST be isolated behind `//go:build` tags (e.g.
`executor/sysproc_unix.go` vs `executor/sysproc_windows.go`) or runtime probes.

1.3. No separate CI build/test matrix is defined for the agent in the
Makefile; correctness across platforms is expected from the platform-isolated
sources (1.2) and the vet pass in `make lint` / `make test`.

### 2. Invocation and Runtime Modes

2.1. The binary MUST accept these flags (`cmd/agent/main.go`):

| Flag | Effect |
|------|--------|
| `-config <path>` | Load config from path (YAML/JSON); defaults to OS-specific path |
| `-register` | Run one-shot registration then exit (no daemon) |
| `-version` | Print `oap-agent <version>` and exit |
| `-list-checkers` | Print registered checkers (name, version, platforms, description) in a table and exit |

2.2. The daemon MUST register before connecting if no `agent_id`/`auth_token`
is configured, falling back when `-register` is not given (main.go:
`cfg.AgentID == "" || cfg.AuthToken == ""`), and must execute the
registration flow even without `-register` before starting the daemon.

2.3. On shutdown (SIGINT/SIGTERM) the agent MUST drain in-flight handlers for
up to 10 seconds before exiting (main.go: `shutdownCtx` with `timeout`).

### 3. Configuration and Identity Persistence

3.1. Configuration MUST be loaded from YAML or JSON at an OS-specific default path:

| OS | Path |
|----|------|
| Linux | `/etc/openagentplatform/agent.yaml` |
| macOS | `/etc/openagentplatform/agent.yaml` |
| Windows | `%PROGRAMDATA%\OpenAgentPlatform\agent.yaml` |

(`config.go: DefaultConfigPath`.) A missing or unreadable file is not an
error; defaults plus environment overrides are returned.

3.2. The config MUST support these keys at minimum: `site_id`, `agent_id`,
`auth_token`, `nats_url`, `nats_ca`, `nats_cert`, `nats_key`, `api_url`,
`api_insecure` (ignored; see 9.4), `log_level`, `heartbeat_interval_sec`
(default 60), `script_timeout_sec` (default 300)
(`config.go: Config`, `validate`).

3.3. Environment variables MUST override file values: `AGENT_SITE_ID`,
`AGENT_AGENT_ID`, `AGENT_TOKEN`/`AGENT_AUTH_TOKEN`, `NATS_URL`,
`NATS_CA_FILE`, `NATS_CERT_FILE`, `NATS_KEY_FILE`, `API_URL`, `LOG_LEVEL`
(`config.go: applyEnv`).

3.4. There is NO UUID file. Identity is `agent_id` + `auth_token`. After
registration the agent MUST persist both to the config file via an
atomic write (temp file + rename, `0600`) so identity survives restarts
(`config.go: Save`; `register.go: RegisterAgent`).

### 4. Registration

4.1. Registration MUST be performed with `POST /api/v1/agents/register`
carrying a JSON body (`register.go: RegisterRequest`) and, if a token is
configured, an `Authorization: Bearer <token>` header.

4.2. The registration payload MUST report:

| Field | Source |
|-------|--------|
| `site_id` | config |
| `hostname` | `os.Hostname()` |
| `os`, `platform`, `arch` | `runtime.GOOS` / `runtime.GOARCH` |
| `num_cpu` | `runtime.NumCPU()` |
| `total_memory`, `total_disk` | gopsutil `mem` / `disk` |
| `agent_version` | `AgentVersion` |
| `tags` | optional slice |

(`register.go: RegisterAgent`, `hostinfo.go: CollectHostInfo`.)

4.3. The response MUST supply `agent_id` and `auth_token` (both required —
registration fails if either is missing) and MAY supply `nats_url`/`api_url`
to override the configured endpoints. The agent MUST persist the returned
values back to config (`register.go: Register`, `RegisterAgent`).

4.4. Registration rate-limiting is a server-side concern; the agent performs a
single POST with a 30-second HTTP client timeout.

### 5. Host Information

5.1. The agent MUST collect host metrics via `CollectHostInfo()` + gopsutil v4,
capturing: hostname, OS, platform, arch, CPU count, total memory, total disk,
uptime (seconds), and live CPU/memory/disk percentages
(`hostinfo.go: HostInfo`). CPU/disk collection may fail non-fatally.

5.2. `AgentVersion` MUST be a build-time constant (`hostinfo.go`:
`const AgentVersion = "0.1.0"`).

### 6. NATS Connection

6.1. The agent MUST connect to the NATS server with mTLS when cert/key/CA
files are configured (`nats.ClientCert`, `nats.RootCAs`); otherwise it
connects without client auth (`nats.go: ConnectNATS`).

6.2. Connection options MUST be: name `oap-agent`, `ReconnectWait` 2s,
`MaxReconnects` -1 (unlimited), `ReconnectJitter` 500ms–2s, connect `Timeout`
10s, `PingInterval` 30s, `MaxPingsOutstanding` 2, plus error/closed/
disconnect log handlers (`nats.go`).

6.3. Reconnect is handled by the NATS client. `MaxReconnects(-1)` means the
agent MUST never permanently give up on the control plane; jitter prevents
thundering-herd reconnects.

6.4. On shutdown the connection MUST be drained (`Close` → `Drain`) so
in-flight messages are flushed.

6.5. All communication uses CORE NATS publish/subscribe. There is no
JetStream: no streams, durable consumers, pull subscriptions, `AckWait`,
`MaxAckPending`, or message acknowledgement.

### 7. Heartbeat

7.1. The agent MUST publish a heartbeat to `oap.agents.{agentID}.heartbeat`
on startup immediately, then every `heartbeat_interval_sec` (default 60)
(`heartbeat.go: RunHeartbeat`, `HeartbeatSubject`).

7.2. The heartbeat payload MUST carry `agent_id`, `timestamp`, `cpu_percent`,
`mem_percent`, `disk_percent`, `uptime_secs`, and `version`, each refreshed
from live host info at publish time (`heartbeat.go: HeartbeatPayload`).

7.3. Heartbeats are one-way publishes; there is no heartbeat ack, and no
agent-side `STALE`/liveness state transition based on missed heartbeats.

### 8. Per-Agent Subjects

8.1. The agent MUST subscribe to these subjects, all scoped to its own
`agentID` (subscriptions established in `main.go`; builders in the referenced
files):

| Subject | Direction | Builder |
|---------|-----------|---------|
| `oap.agents.{id}.checks` | subscribe | `checks.go: ChecksSubject` |
| `oap.agents.{id}.results` | publish | `checks_helpers.go: ChecksResultSubject` |
| `oap.agents.{id}.scripts` | subscribe | `scripts.go: ScriptsSubject` |
| `oap.agents.{id}.scripts.cancel` | subscribe | `scripts.go: ScriptsCancelSubject` |
| `oap.agents.{id}.scripts.result` | publish | `scripts.go: ScriptsResultSubject` |
| `oap.agents.{id}.scripts.{runID}.output` | publish | `scripts.go: ScriptsOutputSubject` |
| `oap.agents.{id}.compliance` | subscribe | `collectors.ComplianceRequestSubject` |
| `oap.agents.{id}.compliance.results` | publish | `compliance.go: ComplianceResultSubject` |
| `oap.agents.{id}.patch_scan` | subscribe | `patcher/handler.go: PatchScanSubject` |
| `oap.agents.{id}.patch_scan.results` | publish | `patcher/handler.go: PatchScanResultSubject` |
| `oap.agents.{id}.patch_install` | subscribe | `patcher/handler.go: PatchInstallSubject` |
| `oap.agents.{id}.patch_install.results` | publish | `patcher/handler.go: PatchInstallResultSubject` |
| `oap.agents.{id}.shell.start` | subscribe | `shell/shell_helpers.go: StartRequestSubject` |
| `oap.agents.{id}.shell.{sid}.stdin` | subscribe | `shell/shell_helpers.go: StdinSubject` |
| `oap.agents.{id}.shell.{sid}.resize` | subscribe | `shell/shell_helpers.go: ResizeSubject` |
| `oap.agents.{id}.shell.{sid}.close` | publish | `shell/shell_helpers.go: CloseSubject` |

8.2. All subscriptions are fan-out `Subscribe` (no persistent queue group for
command delivery — each agent consumes only its own subject).

### 9. Security Posture

9.1. The agent MUST NOT open any inbound network port; all communication MUST
be outbound-initiated.

9.2. The agent MUST NOT have direct database access.

9.3. NATS authentication MUST use mTLS client certificates when configured
(6.1); the REST API MUST use a Bearer token when one is configured (4.1).

9.4. TLS certificate skip-verify for the API client MUST NOT be exposed —
`api_insecure` is read but deliberately ignored to keep mTLS strict
(`register.go: NewAPIClient`).

9.5. The agent MUST NOT log credentials, enrollment tokens, or script source.
Only non-sensitive metadata (IDs, types, counts) is logged.

### 10. Check Execution

10.1. Checks arrive as JSON on the checks subject (`checks.go: CheckCommand`):
`check_id`, `type`, `target`, `timeout_sec`, `options`, `script`, `command`,
`args`, `expected`, and optional `interval_sec`. Payloads are JSON, not
msgpack.

10.2. Each check MUST be dispatched by `checkers.Run`, which resolves the type
in a registry and enforces a hard wall-clock timeout (default 30s, request
`timeout_sec` overrides) via a goroutine + `context.WithTimeout`
(`checkers/registry.go: Run`, `runWithTimeout`).

10.3. Unknown check types MUST produce a synthetic error result
(`... unknown check type: <type>`) rather than executing, which is published
on the results subject (`checks_helpers.go: verifyPayload`).

10.4. Failures MUST be retried: up to `MaxRetries` (default 3) attempts with
exponential backoff (`RetryBackoff` default 1s) (`checks_helpers.go: dispatch`).

10.5. Interval gating: if `interval_sec` is set and the same key
(`check_id`, or `type:target:expected`) ran within the interval, the check
MUST be skipped and a `status: "skipped"` result published
(`checks_helpers.go: ShouldSkip`, `HandleMsg`).

10.6. Results (`CheckResultEnvelope`: `check_id`, `agent_id`, `result`,
`issued_at`, `completed_at`) MUST be published to `oap.agents.{id}.results`.
Results are accumulated in a batch buffer and flushed by the executor on a
5-second window or when the buffer reaches 50 items, publishing each result
individually (`checks_helpers.go: enqueue`, `flushBatch`).

10.7. Results MUST NOT be tied to message ack (there are no acks in core
NATS); the batching exists only to coalesce publish rate.

10.8. The agent MUST expose check `expvar` metrics: `check_count`,
`check_failure_count`, `check_duration_ms`, `check_retry_count`,
`check_timeout_count`, `check_batch_count`, `check_skipped_count`
(`checks_helpers.go`).

### 11. Registered Checkers

11.1. The agent MUST register nine default checkers (`checkers/registry.go`):

| Type | Checker |
|------|---------|
| `ping` | ICMP/ping |
| `http` | HTTP(S) request |
| `tcp` | TCP connect |
| `dns` | DNS resolution |
| `cpu` | CPU usage |
| `memory` | memory usage |
| `disk` | disk usage |
| `service` | service/process state |
| `script` | inline-script check |

11.2. Checkers that implement `MetaChecker` MUST expose name, version,
description, and supported platforms; `-list-checkers` prints this metadata
(`main.go: printCheckers`, `checkers/registry.go: AllMetadata`).

11.3. Checkers MUST be registered under a lowercased key and looked up
case-insensitively (`registry.go: registerInternal`, `Get`).

### 12. Script Execution

12.1. Scripts arrive as JSON on the scripts subject (`scripts.go:
ScriptCommand`): `script_id`, `run_id`, `runtime`, `script`, `url`,
`args`, `env`, `dependencies`, `timeout_sec`, and `sandbox`.

12.2. Supported runtimes and their invocation (`executor/runtimes.go`):

| Runtime | Flags added | Extension |
|---------|-------------|-----------|
| `bash`/`sh`/`zsh` | script path directly | `.sh` |
| `python`/`python3`/`py` | script path directly | `.py` |
| `node`/`nodejs`/`js` | script path directly | `.js` |
| `powershell`/`pwsh`/`ps1`/`ps` | `-NoProfile -ExecutionPolicy Bypass -NonInteractive -File <path>` | `.ps1` |
| `cmd`/`batch`/`bat` | — (reserved) | — |

(`executor/executor_steps.go: normaliseRuntime`, `runtimes.go`.) Runtimes are
probed via `exec.LookPath`; unavailable runtimes report `Available() == false`
and are rejected with `runtime ... not available on this host`.

12.3. When `runtime` is empty, it MUST be inferred from the script's shebang
or content via `DetectRuntime` (python/bash/node/powershell heuristics,
bash fallback).

12.4. The script MUST be staged to a temp file (`oap-script-*` dir,
`script.<ext>` written `0600`) and the temp dir MUST be removed after the run.
No script is ever left on disk afterward (`executor.go: ExecuteWith`).

12.5. Arguments MUST be passed as a separate argv slice appended after the
interpreter flags — never shell-interpolated — preventing command injection
(`executor.go`).

12.6. Output MUST be streamed line-by-line to the caller via
`OutputCallback`, and each line is published as a `ScriptOutputChunk` to both
the per-run output subject and the shared result subject
(`scripts.go: runScript`, `ScriptsOutputSubject`). Output buffers are capped
at `MaxOutputBytes` (64 KB) with a `MaxLineBytes` (1 MB) scanner limit.

12.7. A wall-clock timeout MUST be enforced via `context.WithTimeout`
(default 5 minutes, or `timeout_sec`). On timeout or cancel the whole process
group MUST be killed (`taskkill /T /F` on Windows, negative-PID `SIGKILL` on
Unix) (`executor.go`, `executor_steps.go: killProcessGroup`).

12.8. Each run MUST be placed in a new process group so the agent can signal
children on timeout/cancel (`sysproc_unix.go` `Setpgid`, `sysproc_windows.go`
`CREATE_NEW_PROCESS_GROUP`).

12.9. Environment control is OPT-IN: when `sandbox: true`, the child gets a
minimal `HOME`/`PATH`/`TEMP`/`TMP` environment plus caller `env` overrides
(`EnvSandbox`); when false, the agent's full environment is inherited
(`executor_steps.go: applySandbox`). Environment is NOT filtered to an
allowlist by default.

12.10. Dependencies (`dependencies`) MUST be installed best-effort and
non-fatally before the run (`pip install` / `npm install`) with failures
recorded rather than aborting (`executor_steps.go: installDependency`).

12.11. Cancellation: a `run_id` registry maps in-flight runs to their context
cancel functions; a message on the cancel subject cancels the matching run
(`scripts.go: runRegistry`, `RunScriptsHandler`).

12.12. The final result is a `ScriptOutputChunk` on `stream: "exit"` carrying
`exit_code` and `duration_ms`; failures/timeouts/cancels publish an `error`
chunk (`scripts.go: runScript`).

### 13. Compliance Collection

13.1. Compliance requests arrive as JSON on the compliance subject
(`compliance.go: ComplianceCommand`): `request_id`, `collector`, `policy_id`,
`timeout_sec` (default 30s).

13.2. The agent MUST run the named collector from a registry pre-populated
with nine collectors: antivirus, firewall, encryption, patching,
password_policy, screen_lock, usb_storage, browser_extensions, remote_access
(`compliance.go: defaultCollectors`).

13.3. The result MUST be published to `oap.agents.{id}.compliance.results`
as a `ComplianceResultEnvelope` with the collected `ComplianceData` or an
`error` string (`compliance.go: handle`).

### 14. Patch Scan and Install

14.1. Patch scan requests arrive on the patch_scan subject; the agent MUST run
an auto-selected platform scanner and publish a `PatchScanResultEnvelope`
(patches + optional error) to `...{.id}.patch_scan.results`, with a 60s default
timeout (`patcher/handler.go: handleScan`).

14.2. Patch install requests arrive on the patch_install subject carrying a
`PatchInfo`; the agent MUST run an auto-selected installer and publish a
`PatchInstallResultEnvelope` to `...{.id}.patch_install.results`, with a
5-minute default timeout (`patcher/handler.go: handleInstall`).

14.3. Scanner/installer selection is platform-specific
(`patcher/linux.go`, `windows.go`, `macos.go`, built via `NewAutoScanner` /
`NewAutoInstaller`).

### 15. Remote Shell

15.1. Remote shell sessions are started via the shell.start subject carrying a
`StartRequest` (session_id, user_id, protocol, cols/rows, username, command);
the agent MUST launch the requested protocol and stream I/O over per-session
NATS subjects (`shell/shell.go: Run`, `handleStart`).

15.2. Supported protocols: `ssh` (default `ssh -tt -o BatchMode=yes
user@host`, host defaults to localhost) and `winrm` (a PowerShell stub until
credentials are available) (`shell_helpers.go: defaultCommandBuilder`).

15.3. Concurrency MUST be capped at `DefaultMaxConcurrentShells` (5); when the
cap is reached new sessions are rejected with a busy close
(`shell/shell.go`, `shell_helpers.go`).

15.4. Sessions MUST time out after `DefaultIdleTimeout` (30 minutes).

15.5. stdin/stdout/stderr/resize are carried as base64 JSON payloads; closing
a session (close subject or process exit) is published back to the server
(`shell_helpers.go: pumpStream`, `publishClose`).
