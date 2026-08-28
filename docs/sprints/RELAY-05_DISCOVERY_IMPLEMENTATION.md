# Sprint RELAY-05-S: Discovery Federation Implementation

**Sprint Date:** 2026-08-27
**Sprint Focus:** Implement the approved RELAY-05 discovery contract — the local
DiscoveryRegistry (Phase 1), then the gRPC federation service + hybrid sync
(Phase 2), then the operator admin route and config wiring.
**Priority:** P2 (Normal)
**Status:** COMPLETE — Phases 1–2 and §1.4 provenance signing/verification shipped

Spec reference: `docs/design/RELAY_05_DISCOVERY_FEDERATION_ADR.md` §6–§7
and `openspec/specs/a2a-relay/spec.md` §7.3 D.1–D.2.
Prerequisite: RELAY-05 decision gate (APPROVED) and RELAY-02 I.3 admission.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **NO INVERSION** | Do not rewrite the approved ADR or add new requirements | [x] |
| **REUSE MODELS** | No parallel AgentCard data model (ADR §1.1) | [x] |
| **FAIL CLOSED** | Reject malformed/untrusted records; never store bad payloads | [x] |
| **PRIVACY FIRST** | Cross-tenant visibility is default-deny, per approved scopes | [x] |
| **NO OVERSTATEMENT** | Report PARTIAL while any ADR requirement remains unbuilt | [x] |

---

## PROBLEM STATEMENT

The decision gate froze the discovery contract. This sprint realizes it in two
delivery phases so RELAY-06 full-stack acceptance can unblock on a real
implementation rather than a promise.

---

## SCOPE BOUNDARY

```
PHASE 1 (local, shipped):
  - DiscoveryRegistry: Publish / Withdraw / Resolve / Expire / Snapshot
  - Visibility scopes + operator allowlists (ADR §4)
  - TTL cap (<=24h), monotonic-version replay prevention, tombstones (ADR §1.5–1.6)
  - Skill-filtered, version-sorted Resolve (ADR §4.4)

PHASE 2 (federation, shipped):
  - oap.discovery.v1 protobuf: PushRecord / PullRecords / Ping (ADR §2.1)
  - gRPC wire conversion (AgentCard as JSON — ADR §1.1)
  - Origin-relay-authoritative ingest + withdraw (ADR §3)
  - Hybrid push+pull Federation driver: startup reconciliation, 5m pull,
    30s ping, 3-failure unhealthy marking (ADR §2.3–2.5)
  - FederationSection in trust config (ADR §2.4)

OPERATOR SURFACE (shipped):
  - GET /admin/discovery, role+tenant scoped (ADR §5.2)

PROVENANCE SIGNATURES — ADR §1.4 (shipped):
  - Per-relay Ed25519 signing key via trust config `relay_signing_key`
    (optional; unsigned when absent — mTLS-only mode)
  - Per-peer verification key via `Federation.Peers[].pub_key` (optional;
    fail-closed on malformed keys)
  - Local Publish/Withdraw signed; inbound `IngestRemote`/`IngestRemoteWithdraw`
    verified when a peer key is configured, else accepted at mTLS transport
    level (ADR §2.2)
OUT OF SCOPE:
  - No E2E payload encryption (E.4 remains independent)
  - No migration/persistence seams (in-memory store, per ADR phase boundary)
  - No WSS discovery publish handler yet (agents publish via a later route)
```

---

## EXECUTION DIRECTIONS

### STEP 1: Phase 1 local registry (DONE)

`internal/relay/discovery.go` — local-only registry, no network I/O. Covered by
`discovery_test.go` (publish validity, version replay, TTL cap, withdraw +
tombstone, tenant ownership, multi-scope resolve, operator allowlists, skill
filter/sort, expiry).

### STEP 2: Phase 2 federation gRPC (DONE)

`internal/relay/discoverypb/discovery.proto` (generated via protoc-gen-go and
protoc-gen-go-grpc) plus `internal/relay/discovery_grpc.go` — wire conversion,
origin-authoritative ingest, and the Federation driver (push fan-out via registry
observer, periodic pull/ping loops, startup reconciliation). Covered by
`discovery_grpc_test.go` (proto round-trip, origin/version/replay conflict rules,
withdraw tombstones, in-memory PushRecord/PullRecords/Ping, push broadcast and
startup reconcile end-to-end through a bufconn server).

### STEP 3: Admin route + config wiring (DONE)

`GET /admin/discovery` in `internal/relay/admin.go`; `FederationSection` in
`trust.go`; relayID/registry/Federation wiring in `cmd/relay/main.go`. Covered by
`admin_test.go` (nil registry 404, admin sees all, operator tenant scoping).

### STEP 4: Provenance signatures — ADR §1.4 (DONE)

Optional Ed25519 signing/verification keying in `trust.go` (`relay_signing_key`,
`Federation.Peers[].pub_key`); canonical signing bytes + signature on
local Publish/Withdraw and verification on ingest in `discovery.go` /
`discovery_grpc.go`. Covered by `discovery_test.go` (signed publish/withdraw
verified against the origin key, wrong-key rejection, missing-signature
rejection, mTLS-only acceptance for unkeyed peers).

### STEP 5: Verification (DONE)

`go build ./...`, `go vet`, and `go test -race ./internal/relay/` all pass.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | Local registry works | Publish/Withdraw/Resolve/Expire honored per ADR §1, §4 |
| 2 | Federation RPCs work | PushRecord/PullRecords/Ping round-trip over gRPC |
| 3 | Conflict rules honored | Origin-relay authoritative; replay + conflicting-origin rejected |
| 4 | Sync model wired | push fan-out + 5m pull + 30s ping + startup reconcile |
| 5 | Operator surface scoped | /admin/discovery role+tenant gated |
| 6 | Provenance signatures | local records signed; inbound verified against configured peer keys (ADR §1.4) |

---

## ROLLBACK PROCEDURE

Remove the discovered files (`git rm -f internal/relay/discovery*.go
internal/relay/discoverypb`) and revert `admin.go`, `admin_test.go`, `trust.go`,
`cmd/relay/main.go` to their pre-sprint state. Disable and roll back deployed
relay configuration before reverting source.

---

## BLOCKERS / DEFERRED

- None. ADR §1.4 provenance signing/verification has landed (optional per-relay
  `relay_signing_key` + per-peer `pub_key`). All Phases 1–2 requirements and
  §1.4 are implemented and verified.

---

**Created:** 2026-08-27
**Version:** 1.1
