# Sprint: RMM Operations — VNC/RDP Remote Protocols Decision Gate

**Sprint Date:** 2026-08-24
**Archive After:** 2026-11-22 [+90 days]
**Sprint Focus:** Resolve the binary proxy data-plane design for VNC/RDP remote
protocols. Highest-risk decision gate — the data plane has no existing code to
anchor to; nothing is invented prematurely.
**Priority:** P2 (Normal)
**Estimated Effort:** 8-12 hours (design) + contingent build
**Status:** DECISION RECORDED — WireGuard mesh data plane (merged with RMM-07 into tunnel fabric)
**Dependencies:** RMM-00 (spec records this as DEFERRED / open decision 10.8)

---

## Overview

Only SSH and WinRM are supported for remote access today, tunneled over a
text-PTY NATS bridge: `pkg/agent/shell/` (agent-side subprocess I/O) and
`internal/remote/natsbridge.go` + `shell_manager.go` (server-side session
admission, credential store, recording). Those tunnels carry text/base64 I/O.
VNC and RDP are **binary** protocols: they need a raw byte-stream proxy data
plane with different framing, buffering, and recording semantics. rmm-core
§10.4 marks VNC/RDP as not implemented; this is accurate.

## Problem Statement

The existing `oap.agents.<id>.shell.<session>.{stdin,stdout,resize,close}`
subjects (spec'd by `internal/remote/shell_manager.go`) carry line-ish text
I/O. A binary VNC/RDP stream cannot reuse that bridge without corruption or
pathological latency. Building a proxy data plane is a new subsystem — the
largest single risk in the RMM-operations set — and its design (framing,
transport, session lifecycle, recording, capture of keyboard/mouse) must be
approved before implementation.

**Why:** premature invention here would couple a risky new data plane to the
stable shell path.
**Where:** decision → new proxy data plane; anchors
`internal/remote/natsbridge.go`, `pkg/agent/shell/`, `internal/session/`.

## Scope Boundary

```
IN SCOPE (may modify):
  - This sprint produces a design + decision record (not runtime code until
    approved)
  - Contingent build after approval:
      - internal/remote/     (server proxy session admission, framing)
      - pkg/agent/           (agent-side binary tunnel handling)
      - internal/session/ / internal/terminal/  (recording of binary streams,
        only if the approved model requires it)
      - pkg/models/          (protocol enum extension for vnc/rdp if needed)

OUT OF SCOPE (DO NOT TOUCH / DECIDE):
  - Designing the proxy data plane without approval — THE blocker / highest risk
  - Touching the existing SSH/WinRM text bridge (stable, in use)
  - Reusing the stdin/stdout text subjects for binary data
```

## Open Decision (restated from spec §10.8)

Binary proxy data plane: framing, per-session transport, lifecycle, and
recording model. The decision record MUST capture:
- the binary-capable transport/data plane (NATS framing, WSS, or another
  approved candidate) — do not preselect raw TCP tunneled over NATS
- session admission + credential flow reuse from `internal/remote/`
- recording/replay semantics for binary streams (existing recorder is text-oriented)
- supported VNC/RDP versions and negotiation

## Production-Before-Test Sequence

```
STEP 0 (DESIGN GATE): Data-plane design approved and recorded. If not approved →
    STOP, report BLOCKED; do NOT proceed. This is the gate that makes the rest
    safe to scope. TOOL: design review sign-off

STEP 1 (CONTINGENT — SERVER): Proxy session admission + framing in internal/remote/.

STEP 2 (CONTINGENT — AGENT): Binary tunnel handler in pkg/agent/.

STEP 3 (CONTINGENT — RECORDING): Recording/replay per the approved model
    (internal/session/, internal/terminal/).

STEP 4 (CONTINGENT — MODEL/UI): Protocol enum + web wiring only if needed.

STEP 5 (BUILD): go build ./... && go vet ./... before tests.

STEP 6 (TESTS): After production — byte-exact stream integrity, session
    lifecycle, admission/auth, recording round-trip.

STEP 7 (VALIDATE + COMMIT): see Validation and Commit.
```

## Tests (contingent on approval)

- `go build ./...`, `go vet ./...`.
- Byte-exact integrity through the tunnel (binary stream survives round-trip).
- Session auth/admission reuse from `internal/remote/` (reuse `remote_ws_auth_test.go` pattern).

## Rollback

```bash
# If contingent build landed and must be reverted:
git checkout HEAD -- internal/remote/ pkg/agent/ internal/session/ \
    internal/terminal/ pkg/models/ cmd/
git status
```

## Acceptance Criteria

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Data-plane design approved & recorded | design record | Framing/transport/lifecycle signed off |
| 2 | No proxy built before approval | git diff on build dirs | No code changes if design pending |
| 3 | (If approved) binary integrity preserved | round-trip test | No corruption through the tunnel |
| 4 | (If approved) SSH/WinRM text bridge untouched | git diff shell path | Existing bridge byte-identical |
| 5 | (If approved) recording semantics defined | recorder test | Binary replay matches input |

## Decision Record (2026-08-24)

**Architecture:** WireGuard mesh (Tailscale-like). Agents form an encrypted
peer-to-peer WireGuard mesh. The server is coordination/control plane only —
it manages identities, ACLs, and key distribution. VNC/RDP streams flow
directly between operator and agent through the WireGuard tunnel, not through
NATS or a server-side proxy.

**VNC/RDP specifics:**
- Operator connects to agent's VNC/RDP port through the WireGuard tunnel.
- The server's role is limited to: key distribution, ACL evaluation, and
  session admission (reuse `internal/remote/` session auth).
- No server-side binary proxy or chunking — the tunnel is a raw encrypted
  pipe between operator and agent.
- Recording semantics: agent-side capture of VNC/RDP sessions (if enabled),
  stored as binary files, separate from the existing text-oriented asciinema
  recorder.

**Merged with:** RMM-07 (Agent Self-Update). Both share the same WireGuard
mesh data plane. The existing NATS mTLS control plane remains untouched.
The existing SSH/WinRM text bridge is NOT modified.

**Rationale:** A single encrypted tunnel fabric serves every binary/large-data
need without building per-feature transports. WireGuard provides
kernel-level encryption, low latency, and is production-proven at scale
(Tailscale, Netmaker, Kilo). The coordination server never sees data plane
traffic — consistent with zero-trust architecture.

## Reference

- `openspec/specs/rmm-operations/spec.md` §9 (VNC/RDP), §10.8
- `openspec/specs/rmm-core/spec.md` §10.4/§10.5 (remote access scope)
- `internal/remote/natsbridge.go`, `internal/remote/shell_manager.go`
- `pkg/agent/shell/`, `internal/session/`, `internal/terminal/`
- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` (for the artifact that motivated the gap)

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Last Updated:** 2026-08-24
**Version:** 1.0
