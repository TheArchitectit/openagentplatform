# RMM-09 — Secure Tunnel Fabric (WireGuard mesh + SSH-on-top)

**Sprint Date:** 2026-08-24
**Status:** COMPLETE — build steps 1–5 committed and verified
**Merges:** RMM-07 (agent self-update), RMM-08 (VNC/RDP remote desktop) onto one data-plane fabric
**Dependencies:** RMM-00..05 (complete); control-plane NATS mTLS; existing RBAC (`internal/auth`)

---

## 1. Decision Summary (from RMM-07/08 gates, already recorded)

1. **WireGuard mesh, not per-feature transport.** One encrypted peer mesh serves VNC, RDP, file transfer, and agent updates. The server is coordination-only — it distributes keys, sets ACLs, and admits sessions; it never terminates data-plane bytes.
2. **SSH on top of WireGuard, not instead of it.** WireGuard encrypts the mesh at the kernel/userspace layer. SSH adds per-session auth, fine-grained `-L`/`-R` forwarding, and a connection audit trail. Defense in depth.
3. **Ed25519 code signing for self-update.** The agent verifies every binary with an embedded Ed25519 public key before applying, regardless of transport. Rollout is operator-gated (version pinning), no silent auto-update.
4. **Control plane stays on NATS mTLS.** Heartbeats, checks, patches, KB state, alerts remain on existing subjects. The mesh carries only data-plane traffic.
5. **Session admission reuses existing RBAC + org-scoping.** No separate auth layer for the mesh.

---

## 2. Architecture

```
┌────────────────────┐         NATS mTLS (control)        ┌────────────────┐
│  OAP Server        │ ─── oap.mesh.* subjects ──────────▶│  Agent         │
│  (coordination     │                                     │  ┌───────────┐ │
│   only)            │ ◀── mesh session records ──────────│  │ WireGuard │ │
│  • KeyManager      │                                     │  │  iface   │ │
│  • Admission       │        WireGuard data plane         │  │ 10.0.0.2 │ │
│  • ReleaseRegistry │ ─── encrypted peer link ───────────▶│  └────┬──────┘ │
│  • RBAC hook       │                                     │   SSH server │ │
└────────────────────┘                                     │   VNC :5900  │ │
                                                            │   RDP :3389  │ │
┌────────────────────┐                                     │   Update svc │ │
│  Operator client   │ ─── WG + SSH (per-session cert) ───▶│  (Ed25519)   │ │
│  (desktop/CLI)     │        -L 5900:10.0.0.2:5900        └──────────────┘
└────────────────────┘
```

- **Server never sees VNC/RDP/update bytes.** It provisions an ephemeral operator WireGuard peer + short-lived SSH cert, returns connection params to the operator client.
- **Agent data-plane listeners (VNC/RDP/SSH) bind to the WireGuard tunnel IP only** — never the public interface.
- **Org-scoping:** WireGuard peers are authorized only within the same `org_id`. `mesh_peers.org_id` and `mesh_sessions.org_id` enforce this in every query.

---

## 3. Components & Files

### 3.1 Control plane — `internal/mesh/` (NEW package)

| File | Responsibility |
|------|----------------|
| `key_manager.go` | Generate/store per-agent WireGuard keypairs (`mesh_peers`), derive allowed-IPs per org. Ed25519 for SSH cert CA. |
| `admission.go` | `RequestSession`: validate RBAC + org-scoping, mint short-lived SSH user cert, authorize operator peer, write `mesh_sessions`. Returns WG config + cert to client. |
| `updates.go` | `ReleaseRegistry`: list/pin agent releases per org, Ed25519-sign binaries, verify on fetch. |
| `store.go` | `Store` interface (pgx) — `mesh_peers`, `mesh_sessions`, `agent_releases`. Org-scoped queries only. |
| `subjects.go` | NATS subject builders: `MeshConfigSubject(agentID)`, `MeshConfigResultSubject(agentID)`, `MeshSessionRequestSubject`. **No `rmm.winupdate.*` subjects — uses `oap.mesh.*`.** |

### 3.2 Agent-side — `pkg/agent/mesh/` (NEW package)

| File | Responsibility |
|------|----------------|
| `wireguard.go` | Subscribe to `oap.agents.<id>.mesh.config`; bring up WireGuard interface with the server-provided config. |
| `ssh.go` | SSH server bound to tunnel IP; accepts short-lived certs from the server CA; enforces per-session port-forward allow-list. |
| `updater.go` | Fetch new binary over tunnel HTTP (`:update` listener on WG IP), verify Ed25519, staged apply + operator-gated reboot. |
| `config.go` | Mesh config struct + WireGuard key types; extend `pkg/agent/config.go` with `MeshEnabled`, `MeshListenPort`, `MeshCACert`. |

### 3.3 API — `internal/api/` (extend)

- `POST /mesh/session` — admission request (org-scoped, RBAC-gated). Returns WG peer config + SSH cert.
- `GET /mesh/session` — list operator's sessions (org-scoped).
- `POST /mesh/session/:id/close` — terminate.
- `GET /agents/:id/releases`, `POST /agents/:id/releases` — release registry + pinning (RMM-07).

### 3.4 DB — mesh tables (as designed: Alembic `0015_rmm09_mesh.py`; in the
shipped canonical schema these tables live in
`internal/db/migrations/001_platform_schema.up.sql` — the Alembic set was
deleted 2026-08-24, see data-model spec §9)

- `mesh_peers(agent_id PK, org_id, public_key, allowed_ips, last_seen, status)`
- `mesh_sessions(session_id PK, operator_id, agent_id, org_id, purpose, started_at, ended_at, status)`
- `agent_releases(id PK, org_id, version, platform, binary_sha256, signature, pinned_bool, created_at)`

### 3.5 Agent wiring — `cmd/agent/main.go`

- Register `mesh.NewHandler` alongside existing handlers; subscribe to `MeshConfigSubject`.

---

## 4. WireGuard Implementation Choice (BUILD DECISION)

| Option | Pros | Cons |
|--------|------|------|
| **A. wireguard-go (userspace, embedded)** | Cross-platform, no host tooling, no root beyond TUN | Large dep (~adds to agent binary); TUN device needed |
| B. shell out to `wg`/`wg-quick` | Simple, small | Host tooling dependency; Linux/macOS/Windows diverge |
| C. raw netlink | No dep | Linux-only |

**DECIDED: wireguard-go (embedded).** Agent compiles the userspace library in, behind a `//go:build mesh` tag so hosts without a TUN device can disable it. This matches the "one fabric, all platforms" decision — no host `wg` tooling, no Linux-only netlink. Adds ~1-2MB to the agent binary; acceptable for the tunnel it unlocks.

---

## 5. Self-Update Trust Model (RMM-07 specifics)

- **Signing:** build pipeline signs agent binaries with Ed25519 private key → `signature` column. Public key embedded in agent at compile time (same primitive as `internal/auth` `jwt.SigningMethodEd25519`).
- **Verify-before-apply:** `updater.go` rejects any binary whose signature fails or whose `binary_sha256` mismatches. Tampered binary → refuse, alert.
- **Rollout:** operator-gated via `agent_releases.pinned` + per-org version pin. No silent auto-update. Agent polls `POST /agents/:id/releases` over the tunnel, fetches only if pinned version > current.

---

## 6. Production-Readiness Gates (mandatory)

- Org-scoping in **every** DB query and NATS subject (agent ID already scopes subject; `mesh_*` tables filter by `org_id`).
- Input validation at API boundary (reuse `internal/auth` middleware + existing validators).
- No invented mechanisms — WireGuard + SSH + Ed25519 are all real, existing primitives.
- Subject namespace: `oap.mesh.*` only. **No `rmm.winupdate.*` subjects.**
- `go build ./...` + `go vet ./...` before tests.
- Tests: pgxmock (0-based args), fake `Store`, negative tests for bad signature + unauthorized session + cross-org peer attempts.

---

## 7. Build Sequence

1. ~~**Schema + store + subjects**~~ (0015 migration, `internal/mesh/store.go`, `subjects.go`). ✅ `4da5631`
2. ~~**KeyManager + Admission**~~ (server-side, RBAC hook) + API endpoints + tests. ✅ `d4ef45f`
3. ~~**Agent WireGuard bring-up**~~ (`pkg/agent/mesh/wireguard.go`, `config.go`, `cmd/agent` wiring) + tests. ✅ `d834ce8`
4. ~~**Agent SSH server**~~ (`ssh.go`) + tests (cert accept/reject). ✅ `f044ec1`
5. ~~**Self-update**~~ (`updater.go` + `internal/mesh/updates.go` + API) + negative signature tests. ✅ `9363887`
6. **VNC/RDP + file transfer** documented as tunnel-enabled (operator `-L` exposes ports; SCP over SSH). No new transport code beyond the mesh. ✅ Design-only; no transport code needed.

---

## 8. Out of Scope (this sprint)

- Operator client UI/desktop binary (server returns params; client is a later deliverable).
- Scheduled automation (RMM-06, deferred).
- Cloud inventory, EDR, mobile (post-tunnel gaps, tracked in gap analysis).

---

**Created:** 2026-08-24
**Authored by:** TheArchitectit
**Version:** 1.1 — build complete (all 5 steps + design-only step 6)
