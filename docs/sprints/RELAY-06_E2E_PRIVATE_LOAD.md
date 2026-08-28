# Sprint RELAY-06: E2E / Private / Load Acceptance Stage

**Sprint Date:** 2026-08-23 (Saturday)
**Sprint Focus:** Run acceptance only after authentication, rendezvous,
observability, discovery, and payload-encryption decisions are approved and their
required implementations exist.
**Priority:** P2 (Normal)
**Status:** PARTIAL — E.4 blind-forwarder acceptance run; discovery §1.4 provenance shipped; load/soak acceptance (criterion 4) still has no approved plan

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.4 E.1–E.4.
Prerequisites: completed RELAY-01..05 plus an approved E.4 design.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read approved designs and implemented contracts from RELAY-01..05 | [x] |
| **NO AUTH BYPASS** | Every deployed mode uses verified identity + entitlement | [x] |
| **TEST-ONLY FIXTURES** | Dev identities/bypasses never enter production config paths | [x] |
| **NO CRYPTO INVENTION** | E.4 mechanism must already be approved | [x] |
| **NO COMPLETION OVERSTATEMENT** | Report PARTIAL/BLOCKED while any dependency remains | [x] |

---

## PROBLEM STATEMENT

Acceptance cannot compensate for unresolved security and discovery contracts.
A production `-private` toggle paired with a config-supplied dev identity would
create an authentication bypass, and clear-frame tests cannot satisfy E.4's
requirement that the relay cannot read payload secrets. The stage therefore must
remain blocked until the approved stack actually exists.

---

## SCOPE BOUNDARY

```
IN SCOPE AFTER ALL GATES PASS:
  - Full authenticated, entitled, matched, metered, discovered relayed session
  - Private deployment profile with no authentication bypass
  - Approved E2E payload protection acceptance
  - Per-tenant concurrency/load/soak and failure tests
  - Operator README/status that reports remaining limitations exactly

OUT OF SCOPE:
  - No production dev-identity or unauthenticated mode
  - No new crypto, discovery, identity, or rendezvous protocol choices
  - No claim that clear frames satisfy E2E payload encryption
  - No wiring into cmd/server
```

---

## EXECUTION DIRECTIONS

### STEP 1: Verify prerequisite evidence

Require links to approved I.3, rendezvous, operator API, D.2, and E.4 decisions,
plus passing implementation tests. If any is absent, HALT and report BLOCKED.

### STEP 2: Verify private deployment profile

Every deployed profile, private or managed, MUST require cryptographically
verified platform-issued identity and entitlement. Development fixtures may exist
only in test-only code and must not be selectable through production flags or
environment variables.

### STEP 3: Run full-stack acceptance

Exercise issuance/verification, entitlement, WSS rendezvous, matching,
bidirectional protected frames, exact metering, discovery/federation, teardown,
and audit behavior. Assert the relay cannot recover payload secrets according to
the approved E.4 threat model.

### STEP 4: Run load and soak

Validate per-tenant limits, backpressure, exact accounting units, revocation and
reconnect behavior, discovery churn, shutdown, race safety, and zero session or
goroutine leaks. Thresholds and durations must come from an approved acceptance
plan; do not invent pass/fail numbers.

### STEP 5: Publish accurate status

Document how to run the dedicated relay and its approved deployment profiles.
Mark the overall relay PARTIAL or BLOCKED if any planned requirement or security
acceptance remains incomplete. Never call the managed-relay base complete merely
because clear-frame or local-only tests pass.

---

## ACCEPTANCE CRITERIA

| # | Criterion | Pass Condition |
|---|-----------|----------------|
| 1 | All gates approved | I.3, rendezvous, operator API, D.2, E.4 evidence exists |
| 2 | No deployed bypass | Every production admission cryptographically verified and entitled |
| 3 | Full-stack session passes | Approved discovery, protected frames, metering, teardown work |
| 4 | Load plan passes | Approved thresholds pass under race/soak testing |
| 5 | Status truthful | Remaining blockers keep status PARTIAL/BLOCKED |

---

## ROLLBACK PROCEDURE

Remove every newly created acceptance/load/documentation file with `git rm -f`;
restore only pre-existing production files modified by this sprint. Disable and
roll back deployed relay configuration before reverting source. Never weaken
identity checks as a rollback shortcut.

---

## BLOCKERS / DEFERRED

- I.3 authentication and credential lifecycle (implemented: trust.go + WSS gate).
- Approved rendezvous semantics and secure operator API (implemented).
- D.2 discovery federation protocol and implementation (COMPLETE: §1.4 provenance signing/verification landed).
- E.4 end-to-end payload protection acceptance (RUN: blind-forwarder `internal/relay/e4_acceptance_test.go`).
- E.2 private-relay acceptance (RUN: `internal/relay/e2_private_relay_test.go` — no open forwarder).
- Load/soak acceptance (criterion 4): no approved thresholds/durations yet.

---

**Created:** 2026-08-23
**Version:** 1.4

---

## Gate Verification Record (2026-08-24)

| Gate | Evidence | Verdict |
|------|----------|---------|
| Rendezvous semantics | `docs/design/RELAY_03_RENDEZVOUS_PROTOCOL.md` + `match.go`/`forward.go` + tests | PRESENT |
| Operator API | `docs/design/RELAY_04_OPERATOR_API_ADR.md` + `admin.go` + tests | PRESENT |
| D.2 discovery protocol | `docs/design/RELAY_05_DISCOVERY_FEDERATION_ADR.md` | APPROVED |
| D.2 discovery implementation | `internal/relay/discovery.go` + `discovery_grpc.go` + `discoverypb/` + admin route + tests | PRESENT (§1.4 provenance signing/verification landed) |
| I.3 design | `docs/design/RELAY_02_IDENTITY_ENTITLEMENT_ADR.md` | APPROVED |
| I.3 implementation | `trust.go` (TrustConfig + VerifyToken + CheckEntitlement + jti LRU); WSS TLS `ClientAuth=RequireAndVerifyClientCert` via `--trust-ca`; `extractIdentity` derives principal + tenant from cert SANs; `handleWSS` enforces token + jti + entitlement before Admit | PRESENT |
| E.4 design | blind-forwarder (out-of-band keys) | APPROVED |
| E.4 acceptance | `internal/relay/e4_acceptance_test.go` — two mTLS legs, bidirectional opaque ciphertext forwarded intact + exact metering; relay in-memory state never contains the plaintext; text + oversized frames close the pair | RUN |
| E.2 design | private relay — not an open forwarder (only issued, entitled clients admitted) | APPROVED |
| E.2 acceptance | `internal/relay/e2_private_relay_test.go` — missing mTLS cert, untrusted CA, and non-entitled (default-deny) clients all rejected with nothing registered | RUN |

I.3 admission implementation has landed (RELAY-03 wiring + trust.go + tests).
D.2 discovery implementation is now COMPLETE: ADR §1.4 provenance signatures
are signed on local publish/withdraw and verified on ingest against per-peer
Ed25519 keys (mTLS-only fallback when a peer key is absent).
E.4 blind-forwarder acceptance is now RUN over the full authenticated stack and
passes. The test surfaced and fixed one production bug: the forwarder was bound
to the per-request `r.Context()`, which is cancelled the moment `handleWSS`
returns — so no frame would ever have been forwarded in production. The forwarder
now uses the listener-lifetime context (`srvCtx`, threaded through `ServeWS`).
E.2 private-relay acceptance is also RUN: a client without an mTLS cert, with a
cert from an untrusted CA, or with a valid cert + token but no entitlement grant
is turned away before any connection is registered — the relay is not a general
open forwarder. This closes the spec's former "entitlement-gated admission NOT
yet wired" known limitation.
RELAY-06 remains PARTIAL on a single item: load/soak acceptance (criterion 4)
has no approved thresholds or durations, and this sprint does not invent them.
