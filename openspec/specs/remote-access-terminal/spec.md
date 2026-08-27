# Remote Access — Local Terminal

> **Phase:** 2 (Automation) — remote session shell
> **STATUS: COMPLETE** — `RemoteShell` lifecycle + I/O pipes implemented and
> unit-tested (`shell_test.go`)
> **Source:** authored 2026-08-25 from code (`internal/terminal/`)
> **App Path:** `internal/terminal/`
> **Source files:** `internal/terminal/shell.go`

---

## Description

`internal/terminal/` manages the local terminal process used by a remote session.
It exposes `RemoteShell`, which owns the lifecycle and I/O pipes of a process.
The Go standard library has no portable PTY API, so the process pipes form the
**transport boundary** — a platform PTY is attached by a caller, not by this
package. The package is deliberately small: lifecycle, pipes, and wait — nothing
more.

It is consumed by the remote-access subsystem (`internal/remote/`, `cmd/*/shell`)
and the session recorder (`internal/session/`).

## User Story

**As** a remote session operator,
**I want** a shell process I can start, write input to, read output from, and
terminate cleanly,
**so that** the terminal I drive over NATS behaves like a local session.

---

## Requirements

### 1. Shell Lifecycle

1.1. `NewRemoteShell(factory CommandFactory)` constructs a shell from a
`CommandFactory func(ctx) *exec.Cmd`. The factory is the extension point for
platform-specific shells (PowerShell, bash, cmd).

1.2. `Start(ctx)` allocates process pipes and starts the terminal process.
Returns `ErrAlreadyStarted` if started, `ErrClosed` if closed, and an error if
the factory is nil or returns nil. On start failure all pipes are closed.

1.3. `Close()` releases the shell pipes and terminates a running process
(`process.Kill()`). It nils the writer references before killing so `wait()` and
`Close()` do not race to close the same pipe. Safe to call multiple times.

1.4. `Wait()` blocks until the terminal process exits and returns its exit error.
Returns `ErrNotStarted` if the shell was never started.

### 2. I/O Pipes

2.1. `Stdin()` returns the input stream (`io.WriteCloser`). Returns
`ErrNotStarted` / `ErrClosed` in the corresponding states.

2.2. `Stdout()` and `Stderr()` return the output streams (`io.ReadCloser`).
Output is multiplexed across two `io.Pipe` readers so stdout and stderr can be
consumed independently.

2.3. All three pipe accessors are mutex-guarded and safe to call concurrently.

### 3. Error Contract

3.1. `ErrAlreadyStarted`, `ErrNotStarted`, `ErrClosed` are sentinel errors
returned by the lifecycle methods.

3.2. `waitErr` captures the process exit error and is returned by `Wait()`.
A non-nil exit error means the shell exited non-zero.

---

## Known Limitations

- **No PTY.** The package provides raw process pipes, not a PTY. A caller that
  needs terminal semantics (escape codes, window size, job control) must attach
  a platform PTY to the pipes. This is documented in the package comment.
- **No process group management.** `Close()` kills the direct child process but
  does not kill its process group, so grandchildren may survive on Unix.
- **Single-start only.** A `RemoteShell` cannot be restarted after `Close()`;
  a new instance must be constructed.

---

## Cross-References

- `internal/session/` — records `RemoteShell` input/output for audit playback
- `internal/remote/`, `cmd/*/shell` — consumers that attach platform PTYs
- `remote-access` spec — higher-level remote-access domain
- `data-model` — `RemoteSession` is the entity that owns a shell