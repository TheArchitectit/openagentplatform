# Data Model

> **Phase:** 0 (Foundation) — schema was Phase 0 Sprint 0.1 Story 0.1.3; extended through later phases
> **STATUS: PARTIAL** — Go struct layer complete and in active use; a checked-in Go-oriented
> platform DDL now exists (`deploy/migrations/001`+`002`) and the `TenantMigrator` is wired (opt-in).
> Residual drift is the schema-source divergence and the retention/RLS column assumptions on
> `check_results` (see Known Limitations #2, #6).
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `pkg/models/`, `deploy/postgres/init.sql`, `internal/tenancy/`, `internal/*/store*.go`

---

## Description

`pkg/models/` is the single shared data-model package for OpenAgentPlatform.
Twenty-five Go structs plus one enum type (`PatchSeverity`) define the entire
platform domain: fleet (sites, agents, heartbeats), monitoring checks, the
alert lifecycle, Rego compliance policies, patch approval workflows, script
execution, and audit events. Every server-side store (`internal/api`,
`internal/alerts`, `internal/patches`, `internal/policy`, `internal/audit`,
`internal/checklib`) and the NATS event handlers in `internal/events` consume
these structs directly — there is no per-service redefinition of core entities.

Two structs are also NATS wire formats: `Heartbeat` is the payload agents
publish on `oap.agents.<id>.heartbeat`, and `CheckResult` is the payload
published on `oap.agents.<id>.results`.

Persistence is PostgreSQL via `pgx`. Two checked-in schema sources exist and
diverge (see Known Limitations #2): the Python/Alembic set under
`py/alembic/versions/` (the `oap` database, TimescaleDB hypertables, UUID
`org_id`) and `deploy/migrations/001_platform_schema.sql` +
`002_platform_schema_addendum.sql` + `003_platform_schema_addendum2.sql`,
added in 9996a99/26827e0 (001/002) by reconstructing
the DDL from the SQL embedded in the Go stores (TEXT `org_id`); 003 (2026-08-30
ai04 deploy) adds the a2a tables and `policies.deleted` that 001/002 missed.
`deploy/postgres/init.sql` enables the three extensions (§9.1). The stores are
written to tolerate schema drift (descriptive errors or empty results when
tables are absent). Org tenancy is enforced at the **application layer** via
`org_id` columns and `WHERE org_id = $n` filters; the `internal/tenancy`
row-level-security migrations now target the live table set and are wired into
the server binary behind `OAP_ENABLE_TENANT_MIGRATIONS=1` (W6, 3ad3f4f).

## User Story

**As** a platform engineer building a feature on OpenAgentPlatform,
**I want** one authoritative package that defines every entity, its tenancy
column, its lifecycle-state field, and its JSON columns,
**so that** my service, API handler, and NATS consumer agree with every other
subsystem on shape without negotiating field-by-field, and multi-tenant
scoping is impossible to forget because the column is on every entity.

---

## Requirements

### 1. Entity Inventory

1.1. `pkg/models/` MUST be the sole definition of the following entities,
grouped by domain. Table names are those referenced by the live stores:

| Struct | Table | Domain | Notes |
|--------|-------|--------|-------|
| `Site` | `sites` | Fleet | Carries `registration_token`, `org_id` |
| `Agent` | `agents` | Fleet | Upserted on registration/check-in |
| `Heartbeat` | — | Fleet | NATS payload only; updates `agents` row |
| `User` | *(none in live SQL)* | Fleet | No store references (see Known Limitations) |
| `CheckDefinition` | `check_definitions` | Checks | Reusable named check; `config` JSONB |
| `CheckAssignment` | `check_assignments` | Checks | Check→agent/site link |
| `CheckAssignmentDetail` | — | Checks | Read-only join DTO for `GET /assignments` |
| `CheckResult` | `check_results` | Checks | High-write time series; retention-pruned |
| `Alert` | `alerts` | Alerts | Lifecycle state machine; `dedup_key` |
| `AlertRule` | `alert_rules` | Alerts | Scoping + routing rules |
| `AlertSuppressionWindow` | `alert_suppression_windows` | Alerts | Fleet maintenance windows (RMM-02) |
| `AlertStateMachine` | `alert_state_history` | Alerts | Append-only transition log |
| `NotificationRecord` | `alert_notifications` | Alerts | Per-channel delivery status |
| `Policy` | `policies` | Policies | Rego body; enforcement mode |
| `PolicyAssignment` | `policy_assignments` | Policies | Policy→agent/site many-to-many |
| `PolicyViolation` | `policy_violations` | Policies | Failed evaluation record |
| `PatchJob` | `patch_jobs` | Patches | 8-state approval workflow |
| `PatchJobTarget` | `patch_job_targets` | Patches | Per-endpoint target status |
| `ApprovalRecord` | `patch_approvals` | Patches | One row per approver decision |
| `WinUpdateKBState` | `winupdate_kb_state` | Patches | Per-KB Windows-Update state (RMM-03) |
| `PatchSeverity` | — | Patches | Enum: `critical`, `standard`, `major_os` |
| `PatchStats` | — | Patches | Aggregate DTO for the dashboard |
| `CVEEnrichment` | `cve_enrichment` | Patches | Cached CVE lookups per patch |
| `ScriptDefinition` | `script_definitions` | Scripts | Hard-deleted; no `deleted_at` column |
| `ScriptRun` | `script_runs` | Scripts | One execution per agent |
| `AuditEvent` | *(diverged — see §7.2)* | Audit | Superseded by `internal/audit.Event` |
| `MeshPeer` | `mesh_peers` | Mesh (RMM-09) | WireGuard pubkey + mesh IP; org-scoped |
| `MeshSession` | `mesh_sessions` | Mesh (RMM-09) | Operator→agent tunnel session; org-scoped |
| `AgentRelease` | `agent_releases` | Mesh (RMM-09) | Signed binary release; org-scoped |
| `AutomatedTask` | `automated_tasks` | Scheduled (RMM-06) | Cron-scheduled action; org-scoped |

1.2. Every persisted entity that belongs to a tenant MUST carry an `org_id`
string column. The live exceptions are join/child tables that inherit tenancy
through their parent key: `check_assignments`, `check_results`,
`alert_state_history`, `alert_notifications`, `policy_violations`,
`patch_job_targets`, `patch_approvals`, `script_runs`.

1.3. Entities with a create/update surface MUST carry `created_at` and
`updated_at` timestamps. Soft delete exists only in two places:
`tenants.deleted_at` (001, filtered by `internal/tenancy/store.go`) and
`agents.deleted_at` (Alembic 0002 only — the 001 `agents` table omits it). The
retention purger additionally requires `deleted_at` on its target tables (§9.3,
Known Limitations #6).

### 2. Fleet Domain

2.1. `Site` MUST have `id`, `org_id`, `name`, `region`, `created_at`. The
`sites` table additionally stores `registration_token`; the token SHOULD be a
bcrypt hash — the store logs a warning while plaintext rows exist and exposes
`HashRegistrationToken` for rotation.

2.2. `Agent` MUST model the endpoint inventory snapshot: identity (`id`,
`agent_id`, `site_id`, `client_id`, `org_id`), host facts (`hostname`,
`OperatingSystem` → column `os`, `Arch` → column `arch`, `platform`,
`cpu_count`, `total_memory_mb`, `total_disk_gb`), state (`status`,
`last_seen`, `needs_reboot`), versioning (`agent_version`, `version`), and
free-form blobs (`disks`, `services`, `wmi_detail`, `inventory`, `metadata`,
`tags`).

2.3. Agent persistence MUST be an upsert keyed on `id`
(`ON CONFLICT (id) DO UPDATE`) so re-registration is idempotent and flips
`status` to `online`. The upsert persists 16 of the struct's fields; the JSON
blobs (disks/services/WMI/inventory) are transported in the registration API
but are not persisted columns.

2.4. `Heartbeat` MUST carry `agent_id`, `timestamp`, `cpu_percent`,
`mem_percent`, `disk_percent`, `uptime_secs`, `version`. Processing a
heartbeat MUST update the parent `agents` row (status, `last_seen`, resource
percentages) rather than insert a history row, and a stale-sweep MUST mark
agents offline when no heartbeat arrives within the threshold.

2.5. `User` MUST carry `id`, `email`, `name`, `org_id`, `role`, `created_at`.
Authentication state lives elsewhere (`internal/auth` session claims); this
struct currently has no store.

### 3. Checks Domain

3.1. `CheckDefinition` MUST have `check_type` as a flat discriminator with
type-specific parameters in a single `config` JSONB column, validated against
the check type's schema at API time. It MUST carry scheduling
(`interval_seconds`, `timeout_seconds`), thresholds (`fail_threshold`,
`warn_threshold`, `error_threshold`), `alert_severity`, `enabled`, and an
`is_template` flag for org-template libraries.

3.2. `CheckAssignment` MUST link one `check_id` to either an `agent_id` or a
`site_id` (site fan-out), with `assigned_by` attribution.

3.3. `CheckResult` MUST be an append-only row with `agent_id`, `check_id`,
`timestamp`, `status`, `value` (float), `message`, and optional `metadata`
JSONB. It is written from NATS (`oap.agents.<id>.results`) and is subject to
retention pruning (§9).

3.4. Read joins MUST NOT require callers to assemble their own queries:
`CheckAssignmentDetail` pairs an assignment with the agent's latest result for
that check, and `CheckAssignment.LastResult` is populated by the list-assignments
store path.

### 4. Alerts Domain

4.1. `Alert` MUST carry `dedup_key`, and the store MUST look up by
`dedup_key` (`GetAlertByDedupKey`) before insert so a recurring failure maps
to one alert instance; inserting a duplicate `dedup_key` MUST error.

4.2. `Alert.State` MUST take exactly the six lifecycle states `pending`,
`open`, `acknowledged`, `snoozed`, `resolved`, `closed`; legal transitions are
the `ValidTransitions` map in `internal/alerts/statemachine.go`, and any
(state, event) pair absent from the map MUST be rejected with
`ErrInvalidTransition`. `closed` is terminal.

4.3. `Alert.Severity` MUST be one of `info`, `warning`, `critical`,
`emergency` (the engine's constant set).

4.4. Lifecycle timestamps MUST be tracked as nullable columns:
`snoozed_until`, `resolved_at`, `closed_at`, plus `acknowledged_by`
attribution.

4.5. Every transition MUST be appended to `alert_state_history`
(`AlertStateMachine`: `alert_id`, `from_state`, `to_state`, `event`, `actor`,
`reason`, `created_at`) for audit.

4.6. `AlertRule` MUST scope alert generation by optional `check_id`,
`agent_id`, `site_id`, a `min_severity` floor, `notify_channels`, and
`enabled`.

4.7. `NotificationRecord` MUST record one delivery attempt per channel:
`alert_id`, `channel`, `recipient`, `status` (`pending`, `sent`, `failed`),
`error_msg`, `sent_at`.

### 5. Policies Domain

5.1. `Policy` MUST store the `rego_body` source plus `enforcement_mode`
(`enforce`, `monitor`, `disabled`), `severity` (`info`, `warning`,
`critical`), `category` (`security`, `compliance`, `operational`), and
`enabled`.

5.2. `PolicyAssignment` MUST link one `policy_id` to exactly one of
`agent_id` or `site_id`; a site-scoped assignment evaluates against all agents
in the site.

5.3. `PolicyViolation` MUST record one failed evaluation: `policy_id`,
`agent_id`, `severity`, `message`, `details` JSONB, and resolution state
(`resolved`, `resolved_at`).

### 6. Patches Domain

6.1. `PatchSeverity` MUST be a typed three-value enum: `critical`
(auto-approved on creation with notification), `standard` (one approver),
`major_os` (two distinct approvers, four-eyes). The approval-count map MUST
encode `standard→1`, `major_os→2`.

6.2. `PatchJob.State` MUST take exactly the eight states
`pending_approval`, `approved`, `rejected`, `scheduled`, `in_progress`,
`completed`, `failed`, `rolled_back`, driven by the `ApprovalWorkflow`
transition table in `internal/patches/approval.go`; `rejected` and
`rolled_back` are terminal. An approval timeout with
`auto_approve_on_timeout` MUST transition via the `timeout` event to
`approved` (default deadline 72h).

6.3. `PatchJob` MUST carry the deployment payload (`package_name`,
`package_version`, `rollback_version`), scheduling (`scheduled_at`,
`maintenance_window_start/end`, `approval_timeout`), approval bookkeeping
(`required_approvals`, `auto_approve_on_timeout`, child `approvals`), and
`failure_reason`.

6.4. `PatchJobTarget` MUST track per-endpoint outcome with `status` in
`pending`, `running`, `success`, `failed`, plus `error_msg` and `applied_at`.

6.5. `ApprovalRecord` MUST record one approver's decision (`approved` or
`rejected`) with identity (`approver_id`, `approver_name`) and optional
`comment`; multiple rows per job MUST be supported.

6.6. `PatchStats` MUST be computed aggregates only (`by_state`, `by_severity`,
`pending_approval`, `recent_failures_24h`, `avg_approval_time_hours`) and MUST
NOT be persisted as a table.

### 7. Audit Domain

7.1. `AuditEvent` defines the minimal model: `actor_id`, `action`,
`resource`, `metadata` JSONB.

7.2. The persisted `audit_events` table uses the richer hash-chained schema of
`internal/audit.Event` (15 columns: `event_id`, `prev_hash`, `hash`,
`timestamp`, `actor_type`, `action`, `resource_type`, `resource_id`,
`details`, `outcome`, `ip`, `user_agent`, `org_id`, `site_id`), where each
row's SHA-256 hash incorporates the previous row's hash. Implementations MUST
write through `internal/audit`, not the bare struct.

### 8. Tenancy and Isolation

8.1. Every tenant-owned query path MUST filter on `org_id` (or a parent key
that carries it). Stores MUST accept an `orgID` scope argument on get/list
operations; when empty, the caller assumes responsibility for verification.

8.2. Org/site scope columns MUST be usable as leading index columns so
multi-tenant queries stay selective (per rmm-core §2.4).

8.3. `internal/tenancy` MUST provide row-level-security primitives: a
versioned `TenantMigrator` (opt-in at server start via
`OAP_ENABLE_TENANT_MIGRATIONS=1`, wired in `cmd/server/server_init.go`), and
`tenant_isolation_*` policies keyed on the TEXT
`org_id = current_setting('app.tenant_id', true)` set via `SetTenantContext`
(parameterised `set_config`, tenant-ID validated).

8.4. RLS as authored (migration v3) applies to nine live tables — `agents`,
`check_definitions`, `check_results`, `alerts`, `alert_rules`, `policies`,
`script_definitions`, `patch_jobs`, `audit_events` — each scoped by its
existing TEXT `org_id` column. Migration v2 adds `org_id` indexes to ten
tables (adding `script_runs` and `report_templates`, which gain no policy).
`check_results` and `audit_events` carry the column only in the Alembic
schema; on the `deploy/migrations` schema the v2/v3 statements fail
(Known Limitations #2, #6).

### 9. Schema Management and Retention

9.0. **Migration inventory (relevant to this spec):** the platform uses a
decoupled Python/Alembic migration set under `py/alembic/versions/` for the
`oap` Postgres database. Recent RMM migrations: `0015_rmm09_mesh` (creates
`mesh_peers`, `mesh_sessions`, `agent_releases` — all org-scoped) and
`0016_rmm06_scheduled_automation` (creates `automated_tasks`, org-scoped,
replacing the earlier JSONB-on-`Policy` sketch in `0005_policies`). The Go
stores target these table names directly (e.g. `internal/mesh/store.go`,
`internal/scheduled/store_tasks.go`).

9.1. Database bootstrap MUST enable the `uuid-ossp`, `pgcrypto`, and
`timescaledb` extensions (`deploy/postgres/init.sql`).

9.2. Stores MUST tolerate schema drift: a missing table MUST surface as a
descriptive error or empty result, never a panic, so the API can start before
migrations are applied.

9.3. A `RetentionPurger` MUST run on a 24h interval (wired in `cmd/server`)
over `audit_events` and `check_results`, two-phase: soft-delete by setting
`deleted_at` for rows older than the tenant's retention, then hard-delete rows
soft-deleted longer than the 7-day grace period.

9.4. Per-tenant retention MUST resolve from
`alert_global_preferences.retention_days` when set (>0), falling back to the
tier default (30d community, 90d pro, 365d enterprise). Purge queries MUST be
scoped so one tenant's purge cannot touch another tenant's rows.

9.5. Purge activity MUST be observable via Prometheus counters/histograms
(`purged_records_total{table,action}`, `purge_duration_seconds`,
`purge_errors_total`, `purge_runs_total`, `purge_records_scanned_total`).

---

## Known Limitations

1. **Resolved (2026-08-24).** A checked-in platform DDL now exists:
   `deploy/migrations/001_platform_schema.sql` (9996a99) plus
   `002_platform_schema_addendum.sql` (26827e0), reconstructed from the SQL
   embedded in the Go stores. `003_platform_schema_addendum2.sql` (2026-08-30,
   ai04 test deploy) completes the set for the server's boot path: the a2a
   registry/task tables (stores never run EnsureSchema) and `policies.deleted`
   (queried by `internal/policy/store_crud.go` but missing from 001).
2. **Two divergent schema sources.** The `oap` production database is managed
   by the Python/Alembic set under `py/alembic/versions/` (UUID `org_id`,
   TimescaleDB hypertables, `agents.deleted_at`), while `deploy/migrations/`
   was reconstructed for the Go stores (TEXT `org_id`, `"timestamp"` columns,
   no hypertables). The two disagree on column sets for `check_results`,
   `script_runs`, and `agents` — neither is applied automatically by the Go
   server, and which one a deployment runs depends on how the database was
   bootstrapped. `TenantMigrator` (v2 indexes, v3 policies) is wired into
   `cmd/server` behind `OAP_ENABLE_TENANT_MIGRATIONS=1` (W6, 3ad3f4f) and
   targets the live table names on TEXT `org_id`; on a database built from
   `deploy/migrations` it fails on `check_results`/`audit_events`, whose 001
   DDL omits `org_id` (#6). `SetTenantContext` is parameterised with
   tenant-ID validation (`internal/tenancy/rls.go`).
3. **Unused and diverged structs.** `User` has zero consumers outside
   `pkg/models`. `AuditEvent` diverged from the hash-chained
   `internal/audit.Event` actually written to `audit_events`.
   `CheckAssignmentDetail` and `PatchStats` are read-only DTOs, not rows.
4. **Struct/column drift on `Agent`.** The `TotalMemoryMB` field is tagged
   `db:"total_ram"` (matching the Alembic column) while the Go store SQL uses
   `total_memory_mb` (matching the `deploy/migrations` column) — a concrete
   instance of the #2 divergence. The upsert persists 16 of ~30 struct fields,
   so `disks`, `services`, `wmi_detail`, `inventory`, and `metadata` survive
   only as API transport.
5. **Heartbeat has no history.** It mutates the `agents` row; there is no
   time-series of heartbeats despite timescaledb being enabled.
6. **Retention purger is non-functional as checked in.** `purgeTable`
   (`internal/tenancy/cleanup.go`) requires `deleted_at`, `created_at`, and
   `org_id` on each target table, but no checked-in schema provides
   `deleted_at` on either target: on the `deploy/migrations` schema
   `check_results` has none of the three and `audit_events` has only `org_id`
   (its time column is `"timestamp"`); on the Alembic schema `check_results`
   and `audit_events` carry `org_id`/`created_at` but no `deleted_at`. Every
   tick therefore logs "retention purge failed" rather than purging; the
   Prometheus counters stay at zero. Fixing this needs a deliberate migration
   adding `deleted_at` (plus `org_id`/`created_at` to 001's `check_results`)
   or a purger rewrite against the `"timestamp"` columns — both are
   schema-design decisions, so the gap is documented here rather than patched
   ad hoc. `ScriptDefinition` is also hard-deleted despite §9.3's soft-delete
   design.
7. **Severity vocabulary drift.** Alert severity is
   `info|warning|critical|emergency` in code, while rmm-core spec §3.3 lists
   `critical|high|medium|low|info`.
8. **Registration tokens default to plaintext.** `sites.registration_token`
   is stored plaintext until manually hashed; the store logs a warning and
   offers `HashRegistrationToken`.
9. **Enums are per-package string constants.** Only `PatchSeverity` is a typed
   Go enum; alert/patch/script states are untyped strings with constants
   scattered across `internal/alerts` and `internal/patches`.
