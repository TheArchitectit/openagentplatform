export const meta = {
  name: 'sprint-02-impl',
  description: 'Implement Sprint 0.2: agent CLI binary, registration/heartbeat, endpoint list, audit log, setup guide',
  phases: [
    { title: 'Agent', detail: 'Create agent CLI binary (Go, cross-compiled) with NATS + heartbeat' },
    { title: 'Registration', detail: 'Server-side agent registration + heartbeat flow' },
    { title: 'Endpoints', detail: 'React endpoint list page with real-time WebSocket updates' },
    { title: 'Audit', detail: 'Audit log infrastructure: capture login, API call, agent action' },
    { title: 'Setup', detail: '5-minute setup guide and Docker compose polish' },
    { title: 'Commit', detail: 'Stage, commit, push, close Sprint 0.2 issues' },
  ],
}

log('Sprint 0.2 implementation starting -- Agent Binary + Registration + Endpoints + Audit + Setup')

var repoRoot = '/mnt/data/git/openagentplatform'

// Phase 1: Agent CLI binary (sonnet -- substantial Go code)
phase('Agent')
var agentBinary = await agent(
'Build the OpenAgentPlatform agent CLI binary at ' + repoRoot + '.\n' +
'\n' +
'This is a Go binary that runs on managed endpoints (Windows, Linux, macOS). It:\n' +
'- Connects to the NATS server with mTLS\n' +
'- Registers with the platform API\n' +
'- Sends heartbeats every 60 seconds\n' +
'- Has a check executor that receives check commands and sends results\n' +
'- Has a script executor that runs scripts and streams output\n' +
'- Reports host information (hostname, OS, arch, CPU, memory, disk)\n' +
'\n' +
'Create these files under cmd/agent/ and pkg/agent/:\n' +
'\n' +
'**cmd/agent/main.go** -- entry point:\n' +
'- Parse CLI flags (-config, -register, -version)\n' +
'- Load config from file + env vars\n' +
'- Initialize logger, NATS connection (mTLS), API client (HTTP with auth token)\n' +
'- Run registration flow, start heartbeat ticker (60s)\n' +
'- Start check handler and script handler subscriptions\n' +
'- Listen for shutdown signal (SIGINT/SIGTERM), graceful shutdown\n' +
'\n' +
'**pkg/agent/config.go** -- config from YAML/JSON file, env var overrides (AGENT_SITE_ID, AGENT_TOKEN, NATS_URL, API_URL), defaults per OS\n' +
'\n' +
'**pkg/agent/register.go** -- POST /api/v1/agents/register with hostinfo, receive agent_id + auth token, save token to local config\n' +
'\n' +
'**pkg/agent/heartbeat.go** -- publish heartbeat to NATS subject oap.agents.<agent_id>.heartbeat every 60s, payload includes CPU%/mem%/disk%/uptime\n' +
'\n' +
'**pkg/agent/checks.go** -- subscribe to oap.agents.<agent_id>.checks, dispatch to check runners (ping, http, tcp, dns, cpu, memory, disk, service, script, custom), publish results\n' +
'\n' +
'**pkg/agent/checkers/ping.go** -- ICMP ping checker\n' +
'**pkg/agent/checkers/http.go** -- HTTP status + body match\n' +
'**pkg/agent/checkers/tcp.go** -- TCP connect\n' +
'**pkg/agent/checkers/dns.go** -- DNS resolution\n' +
'**pkg/agent/checkers/cpu.go** -- CPU usage % (cross-platform: gopsutil)\n' +
'**pkg/agent/checkers/memory.go** -- Memory usage %\n' +
'**pkg/agent/checkers/disk.go** -- Disk usage %\n' +
'**pkg/agent/checkers/service.go** -- Service status (system-specific)\n' +
'**pkg/agent/checkers/registry.go** -- map check_type string to Checker interface\n' +
'\n' +
'**pkg/agent/scripts.go** -- subscribe to oap.agents.<agent_id>.scripts, detect runtime (bash, powershell, python, node, cmd), execute with timeout, stream stdout/stderr via NATS\n' +
'\n' +
'**pkg/agent/hostinfo.go** -- gopsutil-based hostname, OS, platform, arch, num CPU, total memory, total disk, agent version\n' +
'\n' +
'**pkg/agent/nats.go** -- NATS wrapper with mTLS (ca, cert, key), Publish/Subscribe/Request, reconnection handling\n' +
'\n' +
'Also read the existing Makefile and add:\n' +
'- build-agent: go build -o bin/oap-agent ./cmd/agent\n' +
'- build-agent-all: GOOS=linux|darwin|windows go build\n' +
'\n' +
'Use packages: github.com/nats-io/nats.go, github.com/shirou/gopsutil/v4, github.com/google/uuid, sigs.k8s.io/yaml\n' +
'\n' +
'After writing all files, run:\n' +
'  cd ' + repoRoot + ' && go get ./... && go mod tidy && go build ./cmd/agent/ && go vet ./...\n' +
'\n' +
'If gopsutil download fails, try GOFLAGS=-mod=mod or skip and report.\n' +
'Report file count and any build errors.',
{ label: 'agent-binary', phase: 'Agent', model: 'sonnet' }
)

// Phase 2: Agent registration + heartbeat server-side (sonnet -- API + NATS)
phase('Registration')
var registration = await agent(
'Implement the server-side agent registration and heartbeat flow at ' + repoRoot + '.\n' +
'\n' +
'Read existing code in internal/api/routes.go, internal/api/handler.go, internal/events/nats.go, internal/db/postgres.go first.\n' +
'\n' +
'**1. Agent Registration API** -- new file internal/api/agents.go:\n' +
'- POST /api/v1/agents/register -- accept agent_token, site_id, hostname, os, arch, platform, cpu_count, total_memory_mb, total_disk_gb, agent_version\n' +
'  - Validate agent_token matches site registration token\n' +
'  - Create agent record in DB with status=online\n' +
'  - Generate agent JWT (EdDSA, 24h expiry, claims: agent_id, site_id, org_id)\n' +
'  - Return: agent_id, token, nats_subjects\n' +
'- GET /api/v1/agents -- list all agents (paginated, filterable by site, status, search)\n' +
'- GET /api/v1/agents/{id} -- agent detail with last check results\n' +
'\n' +
'**2. Update internal/api/routes.go** -- register the agents routes with chi\n' +
'\n' +
'**3. Heartbeat handler** -- new file internal/events/heartbeat.go:\n' +
'- Subscribe to NATS subject oap.agents.*.heartbeat\n' +
'- Parse heartbeat payload, update agent status/last_seen/metrics in DB\n' +
'- If agent was offline, emit AgentOnline event\n' +
'- If heartbeat missed for 120s, mark agent as offline (DB function/trigger)\n' +
'\n' +
'**4. Agent check dispatcher** -- new file internal/events/checkdispatcher.go:\n' +
'- When check assignment created, publish to oap.agents.<agent_id>.checks\n' +
'- Subscribe to oap.agents.*.results, store in check_results, evaluate alerts\n' +
'\n' +
'**5. Update internal/events/nats.go** -- add Subscription management, heartbeat and check handler init, reconnection\n' +
'\n' +
'**6. Update cmd/server/main.go** -- start NATS subscriptions after server starts, graceful shutdown for NATS\n' +
'\n' +
'**7. Update pkg/models/models.go** -- add Agent struct matching DB schema if not present\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back what was created and any errors.',
{ label: 'agent-registration', phase: 'Registration', model: 'sonnet' }
)

// Phase 3: Endpoint list page with real-time updates (sonnet -- React + WebSocket)
phase('Endpoints')
var endpoints = await agent(
'Build the endpoint (agent) list page and real-time infrastructure for the React frontend at ' + repoRoot + '.\n' +
'\n' +
'Read existing code in web/src/ first. DO NOT run pnpm install or npm install.\n' +
'\n' +
'**1. Agent list page** -- new file web/src/routes/agents/index.tsx:\n' +
'- Table: Status icon (online/offline/error), Hostname, Site, OS, Last Seen, CPU, Memory, Disk, Actions\n' +
'- Status filter tabs: All, Online, Offline, Error. Search by hostname. Pagination (50/page).\n' +
'- Click row -- navigate to /agents/$agentId\n' +
'- Status: green dot (<2min), yellow (2-5min), red (>5min or offline)\n' +
'- Auto-refresh: "Updated X seconds ago" with manual refresh button\n' +
'\n' +
'**2. Agent detail page** -- new file web/src/routes/agents/$agentId.tsx:\n' +
'- Agent info card: hostname, OS, version, IP, uptime\n' +
'- Metrics: CPU gauge, Memory bar, Disk bar\n' +
'- Check results table (last 20)\n' +
'- Actions: Run Check, Remote Shell (disabled), View Logs\n' +
'\n' +
'**3. WebSocket hook** -- new file web/src/lib/websocket.ts:\n' +
'- Connect to ws://localhost:8080/ws (or VITE_WS_URL)\n' +
'- Auto-reconnect with exponential backoff (max 30s)\n' +
'- Heartbeat ping every 30s\n' +
'- Subscribe/unsubscribe to channels\n' +
'- TypeScript types for all message types\n' +
'\n' +
'**4. Real-time agent status hook** -- new file web/src/lib/useAgents.ts:\n' +
'- Fetch initial agent list from GET /api/v1/agents\n' +
'- Subscribe to WebSocket channel agents\n' +
'- Merge real-time heartbeat updates into agent list\n' +
'- Export: agents[], isLoading, error, refresh\n' +
'\n' +
'**5. Update sidebar** -- read web/src/components/sidebar.tsx, ensure Agents link points to /agents\n' +
'\n' +
'**6. Server-side WebSocket** -- new file internal/api/websocket.go:\n' +
'- gorilla/websocket upgrade endpoint at /ws\n' +
'- Authenticate via cookie or query param token\n' +
'- Per-client subscription manager\n' +
'- Broadcast when agent heartbeat updates DB, push to subscribers\n' +
'- Channels: agents, checks, alerts\n' +
'\n' +
'**7. Update internal/api/routes.go** -- add /ws endpoint\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created/modified.',
{ label: 'endpoint-list', phase: 'Endpoints', model: 'sonnet' }
)

// Phase 4: Audit log infrastructure (sonnet -- backend with hash chain)
phase('Audit')
var audit = await agent(
'Implement the audit log infrastructure for OpenAgentPlatform at ' + repoRoot + '.\n' +
'\n' +
'Read existing code in internal/ first.\n' +
'\n' +
'**1. Audit service** -- new file internal/audit/audit.go:\n' +
'- AuditService struct with DB pool\n' +
'- Event types: login, logout, api_call, agent_action, check_run, alert_change, policy_change, patch_deploy, script_run, user_manage, config_change\n' +
'- Record(event) method:\n' +
'  - Generate unique event_id (UUID v7 for time-sortable)\n' +
'  - Fetch previous event hash from DB (latest)\n' +
'  - Compute SHA-256(event_id + prev_hash + timestamp + actor_type + actor_id + action + resource_type + resource_id + details_json + outcome)\n' +
'  - Insert into audit_events table, return event\n' +
'- GetEvents(filter): by actor, action, resource, time range, paginated\n' +
'- GetEventChain(resource_id): returns hash chain for verification\n' +
'\n' +
'**2. Audit middleware** -- new file internal/audit/middleware.go:\n' +
'- HTTP middleware that wraps chi router\n' +
'- Captures: method, path, status code, duration, user_id from JWT, IP, user-agent\n' +
'- Records api_call event after response\n' +
'- Skips /health, /docs, /ws paths\n' +
'\n' +
'**3. Integration points** -- update these files:\n' +
'- internal/auth/oidc.go -- record login/logout events\n' +
'- internal/api/routes.go -- add audit middleware to chi stack\n' +
'- cmd/server/main.go -- pass audit service to auth and routes\n' +
'\n' +
'**4. Audit API** -- new file internal/api/audit.go:\n' +
'- GET /api/v1/audit/events -- list (paginated, filterable)\n' +
'- GET /api/v1/audit/events/{id} -- single event detail with chain verification status\n' +
'- GET /api/v1/audit/chain/{resource_id} -- verify hash chain integrity\n' +
'\n' +
'**5. Add audit routes** to internal/api/routes.go\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created/modified and any errors.',
{ label: 'audit-log', phase: 'Audit', model: 'sonnet' }
)

// Phase 5: 5-minute setup guide (haiku -- documentation)
phase('Setup')
var setup = await agent(
'Polish the setup guide and Docker compose for a smooth 5-minute onboarding experience at ' + repoRoot + '.\n' +
'\n' +
'Read these files first: docs/SETUP.md, deploy/docker-compose.yml, deploy/docker-compose.dev.yml, Makefile, .env.example\n' +
'\n' +
'**1. Update docs/SETUP.md** to be a complete, working guide:\n' +
'- Prerequisites: Docker, Docker Compose v2, Go 1.23+ (for agent), Node 20+ (for web dev)\n' +
'- Step 1: Clone + copy .env.example to .env\n' +
'- Step 2: docker compose -f deploy/docker-compose.yml up -d\n' +
'- Step 3: Verify: curl http://localhost:8080/health\n' +
'- Step 4: Login: http://localhost:5173, Sign in, admin@oap.local / password\n' +
'- Step 5: Build and run agent: make build-agent && ./bin/oap-agent -register\n' +
'- Step 6: See agent in dashboard: http://localhost:5173/agents\n' +
'- Troubleshooting section: common issues + fixes\n' +
'\n' +
'**2. Verify deploy/docker-compose.yml** works end-to-end:\n' +
'- Postgres with init.sql, NATS with nats.conf, Dex with config.yaml\n' +
'- Server builds and starts, Web dev server\n' +
'- Healthchecks on all services, correct dependency ordering (condition: service_healthy)\n' +
'\n' +
'**3. Update Makefile** -- add setup, setup-dev, seed, reset targets\n' +
'\n' +
'**4. Update .env.example** -- every required variable with sensible default and comment\n' +
'\n' +
'**5. Update README.md** -- add Quick Start section at top, 5-line copy-paste commands, status badges\n' +
'\n' +
'Use Edit tool for existing files, Write tool for new content. Report back files modified.',
{ label: 'setup-guide', phase: 'Setup', model: 'haiku' }
)

// Phase 6: Commit, push, close issues (haiku -- mechanical)
phase('Commit')
var commit = await agent(
'Stage, commit, and push Sprint 0.2 implementation from ' + repoRoot + '.\n' +
'\n' +
'Run these commands:\n' +
'1. cd ' + repoRoot + '\n' +
'2. git status (see what changed)\n' +
'3. git add -A (stage everything)\n' +
'4. git status (confirm staged)\n' +
'5. git commit -m "Sprint 0.2: agent CLI binary, registration/heartbeat, endpoint list, audit log, setup guide" -m "- Go agent CLI binary with NATS mTLS, heartbeat, check executor, script executor" -m "- Agent registration API + heartbeat handler server-side" -m "- Real-time endpoint list page with WebSocket updates" -m "- Audit log infrastructure with hash chain integrity" -m "- 5-minute setup guide and Docker compose polish" -m "Closes #8, #9, #10, #12, #13"\n' +
'6. git push origin main\n' +
'7. Close issues: for i in 8 9 10 12 13; do gh issue close $i -r completed; done\n' +
'\n' +
'If commit signing fails, retry with: git -c commit.gpgsign=false commit ...\n' +
'Report the commit SHA and confirmation.',
{ label: 'commit-push', phase: 'Commit', model: 'haiku' }
)

return {
  status: 'Sprint 0.2 complete',
  phases: { agentBinary: agentBinary, registration: registration, endpoints: endpoints, audit: audit, setup: setup, commit: commit },
}
