# Reporting

> **Phase:** 6 (Commercial Tiering)
> **STATUS: COMPLETE** (`internal/reports/` is wired and functional since W4)
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** internal/reporting/ (dead code), internal/reports/ (wired)

---

## Description

Enterprise reporting in OpenAgentPlatform exists as **two parallel
implementations that do not share any code**, and this spec documents both.

**`internal/reports/` — the report delivery engine (the wired package).**
This is the implementation the HTTP API actually imports. It is a
PostgreSQL-backed pipeline with four stages: a `ReportEngine`
(`internal/reports/engine.go`) that generates reports from one of 7 built-in
templates via a `DataAggregator` interface; a `PGStore`
(`internal/reports/store.go`) persisting templates, runs, and schedules in
three tables; a tick-based `Scheduler` (`internal/reports/scheduler.go`) that
fires due schedules with a concurrency cap of 5 and a 60 s per-run timeout;
and a `DefaultDeliverer` (`internal/reports/delivery.go`) that sends finished
reports by SMTP email, webhook POST, or presigned download link. The HTTP
surface lives in `internal/api/reports.go`, registered in
`internal/api/routes_routes.go` under `/api/v1/reports`.

**`internal/reporting/` — an in-memory enterprise reporting service (dead
code).** A single file (`internal/reporting/reporting.go`, 386 lines)
implementing a `ReportService` with 4 hardcoded templates, placeholder report
data, CSV export, and daily/weekly/monthly schedule arithmetic — all in
in-memory maps with no persistence. It has 19 unit tests but **zero importers
anywhere in the repository**: no package in `cmd/`, `internal/api`, or
elsewhere references it. It was committed *later* (`c142f2d`, 2026-08-22) than
the delivery engine (`6a1074c`, Phase 6, 2026-06-17). It does not supersede
`internal/reports/` at runtime — it is unreachable.

**Which is used where:** `internal/reports/` is the only implementation with a
runtime role. Since remediation W4, `cmd/server` wires the whole stack
(`wireReports` in `cmd/server/server_reports.go`): it constructs a pgx-backed
`PGStore` and a `PGAggregator` (the `DataAggregator`), a `ReportEngine`, a
`DefaultDeliverer` (SMTP + download secret + base URL from `REPORTS_*` env
vars), and a `Scheduler`, then injects them via
`SetReportsStore`/`SetReportsScheduler`/`SetReportsDeliverer`
(`internal/api/server_wiring.go`). The `/api/v1/reports` endpoints therefore
answer real data and schedules fire. Schema creation is idempotent but
non-fatal — when it fails the endpoints fall back to 503. `internal/reporting/`
remains used nowhere (dead code; see Known Limitations #1).

## User Story

**As** an enterprise org administrator,
**I want** to generate compliance, inventory, alert, and patch reports from
built-in templates — on demand or on a cron schedule — and have them delivered
by email, webhook, or download link,
**so that** my team has recurring audit and executive visibility without
hand-assembling data.

---

## Requirements

### 1. Report Templates and Generation Engine (`internal/reports/engine.go`)

1.1. The engine MUST support exactly 7 built-in template IDs, enumerated in
`AllTemplates`: `agent_inventory`, `check_compliance`, `alert_summary`,
`patch_compliance`, `audit_trail`, `usage_summary`, `executive_summary`.

1.2. `ReportEngine.GenerateReport` MUST reject an empty `orgID` and any
`templateID` not contained in `AllTemplates` (validated via
`AllTemplatesContains`), and MUST default an empty format to `FormatJSON`.

1.3. Data assembly MUST be delegated to the `DataAggregator` interface, which
declares one method per template (`AggregateAgentInventory`,
`AggregateCheckCompliance`, `AggregateAlertSummary`,
`AggregatePatchCompliance`, `AggregateAuditTrail`, `AggregateUsageSummary`,
`AggregateExecutiveSummary`); the engine dispatches by template ID.

1.4. A generated `Report` MUST carry a UUID `ID`, the org and template IDs, a
human-readable `Title` (via `titleFor`), a UTC `GeneratedAt` timestamp, the
aggregated payload as `json.RawMessage`, and `DeliveryStatus` initialized to
`DeliveryPending` — delivery is the scheduler/delivery layer's job, not the
engine's.

### 2. Persistence (`internal/reports/store.go`)

2.1. `PGStore` MUST run over a `pgxpool.Pool` and `EnsureSchema` MUST create
three tables if missing — `report_templates`, `report_runs`,
`report_schedules` — plus indexes `idx_report_templates_org`,
`idx_report_runs_org (org_id, started_at DESC)`, and
`idx_report_schedules_org`.

2.2. The `Store` interface MUST provide org-scoped CRUD for templates
(create/get/list/update/delete), runs (`CreateRun`, `GetRun`, `ListRuns`,
`UpdateRunStatus`), and schedules (create/get/list/update/delete). All reads
MUST filter by `org_id` except `UpdateRunStatus`, which keys on run ID alone.

2.3. `ListRuns` MUST order by `started_at DESC` and default `limit` to 50
when a non-positive limit is passed.

2.4. Missing records MUST surface as the sentinel `ErrNotFound` (mapped from
`pgx.ErrNoRows`); update/delete operations on zero rows MUST return it.

2.5. `ReportRun` MUST track lifecycle state: `status` (`running`,
`completed`, `failed`), `delivery_status` (`pending`, `delivered`, `failed`,
`download`), `delivery_target`, `error_message`, `started_at`, `completed_at`,
and `duration_ms`.

### 3. Scheduling (`internal/reports/scheduler.go`)

3.1. The `Scheduler` MUST run a tick loop every `TickInterval` (30 s) rather
than depend on a third-party cron library; each tick queries due schedules
**across all orgs** via `ListDueSchedules(now)` (enabled rows whose
`NextRunAt` is in the past) and triggers them. (W4 corrected the pre-wiring
tick, which called `ListSchedules(ctx, "")` — an org-less query that matched
no rows.)

3.2. Concurrent report executions MUST be limited to `MaxConcurrentReports`
(5) via a semaphore, and each execution MUST run under a `ReportTimeout`
(60 s) context; failing to acquire the semaphore before the deadline MUST fail
the run.

3.3. `RunNow` MUST persist a `running` `ReportRun` record synchronously and
execute generation + delivery asynchronously in a goroutine; it is the entry
point used by the API for manual generation.

3.4. `computeNextRun` MUST support the cron subset `@hourly`, `@daily`,
`@weekly` (Sunday), `@monthly`, and `M H * * *` (minute/hour fields only;
day-of-month/month/day-of-week are parsed but ignored). An unparseable
expression MUST yield a nil next-run time.

3.5. `AddSchedule` MUST require a non-empty `CronExpr` and MUST persist the
schedule with a computed `NextRunAt`. After a scheduled run fires, the tick
MUST persist updated `LastRunAt`/`NextRunAt`.

### 4. Delivery (`internal/reports/delivery.go`, `helpers.go`)

4.1. `DefaultDeliverer.Deliver` MUST support three delivery methods:
`email`, `webhook`, and `download`; `download` and an empty method MUST
return `DeliveryDownload` without transmitting; unknown methods MUST return
`DeliveryFailed`.

4.2. Email delivery MUST require a target address and a configured
`SMTPHost`; it MUST send an HTML message (subject `Report: <title>`, body
containing the title, RFC 3339 generation time, and the raw JSON data in a
`<pre>` block) using `smtp.SendMail`, with `smtp.PlainAuth` only when a
username is configured.

4.3. Webhook delivery MUST POST the JSON-serialized full `Report` to the
target URL with `Content-Type: application/json`, `X-Report-Id`, and
`X-Report-Template` headers, using an HTTP client with a 30 s timeout; any
response status ≥ 400 MUST fail the delivery.

4.4. Download delivery MUST be able to produce a time-limited presigned URL
via `PresignedURL`: `{BaseURL}/api/v1/reports/runs/{id}/download?token=…&exp=…`
where the token is base64url of `reportID|expiry|mac` and `mac` is
HMAC-SHA256 over `reportID|expiry` keyed with `DownloadSecret`
(`hmacSum` in `helpers.go`). Since W4 the corresponding
`GET /api/v1/reports/runs/{id}/download` route exists
(`internal/api/routes_routes.go`, `downloadReport`) and redeems tokens via
`VerifyDownloadToken` (HMAC over org + report ID + expiry), using token auth
rather than session auth so links open outside the app; W4 pinned it with
`delivery_token_test.go`.

### 5. HTTP API (`internal/api/reports.go`, `internal/api/routes_routes.go`)

5.1. Report routes MUST be registered under `/api/v1/reports` inside the
auth-verified tenant group (kept out of `mountAPISubRoutes` per
`routes_sub.go`):

| Method | Path | Purpose | Roles |
|--------|------|---------|-------|
| GET | `/api/v1/reports/templates` | List org templates + built-in template IDs | any authenticated org member |
| GET | `/api/v1/reports/runs` | List run history (`limit`, `offset`) | any authenticated org member |
| GET | `/api/v1/reports/runs/{id}` | Fetch one run | any authenticated org member |
| POST | `/api/v1/reports/generate` | Trigger an immediate run | admin, technician |
| POST | `/api/v1/reports/schedules` | Create cron schedule | admin, technician |
| GET | `/api/v1/reports/schedules` | List org schedules | admin, technician |
| DELETE | `/api/v1/reports/schedules/{id}` | Delete schedule | admin, technician |

5.2. All report endpoints MUST return `503` with
`{"error":"reports unavailable"}` when the reports `Store` (and, for
generate/schedule-mutating endpoints, the `Scheduler`) is not wired into the
`Server`.

5.3. Every endpoint MUST extract the org from `auth.UserFromContext` claims
and MUST return `401` when claims are absent; org scoping MUST flow into
every `Store` call.

5.4. `POST /generate` MUST reject (`400`) a missing `template_id` or a
template not in `reports.AllTemplates`, and MUST answer `202 Accepted` with
the created `ReportRun`. `POST /schedules` MUST reject (`400`) a missing
`template_id` or `cron_expr`; `enabled` MUST default to `true` when omitted.

### 6. In-Memory Reporting Service (`internal/reporting/reporting.go`)

6.1. `ReportService` MUST maintain in-memory maps of templates, generated
reports, and schedules (no persistence), initialized by
`registerDefaultTemplates` with 4 templates: `compliance`, `patch_status`,
`alert_history`, `endpoint_inventory`, each declaring an explicit column list.

6.2. `GenerateReport` MUST validate the template, build an ID of the form
`rpt_{tenantID}_{templateID}_{unixnano}`, and populate `Data` via
`generateReportData` — which currently returns hardcoded placeholder rows
(`ep-1`/`ep-2`) rather than querying any data source.

6.3. `ExportCSV` MUST write the template's column names as the CSV header and
one row per `map[string]interface{}` data record, formatting values with
`fmt.Sprintf("%v", val)`.

6.4. `CreateSchedule` MUST validate the template and compute `NextRun` via
`calculateNextRun`: daily → next day 08:00 UTC; weekly → next Monday 08:00
UTC; monthly → 1st of next month 08:00 UTC. Schedules MUST support
enable/disable/delete. This package declares `ReportFormatPDF` but implements
CSV export only.

---

## Known Limitations

1. **Dual-package duplication (still open).** `internal/reports/` (Phase 6
   commit `6a1074c`, 2026-06-17) and `internal/reporting/` (commit `c142f2d`,
   2026-08-22) implement overlapping functionality with different models
   (org-scoped PG pipeline with 7 templates vs. tenant-scoped in-memory
   service with 4 templates and placeholder data). `internal/reports/` is the
   wired delivery engine (W4); `internal/reporting/` still has zero importers
   and is dead code. The two remain unconverged — consolidation is needed.
2. **Cron subset only.** Only `@hourly`/`@daily`/`@weekly`/`@monthly` and
   `M H * * *` are supported; day-of-month/month/day-of-week fields are
   ignored. An invalid expression produces a nil `NextRunAt`, silently and
   permanently disabling the schedule.
3. **No delivery hardening.** Webhook payloads are not signed (contrast with
   A2A push-notification HMAC signing) and failed deliveries are not retried
   — the run is simply marked `DeliveryFailed`.
4. **PDF is declared but not implemented** in either package (both expose a
   `pdf` format constant; only `internal/reporting/` implements any exporter,
   and that is CSV).
5. **Test coverage is inverted (partially closed).** `internal/reporting/`
   (the dead package) has 19 test functions; `internal/reports/` gained only
   `delivery_token_test.go` (W4: sign/verify round-trip, wrong secret,
   expired, tampered). The wire path itself — aggregator joins, engine
   dispatch, scheduler, and API handlers — remains untested.
6. **`internal/reporting/` is not concurrency-safe:** `ReportService` mutates
   plain maps without a mutex.
7. **W4 wiring is non-fatal on schema failure.** `wireReports` creates the
   report tables idempotently and logs-and-continues on failure, so a broken
   schema silently degrades every report endpoint to `503` with no startup
   abort — operators get no hard signal during deployment.
