# Remote Access — Session Recorder

> **Phase:** 2 (Automation) — audit playback of remote sessions
> **STATUS: COMPLETE** — `SessionRecorder` captures terminal I/O in memory,
> serializes to JSON, unit-tested (`recorder_test.go`)
> **Source:** authored 2026-08-25 from code (`internal/session/`)
> **App Path:** `internal/session/`
> **Source files:** `internal/session/recorder.go`

---

## Description

`internal/session/` records terminal input and output for audit playback. It
exposes `SessionRecorder`, an in-memory capture buffer that records timestamped
`Event`s (input or output) during a remote session. The recording is serializable
to JSON via `MarshalJSON` and can be persisted by a caller for later playback.

The package is deliberately decoupled from the transport: it captures bytes and
offsets, not NATS subjects or shell handles. It is consumed by the remote-access
subsystem (`internal/remote/`) which wires it to a `RemoteShell` from
`internal/terminal/`.

## User Story

**As** a security auditor,
**I want** every keystroke and terminal response from a remote session captured
with a timestamp,
**so that** I can replay exactly what an operator did during an incident.

---

## Requirements

### 1. Event Model

1.1. `Direction` is a string enum: `DirectionInput = "input"`,
`DirectionOutput = "output"`.

1.2. `Event` carries `Offset time.Duration` (relative to session start),
`Direction`, and `Data []byte`. The offset lets a playback UI reconstruct the
session timeline.

1.3. `Recording` is the serializable representation: `SessionID`, `StartedAt`,
`StoppedAt`, and `Events []Event`.

### 2. Recorder Lifecycle

2.1. `NewSessionRecorder(sessionID)` constructs a recorder. `sessionID` is the
remote-session identifier the recording belongs to.

2.2. `Start(at)` begins recording. Returns `ErrAlreadyStarted` if started. If
`at` is zero it uses `time.Now().UTC()`.

2.3. `Stop(at)` finishes recording. Returns `ErrNotStarted` if never started.
Idempotent — a second call is a no-op. If `at` is zero it uses `time.Now().UTC()`;
if `at` is before `StartedAt` it clamps to `StartedAt`.

2.4. `WriteInput(data)` and `WriteOutput(data)` capture terminal I/O. Both return
`ErrNotStarted` if not started, `ErrStopped` if stopped. Empty data is a no-op.

### 3. Serialization

3.1. `Recording()` returns a deep copy of the captured session (events and their
`Data` are copied, not aliased).

3.2. `MarshalJSON()` serializes the current recording as a JSON object. It is
provided as a method so callers can embed the recorder in larger response structs
without worrying about encoding details.

### 4. Concurrency

4.1. All methods are mutex-guarded (`sync.Mutex`) and safe to call concurrently
from the input-capture and output-capture goroutines.

---

## Known Limitations

- **In-memory only.** Events are held in a slice for the process lifetime; there
  is no flush-to-disk or size bound. A long-running session with heavy I/O can
  grow without limit. A caller must persist the recording via `Recording()` /
  `MarshalJSON()` and is responsible for storage.

- **No encryption at rest.** The recording contains raw terminal bytes (which may
  include credentials typed into the shell). A persisting caller must encrypt
  the stored recording; this package does not.

- **No playback engine.** This package captures; it does not replay. A playback
  UI must consume the `Recording` JSON and reconstruct the timeline from `Offset`
  + `Direction` + `Data`.

---

## Cross-References

- `internal/terminal/` — `RemoteShell` whose pipes feed `WriteInput`/`WriteOutput`
- `internal/remote/` — consumer that wires recorder to a live session
- `remote-access` spec — higher-level remote-access domain
- `audit-log` spec — recordings are a distinct audit artifact from hash-chained events