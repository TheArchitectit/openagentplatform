# Changelog

All notable changes to OpenAgentPlatform are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for the BSL-licensed releases.

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

- [RELEASE_v1.10.0.md](RELEASE_v1.10.0.md)
- [RELEASE_v2.9.0.md](RELEASE_v2.9.0.md)
- [RELEASE_v1.9.0.md](RELEASE_v1.9.0.md) through [RELEASE_v1.9.6.md](RELEASE_v1.9.6.md)

## Related documents

- [ROADMAP_AND_SPRINTS.md](ROADMAP_AND_SPRINTS.md) -- planned sprints
- [ARCHITECTURE.md](ARCHITECTURE.md) -- system design
- [SETUP.md](SETUP.md) -- local setup
