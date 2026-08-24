# Project Status — OpenAgentPlatform

**Last Updated:** 2026-08-23
**Branch:** main
**Current Version:** v1.1.0
**Roadmap:** [docs/plans/MASTER_IMPLEMENTATION_PLAN.md](docs/plans/MASTER_IMPLEMENTATION_PLAN.md)
**Changelog:** [CHANGELOG.md](CHANGELOG.md)

> Note: this file previously described the Guardrail MCP Server (a separate module in
> `mcp-server/`). That content has been archived; see git history if you need it.

---

## What This Is

OpenAgentPlatform is an agent-first RMM + A2A platform, delivered as a monorepo:

| Component | Path | Stack |
|-----------|------|-------|
| API server | `cmd/server`, `internal/*` | Go |
| Endpoint agent | `cmd/agent`, `pkg/agent/*` | Go (single static binary) |
| A2A gateway | `a2a/*` | Go (separate module) |
| Framework adapters | `py/oap/adapters/` | Python / FastAPI |
| Secrets platform | `secrets/*` | Go (separate module) |
| Web UI | `web/` | React SPA |

Go workspace modules (`go.work`): root `.`, `./a2a`, `./secrets`. `mcp-server/` is an
unrelated vendored module (`guardrail-mcp`), not part of the platform.

---

## Phase Status

Phases follow `MASTER_IMPLEMENTATION_PLAN.md` §6 (7 phases, 46 weeks):

| Phase | Focus | Status |
|-------|-------|--------|
| 0 | Foundation: scaffold, DB, NATS, auth, agent MVP | ✅ Complete |
| 1 | Core RMM: checks, alerts, policies, patches, scripts, remote | ✅ Complete |
| 2 | A2A + Agents: gateway, adapters, process pool, bridge | ✅ Complete |
| 3 | Secret Management: Vault, Infisical, reference resolver | ✅ Complete |
| 4 | Frontend: full React UI, dashboards, real-time, terminal | ✅ Complete |
| 5 | Production: observability, load test, security, docs, CI/CD | ✅ Complete |
| 6 | Commercial: gating, multi-tenancy, relay, reporting, billing | ✅ Complete (v1.1.0) |

Phase 6 delivery detail: Sprint 6.1 feature gating + BSL licensing,
6.2 multi-tenancy, 6.3 Stripe billing + enterprise reporting + managed relay.
See `CHANGELOG.md` for release notes.

---

## Known Issues / Outstanding Work

| Item | Severity | Tracking |
|------|----------|----------|
| Built-but-unwired subsystems: heartbeats never persist (decode contract broken), check results double-inserted, notifications/shell/reports handlers return 503, RLS never executed, adapter proxy path mismatch | **Critical** | docs/SPRINT_WIRING_REMEDIATION_PLAN.md (W1–W8) |
| A2A dashboard broken end-to-end — proxy exists but upstream paths/formats mismatch Python; registry empty at runtime | High | SPRINT_WIRING_REMEDIATION_PLAN W7; openspec/specs/adapter-service |
| OpenSpec tree reconciled 2026-08-23 (P0–P2 done); P3 RMM parity gaps remain: WinUpdate/AutomatedTask automation, maintenance windows, agent auto-update, offline-agent SLA alerting, cloud/hypervisor monitoring | Medium | docs/QA_REVIEW_OPENSPEC_COVERAGE.md; GAP_ANALYSIS_RMM_PLATFORM.md |

---

## Verification Gates

All changes ship through the standard gate:

- `deploy.sh` + `regression_check.py` (see `docs/workflows/REGRESSION_PREVENTION.md`)
- CI workflows: `.github/workflows/` (10 workflows incl. security scan + release)
