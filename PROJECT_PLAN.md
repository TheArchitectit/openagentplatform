# OpenAgentPlatform — Project Plan

**Version:** v1.2.0 (Commercial)
**Status:** Phases 0–6 delivered + v1.2.0 wiring remediation
**Canonical roadmap:** [docs/plans/MASTER_IMPLEMENTATION_PLAN.md](docs/plans/MASTER_IMPLEMENTATION_PLAN.md)
**A2A detail:** [docs/plans/A2A_PLAN.md](docs/plans/A2A_PLAN.md)

> This file previously contained an implementation plan for "Project Sentinel," a
> governance-layer MCP product that is not part of this repository. It was inherited
> from a template clone and never reconciled. The Sentinel plan lives in git history;
> the related Guardrail MCP Server code sits in `mcp-server/` as a vendored, unrelated
> module. See `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` for the audit that surfaced this.

---

## Mission

Agent-first RMM + A2A platform: manage fleets of endpoints (checks, alerts, patches,
policies, scripts, remote access) and let autonomous agents interoperate through the
A2A protocol — with commercial tiering (licensing, multi-tenancy, billing) on top.

## Architecture (5 deployable units)

| Component | Path | Stack |
|-----------|------|-------|
| API server | `cmd/server`, `internal/*` | Go |
| Endpoint agent | `cmd/agent`, `pkg/agent/*` | Go single binary, outbound-only over NATS |
| A2A gateway | `a2a/*` | Go module (`bridge/`, `gateway/`, `hitl/`, `manager/`, `models/`, `pool/`, `registry/`, `router/`, `spec/`) |
| Framework adapters | `py/oap/adapters/` | Python/FastAPI; 7 adapters + orchestrator, pool, cost |
| Secrets platform | `secrets/*` | Go module; Vault, Infisical, DB, env, memory backends |

Web UI: React SPA in `web/`. Event backbone: NATS (`oap.agents.*`, `oap.events.*`).
Database: PostgreSQL with per-tenant RLS.

## Roadmap Summary

Full sprint breakdown: MASTER_IMPLEMENTATION_PLAN.md §6 (7 phases, 46 weeks).

- **Phase 0 — Foundation** (4w): scaffold, DB schema, NATS mTLS, OIDC auth, agent MVP ✅
- **Phase 1 — Core RMM** (10w): checks, alerts, policies, patches, scripts, remote access ✅
- **Phase 2 — A2A + Agents** (6w): gateway, framework adapters, process pool, event bridge ✅ (adapter proxy contract aligned in v1.2.0, W7)
- **Phase 3 — Secret Management** (4w): Vault/Infisical backends, reference resolver, rotation ✅
- **Phase 4 — Frontend** (6w): dashboards, real-time updates, terminal ✅
- **Phase 5 — Production Readiness** (8w): observability, security hardening, CI/CD, docs ✅
- **Phase 6 — Commercial Tiering** (8w): feature gating, BSL licensing, multi-tenancy, Stripe billing, enterprise reporting, managed relay ✅ (v1.1.0)

## What's Next

Completed in v1.2.0 (2026-08-23): the A2A dashboard contract divergence (W7) and
the OpenSpec reconciliation (audit P0–P2) are done. Remaining candidate work items,
in rough priority order:

1. **Close the eight deferred RMM operations** using the planned contract in
   `openspec/specs/rmm-operations/spec.md` and the ordered RMM-00..08 sprint set.
   Decision-gated work must stop rather than inventing unresolved mechanisms.
2. **Ship the Managed A2A relay transport** using RELAY-00..06. `RelayService`
   remains an in-memory library today; the WSS transport/security contract is
   planned, not shipped (`openspec/specs/a2a-relay/spec.md`).

Execution order, validation rules, and lower-capability-agent stop conditions are
canonical in `docs/plans/DEFERRED_WORK_HANDOFF.md`. The separate spec-review
repository publication runbook is `docs/reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md`;
publication remains blocked until the user supplies an authorized remote URL.
