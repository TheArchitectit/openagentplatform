# Sprint RELAY-04: Metering & Observability

**Sprint Date:** 2026-08-23 (Saturday)
**Archive After:** 2026-08-30 (+7 days)
**Sprint Focus:** Surface the relay's usage metering and liveness to operators via
an admin HTTP listener, reusing the existing `GetMetrics` (spec 4.3) and
structured slog fields (spec 6.1), with no net-new accounting logic. Frames are
already metered by RELAY-03.
**Priority:** P2 (Normal)
**Estimated Effort:** 1.5 hours
**Status:** PENDING

Spec reference: `openspec/specs/a2a-relay/spec.md` §4.3, §6.1, §7.1 R.4.
Decision gate: [RELAY-00](./RELAY-00_ARCHITECTURE_SECURITY.md).
Prerequisite: [RELAY-03](./RELAY-03_WSS_MATCHING_FORWARDING.md).

---

## SAFETY PROTOCOLS (MANDATORY)

| Check | Requirement | Verify |
|-------|-------------|--------|
| **READ FIRST** | Read `relay.go` (GetMetrics) + `relay_metrics_test.go` | [ ] |
| **SCOPE LOCK** | Only files in scope below | [ ] |
| **NO FEATURE CREEP** | No discovery, no billing export, no new counters | [ ] |
| **PRODUCTION FIRST** | `metrics_http.go` before its test | [ ] |
| **TEST/PROD SEPARATION** | Tests in `metrics_http_test.go` | [ ] |
| **BACKUP AWARENESS** | Rollback command known (below) | [ ] |

Full guardrails: [docs/AGENT_GUARDRAILS.md](../AGENT_GUARDRAILS.md)

---

## PROBLEM STATEMENT

RELAY-03 forwards frames and meters them into `RelayMetrics`, but an operator has
no way to observe liveness or per-tenant usage without code. The relay already
computes correct metrics via `GetMetrics`; it just has no HTTP surface. This
sprint adds an operator admin listener exposing `GET /healthz` and
`GET /metrics` so a probe/scraper can consume them. No new accounting state is
introduced.

**Root Cause:** No observability surface exists for the relay service.

**Where:** `internal/relay/metrics_http.go` (new) + `cmd/relay/` admin wiring.

---

## SCOPE BOUNDARY

```
IN SCOPE (may create/modify):
  - File: internal/relay/metrics_http.go      NEW   /healthz, /metrics (per-tenant JSON)
  - File: internal/relay/metrics_http_test.go NEW   endpoint + payload tests
  - File: cmd/relay/config.go + main.go       ADDITIVE ONLY  -admin-addr flag + goroutine

OUT OF SCOPE (DO NOT TOUCH):
  - No aggregation/persistence/billing export (separate decision; no persistence)
  - No changes to GetMetrics / RelayMetrics fields
  - No discovery federation                    (RELAY-05)
  - No E2E encryption                          (spec E.4 BLOCKED)
```

---

## EXECUTION DIRECTIONS

### Overview

```
STEP 1: Admin HTTP server ----------------> /healthz + /metrics
STEP 2: Wire -admin-addr into cmd/relay --> operator surface live
STEP 3: Tests ----------------------------> green
DONE:   Commit observability --------------> RELAY-05 ready
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
- `GET /healthz` — always `200 {"status":"ok"}` (liveness only; no deep health).
- `GET /metrics?tenant=ID` — JSON of `s.GetMetrics(ctx, tenant)`; per spec 4.3 an
  unknown tenant returns a zeroed `RelayMetrics{tenantID}`, never 500.
- `GET /metrics` (no tenant) — JSON array across tenants via a small additive
  accessor (`ServedTenants()` / tenant iteration) under the relay mutex — do NOT
  bypass the lock.

Content-type `application/json`; use the existing marshaled field names
(`tenant_id`, `connection_count`, ...).

### STEP 2: Wire into `cmd/relay`

Add `-admin-addr` (default `:7001`) to `config.go`. In `main.go`, run `AdminServer`
in a goroutine and include it in shutdown teardown so it closes on signal.

### STEP 3: Tests

**Action:** Create `internal/relay/metrics_http_test.go` using `httptest`.

Tests (exact names):
- `TestRelayMetricsHandler_Healthz_200`.
- `TestRelayMetricsHandler_TenantMetrics_JSON` — after `EstablishConnection` +
  `RecordBytes`, `/metrics?tenant=X` matches `GetMetrics`.
- `TestRelayMetricsHandler_EmptyTenant_Zeroed` — unknown tenant → zeroed
  `RelayMetrics{tenant_id}` (spec 4.3).
- `TestRelayMetricsHandler_AllTenants`.
- Extend `cmd/relay/config_test.go` for `-admin-addr`.

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

- **Billing/aggregate export** — no persistence today (spec Known Limitations);
  this sprint exposes current in-memory metrics only.
- **Prometheus wire format** — out of scope; JSON is the contract.
- **Discovery federation** — RELAY-05.
- **E2E encryption (spec E.4)** — BLOCKED.

---

## QUICK REFERENCE CARD

```
+------------------------------------------------------------------+
| RELAY-04 : metering + observability                               |
| CREATE:    internal/relay/metrics_http.go, metrics_http_test.go  |
| REUSE:     GetMetrics (4.3) verbatim; admin listener only        |
| ROUTES:    GET /healthz, GET /metrics?tenant=<id>                |
| BLOCKERS:  billing export (separate), discovery (RELAY-05)       |
|            E2E encryption (E.4 BLOCKED)                          |
| ROLLBACK:  checkout metrics_http.go + cmd/relay; rm the test    |
+------------------------------------------------------------------+
```

---

**Created:** 2026-08-23
**Authored by:** TheArchitectit
**Archive Date:** 2026-08-30
**Version:** 1.0
