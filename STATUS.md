# Project Status — OpenAgentPlatform

**Last Updated:** 2026-09-01
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

**Resolved since v1.2.0:**

- **RMM deferred sprints COMPLETE (2026-08-24/25).** All nine RMM-00..09 items
  shipped on `main`: RMM-00 ops foundation, RMM-01 offline-SLA alerting
  (`6ff3a17`), RMM-02 alert-suppression windows (`4cf02a0`-era `4cf3602`),
  RMM-03 WinUpdate per-KB state machine, RMM-04 reboot coordination, RMM-05
  CVE correlation, RMM-06 scheduled automation (cron grammar, `3f8e495`),
  RMM-07/08 merged into RMM-09, RMM-09 secure tunnel fabric (steps 1–5 +
  Ed25519 self-update, `5ea6076`). `rmm-operations` spec §2–§3 marked
  COMPLETE at the source.
- **Auth-rbac §14 first-boot org bootstrap** shipped (`dd4fe0f` + race-arbiter
  fix `a3a7680`): `POST /api/v1/auth/bootstrap` with `BOOTSTRAP_TOKEN`,
  `user_org_bindings`/`app_state` (migration 004), login org resolution.
  Live-verified on the ai04 box, including the real stack's in-place
  self-migration to schema v4 (deploy/AI04_DEPLOY_NOTES.md §8–§9).
- **Single canonical schema source:** golang-migrate runner with embedded
  migrations applied at boot (`852e0f7`); the Alembic set and loose
  `deploy/migrations/` copies are deleted (`2dd82ed`, `86723ce`).

| Item | Severity | Tracking |
|------|----------|----------|
| OpenSpec P3 RMM parity gaps remain open — cloud control plane, hypervisor monitoring, active security/EDR, power/UPS monitoring are absent in both code and spec. RMM-00..09 are shipped; what remains is genuinely new surface, spec-first per the audit process. | Medium | openspec/specs/rmm-operations/spec.md; docs/plans/DEFERRED_WORK_HANDOFF.md |
| Managed A2A relay is complete as code (RELAY-00..06, acceptance green, audit `460b18a`) but not yet operated: next steps are compose deployment wiring + state persistence (spec Known Limitations). In progress 2026-09-01. | Medium | openspec/specs/a2a-relay/spec.md; docs/plans/DEFERRED_WORK_HANDOFF.md §5.2 |
| Separate spec-review repository publication is blocked until the user supplies an authorized remote URL. | Low | docs/reviews/SPEC_REVIEW_BUNDLE_HANDOFF.md |

---

## Verification Gates

All changes ship through the standard gate:

- `deploy.sh` + `regression_check.py` (see `docs/workflows/REGRESSION_PREVENTION.md`)
- CI workflows: `.github/workflows/` (10 workflows incl. security scan + release)
