# Wiring Remediation Plan — Built-but-Not-Connected Subsystems

**Created:** 2026-08-24
**Origin:** OpenSpec P2 spec-writing pass (commits `66921fa`–`b79f6c9`). Writing
honest specs for 13 capabilities exposed a systemic pattern: packages delivered
with unit tests but **never wired into `cmd/server`**, so they 503, no-op, or
write wrong data in the shipped binary.
**Related:** docs/QA_REVIEW_OPENSPEC_COVERAGE.md (audit),
openspec/specs/*/spec.md Known Limitations sections (full detail per item).

---

## The Pattern

Sprints delivered components against their own unit tests; there was no
integration step that verified `cmd/server` actually constructs and injects
them. Regression gates (`deploy.sh` + `regression_check.py`) did not catch
missing wiring because nothing crashed — features just silently answered 503.

## Fix Order (highest value first)

### W1. Heartbeat decode contract (P0 — core liveness broken)
- **Bug:** agent publishes `Timestamp int64` unix-seconds
  (`pkg/agent/heartbeat.go:14`, `time.Now().Unix()`); server handler parses
  `models.Heartbeat.Timestamp time.Time` (RFC3339). `json.Unmarshal` rejects
  the number → every Go-agent heartbeat fails to decode and is never
  persisted. Agents appear online only via registration upserts.
- **Fix:** custom `UnmarshalJSON` on `models.Heartbeat` accepting both int64
  seconds and RFC3339 string, OR change the handler to a dedicated wire struct.
  Prefer the tolerant decoder: older agents in the field must keep working.
- **Spec:** openspec/specs/event-bus/spec.md (Known Limitations #1).
- **Test:** unit test decoding both shapes; integration test publishing an
  agent heartbeat through NATS asserts the `agents` row updates.

### W2. Duplicate check-result persistence (P0 — data corruption)
- **Bug:** `CheckDispatcher` (queue group `oap-check-evaluator`,
  `internal/events/checkdispatcher.go:65`) AND `ResultIngestor` (queue
  `oap-check-ingest`, `internal/checks/ingest.go:76`) both subscribe
  `oap.agents.*.results` and both call `InsertCheckResult` (plain INSERT,
  no upsert) → every result stored twice.
- **Fix:** pick one persistence owner. Recommend: ingestor persists;
  dispatcher keeps evaluation-only duties and drops its insert path (its
  AlertSink is nil-wired anyway). Alternatively make insert idempotent
  (upsert on natural key) as defense-in-depth.
- **Spec:** event-bus Known Limitations #2/#3.

### W3. Notifications registry (P1 — zero alert notifications send)
- **Bug:** `SetNotifierRegistry` never called from any wiring path; alert
  engine constructed without registry → `dispatchNotifications` returns
  immediately. Channel CRUD works (fallback registry) but `/test` returns
  503 and real alerts notify nobody.
- **Fix:** construct `notify.InitDefaultRegistry()` in `wireSupportServices`,
  call `apiServer.SetNotifierRegistry(...)` and pass it into
  `alerts.Config.NotifierRegistry`.
- **Spec:** notifications Known Limitations.

### W4. Reports store + scheduler (P1 — all /reports endpoints 503)
- **Bug:** `SetReportsStore`/`SetReportsScheduler` never called; also no
  `DataAggregator` implementation exists anywhere (generation would have no
  data source even if wired), and `Scheduler.tick` queries
  `ListSchedules(ctx, "")` → matches no rows, so schedules never fire.
  Download route (`PresignedURL` target) is unregistered.
- **Fix:** implement a pgx-backed `DataAggregator` for at least the high-value
  templates (agent_inventory, patch_compliance, audit_trail), wire store +
  scheduler in `wireSupportServices`, fix tick to iterate orgs or add an
  org-scoped due-schedules query, register `/runs/{id}/download` with token
  verification.
- **Spec:** reporting Known Limitations 1/2/4/5.

### W5. Remote shell handlers (P1 — all /shell/* routes 503)
- **Bug:** `SetRemoteHandler`, `SetRecordingStore`, `SetSessionRecorderFactory`
  never called from `cmd/server`. Deeper: `ShellManager.CreateSession` never
  publishes `shell.start`, so the agent never spawns a process end-to-end;
  live recorder factory is dead; SSH dial target is a placeholder session ID.
- **Fix order:** (a) wire setters so routes stop 503ing; (b) publish
  StartRequest on create-session; (c) attach live recorder; (d) resolve real
  SSH host from agent inventory/credentials.
- **Spec:** remote-access Known Limitations 1/2/3.

### W6. Tenancy wiring (P2 — multi-tenancy effectively Community-only)
- **Bugs:** (a) `TenantMigrator` never run — RLS tables/policies don't exist,
  AND migrations target diverged table names (`endpoints` vs `agents`,
  `checks` vs `check_definitions`, `audit_log` vs `audit_events`) and
  `tenant_id UUID` vs live string `org_id` — migration SQL needs rewriting
  before it can run; (b) `QuotaMiddleware` unwired; (c) `resolveOrgTier`
  stubs everyone to Community; (d) retention purger phase-1 queries
  nonexistent `alert_preferences` table (errors every run);
  (e) `SetTenantContext` SQL-injects tenant id into SET statement.
- **Fix:** rewrite RLS migration set against live table names + org_id model
  (or migrate columns), then wire migrator at startup behind a flag; wire
  tier resolution to license/billing state; fix phantom table + parameterize
  SET.
- **Specs:** multi-tenancy + data-model Known Limitations.

### W7. Adapter proxy contract (P2 — A2A dashboard still broken)
- **Bugs:** proxy forwards un-prefixed paths (`/adapters/invoke`, …) but
  Python serves only `/api/v1/adapters/*` (+2 legacy aliases) → most calls
  404; two Go callers disagree (`bridge.AdapterClient` uses correct
  `/api/v1/…`); cost endpoints send RFC3339 where Python wants float epoch →
  422; Python adapter registry empty at runtime (no module imports fire
  `@register_adapter`); `/adapters/tasks/events` has no upstream.
- **Fix:** align proxy paths to `/api/v1/adapters/*`; convert cost params to
  epoch floats; import the adapter modules in app assembly (or explicit
  registration list); decide task-events SSE ownership.
- **Spec:** adapter-service Known Limitations; supersedes stale "orphaned"
  claim in QA_REVIEW_PHASE2_v2.md.

### W8. Smaller correctness items (P3)
- resilience: execute or delete `adapterBreaker`; dedupe double rate limiting
  (inner API limiter + outer HTTP limiter both 100/200).
- observability: replace deprecated no-op `telemetry.TraceDB` call with
  `TraceDBFromDSN`; wire monitoring.HealthChecker into `/readyz` or delete;
  give metrics summary writers call sites or remove endpoint.
- audit-log: chain verify semantics (global-chain vs per-resource subset
  mismatch reports false breaks); add RequireRole gate on /audit reads;
  serialize chain extension.
- billing: persist OrgBillingState (currently in-memory, lost on restart);
  webhook dispatch beyond ack-only.
- relay: wire RelayService into a binary or park it; idle-reap by last
  activity not EstablishedAt age.

## Gate

Every item lands through `deploy.sh` + `regression_check.py` with a FAIL-*
registry entry added first (per REGRESSION_PREVENTION.md), plus the unit +
integration tests named in each section. One item per commit.

## Status Tracker

| Item | Status | Commit |
|------|--------|--------|
| W1 heartbeat decode | done | ae11d37 |
| W2 dup results | done | d97532c |
| W3 notifier registry | done | 4d90fad |
| W4 reports wiring | done | 61b29e8 |
| W5 shell wiring | pending | — |
| W6 tenancy wiring | pending | — |
| W7 adapter proxy | pending | — |
| W8 small items | pending | — |
