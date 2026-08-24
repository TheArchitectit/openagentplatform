# Audit Log

> **Phase:** 0 (Foundation) — MASTER_IMPLEMENTATION_PLAN Sprint 0.2, Story 0.2.4
> "Audit log infrastructure"
> **STATUS: COMPLETE**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** `internal/audit/` (audit.go, audit_helpers.go, audit_query.go,
> middleware.go), `internal/api/audit.go` (handlers), `internal/api/routes_sub.go`
> (routes), `internal/api/handler.go` (middleware mounting)

---

## Description

The Audit Log is a tamper-evident, hash-chained record of platform actions.
Every audited action is persisted as an immutable `Event` row in the
`audit_events` Postgres table together with a SHA-256 hash that incorporates
the hash of the previously recorded event, forming a Merkle-like chain that
can be re-verified after the fact to detect tampering.

The capability has three parts:

1. **`AuditService`** (`internal/audit/audit.go`) — records events and answers
   point lookups, filtered listings, and per-resource chain verification.
2. **HTTP middleware** (`internal/audit/middleware.go`) — a chi middleware
   mounted on the whole API router that records one `api_call` event per
   request, asynchronously and outside the request path.
3. **Read API** (`internal/api/audit.go`, routes in `internal/api/routes_sub.go`)
   — three authenticated GET endpoints for listing events, fetching one event
   with hash re-verification, and walking a resource's chain.

Recording is also invoked directly (not just via middleware) for
security-relevant actions such as login/logout, policy violation lifecycle,
patch deploys, check run-now, and remote sessions, using the typed
`EventType` constants.

## User Story

**As** a platform administrator or compliance reviewer,
**I want** every login, API call, and privileged action recorded in a
hash-chained log that I can query and re-verify at any time,
**so that** I can reconstruct who did what and detect whether any stored
record has been altered after the fact.

---

## Requirements

### 1. Event Model

1.1. Every audit record MUST be an immutable `audit.Event`
(`internal/audit/audit.go`) carrying: `EventID`, `PrevHash`, `Hash`,
`Timestamp`, `ActorType`, `ActorID`, `Action`, `ResourceType`, `ResourceID`,
`Details` (`json.RawMessage`), `Outcome`, and optional `IP`, `UserAgent`,
`OrgID`, `SiteID`.

1.2. The service MUST recognise 11 event types (`EventType`): `login`,
`logout`, `api_call`, `agent_action`, `check_run`, `alert_change`,
`policy_change`, `patch_deploy`, `script_run`, `user_manage`,
`config_change`.

1.3. Actor identity MUST be typed by `ActorType` (`user`, `agent`, `system`,
`api_key`, `unknown`), and action result by `Outcome` (`success`, `failure`,
`denied`, `error`).

1.4. Callers MUST supply only the user-controlled subset via
`audit.EventInput`; `EventID`, `PrevHash`, `Hash` MUST be computed by
`Record`, and defaults MUST be applied: empty `ActorType` → `unknown`, empty
`Outcome` → `success`, zero `Timestamp` → `time.Now().UTC()`.

1.5. `EventID` MUST be a freshly generated UUIDv4 (`uuid.NewString()`).

### 2. Hash-Chained Recording

2.1. `AuditService.Record` (`internal/audit/audit.go`) MUST serialize chain
extension with `writeMu` (W8): it locks before fetching the hash of the most
recently recorded event (`latestHash`: `SELECT hash FROM audit_events ORDER BY
timestamp DESC, event_id DESC LIMIT 1`), stores it as the new event's
`PrevHash`, and inserts — making the `latestHash`→`INSERT` pair atomic per
process and preventing sibling-orphan forks under concurrency. The first
event in an empty log MUST have an empty `PrevHash` (the genesis link).

2.2. `computeHash` (`internal/audit/audit_helpers.go`) MUST compute the event
hash as hex-encoded SHA-256 over the fields `EventID`, `PrevHash`,
`Timestamp` (UTC, RFC3339Nano), `ActorType`, `ActorID`, `Action`,
`ResourceType`, `ResourceID`, `Details`, `Outcome` — concatenated in that
order, each separated by a single NUL byte (`0x00`).

2.3. An empty `Details` MUST be hashed (and stored) as the literal JSON
`null` (`marshalDetails`) so equivalent inputs yield identical hashes.

2.4. `IP`, `UserAgent`, `OrgID`, and `SiteID` are stored but MUST NOT be part
of the hash input.

2.5. `Record` MUST persist the full 15-column row in a single `INSERT INTO
audit_events` statement and return the fully populated `*Event`. Blank
optional strings MUST be stored as SQL NULL (`nullString`).

2.6. All service methods MUST fail with `audit: service not initialised` when
the service or its `pgxpool.Pool` is nil.

### 3. Verification

3.1. `audit.VerifyHash(ev)` MUST recompute the hash from an event's fields
and compare it to the stored `Hash`, returning false on mismatch or nil
input. The single-event read endpoint MUST expose this result.

3.2. `GetEventChain(ctx, resourceID)` (`internal/audit/audit_query.go`) MUST
return a `ChainVerification` (`ResourceID`, `Links`, `Intact`, `BrokenAt`,
`TotalChecked`, `GapCount`) for all events of one resource ordered oldest →
newest (`ORDER BY timestamp ASC, event_id ASC`). Since W8 each link is
verified **in isolation**: its stored `Hash` MUST recompute from its own
contents; a failed recomputation sets `Valid=false`, `Intact=false`, and
`BrokenAt` to the first failing link's event ID.

3.3. Because the write-side chain is global (every event links to the latest
event of any resource), a per-resource view necessarily skips foreign links,
so a `PrevHash` discontinuity within the subset is recorded as `GapCount`
(informational metadata), not as a break — only a failed hash recomputation
marks `Intact=false` (W8 fix for false positives).

### 4. Query API

4.1. `GetEvents(ctx, EventFilter)` MUST support equality filters on
`ActorID`, `Action`, `ResourceType`, `ResourceID` and range filters `Since`
(`timestamp >=`) / `Until` (`timestamp <=`), combined with AND and passed as
parameterised bind arguments.

4.2. `GetEvents` MUST return both the page of events (ordered
`timestamp DESC`) and the total matching row count (a separate `COUNT(*)`
query with the same filter).

4.3. Pagination MUST clamp invalid input: `Limit <= 0` or `Limit > 500`
defaults to 100; negative `Offset` defaults to 0.

4.4. `GetEvent(ctx, eventID)` MUST return `audit.ErrNotFound` when the ID is
absent (`pgx.ErrNoRows` mapping).

### 5. HTTP Read Endpoints

5.1. Three routes MUST be mounted under the authenticated `/api/v1` group
(`internal/api/routes_sub.go` `/audit` block, handlers in
`internal/api/audit.go`). The whole block MUST apply
`auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)` (W8; the audit log
carries actor identities, IPs, and cross-org resource references), so reads
are privileged rather than open to any org member:

| Method | Path | Purpose | Roles |
|--------|------|---------|-------|
| GET | `/api/v1/audit/events` | Filtered, paginated listing | admin, technician |
| GET | `/api/v1/audit/events/{id}` | Single event + hash re-verification | admin, technician |
| GET | `/api/v1/audit/chain/{resource_id}` | Per-resource chain verification | admin, technician |

5.2. `GET /audit/events` MUST accept query parameters `actor_id`, `action`,
`resource_type`, `resource_id`, `since`, `until` (RFC3339), `limit`,
`offset`; malformed `since`/`until`/numeric values MUST be ignored (filter
field left unset) rather than rejected. Response shape:
`{"events": [...], "total": N, "limit": N, "offset": N}`.

5.3. `GET /audit/events/{id}` MUST respond with
`{"event": {...}, "hash_valid": bool, "verification": "sha256"}` and 404
`{"error":"not_found"}` for unknown IDs.

5.4. `GET /audit/chain/{resource_id}` MUST respond with the JSON-encoded
`ChainVerification`.

5.5. All three handlers MUST return 503 `{"error":"audit_unavailable"}` when
no `AuditService` is wired into the server (`s.audit == nil`).

### 6. HTTP Audit Middleware

6.1. `audit.Middleware(svc, log)` MUST be installed on the root router ahead
of authentication (`internal/api/handler.go`) so requests are audited
regardless of auth outcome.

6.2. Requests whose path starts with `/health`, `/docs`, or `/ws` MUST be
skipped (`skippedPathPrefixes`).

6.3. The middleware MUST run the downstream handler first, then record an
`api_call` event **asynchronously** in a goroutine using a detached context
with a 5-second timeout, so a slow or failing audit insert never blocks or
fails the request. Insert failures MUST be logged, not returned.

6.4. For each request the recorded fields MUST be: `Action` = `"<METHOD>
<path>"`; `ResourceType` = `"http"`; `ResourceID` = the chi route pattern
when available (`routePattern`), falling back to the raw path; `ActorID`,
`OrgID`, `SiteID` from the auth claims context when present; `Details` =
`{method, path, status, bytes, duration_ms}`.

6.5. `Outcome` MUST be derived from the response status (`outcomeFromStatus`):
0 (handler never wrote a response) → `error`; 200–399 → `success`; 401/403 →
`denied`; other 4xx → `failure`; 5xx → `error`. This mapping is pinned by
`middleware_test.go`, which documents the status-0 case as a fixed bug
(previously recorded as success).

6.6. Client IP MUST be taken from the first entry of `X-Forwarded-For`, then
`X-Real-IP`, then `r.RemoteAddr` (`clientIP`).

6.7. A nil `Recorder` MUST make the middleware a pass-through.

### 7. Integration & Retention

7.1. The service MUST be constructed once at server startup via
`audit.New(pool)` (`cmd/server/server_adapters.go`) and passed to the API
server (`cmd/server/server_init.go`).

7.2. Security-relevant actions outside the middleware MUST call
`AuditService.Record` directly with typed `EventType` values (observed
callers: login/logout in `internal/api/routes_helpers_extra.go`, policy
violation dismiss/remediate in `policy_violations.go`, check run-now in
`checks_handlers.go`, patch deploy in `patches.go`, remote sessions in
`remote_handlers.go`).

7.3. Audit rows MUST be subject to the per-tenant retention purger, which
hard-deletes aged rows from `audit_events` and `check_results` on a daily
cadence (`tenancy.NewRetentionPurger`, `cmd/server/server_init.go`).

---

## Known Limitations

- **Per-resource verification is structural, not link-contiguous.** Because
  the write-side chain is global, `GetEventChain` cannot reconstruct a
  per-resource hash chain; W8 changed it to verify each link's stored hash in
  isolation and report `PrevHash` discontinuities as `GapCount`. That catches
  tampering with recorded content but **cannot detect an altered `PrevHash`**
  or a missing/deleted event within the subset — the prior contiguous-chain
  guarantee no longer applies.
- **Partial hash coverage.** `IP`, `UserAgent`, `OrgID`, `SiteID` are not in
  the hash input, so those columns can be altered without detection.
- **Retention vs. immutability.** The tenancy retention purger deletes audit
  rows; the chain is tamper-evident but not permanent.
- **`writeMu` is per-process only.** Chain extension is serialized within one
  server (W8), but two replicas sharing one database can still race and fork
  the chain; there is no distributed lock or transactional `... ORDER BY` +
  insert.
- **No in-repo DDL.** No `CREATE TABLE audit_events` migration or seed file
  exists in this repository; the 15-column schema is implied by the SQL in
  `audit.go` and managed externally.
- **`Details` from raw bytes is not validated.** `marshalDetails` passes
  `[]byte`/`json.RawMessage` through unchanged; malformed JSON can be stored
  and hashed as-is.
