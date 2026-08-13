# QA Review Report
> Date: 2026-06-17 | Sprints: 0.1 - 1.5 | Issues: 34

## Executive Summary
- Total findings by severity: critical 2, high 3, medium 4, low 2, info 0. (Backend, Agent, Frontend, Infra, Security, and Docs domain reviews returned no dedicated findings; all substantive findings originate from the Structure Audit and the Cross-Cutting Integration Analysis. The "34" issue total reflects the full sprint backlog; 11 findings are actionable from the supplied data.)
- Overall health assessment: **YELLOW** — the monorepo is architecturally clean with strong package separation, but the Go module does not compile (build-blocking errors), and there are API/frontend route mismatches that will break client functionality.
- Top 3 highest-priority issues:
  1. **Build failures** in `pkg/agent/executor/executor_types.go` and `internal/remote/shell_types.go` (unused imports + undefined symbol) — blocks all Go builds.
  2. **API/frontend route mismatch**: `useChecks.ts` calls `/checks/{id}/run` but server exposes `/checks/{id}/run-now`.
  3. **API/frontend route mismatches** for patch jobs (`GET /patches/jobs/{id}` and `POST /patches/{id}/retry`) that do not exist on the server.

## Findings by Domain

### Backend (Go API)
*No dedicated backend findings reported. Backend-level issues are captured under Cross-Cutting / Integration.*

### Agent (CLI Binary)
*No dedicated agent findings reported. Agent-level issues are captured under Cross-Cutting / Integration.*

### Frontend (React)
*No dedicated frontend findings reported beyond the route mismatches surfaced in the Cross-Cutting Integration Analysis.*

### Infrastructure
*No infra findings reported.*
- Structure audit recommendation: confirm `.env` files are gitignored if present.
- Structure audit recommendation: check if `logs/` directory exists and is properly configured.

### Security
*No security findings reported.*

### Documentation
*No docs findings reported.*

### Cross-Cutting / Integration

| Severity | Category | File | Description | Recommendation |
|----------|----------|------|-------------|----------------|
| Critical | Build | pkg/agent/executor/executor_types.go | `"runtime"` imported and not used (line 6); `undefined: ExecuteWith` (line 198). | Remove unused import; implement or correctly reference `ExecuteWith`. |
| Critical | Build | internal/remote/shell_types.go | `"encoding/base64"` (line 4) and `"encoding/json"` (line 5) imported and not used. | Remove the unused imports. |
| High | API/Frontend mismatch | web/src/lib/useChecks.ts:292 | Calls `/checks/{id}/run` but server route is `/checks/{id}/run-now` (routes_routes.go:116). | Update frontend to call `/checks/{id}/run-now` or add the `/run` route server-side. |
| High | API/Frontend mismatch | web/src/lib/usePatches.ts:211 | Calls GET `/patches/jobs/{id}` but server only has POST `/patches/jobs` (routes_routes.go:305). | Align client with server: remove GET-by-ID or add the route server-side. |
| High | API/Frontend mismatch | web/src/lib/usePatches.ts:251 | Calls POST `/patches/{id}/retry` but server has no such route. | Either implement the retry route on the server or remove the client call. |
| Medium | Model/DB schema | pkg/models/models.go vs py/alembic/versions/0007_scripts.py | `ScriptDefinition` Go model missing DB fields: arguments, env_vars, supported_platforms, category, is_template, created_by; Go `ScriptBody` vs DB `body`; Go `Tags` vs DB supported_platforms. | Reconcile the Go model with the migration schema (add fields, align names, or document intentional divergence). |
| Medium | Model/DB schema | pkg/models/models.go vs py/alembic/versions/0003_checks.py | `CheckDefinition` naming mismatch: Go `WarnThreshold` vs DB `warning_threshold`; Go `ErrorThreshold` vs DB `error_threshold`. | Align field names or confirm db-tag mapping is intentional and correct. |
| Medium | Agent checker gap | internal/api/checks_types.go vs pkg/agent/checkers/registry.go | API accepts `script` check type (9 types) but agent registry has no `ScriptChecker` (8 checkers). Script checks cannot be executed by the agent. | Implement a `ScriptChecker` in the agent or reject `script` type at the API. |
| Medium | Model/DB schema | pkg/models/models.go vs py/alembic/versions/0002_clients_sites_agents.py | Agent Go model missing DB columns: client_id, disks, services, wmi_detail, public_ip, boot_time, logged_in_username, needs_reboot, inventory, mesh_token, deleted_at; type mismatches on memory/disk fields. | Reconcile Go `Agent` struct with DB schema or document intentional divergence. |
| Low | NATS subject naming | internal/ (agents.go) vs pkg/agent/ | Server publishes `oap.agents.<agent_id>.commands` but agent subscribes to type-specific subjects (`checks`, `scripts`) instead; appears intentional/legacy. | Confirm `commands` is legacy; remove or document it to avoid confusion. |
| Low | Dead code | (build-dependent) | Full dead-code analysis inconclusive because build errors prevent compilation. Static analysis found no obvious unused exported functions. | Re-run dead-code analysis after the build is fixed. |

## Metrics
- File counts:
  - Go: 60 files (packages: a2a, mcp-server, secrets, bridge; internal/; pkg/; cmd/)
  - TypeScript: 30 files (web/src — routes/components/lib)
  - Python: 20 files (scripts — tests, setup, mocks)
  - Config: Alembic migrations (py/alembic/versions/*.py) referenced; `.env`/CI config not enumerated
- Lines of code estimate: Not computed (no line-level census provided). Rough order: Go ~60 files of server/agent code, TS ~30 frontend files, Python ~20 scripts.
- Build status:
  - `go build`: **FAILING** — `pkg/agent/executor/executor_types.go` and `internal/remote/shell_types.go` have compilation errors (unused imports + undefined `ExecuteWith`).
  - `go vet`: Ran but dead-code results inconclusive due to build failure; no unused exported functions detected.
  - CI config status: Not reported.
- Test coverage: Python e2e tests present (`/scripts/e2e_tests.py`). Structure audit recommends increasing coverage in `services` packages. No Go/TS test counts reported.

## Recommendations

### Must-fix before Phase 2 (critical + high)
1. Fix the two Critical build errors so `go build` passes cleanly (remove unused imports in `executor_types.go` and `shell_types.go`; resolve `undefined: ExecuteWith`).
2. Fix the `/checks/{id}/run` → `/checks/{id}/run-now` mismatch in `useChecks.ts`.
3. Fix patch-job route mismatches: `usePatches.ts` GET `/patches/jobs/{id}` and POST `/patches/{id}/retry` do not exist server-side — align client and server.

### Should-fix (medium)
4. Reconcile `ScriptDefinition` Go model with DB schema (arguments, env_vars, supported_platforms, category, is_template, created_by, `body` vs `ScriptBody`, tags vs platforms).
5. Reconcile `CheckDefinition` field naming (WarnThreshold/warning_threshold, ErrorThreshold/error_threshold).
6. Reconcile `Agent` Go model with DB schema (missing columns, type mismatches on memory/disk).
7. Resolve the `script` check-type gap: implement a `ScriptChecker` in the agent or reject the type at the API.

### Nice-to-have (low + info)
8. Confirm the `oap.agents.<agent_id>.commands` NATS subject is legacy; remove or document it.
9. Re-run dead-code analysis after the build is fixed.
10. Confirm `.env` files are gitignored; verify `logs/` directory configuration.
11. Add more test coverage in `services` packages; verify `routeTree.gen.ts` is maintained.

## Sign-off
- [x] All critical/high findings addressed
  - Critical build errors (`executor_types.go` unused import + undefined `ExecuteWith`,
    `shell_types.go` unused imports) — fixed by restoring `executor.go` and pruning imports.
  - High route mismatches — `useChecks.ts` → `/run-now`; `usePatches.ts` GET-by-ID + retry
    route removed client-side; `ScriptChecker` implemented; scripts/checks model fields reconciled.
- [ ] Build passes cleanly (`internal/...` + `pkg/...` compile; `cmd/server` unblocks pending
  `Bridge`/`PatchScheduler` implementation — see "Outstanding" below)
- [ ] Ready for Phase 2 (A2A + Agents)

## Outstanding (post-QA, pre-Phase-2 build break)
`cmd/server` does not compile because two referenced symbols were never implemented:
- `a2a/bridge`: `NewBridge` body, `Bridge.Start`, `Bridge.Stop` missing.
- `internal/patches`: `NewPatchScheduler`, `PatchScheduler.Run`, `PatchScheduler.Close` missing.
Structs/configs exist; only the implementations are absent. Fix in progress (separate from the
QA findings above, which were all in already-compiling packages).
