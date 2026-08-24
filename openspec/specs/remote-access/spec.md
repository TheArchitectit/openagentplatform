# Remote Access

> **Phase:** 1 (Core RMM) — rmm-core §10.4/§10.5
> **STATUS: PARTIAL**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/remote/`, `internal/terminal/`, `internal/session/`

---

## Description

The remote access capability lets operators open interactive shell sessions
to managed endpoints through their OAP agents, without inbound firewall
rules: all traffic is tunneled over NATS subjects that the agent already
holds open. The server side (`internal/remote/`) owns session admission,
limits, credential storage, NATS bridging, and audit recording; the agent
side (`pkg/agent/shell/`) spawns the protocol process and pumps its I/O;
and `internal/terminal/` + `internal/session/` provide supporting local
process-management and in-memory recording primitives.

Sessions are driven from a browser WebSocket, bridged through per-session
NATS subjects (`oap.agents.<agent>.shell.<session>.{stdin,stdout,resize,close}`),
and every session is intended to be recorded for later replay via SSE
playback or asciinema v2 export, backed by Postgres
(`session_recordings` / `session_recording_chunks`).

Two supporting packages are currently standalone: `internal/terminal/`
(a pipe-based local process manager, no PTY) and `internal/session/`
(an in-memory offset-based recorder) have no production importers — they
are tested libraries, not yet integrated into the shell path.

## User Story

**As** an operator with technician or admin privileges,
**I want** to open a web terminal to any managed agent (SSH or WinRM),
have my keystrokes tunneled over the existing NATS connection, and get a
tamper-evident recording I can replay or export later,
**so that** I can troubleshoot endpoints without VPN or inbound access and
leave an audit trail of everything typed.

---

## Requirements

### 1. NATS Tunneling

1.1. All shell traffic MUST flow over four per-session NATS subjects built
by `ShellStdinSubject` / `ShellStdoutSubject` / `ShellResizeSubject` /
`ShellCloseSubject` (`internal/remote/shell_manager.go`):
`oap.agents.<agentID>.shell.<sessionID>.{stdin,stdout,resize,close}`.
The agent-side builders in `pkg/agent/shell/shell_helpers.go` MUST mirror
these names exactly.

1.2. `NATSConnAdapter` (`internal/remote/natsbridge.go`) MUST wrap
`*nats.Conn` so the API layer depends on Subscribe/Publish without
importing nats.io directly.

1.3. Wire payloads MUST be JSON: `StdinPayload` (session_id + base64
data), `ResizePayload` (session_id + cols + rows), `ClosePayload`
(session_id + reason). Agent output is published on the stdout subject
reusing the `StdinPayload` shape (`pkg/agent/shell` `pumpStream`,
line-buffered via `bufio.Scanner`).

1.4. The agent MUST subscribe to `oap.agents.<agentID>.shell.start`
(`StartRequestSubject`) and a close subject, spawning a process per
`StartRequest` (session_id, user_id, protocol, cols, rows, username,
command), capped by a `MaxConcurrentShells` semaphore (default 5,
`cmd/agent/main.go` wiring).

### 2. Shell Session Lifecycle (ShellManager)

2.1. `ShellManager.CreateSession` MUST validate agent_id, user_id, and
protocol (`ssh` or `winrm` only; `ProtocolSSH`/`ProtocolWinRM` in
`internal/remote/shell_types.go`), default terminal size to 80×24, assign
a UUID, and derive the four NATS subjects at creation.

2.2. Admission control MUST enforce `MaxSessionsPerUser` (default 10) and
`MaxSessionsPerAgent` (default 5) via cached per-user/per-agent counters;
violations MUST return an error the API maps to HTTP 429.

2.3. Every session MUST get a per-session input rate limiter
(`rateBucket`, 1-second window, default 4096 bytes/sec);
`PublishStdin` MUST drop input over the limit (returning false, no error)
and MUST `Touch` the session on accepted input.

2.4. Sessions MUST track status `active` / `closing` / `closed`
(`SessionStatus`). Note: `StatusClosing` is declared but never assigned;
real transitions are active → closed.

2.5. `Kill` MUST refuse non-admin callers targeting another user's session
(`ErrSessionForbidden`), publish `ClosePayload` to the close subject
best-effort, decrement counters, and fire the registered `ShutdownFn`
(`SetShutdownHook`) so the WebSocket bridge can close the user's socket.

2.6. `CloseByAgent` MUST be idempotent and used when the agent signals EOF.

2.7. An idle reaper (`Run`, 1-minute ticker) MUST `Kill` sessions whose
`LastActivity` is older than `IdleTimeout` (default 30 minutes) with
reason `idle_timeout`. `Stop` MUST close all sessions.

2.8. `List` MUST scope visibility: admins see all sessions, others only
their own.

### 3. Web Terminal Bridge (WebSocket ↔ NATS)

3.1. `POST /api/v1/agents/{id}/shell` MUST create a session and return
`session_id` plus `ws_url` (`<BaseURL>/api/v1/shell/<id>/ws`).
`GET /api/v1/shell/sessions`, `GET /{session_id}`, and
`POST /{session_id}/kill?reason=` MUST list, inspect, and kill sessions.
These routes MUST be gated by `auth.RequireRole(RoleAdmin, RoleTechnician)`
(`internal/api/routes_sub.go`).

3.2. The WebSocket endpoint `GET /api/v1/shell/{session_id}/ws` is mounted
outside the auth middleware group and MUST authenticate itself: token from
Bearer header, session cookie (`oap_session`), or `?token=` query param,
verified via `SessionMinter.Parse`; with no minter configured it MUST
fail closed (reject all upgrades). Non-owners of a session MUST get 403
unless admin; creators/attachers MUST hold a shell-capable role
(`hasRemoteShellPermission`: admin/owner/superadmin/operator).

3.3. The `shellBridge` (`internal/api/remote_bridge.go`) MUST subscribe to
the session's stdout subject, send a `hello` frame (session_id, protocol),
then run read/write loops: browser `stdin` frames are base64-decoded and
published via `PublishStdin`; `resize` frames via `PublishResize`; `ping`
frames get `pong`.

3.4. The bridge MUST apply backpressure by dropping outbound frames when
the 128-slot `wsOut` channel is full, enforce a 64 KB WebSocket read
limit, a 90-second read deadline refreshed by pong, and emit WebSocket
pings every 30 seconds.

3.5. Bridge shutdown MUST be idempotent (`sync.Once`) and MUST unsubscribe
NATS, close the socket, mark the session closed via `CloseByAgent`, and
call `CredStore.RotateOnClose(agentID)` to purge temporary credentials.

### 4. Credential Store and Resolution

4.1. `RemoteCredential` MUST support types `password`, `key`, `certificate`,
`temporary`, scoped by AgentID, SiteID, or `OrgDefault` fallback.

4.2. Credential secrets MUST be encrypted at rest with AES-256-GCM
(`CredentialStore`, `internal/remote/auth.go`); keys under 16 bytes MUST be
rejected, shorter keys zero-padded to 32. `EncryptedData` MUST carry
`json:"-"` so ciphertext never reaches clients; list responses MUST return
masked copies.

4.3. `Resolver.Resolve(agentID, siteID)` MUST pick the most-specific
credential in order: exact agent match → site match (agent-empty) →
org default; returning nil (no error) when nothing matches.

4.4. `GenerateTemporary` MUST mint a one-time credential (random
`RandomID(24)` hex token, type `temporary`, `OneTime: true`, default 1h
expiry). `ConsumeTemporary` MUST mark it used and delete it on first use,
rejecting reuse and expiry. `RotateOnClose(agentID)` MUST delete all
temporary credentials for the agent when a session ends.

4.5. Credential CRUD (`POST/GET /api/v1/shell/credentials`,
`DELETE /credentials/{id}`) MUST be admin-only; delete MUST 404 when the
credential belongs to a different org.

4.6. The shipped `CredentialStore` is in-memory only; the code itself
documents Postgres-backed persistence as future work.

### 5. Session Recording (internal/remote/recorder.go)

5.1. `SessionRecorder` MUST capture every non-empty I/O event as a
`RecordingEvent` with wall-clock UTC `Timestamp`, direction `in`
(user → agent) or `out` (agent → user), hex-encoded `Data`
(`encodeForJSON`), and raw `Size`. Terminal resizes MUST be recorded as
synthetic `out` events carrying the CSI `8;rows;cols t` sequence.

5.2. Events MUST buffer in memory and flush to the
`SessionRecordingStore` when either threshold trips: 100 events
(`FlushEventThreshold`) or ~1 MiB estimated buffer (`MaxChunkSize`).
Flush MUST gzip (BestSpeed) the JSON-encoded event array and write it as
a sequentially-numbered chunk (`chunkIndex` from 0). Note: the documented
`FlushInterval` (5s) config is accepted but no timer implements it — only
count/size trigger flushes.

5.3. `Close` MUST flush remaining events and upsert `RecordingMetadata`
(session/agent/user IDs, protocol, terminal size, started/ended, duration,
bytes in/out, event/chunk counts, `ContentHash`). A nil store MUST make
recording a silent no-op (best-effort).

5.4. `ContentHash` MUST be SHA-256 over the concatenated JSON of events
via `computeContentHash`. Caveat: `Close` flushes the buffer before
computing metadata, so the persisted hash covers only unflushed events —
in practice the empty buffer (empty-string SHA-256). The file header's
"SHA-256 hash chain for tamper verification" claim is not fully realized.

### 6. Recording Storage (internal/remote/recording_store.go)

6.1. `PGStore` MUST persist recordings in two lazily-created tables:
`session_recordings` (metadata, PK session_id, indexes on agent_id,
user_id, started_at DESC) and `session_recording_chunks` (PK
(session_id, chunk_index), BYTEA gzip payload, FK with ON DELETE CASCADE).

6.2. `InsertRecordingChunk` MUST create a minimal placeholder metadata row
on first chunk (`ON CONFLICT DO NOTHING`) so the chunk FK is satisfied,
and MUST upsert chunk contents on index collision.

6.3. `ListRecordings` MUST filter by agent_id, user_id, session_id
(ILIKE substring), since/until window; default limit 50, hard cap 500,
ordered `started_at DESC`, returning rows plus total count.

6.4. `DeleteRecording` MUST cascade-remove chunks and MUST return
`ErrRecordingNotFound` on zero rows; `PurgeOlderThan` MUST support
retention sweeps by started_at age.

6.5. `DecodeChunk` MUST gunzip and JSON-parse a chunk back into
`[]RecordingEvent`; `DecodeForJSON` MUST reverse the hex encoding.

### 7. Recording Playback and Export (internal/api/session_audit.go)

7.1. Under `GET /api/v1/shell/recordings` (admin/technician role):
list, `GET /{session_id}` metadata, `GET /{session_id}/play`,
`GET /{session_id}/export`, `DELETE /{session_id}` (delete admin-only).
Non-admin callers MUST be scoped to recordings where `user_id` matches
their subject (`canAccessRecording`).

7.2. `/play` MUST stream Server-Sent Events — one `event: data` per
recording event with JSON `{t (offset ms), dir, data, size}` — pacing
events against wall clock scaled by `speed` (0 < speed ≤ 64), honoring
`from` (resume offset in ms), and terminating with `event: end`.
`format=json-array` MUST instead return all events in one envelope with
`offset_ms`, base64 `data_b64`, and hex `data_hex`.

7.3. `/export` MUST emit an asciinema v2 `.cast` file
(`application/x-asciicast`): header line `{version: 2, width, height,
timestamp, env, title}` then one JSON line per `out` event
`[elapsed_seconds, "o", data]`. Input events MUST NOT be exported.

7.4. The recording store and live-recorder factory MUST be injected via
`Server.SetRecordingStore` / `SetSessionRecorderFactory`; unset
dependencies MUST yield HTTP 503, never a panic.

### 8. Local Terminal Primitives

8.1. `terminal.RemoteShell` (`internal/terminal/shell.go`) MUST manage a
terminal process through a `CommandFactory`, exposing `Start`, `Stdin`,
`Stdout`, `Stderr`, `Wait`, `Close` over `io.Pipe` plumbing. It MUST
guarantee single-close semantics on the pipe writers even when `wait()`
and `Close()` race, and MUST `Kill` the process on Close, blocking on
`waitCh` for exit.

8.2. `internal/terminal` MUST NOT allocate a PTY itself (no portable PTY
API in the stdlib); the process pipes are the transport boundary and a
caller attaches a platform PTY.

8.3. `session.SessionRecorder` (`internal/session/recorder.go`) MUST
capture terminal I/O in memory as `Event{Offset, Direction, Data}` with
offsets relative to `Start(at)`, enforcing start-once
(`ErrAlreadyStarted`), no-write-before-start (`ErrNotStarted`) and
no-write-after-stop (`ErrStopped`) semantics, and MUST serialize via
`Recording` / `MarshalJSON` for persistence or playback.

### 9. Agent-Side Shell Handler (pkg/agent/shell)

9.1. The agent MUST run `shell.Handler` with `MaxConcurrentShells`
(default 5) and `IdleTimeout` (default 30m) from `cmd/agent/main.go`;
handler errors MUST be non-fatal to agent startup.

9.2. `defaultCommandBuilder` MUST build `ssh -tt -o BatchMode=yes
[user@host]` for `ssh` (default user `oap`) and a powershell stub for
`winrm`; unknown protocols MUST error. The SSH host is currently a
placeholder (the session ID), i.e. real dial-out to a target host is not
yet implemented.

9.3. The agent MUST pump stdout/stderr line-by-line (64 KB scanner
buffer, base64-encoded) to the stdout subject, forward stdin to the
process pipe, and publish an EOF/close signal when the process exits.
Resize requests are currently logged only (no SIGWINCH path wired).

---

## Known Limitations

1. **Server wiring not connected.** `SetRemoteHandler`, `SetRecordingStore`,
   and `SetSessionRecorderFactory` are never called from `cmd/server/`,
   so every `/api/v1/shell/*` route returns 503 (`remote_not_configured`)
   in the shipped server. Routes and handlers exist and are tested in
   isolation; the capability is code-present but not deployed-enabled.
2. **No shell.start publisher.** The agent subscribes to
   `oap.agents.<id>.shell.start`, but no server code ever publishes a
   `StartRequest` — `CreateSession` deliberately skips NATS interaction,
   so end-to-end agent process spawn is not connected.
3. **Live recording not attached.** No production caller invokes
   `RecordInput`/`RecordOutput`; the recorder factory field is dead.
   Playback/export/store are complete, but nothing fills them during a
   live session today.
4. **Standalone packages.** `internal/terminal/` and `internal/session/`
   have zero production importers (own tests only) — supporting
   primitives awaiting integration.
5. **No RemoteSession state machine.** The QA audit (Req 4) expects a
   7-state `RemoteSession` machine; only the 3-value `SessionStatus`
   exists here, and `StatusClosing` is never assigned. State-machine
   tracking belongs to rmm-core §4.4.
6. **VNC/RDP not implemented** — SSH/WinRM/web-terminal only
   (rmm-core §14.8). WinRM itself is a powershell stub; SSH dial-out
   targets a placeholder host.
7. **Credential store is in-memory**, lost on restart; hash-chain tamper
   evidence is nominal only (see 5.4); `FlushInterval` time-based flush
   unimplemented; subject builders do not escape dots in agent/session
   IDs (validation is delegated upstream).
8. **Audit trail is partial.** `recordAudit` logs via slog instead of the
   audit service (not injected); only `shell.kill` emits an audit event.
