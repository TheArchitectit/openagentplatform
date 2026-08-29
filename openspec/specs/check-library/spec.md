# Check Library

> **Phase:** 1 (Core RMM)
> **STATUS: COMPLETE**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/checklib/`

---

## Description

The Check Library is the **server-side catalog of built-in check
templates**. It complements the agent-side checker implementations in
`pkg/agent/checkers/` (see spec `rmm-core` §3.2/§6.2): the agents know
*how* to execute check types; the library defines *which* checks exist as
first-class offerings, with default configuration, sensible scheduling,
and per-parameter schema hints — and provides the HTTP endpoints to list
them and instantiate them into real `check_definitions` rows.

The package has two parts:

1. **Catalog + HTTP API** (`library.go`): a stateless `Library` serving
   the templates returned by `BuiltInChecks()`, with a read endpoint
   (`GET /library`) and a mutating instantiation endpoint
   (`POST /library/{template_id}/create`).
2. **Bootstrap seeder** (`seeder.go`): `Seed()` inserts one disabled
   `check_definitions` row per template at server startup, idempotently.

How definitions reach agents: instantiation (or seeding) creates a
`check_definitions` row; the check pipeline then schedules due checks
and dispatches them to agents over NATS (`rmm-core` §6.4). The library
itself never talks to agents — it only produces definitions.

Delivered in Sprint 1.1 (Story 1.1.3, commit `3b89f01`); mutating-route
RBAC hardened in commit `432e966`. The catalog was extended from 5 to the
full 9-template set (§2.1, spec reconciliation approved by user
2026-08-27) with a test suite covering the catalog, HTTP API, and seeder.

## User Story

**As** an operator setting up monitoring for my fleet,
**I want** a curated list of built-in checks with sane defaults that I
can instantiate with one request, overriding only what I need,
**so that** I get working monitoring immediately without hand-writing
check configuration for common cases like ping, HTTP, TCP, DNS, CPU,
memory, disk, service status, and script execution.

---

## Requirements

### 1. Template Model

1.1. Each built-in check MUST be described by a `CheckTemplate`
(`internal/checklib/library.go`) carrying: `ID`, `Name`, `CheckType`,
`Description`, `Category`, `DefaultConfig` (map),
`DefaultIntervalSecs`, `DefaultTimeoutSecs`, and an optional
`ConfigSchema`.

1.2. The canonical catalog MUST be returned by `BuiltInChecks()`; its
slice order MUST be the order presented in the UI. The catalog is
in-memory and rebuilt on every call — `Library` is a stateless service
holding only an optional `*pgxpool.Pool`.

1.3. `NewLibrary(db)` MUST accept a nil pool; the catalog endpoints MUST
keep working without a database, and only the instantiate endpoint MUST
degrade (see 4.5).

1.4. Every template's `CheckType` MUST correspond to a checker name
registered in the agent-side default registry
(`pkg/agent/checkers/registry.go` `defaultReg`) so that any check
instantiated from the catalog is executable by an agent.

### 2. Catalog Contents

2.1. The catalog MUST define exactly 9 templates — one per rmm-core §3.2
checker type (reconciled 2026-08-27, approved by user):

| Template ID | Name | CheckType | Category | Default interval / timeout |
|-------------|------|-----------|----------|---------------------------|
| `builtin-ping` | Ping | `ping` | `network` | 60 s / 10 s |
| `builtin-cpu` | CPU Usage | `cpu` | `system` | 60 s / 15 s |
| `builtin-memory` | Memory Usage | `memory` | `system` | 60 s / 10 s |
| `builtin-disk` | Disk Usage | `disk` | `system` | 300 s / 10 s |
| `builtin-service` | Service Status | `service` | `system` | 60 s / 10 s |
| `builtin-http` | HTTP Endpoint | `http` | `network` | 60 s / 15 s |
| `builtin-tcp` | TCP Port | `tcp` | `network` | 60 s / 10 s |
| `builtin-dns` | DNS Resolution | `dns` | `network` | 60 s / 10 s |
| `builtin-script` | Script | `script` | `system` | 300 s / 60 s |

2.2. Each template MUST ship a `DefaultConfig` usable without
modification (e.g. ping: `host 8.8.8.8, count 3, timeout_ms 3000`;
cpu: `threshold_percent 90, duration_seconds 60`).

2.3. Each template SHOULD carry a `ConfigSchema` describing its
parameters as a per-key map (`type`, `required`, `default`, `min`,
`max`, `enum`, `description`). The schema is informational metadata for
clients; the server MUST NOT treat it as a validation gate.

### 3. Template Lookup

3.1. `FindTemplate(id)` MUST return the template with the given ID or an
error (`template not found: <id>`); it backs the instantiate endpoint's
404 behavior.

3.2. `GetTemplateByName(name)` MUST match `Name` case-insensitively
(`strings.EqualFold`). (Note: exported for seeder use but currently
uncalled — see Known Limitations.)

### 4. HTTP API

4.1. `GET /library` (registered by `RegisterReadRoutes`) MUST return
JSON `{"templates": [...], "total": N}` with the full catalog.

4.2. `POST /library/{template_id}/create` (registered by
`RegisterMutatingRoutes`) MUST create one `check_definitions` row from
the named template and return `201` with the created row's fields
including server-generated `id` (UUIDv4 via `uuid.NewString()`) and
`RETURNING created_at, updated_at`.

4.3. The request body MUST be optional. Provided fields MUST override
template defaults with these merge rules: `config` merges key-by-key on
top of `DefaultConfig` (request keys win, unspecified keys keep
defaults); empty `name`/`description` fall back to the template's;
non-positive `interval_seconds`/`timeout_seconds` fall back to the
template defaults; `enabled` defaults to `true` and is only overridden
when explicitly present (`*bool`).

4.4. The new check MUST be attributed to the authenticated user's org
(`claims.OrgID` from `auth.UserFromContext`); when no identity is
present the row MUST be created with an empty `org_id`.

4.5. Error responses MUST be JSON bodies: `503 {"error":"db_unavailable"}`
when the pool is nil; `400 {"error":"missing_template_id"}` when the
path param is empty; `404 {"error":"template_not_found",...}` for an
unknown template; `500 {"error":"insert_failed",...}` (with detail) on
database failure.

4.6. Read and mutating routes MUST be registerable separately
(`RegisterReadRoutes` / `RegisterMutatingRoutes`; `RegisterRoutes`
registers both). The mutating route MUST be registered as a flat path
`POST /library/{template_id}/create` rather than a nested `/library`
route so it cannot collide with the read route in chi's router.

4.7. Wiring (`internal/api/routes_sub.go`, `mountAPISubRoutes`): both
endpoints MUST be mounted under the authenticated `/api/v1/checks`
group; the read catalog MUST be available to any org member, and the
instantiate endpoint MUST be gated behind
`auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)` on a dedicated
sub-router.

### 5. Bootstrap Seeding

5.1. `Seed(ctx, pool, log)` (`internal/checklib/seeder.go`) MUST insert
one `check_definitions` row per built-in template with `enabled=false`,
empty `org_id`, `gen_random_uuid()` ID, and the template's default
config/interval/timeout. Seeded checks MUST be visible in the library
but MUST NOT run until an operator explicitly enables and assigns them.

5.2. Seeding MUST be idempotent: a template whose `name + check_type`
already exists in `check_definitions` MUST be skipped, so rerunning
against a populated database is a no-op.

5.3. Per-template failures (exists-check query, config marshal, insert —
including a missing table) MUST be recorded in `SeedResult.Errors` and
logged, but MUST NOT abort seeding of remaining templates or the server
boot.

5.4. `SeedResult` MUST report `Seeded`, `Skipped`, `TotalChecks`, and
`Errors`. A nil pool MUST return `ErrNoDB`.

5.5. Server wiring (`cmd/server/main.go`): seeding MUST run at startup
against the Postgres pool under a 15-second timeout; a seeder failure
MUST log a warning and MUST NOT prevent the server from starting.

### 6. Agreement with the Agent-Side Checker Set

6.1. Per `rmm-core` §3.2, the full built-in checker set is 9 types:
`ping`, `http`, `tcp`, `dns`, `cpu`, `memory`, `disk`, `service`,
`script` (all registered in `pkg/agent/checkers` default registry,
including `script`).

6.2. This spec covers the SERVER-side catalog only, which MUST contain
the 9 templates of §2.1 — the full rmm-core §3.2 set, matching §6.1
exactly. Every catalog type MUST remain executable agent-side.

---

## Known Limitations

- **`ConfigSchema` is informational by design.** Per §2.3 the server does
  not treat the schema as a validation gate: instantiation accepts
  arbitrary `config` keys/values and does not check types, `required`,
  `min`/`max`, or `enum`. Overrides are only sanity-checked indirectly
  (the checks CRUD's `validateCheckConfig` runs on later updates via
  `PUT /checks/{id}`). This is intentional design, not a missing
  validation gate. Template `DefaultConfig`s follow the same per-type key
  set as `validateCheckConfig` (e.g. dns `hostname`, script
  `runtime`+`script_body`) so instantiated rows survive that later
  validation.
- **Not end-to-end runnable without an assignment translation layer.** The
  dispatcher publishes `CheckAssignment{config: {...}}` while the agent's
  `CheckCommand` consumes `target`/`options`; no server-side translator
  converts a definition's `config` map into those fields, and the agent
  checkers read `target` + `options` keys (`threshold`, `path`, …) rather
  than the catalog's `*_percent`/`host`-style config keys. §1.4's
  executability guarantee is at the type level (registered checker), not a
  verified config-key handoff.
- **Scheduling bounds not enforced here.** `rmm-core` §6.3 requires
  `interval_seconds ≥ 30` and `timeout_seconds ≤ 3600` enforced by
  validation; the instantiate handler passes values through unbounded.
- **`GetTemplateByName` is exported but uncalled** (kept for API
  compatibility; covered by a unit test). `Seed()` matches on raw
  `name + check_type` SQL instead.
- **Seeded rows are org-less** (`org_id ''`) and instantiation permits
  empty org when unauthenticated; both rely on the authenticated mount
  in `routes_sub.go` for any real access control.
- **Template IDs are string constants** (`builtin-ping`, …), not UUIDs,
  and there is no versioning of template definitions.
