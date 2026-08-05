# OAP 10-Day Autonomous Workstream — PRD

**Repo:** openagentplatform (fully public, Go + Python + React/pnpm monorepo)
**Orchestrator:** EpicJaguar (pi-messenger Crew)
**Mode:** Autonomous — workers execute, reviewer gates, no human input required.
**Contract:** Every change must keep the repo buildable and must NOT introduce
secrets (public repo — hard gate via `scripts/scan-secrets.sh` and
`scripts/regression_check.py`).

---

## Context / Completed Foundation (DO NOT REDO)

Already merged — these are done:

- `scripts/regression_check.py` — evolved file-size + soft-as-hard headroom +
  pnpm-audit + settings-leak gate (commit `ed2ca32`).
- `deploy.sh` + `scripts/scan-secrets.sh` — public-repo release pipeline with a
  hard secret/sensitive-data gate (commit `b0badfa`).

A guardrails adaptation pass exists at `docs/GAP_ANALYSIS_RMM_PLATFORM.md`.

Constraints for ALL tasks:

- `web/` is built with pnpm (dockerized CI). There is NO local `node_modules`.
  Workers MUST NOT attempt `pnpm install` / `pnpm build` locally (no network
  toolchain). Instead: keep TypeScript valid, add deps to `package.json` AND
  verify they already exist in `pnpm-lock.yaml` (do not edit the lockfile by
  hand). Type-check with the global `tsc`.
- `scripts/scan-secrets.sh` MUST pass (exit 0) after any change.
- Go submodules (mcp-server/, cmd/team-cli/, etc.) are separate deployables —
  only the root `go vet ./...` is part of the release contract.

---

## Task Queue (10 days, ordered, dependency-safe)

### Tracker 01 — Dashboard foundation into web/

Port the pi-mega-compact dashboard's *generic, reusable* React infrastructure
into `web/src/` so OAP's WebUI is rebuilt on it. Do NOT rip out the 32 existing
TanStack routes. Deliver (each as a small reviewable PR-style unit):

1. `web/src/lib/cn.ts` — dependency-light `cn()` (use `clsx`, already a
   transitive dep; avoid new deps). If `tailwind-merge` is required, add it to
   `package.json` dependencies only (never the lockfile by hand).
2. `web/src/lib/format.ts` — `fmtBytes`, `fmtSec`, `fmtPct`, `fmtMs`, `fmtNum`
   (port from dashboard-client, adapted to OAP's existing formatting needs).
3. `web/src/components/ErrorBoundary.tsx` — top-level error boundary (port the
   battle-tested one). Wire into `web/src/app.tsx` around the router.
4. `web/src/hooks/useApi.ts` — typed fetch + exponential-backoff retry +
   optional polling, layered on OAP's existing `lib/api.ts` `apiFetch`
   (which already handles auth/401). Do NOT duplicate auth logic.
5. `web/src/hooks/useSSE.ts` — OR adapt OAP's existing `lib/websocket.ts`
   (multiplexed WS client) which is already superior to SSE. Prefer extending
   `websocket.ts` for live event surfaces over adding a new SSE hook.

Acceptance: all files present, tsc-clean against existing project types, no
new runtime secrets, `scan-secrets.sh` green.

### Tracker 02 — Guardrails completion

The `.guardrails/` prevention-rules were ported but the `deploy.sh` references
`pattern-rules.json` + `semantic-rules.json` + a failure registry. Verify:

1. `.guardrails/prevention-rules/pattern-rules.json` and
   `semantic-rules.json` are consistent with `scripts/regression_check.py`
   (which loads them). Fix any rule-id/severity mismatches.
2. `scripts/regression_check.py --all --json` runs clean (no crash) and the
   `.guardrails/failure-registry.jsonl` is valid.
3. `scripts/guardrails-scan.mjs` (if it exists, else skip) consistency.
Acceptance: `python3 scripts/regression_check.py --all --no-audit` exits 0.

### Tracker 03 — WebUI live-event surface

Use the foundation from Tracker 01 to add ONE real, user-visible live surface:

- A `Live Events` panel/drawer fed by `lib/websocket.ts` channels
  (`agents | checks | alerts | patches | scripts`), with auto-reconnect status
  indicator. Pick the highest-signal channel (alerts) first.
Acceptance: component compiles, tsc-clean, hooks wired to existing WS client,
no new secrets.

### Tracker 04 — Settings-leak audit hardening

`regression_check.py` has a settings-surface audit (WEB_SENSITIVE_SETTINGS).
Verify no runtime-sensitive setting (POSTGRES_DSN, JWT_SECRET, OIDC_CLIENT_
SECRET, SENTRY_DSN, NATS_*) leaks into `web/src/`. Fix any violation found.
Acceptance: `--no-audit` settings audit reports 0 sensitive leaks.

### Tracker 05 — Regression coverage

Add `scripts/regression_check.py` unit tests (a `tests/` for scripts) covering:

- `_classify_file` for web/py/go/test paths.
- soft-as-hard changed-file gating.
- settings leak detection (positive + negative).
Acceptance: `python3 -m pytest scripts/tests/` passes (or the repo's existing
py test runner).

### Tracker 06 — Docs / INDEX maps

Per CLAUDE.md, update `INDEX_MAP.md` / `HEADER_MAP.md` (if present) to document
`deploy.sh`, `scan-secrets.sh`, and the new dashboard foundation files.
Acceptance: docs reference the new scripts accurately, no fabricated paths.

### Tracker 07 — Final gate sweep

Run the full deploy gate logic in a dry-run (no push): clean tree check,
`scan-secrets.sh`, `regression_check --all --soft-as-hard --pre-commit
--no-audit`. Fix any findings introduced by the 10-day work.
Acceptance: all gates green, `git status` clean (committed), `git log` shows a
clear commit trail.

---

## Coordination Rules

- One writer per file at a time — workers reserve files before editing
  (`pi_messenger reserve`).
- Each tracker = 1-2 tasks. Trackers depend on 01 before 03; 02/04/05/06 are
  independent; 07 depends on all.
- After each task, run `scripts/scan-secrets.sh` (must be exit 0) and commit
  with a clear message.
- Reviewer gates: use `pi_messenger review` on each merged increment.
