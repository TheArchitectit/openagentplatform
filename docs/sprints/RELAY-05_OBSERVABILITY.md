# Sprint RELAY-05: Relay Observability — Health & Metrics Endpoints

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Expose relay liveness and per-tenant usage over an operator
admin HTTP listener, reusing the existing `GetMetrics` (spec 4.3) and structured
slog fields (spec 6.1), with no net-new accounting logic.
**Priority:** P2 (Normal)
**Estimated Effort:** 1.5 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §7.1; accounting §4.3 and §6.1
reused verbatim.

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` (GetMetrics) + `relay_metrics_test.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No auth, no persistence, no new counters | [ ] |
| **PRODUCTION FIRST** | `metrics_http.go` before its test | [ ] |
| **TEST/PROD SEPARATION** | Tests in `metrics_http_test.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

An operator running `cmd/relay` (RELAY-04) has no way to observe liveness or
per-tenant usage without code. The relay already computes correct
`RelayMetrics` via `GetMetrics`; it just has no HTTP surface. This sprint adds a
separate admin listener exposing `GET /healthz` and `GET /metrics`, so an
external scraper/orchestrator (e.g. a probe or Prometheus exporter later) can
consume them. No new accounting state is introduced.

**Root Cause:** No observability surface existed for the relay service.

**Where:** `internal/relay/metrics_http.go` (new).

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/metrics_http.go      NEW
    Change: admin HTTP server: /healthz, /metrics (per-tenant JSON)
  - File: internal/relay/metrics_http_test.go NEW
    Change: endpoint + payload tests
  - File: cmd/relay/config.go + main.go       ADDITIVE ONLY
    Change: -admin-addr flag and goroutine serving the admin listener

OUT OF SCOPE (DO NOT TOUCH):
  - No aggregation/persistence of metrics (billing export is a separate decision)
  - No changes to GetMetrics / RelayMetrics fields
  - No per-leg authentication or E2E encryption (blockers S.1/S.2)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Admin HTTP server -----------------> /healthz + /metrics
STEP 2: Wire -admin-addr into cmd/relay ----> operator surface live
STEP 3: Tests ------------------------------> green
DONE:   Commit observability ----------------> RELAY-06 ready
```

---

## STEP-BY-STEP EXECUTION

### STEP 1: Admin HTTP server

**Action:** Create `internal/relay/metrics_http.go`:

```go
func (s *RelayService) AdminServer(addr string) *http.Server
func (s *RelayService) metricsHandler(w http.ResponseWriter, r *http.Request)
```

Routes:
- `GET /healthz` — always `200 {"status":"ok"}` (liveness only; do NOT encode
  deep health).
- `GET /metrics?tenant=ID` — returns JSON of `s.GetMetrics(ctx, tenant)`
  (spec 4.3 responds with a zeroed `RelayMetrics{tenantID}` when absent — the
  handler reflects that, never 500). `GET /metrics` with no `tenant` returns a
  JSON array of all tenants' metrics (iterated under the relay mutex via a small
  additive accessor like `ServedTenants()` — do NOT bypass the lock).

Content-type `application/json`; structure is the existing marshaled field
names (`tenant_id`, `connection_count`, ...).

### STEP 2: Wire into `cmd/relay`

Add `-admin-addr` (default `:7001`) to `config.go`. In `main.go`, run the admin
server in a goroutine, and include it in `Shutdown` teardown (RELAY-03) so the
admin listener closes on SIGINT/SIGTERM.

### STEP 3: Tests

**Action:** Create `internal/relay/metrics_http_test.go` using `httptest`.

Tests (exact names):
- `TestRelayMetricsHandler_Healthz_200`.
- `TestRelayMetricsHandler_TenantMetrics_JSON` — after `EstablishConnection` +
  `RecordBytes`, `/metrics?tenant=X` returns `connection_count`, `total_bytes_relayed`,
  `total_connections` matching `GetMetrics`.
- `TestRelayMetricsHandler_EmptyTenant_Zeroed` — unknown tenant returns zeroed
  `RelayMetrics` with that `tenant_id` (mirrors spec 4.3).
- `TestRelayMetricsHandler_AllTenants` — `/metrics` (no param) lists every tenant.

Also extend `cmd/relay/config_test.go` for `-admin-addr` parsing.

**Validation loop (max 3):**
```
go build ./internal/relay/ ./cmd/relay/
go test ./internal/relay/ -run TestRelayMetricsHandler -v
go test ./cmd/relay/ -v
```

**Decision Point:**
- [ ] Green → proceed
- [ ] Red → fix, re-run (ROLLBACK if stuck)

---

## ACCEPTANCE CRITERIA

| # | Criterion | Test | Pass Condition |
|---|-----------|------|----------------|
| 1 | Health endpoint | `..._Healthz_200` | 200 + ok |
| 2 | Tenant payload correct | `..._TenantMetrics_JSON` | Matches GetMetrics |
| 3 | Empty tenant zeroed | `..._EmptyTenant_Zeroed` | Zeroed + tenant_id |
| 4 | All-tenants list | `..._AllTenants` | All represented |
| 5 | Contract intact | full `go test ./internal/relay/` | Prior tests pass |

---

## ROLLBACK PROCEDURE

```bash
git checkout HEAD -- internal/relay/metrics_http.go cmd/relay/config.go cmd/relay/main.go
git rm -f internal/relay/metrics_http_test.go
git status
```

---

## BLOCKERS / DEFERRED

- **Billing/aggregate export** — exporting meters to a durable store is a
  separate decision (no persistence today, spec Known Limitations); this sprint
  only exposes current in-memory metrics.
- **Prometheus wire format** — out of scope; JSON is the contract here.
- **Per-leg authentication / E2E encryption (spec S.1, S.2)** — not implemented.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-05 : observability admin HTTP surface                       |
| CREATE:    internal/relay/metrics_http.go, metrics_http_test.go   |
| REUSE:     GetMetrics (4.3) verbatim; admin listener only        |
| ROUTES:    GET /healthz, GET /metrics?tenant=<id>                 |
| BLOCKERS:  billing export (separate), auth (S.1), E2E (S.2)       |
| ROLLBACK:  git checkout HEAD on metrics_http.go + cmd/relay      |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
