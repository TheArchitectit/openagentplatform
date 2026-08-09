# Release v1.1.0 — File Size Compliance & Code Organization

Released: 2026-01-15

## Overview

This release introduces and enforces a **500-line hard limit** across all source files (Go, TypeScript/TSX, Python) to improve maintainability, review-ability, and code quality. **~45 files were split** into focused, single-responsibility modules.

## ⚠️ Known Violations (Documented)

The following **4 files exceed the 500-line hard limit** and are documented here as known technical debt. Work to split these is in progress:

| File | Lines | Over By | Status |
|---|---|---|---|
| `web/src/routes/patches/index.tsx` | 1781 | +1281 | Pending — needs 4+ file split (helpers, sub-components, custom hook) |
| `web/src/routes/patches/$jobId.tsx` | 1135 | +635 | Pending — needs 3+ file split (helpers, sub-components, custom hook) |
| `mcp-server/internal/mcp/server.go` | 888 | +388 | Pending — needs setupHandlers() refactoring to extract tool definitions |
| `web/src/routes/policies/$policyId.tsx` | 586 | +86 | Partial split done (helpers + hook extracted), right column JSX extraction pending |

## Key Changes

### 📐 File Size Limits (NEW)
- **Hard limit: 500 lines** per source file (all languages)
- **Soft limit: 300 lines** (warning threshold)
- Auto-generated files (`.gen.ts`, `.gen.tsx`) excluded from limits
- `scripts/regression_check.py` — automated enforcement with `--no-audit` mode
- 29 regression tests in `scripts/tests/` — all passing

### 🔧 Code Splits (Go — 30+ files split)

| Original File | Lines | Split Into |
|---|---|---|
| `mcp/tools_extended.go` | 2246 | 8 files |
| `mcp/team_tool_handlers.go` | 1399 | 4 files |
| `web/handlers.go` | 1375 | 6 files |
| `alerts/store.go` | 1356 | 4 files |
| `mcp/team_tool_handlers_test.go` | 1467 | 5 files |
| `api/routes.go` | 1194 | 3 files |
| `team-cli/main.go` | 1065 | 4 files |
| `mcp/server.go` (1st split) | 929 | 3 files |
| `team/manager.go` | 766 | 4 files |
| `policy/engine.go`, `opa.go`, `store.go` | 3×500+ | 9 files |
| `patches/`, `a2a/`, `oauth/`, `vault/` | multiple | 19 files |
| `metrics/metrics.go` | 721 | 2 files |
| `validation/engine.go` | 539 | 2 files |
| `ingest/rule_parser.go` | 530 | 2 files |
| `remote/shell.go` | 536 | 2 files |
| `a2a/bridge/bridge.go` | 533 | 2 files |
| `agent/executor/executor.go` | 528 | 2 files |
| `updates/checker.go` | 507 | 2 files |
| `patches/scheduler.go` | 541 | 2 files |
| `mcp-server/internal/metrics/metrics.go` | 721 | 2 files |
| `mcp-server/internal/mcp/integration_test.go` | 560 | 3 files |
| `mcp-server/internal/validation/engine.go` | 539 | 2 files |

### 🎨 Code Splits (TypeScript/TSX — 15+ files split)

| Original File | Lines | Split Into |
|---|---|---|
| `dashboard.tsx` | 772 | 3 files (page + hook + components) |
| `usePatches.ts` | 805 | 3 files (hook + types + WS hook) |
| `alerts/index.tsx` | 711 | 2 files |
| `checks/index.tsx` | 721 | 2 files |
| `monaco-editor.tsx` | 587 | 2 files |
| `useSettings.ts` | 657 | 2 files |
| `policy-editor.tsx` | 533 | 2 files |
| `users.tsx` | 576 | 2 files |
| `$scriptId.tsx` | 546 | 2 files |
| `sso.tsx` | 518 | 2 files |
| `useA2A.ts` | 522 | 2 files |
| `alerts/$alertId.tsx` | 839 | 3 files |
| `checks/$checkId.tsx` | 853 | 4 files |
| `policies/$policyId.tsx` | 864 | 3 files (partial — right column pending) |

### 🐍 Code Splits (Python — 1 file split)

| Original File | Lines | Split Into |
|---|---|---|
| `py/oap/adapters/cost.py` | 503 | 2 files |

### 🛠️ Refactoring Tools Created
- `scripts/go_splitter.py` — Go file splitter (line-based)
- `scripts/go_split_v2.py` — Go file splitter (package+import preserving)
- `scripts/regression_check.py` — File size limit enforcement

### 📊 Metrics
- **29/29 regression tests passing**
- **~45 files split** across Go, TS/TSX, and Python
- **4 files remaining** over 500-line limit (documented above)

## Upgrade Notes

No breaking changes. All splits are purely organizational — no behavioral changes.

## Previous Release

v1.0.0 — Initial release with security hardening, RBAC, A2A gateway, billing/metering, WebSocket hub, and web dashboard foundation.
