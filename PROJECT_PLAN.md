# OpenAgentPlatform — Project Plan

**Version:** v1.1.0 (Commercial)
**Status:** Phases 0–6 delivered
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
- **Phase 2 — A2A + Agents** (6w): gateway, framework adapters, process pool, event bridge ⚠️ complete with known issue (dashboard contract divergence, see STATUS.md)
- **Phase 3 — Secret Management** (4w): Vault/Infisical backends, reference resolver, rotation ✅
- **Phase 4 — Frontend** (6w): dashboards, real-time updates, terminal ✅
- **Phase 5 — Production Readiness** (8w): observability, security hardening, CI/CD, docs ✅
- **Phase 6 — Commercial Tiering** (8w): feature gating, BSL licensing, multi-tenancy, Stripe billing, enterprise reporting, managed relay ✅ (v1.1.0)

## What's Next

Candidate work items, in rough priority order:

1. **Fix the A2A dashboard contract divergence** (P2 remediation from
   QA_REVIEW_PHASE2_v2.md) — reconcile gateway routes, Python adapter paths, and
   frontend calls; must pass the `deploy.sh` + `regression_check.py` gate.
2. **Reconcile OpenSpec tree with reality** (P0–P2 findings in
   docs/QA_REVIEW_OPENSPEC_COVERAGE.md) — rmm-core rewrite, path drift, PLANNED→COMPLETE
   flips, missing capability specs.
3. **Close RMM parity gaps** where scope is decided (see docs/GAP_ANALYSIS_RMM_PLATFORM.md):
   maintenance windows, offline-agent SLA alerting, agent auto-update channel,
   WinUpdate/AutomatedTask automation.
