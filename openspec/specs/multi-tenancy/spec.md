# Multi-Tenancy

> **Phase:** 6 (Commercial: gating, multi-tenancy, relay, reporting, billing)
> **STATUS: PARTIAL** — isolation/quota/tier wiring applied (W6); store layer still unwired
> **Source:** authored 2026-08-23 from code (audit `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §4)
> **App Path:** `internal/tenancy/`
>
> Remediation W6 closed the main wiring gaps: `TenantMigrator` now runs at
> startup behind `OAP_ENABLE_TENANT_MIGRATIONS=1` (rewritten against the live
> `org_id` schema), tier resolution is backed by the signed license file via
> `SetTierResolver`, `QuotaMiddleware` is mounted with per-org usage from the
> billing metering service, `SetTenantContext` is parameterised, and the
> retention purger reads `alert_global_preferences` (not the phantom
> `alert_preferences`). The `TenantStore`/`TenantConfigStore` layer remains
> unwired.

---

## Description

The multi-tenancy package provides per-tenant data isolation, per-tier quota
limits, and retention-based cleanup for OpenAgentPlatform's commercial tiers
(Community / Professional / Enterprise, as defined in `internal/license`). It
implements the PostgreSQL Row-Level Security (RLS) isolation option named in
the master plan's Phase 6 risk register (R7) and open question O2, along with
a quota library keyed on `license.Tier` and a two-phase retention purger.

Isolation is modeled as three SQL migrations (`TenantMigrations` in
`rls_migration.go`): create a `tenants` table, add `org_id` indexes to the
eleven tenant-scoped tables (the live schema already carries TEXT `org_id`),
then ENABLE RLS on nine of them with a `tenant_isolation_<table>` policy
comparing `org_id = current_setting('app.tenant_id', true)` (rewritten in W6
from a `tenant_id` UUID FK model that did not match the live schema). Helper
functions (`WithTenant`, `SetTenantContext`) set that session variable around
query execution, `SetTenantContext` now via the parameterised
`set_config('app.tenant_id', $1, false)`.

Request-scoped tenancy is carried by `TenantContext` (org ID, tier, quota
snapshot, feature flags), attached by `TenantMiddleware`, which is mounted in
`internal/api/routes_routes.go` after auth verification. Quota enforcement is
a library (`EnforceQuota`, `QuotaMiddleware`) producing structured 429
responses; the retention purger (`RetentionPurger`) is a live background
worker that soft- then hard-deletes expired `audit_events` and
`check_results` rows.

The package is **wired for the isolation/quota/tier path** since remediation
W6: the tenant-context middleware and retention purger run in the server, the
`TenantMigrator` is applied at startup behind the
`OAP_ENABLE_TENANT_MIGRATIONS=1` flag (non-fatal on failure), tier resolution
is backed by the signed license file (`SetTierResolver`,
`cmd/server/server_tier.go`), and `QuotaMiddleware` is mounted after the
tenant middleware with per-org API-call usage sourced from the billing
metering service. The `TenantStore`/`TenantConfigStore` persistence layer
still has no callers. See Known Limitations.

## User Story

**As** a platform operator running a commercial deployment,
**I want** each customer organization's data strictly isolated at the database
layer, with per-tier caps on agents/users/sites/API traffic and automatic
retention cleanup,
**so that** I can host multiple paying customers on shared infrastructure and
sell Community, Professional, and Enterprise tiers without cross-tenant data
exposure.

---

## Requirements

### 1. Row-Level Security Schema

1.1. The package MUST define tenant schema migrations as an ordered slice
`TenantMigrations` of `TenantMigration{Version, Name, Up, Down}` values. Every
migration MUST have non-empty `Up` and `Down` SQL and a unique increasing
version (asserted by `TestTenantMigrationVersions`, `TestTenantMigrationSQL`).

1.2. Migration 1 (`create_tenants_table`) MUST create the `tenants` table with
a UUID primary key (`gen_random_uuid()`), required `name`, unique `slug`,
JSONB `settings`, timestamps, and soft-delete `deleted_at`, plus partial
indexes on `slug` and `deleted_at`.

1.3. Migration 2 (`add_org_indexes_to_tenant_tables` — rewritten in W6) MUST
NOT add a second tenancy column. The live application schema already carries
`TEXT org_id` on every tenant-scoped table, so this migration only adds
missing indexes on `org_id` for exactly these eleven tables: `agents`,
`check_definitions`, `check_results`, `alerts`, `alert_rules`, `policies`,
`script_definitions`, `script_runs`, `patch_jobs`, `report_templates`,
`audit_events`.

1.4. Every migration MUST be reversible; `Down` SQL MUST undo the `Up`
(`TestTenantMigrationRollback`).

### 2. RLS Policies and Tenant Session Context

2.1. Migration 3 (`enable_rls`) MUST issue `ENABLE ROW LEVEL SECURITY` on the
tenant-scoped tables (currently nine of the eleven that got indexes —
`agents`, `check_definitions`, `check_results`, `alerts`, `alert_rules`,
`policies`, `script_definitions`, `patch_jobs`, `audit_events`). This
implementation does not issue `FORCE ROW LEVEL SECURITY`.

2.2. Migration 3 MUST create one policy per table, named
`tenant_isolation_<table>`, `FOR ALL`, with both `USING` and `WITH CHECK`
expressions equal to `org_id = current_setting('app.tenant_id', true)` — the
session variable compared as **TEXT** against the string `org_id` column, not
as a UUID cast (verified by `TestTenantMigrationEnableRLS`,
`TestTenantMigrationPolicies`).

2.3. The migrator (`TenantMigrator.Migrate`) MUST track applied versions in a
`tenant_migrations` table, run only migrations newer than the current version,
and execute each migration and its version record in a single transaction.
`Rollback` MUST undo migrations in reverse order down to a target version.
In the shipped binary (`cmd/server/server_init.go`) the migrator runs only
when `OAP_ENABLE_TENANT_MIGRATIONS=1` and is non-fatal on failure.

2.4. Helper primitives MUST exist for per-connection tenant scoping:
`EnableRLS`, `ForceRLS`, `CreatePolicy` (with optional `WITH CHECK`),
`DropPolicy`, `SetTenantContext` (`SET app.tenant_id`), `ClearTenantContext`
(`RESET`), and `WithTenant`, which MUST set the context, run the callback, and
clear the context via deferred cleanup.

### 3. Tenant Context Middleware

3.1. A `TenantContext` MUST carry `OrgID`, `Tier` (`license.Tier`), a
`QuotaSnapshot`, and `FeatureFlags`, stored in the request context under a
private key type (`ctxKey`) via `WithTenantContext` and retrieved with
`GetTenant` (returning `ok=false` when absent).

3.2. `TenantMiddleware` MUST run after `auth.VerifierMiddleware` and org
context middleware; it MUST reject requests lacking user claims or a non-empty
`OrgID` with HTTP 403 and respond 500 if tenant resolution fails.

3.3. Tenant tier MUST be resolved through an injected `tierResolver
func(orgID string) license.Tier` closure, defaulting to `TierCommunity` when
the resolver is nil, keeping the package decoupled from license runtime
resolution. In production (W6) `cmd/server` wires `SetTierResolver` with
`newTierResolver` (`cmd/server/server_tier.go`), which validates the
Ed25519-signed license file (`OAP_LICENSE_FILE` + `OAP_LICENSE_PUBLIC_KEY`,
48-hex-char key, expiry-checked), maps the licensing vocabulary
(`"pro"`→`"professional"`) via `mapLicensingTier`, and fails closed to
Community on any error or missing key. `Server.resolveOrgTier`
(`internal/api/handler.go`) still defaults to Community when no resolver is
wired.

3.4. Feature flags MUST be tier-derived via `featureFlagsForTier`: all tiers
get `audit_log`, `agent_registration`, `basic_monitoring`; Professional adds
`policy_engine`, `patch_deployment`, `custom_scripts`; Enterprise additionally
adds `multi_region`, `sso_saml`, `priority_support`.

3.5. `TenantMiddlewareFromContext` MUST allow injecting a pre-resolved
`TenantContext` (test usage) and MUST return 500 for a nil tenant.

### 4. Per-Tier Quotas

4.1. Quota limits MUST be defined per tier in `TierQuotas`
(`QuotaDefinition`), and MUST agree with `internal/license` /
`internal/billing`:

| Tier | MaxAgents | MaxUsers | MaxSites | MaxAPIReq/Hour | MaxA2ACalls/Day | RetentionDays |
|------|-----------|----------|----------|----------------|-----------------|---------------|
| Community | 10 | 2 | 1 | 1,000 | 100 | 30 |
| Professional | 100 | 10 | 5 | 10,000 | 1,000 | 90 |
| Enterprise | 0 (unlimited) | 0 (unlimited) | 0 (unlimited) | 100,000 | -1 (unlimited) | 365 |

4.2. Unlimited semantics MUST use `0` for collection limits (agents, users,
sites) and `-1` for rate limits (API/hour, A2A/day), since 0 is a valid rate.
Unrecognized tiers MUST fall back to Community limits (`QuotasForTier`,
`EnforceQuota`).

4.3. `EnforceQuota(tc, resource, current, retryAfterSeconds)` MUST return a
`*QuotaError` when `current >= limit` and nil otherwise; it MUST return nil
for unlimited resources and for unknown `QuotaResource` values. Five resources
MUST be supported: `agents`, `users`, `sites`, `api_call`, `a2a_call`.

4.4. `QuotaError` MUST carry `Resource`, `Limit`, `Current`, `RetryAfter`, and
`Tier` so callers can build an actionable response; `Error()` MUST include the
resource, tier, and current/limit usage.

4.5. `WriteQuotaResponse` MUST emit HTTP 429 with a JSON body containing
`error`, `resource`, `limit`, `current`, `tier`, `retry_after`, and MUST set
the `Retry-After` header only when `RetryAfter > 0`.

4.6. `QuotaMiddleware` MUST read the `TenantContext`, resolve current usage via
an injected `currentUsage func(orgID string) int64`, enforce `QuotaAPICall`
with a 3600-second retry hint, and pass requests through when no tenant
context is present. In production (W6) it is mounted in
`internal/api/routes_routes.go` after `TenantMiddleware`, backed by
`Server.currentOrgUsage`, which returns the org's current-month API-call count
from the billing metering service (0 when no metering service is wired, so
Community deployments never trip a quota).

### 5. Tenant Persistence

5.1. `TenantStore` MUST persist tenants in the `tenants` table via
`database/sql`: `Create` MUST validate non-empty name and slug, reject
duplicate active slugs, and generate a UUID; `Get`/`GetBySlug`/`List` MUST
exclude soft-deleted rows (`deleted_at IS NULL`); `Update` and `Delete` MUST
affect only active rows and report not-found when zero rows match. `Delete`
MUST soft-delete by setting `deleted_at`.

5.2. `TenantConfigStore` MUST manage per-tenant key/value configuration in
`tenant_configs` with upsert semantics (`ON CONFLICT (tenant_id, key) DO
UPDATE`), JSON-encoded values, `GetAll` ordered by key, and key deletion.

### 6. Retention Purger

6.1. `RetentionPurger` MUST run as a background worker on a configurable
interval (default 24h) with an initial run after a 10-second warm-up delay;
`Stop` MUST signal cancellation and wait up to 30 seconds for the current
iteration. In the server it is constructed in `cmd/server/server_init.go` for
tables `audit_events` and `check_results`, started in `server_start.go`, and
stopped at shutdown.

6.2. Deletion MUST be two-phase: phase 1 soft-deletes by setting `deleted_at`
on rows whose `created_at` is older than the retention threshold; phase 2
hard-deletes rows soft-deleted longer than the grace period (default
`DefaultGracePeriod` = 7 days).

6.3. Retention duration MUST resolve per-tenant first (a positive
`retention_days` preference looked up by the row's `org_id`), falling back to
the configured `DefaultRetentionDays` via `COALESCE`. Tier defaults documented
in code are 30/90/365 days for Community/Professional/Enterprise.

6.4. The purger MUST emit Prometheus metrics, registered once via `sync.Once`:
`purged_records_total` (labels `table`, `action` soft|hard),
`purge_duration_seconds`, `purge_errors_total`, `purge_runs_total`, and
`purge_records_scanned_total` (label `table`).

6.5. A failed purge of one table MUST increment `purge_errors_total` and be
logged with the table name, without aborting purges of remaining tables or
crashing the worker. A nil pool MUST be a no-op.

---

## Known Limitations

- **RLS is opt-in and non-fatal.** `TenantMigrator` now runs at server boot
  (W6) but only when `OAP_ENABLE_TENANT_MIGRATIONS=1`, and its failure is
  logged-and-ignored — a deployment that omits the flag (or whose migration
  fails) silently ships without RLS isolation. R7's "integration test for
  every query path" is also still absent: `rls_migration_test.go` asserts
  migration SQL text and `SetTenantContext` rejection behavior, not actual
  isolation against a real PostgreSQL.
- **RLS coverage is incomplete by design.** Only nine tables get policies;
  `script_runs` and `report_templates` get indexes but no `ENABLE RLS` /
  policy. The migration's comment claims a `COALESCE`-style permissive
  fallback for rows whose `org_id` may be empty, but the policy expressions
  are plain `org_id = current_setting('app.tenant_id', true)` with no such
  fallback — so empty-`org_id` rows are hidden once RLS is enabled, and no
  policy admits platform operators outside a tenant context.
- **Quota enforcement is fail-open and single-metric.** `EnforceQuota`
  returns nil when no `TenantContext` is present, and `QuotaMiddleware`
  passes requests through without one; the only wired resource is
  `api_call` (from metering, 0 when unwired). Agent/user/site and A2A quotas
  are never enforced anywhere. (`internal/license` still has a separate
  parallel `EnforceQuota` implementation.)
- **Dead code in store.go.** `TenantStore`, `TenantConfigStore`, and the
  `tenants`/`tenant_configs` tables are used nowhere; `store_test.go` covers
  only struct fields and input validation, no database behavior. The W6 RLS
  migrations create `tenants` and `org_id` indexes but nothing populates them.
- **Per-tenant retention relies on a preferences row.** The purger now reads
  `alert_global_preferences` (W6 fixed the phantom `alert_preferences`
  reference), but per-org `retention_days` is only honoured when such a row
  exists; otherwise the configured `DefaultRetentionDays` applies.
- **Doc/code mismatch in cleanup.go (partially open).** The retention reads
  are now org-scoped via the per-row subquery, but the comment's claim that
  "all queries filter on org_id" is still false: the phase-2 hard-delete is
  unscoped (deletes any row whose `deleted_at` exceeds the grace period), and
  `RecordsScanned` is incremented from `RowsAffected()` (rows deleted), not
  rows scanned.
- **Identifier interpolation in migration helpers.** `SetTenantContext` is
  parameterised (`SELECT set_config('app.tenant_id', $1, false)`) and
  validated by `TestSetTenantContextRejectsUnsafeIDs` (W6), but
  `CreatePolicy`/`EnableRLS` still format identifiers with `fmt.Sprintf`.
- **Minor smells.** `isolation.go` imports `chi` solely for a
  `var _ = chi.NewRouter` suppression hack; purge metrics register on the
  global Prometheus registry, preventing a custom registry.
