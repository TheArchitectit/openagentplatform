# Project Status — OpenAgentPlatform

**Last Updated:** 2026-08-23
**Branch:** main
**Current Version:** v1.2.0
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

**Resolved in v1.2.0 (2026-08-23):** the W1–W8 built-but-unwired subsystems
(heartbeat persistence, duplicate check results, notification/shell/reports 503s,
RLS execution, adapter proxy contract) and the A2A dashboard contract divergence
are now wired and reconciled. See [RELEASE_NOTES_v1.2.0.md](RELEASE_NOTES_v1.2.0.md)
and [docs/SPRINT_WIRING_REMEDIATION_PLAN.md](docs/SPRINT_WIRING_REMEDIATION_PLAN.md).

| Item | Severity | Tracking |
|------|----------|----------|
| OpenSpec P3 RMM parity gaps remain open: WinUpdate/AutomatedTask automation, maintenance windows, agent auto-update channel, offline-agent SLA alerting, cloud/hypervisor monitoring | Medium | docs/QA_REVIEW_OPENSPEC_COVERAGE.md; GAP_ANALYSIS_RMM_PLATFORM.md |
| Managed A2A relay transport not shipped — RelayService parked as a library pending network-transport + auth design | Low | openspec/specs/a2a-relay; internal/relay |

---

## Verification Gates

All changes ship through the standard gate:

- `deploy.sh` + `regression_check.py` (see `docs/workflows/REGRESSION_PREVENTION.md`)
- CI workflows: `.github/workflows/` (10 workflows incl. security scan + release)
