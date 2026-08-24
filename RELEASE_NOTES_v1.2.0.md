# Release v1.2.0 — Wiring Remediation & OpenSpec Reconciliation

**Released:** 2026-08-23
**Component:** OpenAgentPlatform (monorepo: Go API + agent, Python A2A adapters, React web)
**License:** BSL 1.1 (see [LICENSE](LICENSE))

## Overview

v1.2.0 is a correctness and truth-alignment release. Phase 6 (Commercial Tiering)
shipped all of its features in v1.1.0, but two systemic problems remained:

1. **Built-but-unwired subsystems.** Several components were delivered with unit
   tests but never constructed and injected into `cmd/server`, so in the shipped
   binary they silently answered `503`, no-op'd, or wrote wrong data. v1.2.0 wires
   them end-to-end (heartbeat persistence, single check-result owner, notification
   dispatch, reporting, remote shell, tenancy, and the A2A adapter proxy).
2. **Specs drifted from reality.** The OpenSpec tree carried `PLANNED` statuses for
   features that were already built, stale paths, and 13 capabilities with no spec at
   all. v1.2.0 reconciles the tree and adds the missing capability specs.

This release is **documentation- and correctness-focused**: it does not change the
public API surface, licensing tiers, or package versions.

## Highlights

- **W1–W8 wiring remediation complete** — every "built but never connected"
  subsystem identified in `docs/SPRINT_WIRING_REMEDIATION_PLAN.md` is now wired.
- **Agent heartbeats persist again** — tolerant timestamp decoding fixed core
  endpoint liveness; agents are no longer silently dropped after registration.
- **Zero duplicate check results** — a single owner now persists results instead of
  two competing consumers.
- **Alert notifications actually send** — the notifier registry is wired into the
  alert engine and API.
- **All `/reports` and `/shell/*` routes live** — reporting store/scheduler and the
  remote-shell stack are wired end-to-end.
- **Multi-tenancy is real** — RLS migrations target live tables, tenant context is
  parameterized, and tier resolution is wired to license/billing state.
- **A2A adapter proxy contract aligned** — the frontend-facing proxy now targets the
  versioned Python router and cost windows use epoch floats; all seven adapter
  modules register at startup.
- **OpenSpec tree reconciled (audit P0–P2)** — `rmm-core`, `STATUS.md`, and
  `PROJECT_PLAN.md` rewritten to Go reality; 6 `PLANNED` specs flipped to
  `COMPLETE`; 13 missing capability specs authored.

## Key Changes

### OpenSpec Reconciliation (Spec Audit)

- **P0 — truth-layer rewrite** (`b74b4da`): `STATUS.md`, `PROJECT_PLAN.md`, and
  `openspec/specs/rmm-core/spec.md` rewritten to describe the Go implementation
  (not the abandoned Python/Celery roadmap). `rmm-core` §14 now documents planned
  gaps.
- **P1 — drift and status fixes** (`c0474f7`, `6c35656`): fixed path drift and
  stale source-doc banners in specs; flipped 6 `PLANNED`-but-built specs to
  `COMPLETE` where code exists.
- **P2 — missing capability specs** (`66921fa`, `be16965`, `b79f6c9`): authored 13
  capability specs (`a2a-relay`, `adapter-service`, `audit-log`, `check-library`,
  `data-model`, `event-bus`, `multi-tenancy`, `notifications`, `observability`,
  `platform-foundation`, `remote-access`, `reporting`, `resilience`) plus
  `billing-stripe`. Each spec records honest "Known Limitations", which became the
  basis for the wiring plan. The duplicate Stripe client was merged into
  `client.go`.
- **Navigation maps updated** (`1aaf311`): `INDEX_MAP.md` and `HEADER_MAP.md`
  refreshed for the truth-layer rewrites.

### Wiring Remediation W1–W8

Full detail in `docs/SPRINT_WIRING_REMEDIATION_PLAN.md`.

| Item | What was broken | Fix | Commit |
|------|-----------------|-----|--------|
| W1 | Heartbeat timestamp decode failed (`int64` vs RFC3339) → heartbeats never persisted | Tolerant `UnmarshalJSON` accepts both shapes | `ae11d37` |
| W2 | Check results inserted twice (dispatcher + ingestor both persisted) | Single persistence owner; evaluation-only dispatcher | `d97532c` |
| W3 | Notifier registry never wired → zero alert notifications sent, `/test` returned 503 | Registry constructed and injected into alerts + API | `4d90fad` |
| W4 | All `/reports` endpoints 503; no scheduler/store wiring | Store + scheduler wired; due-schedule iteration fixed | `61b29e8` |
| W5 | All `/shell/*` routes 503; shell never reached agents | Remote handlers, recording store, and session publisher wired | `4da9d9e` |
| W6 | RLS migrations never ran and targeted wrong tables; tier stubbed to Community; quota unwired | RLS set rewritten against live tables + wired at startup; tiers/quota wired | `3ad3f4f` |
| W7 | Adapter proxy paths disagreed with Python routes (6/7 → 404); cost format 422; empty adapter registry | Proxy aligned to `/api/v1/adapters/*`; epoch-float cost params; adapters imported at assembly | `b5963b7` |
| W8 | Smaller correctness items across resilience, telemetry, audit, billing, relay | See below | `d619f08` |

### W8 Correctness Fixes (detail)

- **Resilience**: the adapter circuit breaker now actually guards adapter-service
  calls (`adapterBreaker` executed via `Server.callAdapter`); double rate limiting
  removed (inner API limiter deleted in favor of the outer HTTP limiter).
- **Telemetry**: deprecated no-op `TraceDB` call replaced with pool-creation wiring
  (`db.WithTracing()`); `monitoring.HealthChecker` wired into `/readyz` with a
  database component check; metrics summary now reports live request counts instead
  of an empty document.
- **Audit**: chain verification semantics fixed — per-resource subset no longer
  reports false breaks; `RequireRole(Admin, Technician)` gate added on `/audit`
  reads; chain extension serialized under a write mutex.
- **Billing**: `OrgBillingState` persisted via a new `PGStateStore`
  (`org_billing_state` table, `EnsureSchema` at startup) so restarts no longer lose
  subscription state; memory-only mode preserved on schema failure.
- **Relay**: idle-reap now keys off `LastActivityAt` (not `EstablishedAt`) so busy
  long-lived connections survive; `RelayService` parked as a library pending the
  missing network-transport + auth design (spec updated accordingly).

## Test & Validation

- Every wiring item landed through the standard gate: `deploy.sh` +
  `regression_check.py` with a `FAIL-*` registry entry first
  (per `REGRESSION_PREVENTION` protocol), plus the unit/integration tests named
  for each item in the remediation plan.
- W7 added mock-upstream tests pinning the corrected adapter-proxy contract and
  verified all 7 adapters register at runtime.
- Existing 29-file-size regression checks remain green; no source file exceeds the
  500-line limit.

## Upgrade Notes

- **No breaking changes.** Public API endpoints, REST schemas, NATS subjects, and
  environment variables are unchanged.
- Tenancy RLS migration now runs at startup behind a flag (W6); existing single-tenant
  installs are unaffected until the flag is enabled.
- The A2A adapter proxy now uses the versioned `/api/v1/adapters/*` paths — no
  client change is required, but any external caller using the un-prefixed legacy
  aliases should switch to the versioned routes.

## Known Limitations / Not In This Release

- **RMM parity gaps** (OpenSpec P3) remain: WinUpdate/AutomatedTask automation,
  maintenance windows, agent auto-update channel, offline-agent SLA alerting, and
  cloud/hypervisor monitoring. See `docs/GAP_ANALYSIS_RMM_PLATFORM.md` and
  `docs/QA_REVIEW_OPENSPEC_COVERAGE.md`.
- **Managed A2A relay transport** is not shipped — `RelayService` is a parked
  library awaiting network-transport + auth design. Accounting core and idle-reap
  are correct and tested.
- Adapter **task-events SSE** has no global feed by design until NATS-backed fan-out
  lands; the proxy keeps targeting the adapter service with keep-alive fallback.

## Previous Release

**v1.1.0 (Phase 6 — Commercial Tiering COMPLETE)** — BSL 1.1 licensing and Ed25519
validation, feature-gated tiers, PostgreSQL RLS multi-tenancy, Stripe billing and
usage metering, enterprise reporting, and the managed A2A relay. See
[RELEASE_NOTES_v1.1.0.md](RELEASE_NOTES_v1.1.0.md) and the v1.1.0 section of
[docs/CHANGELOG.md](docs/CHANGELOG.md).
