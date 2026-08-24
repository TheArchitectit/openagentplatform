# Changelog

All notable changes to OpenAgentPlatform are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for the BSL-licensed releases.

---

## [1.2.0] - 2026-08-23 -- Wiring Remediation & OpenSpec Reconciliation COMPLETE

### Summary

v1.2.0 closes the two systemic gaps left by Phase 6: (1) subsystems that shipped
with unit tests but were never wired into `cmd/server` (silently 503'ing or writing
wrong data), and (2) an OpenSpec tree that had drifted from the Go implementation.
All W1–W8 wiring items from `docs/SPRINT_WIRING_REMEDIATION_PLAN.md` are complete,
and the OpenSpec audit is reconciled through P2. No breaking changes, API changes,
or package-version changes are included.

### Wiring Remediation (W1–W8) ✓

- **W1 Heartbeat persistence** — tolerant `UnmarshalJSON` on `models.Heartbeat`
  accepts both `int64` seconds and RFC3339 (`ae11d37`); agent heartbeats persist.
- **W2 Single check-result owner** — dispatcher is evaluation-only; the ingestor is
  the sole persister, eliminating duplicate inserts (`d97532c`).
- **W3 Notifier registry** — constructed and injected into alerts + API; alert
  notifications dispatch and `/test` no longer 503s (`4d90fad`).
- **W4 Reporting** — store + scheduler wired into `wireSupportServices`, due-schedule
  iteration fixed; all `/reports` endpoints live (`61b29e8`).
- **W5 Remote shell** — remote handlers, recording store, and session publisher
  wired end-to-end; `/shell/*` routes live (`4da9d9e`).
- **W6 Tenancy** — RLS migration set rewritten against live tables + wired at startup
  behind a flag; tenant context parameterized; tier resolution and quota middleware
  wired (`3ad3f4f`).
- **W7 Adapter proxy** — proxy aligned to `/api/v1/adapters/*`; cost windows use
  epoch floats; all seven adapter modules register at assembly (`b5963b7`).
- **W8 Correctness** — resilience, telemetry, audit, billing, relay fixes (see
  `RELEASE_NOTES_v1.2.0.md` for detail) (`d619f08`).

### OpenSpec Reconciliation (audit P0–P2) ✓

- **P0** truth-layer rewrite: `STATUS.md`, `PROJECT_PLAN.md`, `rmm-core` spec
  rewritten to Go reality (`b74b4da`).
- **P1** fixed spec path drift + stale source banners; flipped 6 `PLANNED`-but-built
  specs to `COMPLETE` (`c0474f7`, `6c35656`).
- **P2** authored 13 missing capability specs + `billing-stripe`; merged duplicate
  Stripe client into `client.go` (`66921fa`, `be16965`, `b79f6c9`).
- Navigation maps (`INDEX_MAP.md`, `HEADER_MAP.md` + docs/ variants) updated.

### W8 Detail Highlights

- **Resilience**: adapter circuit breaker now executes; double rate limiting removed.
- **Telemetry**: no-op `TraceDB` replaced with `db.WithTracing()`; `HealthChecker`
  wired into `/readyz`; metrics summary reports live counts.
- **Audit**: chain-verify semantics fixed; `RequireRole(Admin, Technician)` gate on
  `/audit` reads; chain extension serialized.
- **Billing**: `OrgBillingState` persisted via `PGStateStore`.
- **Relay**: idle-reap keys off `LastActivityAt`; service parked as library pending
  transport + auth design.

### Commits

- `ae11d37` W1 heartbeat decode · `d97532c` W2 dup results · `4d90fad` W3 notifier ·
  `61b29e8` W4 reports · `4da9d9e` W5 shell · `3ad3f4f` W6 tenancy ·
  `b5963b7` W7 adapter proxy · `d619f08` W8 small items
- `b74b4da` P0 truth-layer rewrite · `c0474f7`/`6c35656` P1 drift/status ·
  `66921fa`/`be16965`/`b79f6c9` P2 specs

### Next

OpenSpec P3 RMM parity gaps remain open: agent auto-update channel,
WinUpdate/AutomatedTask automation, and cloud/hypervisor monitoring. (Maintenance
windows and offline-agent SLA alerting are now implemented and verified in-tree,
uncommitted.) See `docs/GAP_ANALYSIS_RMM_PLATFORM.md`.

---

## [1.1.0] - 2026-08-22 -- Phase 6: Commercial Tiering COMPLETE

### Summary

Phase 6 (Commercial Tiering) is now complete with all 3 sprints delivered. The platform now supports commercial licensing, multi-tenant isolation, Stripe billing, enterprise reporting, and managed A2A relay.

### Sprint 6.1: Feature Gating + Licensing ✓

- **BSL 1.1 License**: Business Source License with Change Date 2030-01-01
- **Contributor License Agreement**: CLA template for external contributors
- **Ed25519 License Validation**: Cryptographic license verification with tamper detection
- **License Tiers**: Community, Pro, Enterprise with feature gating
- **Grace Period**: 30-day offline grace period for license validation
- **Endpoint Limits**: Per-tier endpoint limits with clear error messages

### Sprint 6.2: Multi-Tenancy ✓

- **PostgreSQL RLS**: Row Level Security for tenant data isolation
- **Tenant Model**: UUID-based tenant identification with slug
- **Tenant Store**: CRUD operations for tenant management
- **Per-Tenant Config**: Isolated configuration storage per tenant
- **Migration System**: Versioned migrations for tenant schema

### Sprint 6.3: Billing + Enterprise ✓

- **Stripe Billing**: Customer creation, subscription management, webhook handling
- **Usage Metering**: Agent count, A2A tasks, API calls tracked per tenant
- **Enterprise Reporting**: 4 report templates (compliance, patch status, alerts, endpoints)
- **Scheduled Reports**: Daily, weekly, monthly delivery with CSV export
- **Managed A2A Relay**: Cross-network agent communication with tenant isolation

### Test Coverage

| Package | Tests | Coverage |
|---------|-------|----------|
| internal/licensing/ | 8 | License validation, features, tiers, endpoints |
| internal/tenancy/ | 25 | RLS migrations, tenant store, config store |
| internal/billing/ | 3 | Stripe client, metering service |
| internal/reporting/ | 25 | Templates, reports, schedules, CSV export |
| internal/relay/ | 20 | Connections, metrics, cleanup |

### Commits

- `88b27ed` — BSL 1.1 license file + contributor agreement
- `22ecd69` — Ed25519 license validation + feature gating
- `ef7fefc` — PostgreSQL RLS migration for multi-tenant isolation
- `84697d4` — Tenant store + per-tenant config storage
- `6d6105a` — Stripe client with subscription management
- `c142f2d` — Enterprise reporting with templates and scheduling
- `fcf49af` — Managed A2A relay service

### Next Phase

All phases complete. Platform is ready for v1.1.0 release.

---

## [1.0.0] - 2026-08-22 -- Phase 5: Production Hardening COMPLETE

### Summary

Phase 5 (Production Hardening) is now complete with all 4 sprints delivered and QA-reviewed. The codebase has undergone comprehensive security hardening, observability instrumentation, resilience testing, and documentation.

### QA Review Results

- **40 findings** identified and resolved (5 CRITICAL, 8 HIGH, 12 MEDIUM, 15 LOW)
- **59 new tests** replacing 3 placeholder stubs
- **19 packages** pass with race detector clean
- **1,867 lines** of test coverage added

### Key Fixes

#### Security
- SQL injection prevention via table name validation (`^[a-zA-Z_][a-zA-Z0-9_]*$`)
- Path traversal protection with `filepath.Clean` + `HasPrefix` containment
- SSRF protection for webhook notifications (multi-IP validation)
- Generic error messages to clients (no internal details leaked)
- OAuth token cleanup goroutine for expired tokens/codes/nonces

#### Reliability
- HealthChecker race condition fixed with `sync.RWMutex`
- CAS TOCTOU race fixed with atomic conditional INSERT
- CircuitBreaker `recordSuccess` mutex protection
- MemoryBackend per-path version counter (`map[string]int`)
- Injector double-append removed, cleanup zeroing added

#### Correctness
- W3C traceparent format (`version-traceID-spanID-flags`)
- `errors.As` usage for proper error type checking
- YAML comment false positive filtering
- Placeholder filter checks matched value, not entire line
- TokenType returns "Bearer" vs "DPoP" based on key presence

#### Observability
- OpenTelemetry instrumentation (Go + Python + TypeScript)
- Prometheus metrics export with `InitMeter` storing serviceName
- Distributed tracing with W3C traceparent propagation
- Grafana dashboards for system, A2A, and agent health

#### Resilience
- Circuit breaker with half-open probe and mutex protection
- Retry config validation (reject `InitialDelay=0` when `MaxAttempts>1`)
- Chaos-mesh resilience testing infrastructure
- Load testing with k6 (10K endpoint target)

#### Documentation
- MkDocs Material documentation site
- API reference generation from OpenAPI specs
- Contributor guide + architecture decision records
- QA review documentation (`docs/QA_REVIEW_PHASE3-5.md`)

### Test Coverage

| Package | Tests | Coverage |
|---------|-------|----------|
| secrets/ | 45+ | SQL injection, path traversal, SSRF, token cleanup |
| gate/ | 25+ | SecretScan, SchemaScan, GateRunner integration |
| resilience/ | 15+ | Circuit breaker, retry validation |
| internal/checks/ | 19 | Threshold evaluation, severity mapping |
| internal/events/ | 22 | NATS dispatch, heartbeat, retry |
| internal/notify/ | 18 | Slack/webhook/email, HMAC signing |
| internal/monitoring/ | 8 | HealthChecker concurrent safety |
| internal/audit/ | 10 | Route pattern extraction, 3xx handling |
| internal/api/ | 12 | RBAC enforcement |

### Commits

- `0131b66` — 21 CRITICAL/HIGH/MEDIUM bug fixes (19 files)
- `fa83309` — All remaining findings (20+ files)
- `c71dfd2` — Placeholder test replacement + new test coverage (5 files, +1,867 lines)

### Next Phase

Phase 6 (Commercial Tiering) begins with:
- Sprint 6.1: Feature Gating + Licensing
- Sprint 6.2: Multi-Tenancy
- Sprint 6.3: Billing + Enterprise

---

## [1.5.0] - 2026-06-15 -- Sprint 1.5: Scripts, Remote Shell, Monaco

### Added

- **Script CRUD API**: full REST endpoints for scripts (`/api/v1/scripts`)
  with content-hash deduplication, versioning, and tags
- **4-runtime script executor**: bash, Python, PowerShell, Node.js
  runtimes via Docker exec with resource limits
- **Monaco editor UI**: integrated `@monaco-editor/react` for script
  authoring with syntax highlighting and IntelliSense
- **Remote shell sessions**: WebSocket-based terminal sessions
  proxied through OAP server with mTLS to agents
- **Session recording**: all shell sessions recorded to TimescaleDB
  for playback and audit
- **xterm.js terminal**: browser-based terminal UI with resizing and
  copy/paste support
- **Agent executor enhancements**: script dispatch via NATS subject
  `oap.scripts.<agent_id>`

### Changed

- Agent daemon subscribes to script commands in addition to check
  commands
- Web UI adds Scripts, Remote Shell, and Session Replay routes

### Fixed

- Script content validation rejects unsafe characters in interpreter
  paths
- Shell session cleanup on WebSocket disconnect
- Monaco editor dark mode color scheme

---

## [1.4.0] - 2026-06-01 -- Sprint 1.4: Patches

### Added

- **Patch approval workflow**: scan results require human approval
  before application
- **OS inventory scanner**: detect installed packages and available
  updates per agent
- **Deployment engine**: staged rollout with canary, wave, and
  immediate deployment strategies
- **Patch status UI**: dashboard with pending, in-progress, succeeded,
  and failed patch jobs
- **Patch API**: `/api/v1/patches`, `/api/v1/patches/{id}/approve`,
  `/api/v1/patches/{id}/apply`
- **Notification on patch completion**: webhook + email per user
  preference

### Security

- Patch application requires admin or operator role
- Approval audit trail captured in `audit_events`

---

## [1.3.0] - 2026-05-15 -- Sprint 1.3: Policies & Compliance

### Added

- **OPA policy engine**: declarative rego policies evaluated against
  agent and system state
- **Compliance collectors**: periodic scans for CIS benchmarks, OS
  hardening, and custom checks
- **Violation alerts**: policy violations create alert events with
  severity mapping
- **Policy library UI**: browse, enable, and disable policies from
  the dashboard
- **Built-in policies**: 10 starter policies (SSH config, firewall,
  password policy, disk encryption, etc.)

### Security

- Policy files are signed and verified before evaluation
- Policy changes are audited

---

## [1.2.0] - 2026-05-01 -- Sprint 1.2: Alerts

### Added

- **Alert rule engine**: declarative YAML rules with threshold,
  duration, and severity
- **Notification channels**: email (SMTP), webhook, Slack, PagerDuty
- **Alert inbox UI**: triage, acknowledge, resolve, and assign
  alerts
- **Alert preferences**: per-user routing and quiet hours
- **Alert deduplication**: fingerprint-based to suppress duplicates
- **Alert API**: `/api/v1/alerts`, `/api/v1/alerts/{id}/acknowledge`,
  `/api/v1/alerts/{id}/resolve`

### Changed

- Check results now flow through the alert engine for real-time
  evaluation

---

## [1.1.0] - 2026-04-15 -- Sprint 1.1: Checks

### Added

- **Check CRUD API**: `/api/v1/checks` with scheduling, thresholds,
  and notifications
- **Built-in check library**: ping, HTTP, TCP, DNS, CPU, memory,
  disk, service, certificate
- **Executor enhancements**: parallel execution, timeout, retry
  with backoff
- **Ingest pipeline**: check results published via NATS and
  persisted to TimescaleDB hypertables
- **Checks dashboard**: real-time status grid with filtering and
  search

### Performance

- Check results batched (100 events / 5s) before DB write
- Index on `(agent_id, check_id, time)` for time-range queries

---

## [1.0.0] - 2026-04-01 -- Sprint 0.2: Agent & Foundation

### Added

- **Agent CLI binary**: `./bin/oap-agent` with `-register` and
  `-daemon` modes
- **Agent registration**: generates mTLS cert, persists to
  `~/.oap/agent.crt`, registers with server
- **Heartbeat**: 30s interval, published to `oap.agents.<id>.heartbeat`
- **Endpoint list**: web UI shows registered agents with status
- **Audit log**: append-only log of agent registration, deregistration,
  and config changes
- **Setup guide**: docs/SETUP.md (5-minute quickstart)
- **Agent health checks**: OS, disk, memory, load average

### Fixed

- Agent reconnect on NATS disconnect
- Cert rotation at 60 days (30 days before expiry)

---

## [0.2.0] - 2026-02-15 -- Sprint 0.1: Foundation

### Added

- **Monorepo scaffold**: `/oap-server` (Go), `/web` (React),
  `/oap-data` (Python migrations), `/mcp-server` (Go)
- **CI pipeline**: GitHub Actions for lint, test, build, and
  Docker image publish
- **Database schema**: initial Alembic migrations for users, sites,
  agents, checks, alerts
- **NATS messaging**: subject hierarchy and mTLS bootstrap
- **OIDC integration**: Dex with static users config
- **OpenAPI spec**: auto-generated from Go server annotations
- **React shell**: TanStack Router, TanStack Query, Tailwind CSS
- **Health endpoint**: `/health` returns `{"status":"ok"}`

### Security

- Default `JWT_SECRET` rejected in production
- All secrets must be set via environment variables
- Network isolation in docker-compose for DB and NATS

---

## [2.1.0] - 2026-04-30 -- Sprint 2.1: A2A Gateway

### Added

- **A2A Gateway**: JSON-RPC, HTTP, and REST endpoints for
  agent-to-agent task delegation
- **AgentCard registry**: discoverable agent capabilities
- **TaskManager**: stateful task lifecycle (pending, running,
  completed, failed, cancelled)
- **EventBridge**: real-time task event streaming via Server-Sent
  Events
- **A2A routes**: `/api/v1/a2a/tasks`, `/api/v1/a2a/agents`,
  `/api/v1/a2a/events`

---

## [2.2.0] - 2026-05-15 -- Sprint 2.2: Framework Adapters

### Added

- **AgentWrapper ABC**: Python abstract base class for LLM agent
  adapters
- **6 framework adapters**:
  - Anthropic Claude
  - OpenAI GPT
  - AutoGen
  - CrewAI
  - LangGraph
  - Semantic Kernel
- **ProcessPool**: parallel adapter execution with concurrency limits
- **Orchestration**: multi-agent task coordination (sequential,
  parallel, debate, vote)
- **Cost management**: token usage tracking, cost calculation, budget
  alerts

---

## [2.3.0] - 2026-06-01 -- Sprint 2.3: Bridge & End-to-End

### Added

- **Python-Go bridge**: HTTP RPC with Pydantic schema validation
- **Adapter REST API**: `/api/v1/a2a/adapters` for adapter
  registration and health
- **A2A dashboard**: web UI for browsing agents, tasks, and cost
  analytics
- **End-to-end wiring**: A2A tasks flow from web UI to Go gateway
  to Python adapter and back
- **Adapter health checks**: periodic liveness probes; unhealthy
  adapters are excluded from routing

### Fixed

- Aligned Go-Python JSON-RPC contract
- SSE event ordering and replay
- A2A route prefix consistency

---

## [3.0.0] - 2026-06-10 -- Phase 3: Secrets & Security

### Added

- **SecretBackend ABC**: pluggable secret storage interface
- **5 secret backends**: local encrypted, HashiCorp Vault, AWS
  Secrets Manager, GCP Secret Manager, Azure Key Vault
- **Secret resolver**: agent credential injection at task time
- **A2A auth**: per-task authorization scopes
- **Script safety**: static analysis of scripts before execution
  (deny-list of dangerous patterns)
- **OAuth for A2A**: agents can call external APIs on behalf of users
  with OAuth 2.0 flows

### Security

- Envelope encryption (AES-256-GCM) for all secrets at rest
- Secret access logged in audit log
- Master key stored in env var or KMS

---

## [4.0.0] - 2026-06-12 -- Phase 4: Settings & UI Polish

### Added

- **Settings pages**: user profile, org settings, integrations,
  notifications
- **Monaco editor**: integrated as the default code editor for
  scripts and config
- **Dark mode theming**: full dashboard dark mode with WCAG 3.0+
  compliance
- **Accessibility**: keyboard navigation, screen reader support,
  focus management, color contrast
- **Responsive layout**: mobile-friendly dashboard with collapsible
  sidebars
- **Multi-tenant org scoping**: data isolation by organization

---

## [5.0.0] - 2026-06-14 -- Phase 5: Observability

### Added

- **OpenTelemetry tracing**: distributed tracing across Go server,
  Python adapters, and agent daemons
- **Prometheus metrics**: HTTP latency, agent count, check rate,
  alert rate, NATS throughput, DB pool stats
- **Resilience patterns**: circuit breaker, retry with backoff,
  rate limiting
- **Health probes**: `/health` (liveness), `/ready` (readiness with
  DB + NATS checks)
- **Go tests**: unit and integration tests for core packages
- **Grafana dashboards**: pre-built for OAP overview, agents, API,
  database

---

## [5.1.0] - 2026-06-15 -- Ozore AI Integration

### Added

- **Ozore AI adapter**: OpenAI-compatible adapter for Ozore
  hosted LLM
- **Default adapter wiring**: Ozore used as the default LLM across
  all AI background tasks (policy suggestions, natural-language
  queries, automated remediation)
- **Env config**: `OZORE_API_KEY`, `OZORE_MODEL`, `OZORE_BASE_URL`
- **UI**: "AI Agents" section in settings to manage API keys and
  model selection

---

## [6.0.0] - 2026-06-17 -- Live Dashboard & Mission Control

### Added

- **Live dashboard data**: real-time WebSocket-driven metrics on
  the home page (no more static demo data)
- **Multi-tenant org scoping**: enforced on all queries and
  mutations; cross-org access rejected
- **Mission control aesthetic**: dark dashboard with monospace
  accents, status badges, and pulse indicators
- **PostCSS config**: missing postcss.config.js added to enable
  Tailwind CSS compilation
- **Settings CSS fix**: relative import paths corrected

---

## Release notes

For detailed release notes, see:

- [RELEASE_NOTES_v1.2.0.md](../RELEASE_NOTES_v1.2.0.md) -- v1.2.0 wiring remediation
- [RELEASE_NOTES_v1.1.0.md](../RELEASE_NOTES_v1.1.0.md) -- v1.1.0 file-size compliance

## Related documents

- [MASTER_IMPLEMENTATION_PLAN.md](plans/MASTER_IMPLEMENTATION_PLAN.md) -- canonical roadmap
- [ARCHITECTURE.md](ARCHITECTURE.md) -- system design
- [SETUP.md](SETUP.md) -- local setup
- [QA_REVIEW_OPENSPEC_COVERAGE.md](QA_REVIEW_OPENSPEC_COVERAGE.md) -- OpenSpec audit
