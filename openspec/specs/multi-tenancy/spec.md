# Multi-Tenancy

> **Phase:** 6 (Commercial: gating, multi-tenancy, relay, reporting, billing)
> **STATUS: PARTIAL**
> **Source:** authored 2026-08-23 from code (audit `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §4)
> **App Path:** `internal/tenancy/`

---

## Description

The multi-tenancy package provides per-tenant data isolation, per-tier quota
limits, and retention-based cleanup for OpenAgentPlatform's commercial tiers
(Community / Professional / Enterprise, as defined in `internal/license`). It
implements the PostgreSQL Row-Level Security (RLS) isolation option named in
the master plan's Phase 6 risk register (R7) and open question O2, along with
a quota library keyed on `license.Tier` and a two-phase retention purger.

Isolation is modeled as three SQL migrations (`TenantMigrations` in
`rls_migration.go`): create a `tenants` table, add a `tenant_id` foreign key
to ten resource tables, then ENABLE + FORCE RLS on each with a
`tenant_isolation_<table>` policy comparing `tenant_id` against the session
variable `app.tenant_id`. Helper functions (`WithTenant`, `SetTenantContext`)
set that session variable around query execution.

Request-scoped tenancy is carried by `TenantContext` (org ID, tier, quota
snapshot, feature flags), attached by `TenantMiddleware`, which is mounted in
`internal/api/routes_routes.go` after auth verification. Quota enforcement is
a library (`EnforceQuota`, `QuotaMiddleware`) producing structured 429
responses; the retention purger (`RetentionPurger`) is a live background
worker that soft- then hard-deletes expired `audit_events` and
`check_results` rows.

The package is **partially wired**: the tenant-context middleware and the
retention purger run in the server, but the RLS migrator, tenant/config
stores, and quota middleware have no callers yet, and tier resolution is
stubbed to Community. See Known Limitations.

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

1.3. Migration 2 (`add_tenant_id_to_tables`) MUST add a nullable `tenant_id
UUID REFERENCES tenants(id)` column and an index to exactly these ten tables:
`endpoints`, `checks`, `check_results`, `alerts`, `alert_rules`, `policies`,
`scripts`, `secrets`, `secret_backends`, `audit_log`.

1.4. Every migration MUST be reversible; `Down` SQL MUST undo the `Up`
(`TestTenantMigrationRollback`).

### 2. RLS Policies and Tenant Session Context

2.1. Migration 3 (`enable_rls`) MUST issue `ENABLE ROW LEVEL SECURITY` and
`FORCE ROW LEVEL SECURITY` on all ten tenant-scoped tables so policies apply
even to the table owner.

2.2. Migration 3 MUST create one policy per table, named
`tenant_isolation_<table>`, `FOR ALL`, with both `USING` and `WITH CHECK`
expressions equal to `tenant_id = current_setting('app.tenant_id')::uuid`, so
reads and writes are confined to the current tenant (verified by
`TestTenantMigrationEnableRLS`, `TestTenantMigrationPolicies`).

2.3. The migrator (`TenantMigrator.Migrate`) MUST track applied versions in a
`tenant_migrations` table, run only migrations newer than the current version,
and execute each migration and its version record in a single transaction.
`Rollback` MUST undo migrations in reverse order down to a target version.

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
resolution. (The wired resolver `Server.resolveOrgTier` in
`internal/api/handler.go` currently returns Community for all orgs.)

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
context is present.

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

- **RLS is authored but not applied.** `TenantMigrator` has no caller in
  `cmd/` or elsewhere: migrations 1-3 never execute in server boot, so the
  `tenants` table, `tenant_id` columns, and RLS policies do not exist in a
  live database. Master-plan risk R7's "integration test for every query path"
  is also absent — tests assert migration SQL text only, not isolation
  behavior against a real PostgreSQL.
- **Quota enforcement is not wired.** `QuotaMiddleware`, `EnforceQuota`, and
  `WriteQuotaResponse` have no callers outside the package; only
  `TenantMiddleware` is mounted in `internal/api`. (`internal/license` has a
  separate parallel `EnforceQuota` implementation.)
- **Tier resolution is a stub.** `Server.resolveOrgTier` returns
  `TierCommunity` for every org ("Future: query the license service or
  org_tiers table"), so Professional/Enterprise quotas and feature flags are
  unreachable in the running server.
- **Dead code in store.go.** `TenantStore`, `TenantConfigStore`, and the
  `tenants`/`tenant_configs` tables are used nowhere; `store_test.go` covers
  only struct fields and input validation, no database behavior.
- **Two coexisting isolation models.** RLS migrations key on `tenant_id`
  (UUID FK) for `check_results`/`audit_log`, while the live purger, audit
  events, and middleware key on `org_id` (string). The purger's per-tenant
  lookup also queries `alert_preferences`, but the alerts store creates
  `alert_global_preferences` — the subquery references a table that exists
  nowhere, so phase-1 soft-delete SQL likely errors on every run (surfaced as
  `purge_errors_total`).
- **Fail-open behaviors.** `EnforceQuota` returns nil when `TenantContext` is
  nil (no enforcement), and `QuotaMiddleware` passes requests through without
  a tenant context.
- **Doc/code mismatch in cleanup.go.** The comment claims "all queries filter
  on org_id", but the hard-delete statement is unscoped (deletes any row whose
  `deleted_at` exceeds the grace period), and the soft-delete relies on the
  per-row subquery rather than an org filter. `RecordsScanned` is incremented
  from `RowsAffected()` (rows deleted), not rows scanned.
- **SQL injection surface.** `SetTenantContext` interpolates the tenant ID
  into `SET app.tenant_id = '%s'` without quoting/parameterization; safe only
  if callers pass trusted UUIDs. The `CreatePolicy`/`EnableRLS` helpers also
  format identifiers with `fmt.Sprintf`.
- **Minor smells.** `isolation.go` imports `chi` solely for a
  `var _ = chi.NewRouter` suppression hack; purge metrics register on the
  global Prometheus registry, preventing a custom registry.
