export const meta = {
  name: 'sprint-11-impl',
  description: 'Implement Sprint 1.1: Check CRUD API, check library, check executor, ingest pipeline, checks dashboard',
  phases: [
    { title: 'CRUD', detail: 'Check CRUD API with all 10 check types' },
    { title: 'Library', detail: 'Wire built-in check library end-to-end' },
    { title: 'Executor', detail: 'Enhance agent check executor' },
    { title: 'Ingest', detail: 'Check result ingest pipeline with threshold evaluation' },
    { title: 'Dashboard', detail: 'Checks dashboard with live status' },
    { title: 'Commit', detail: 'Stage, commit, push, close Sprint 1.1 issues' },
  ],
}

log('Sprint 1.1 implementation starting -- Checks')

var repoRoot = '/mnt/data/git/openagentplatform'

// Phase 1: Check CRUD API (sonnet -- API design + DB)
phase('CRUD')
var crud = await agent(
'Implement the Check CRUD API at ' + repoRoot + '.\n' +
'Read existing internal/api/routes.go, internal/api/agent_store.go, pkg/models/models.go first.\n' +
'All 10 check types must be supported: ping, http, tcp, dns, cpu, memory, disk, service, script, custom.\n' +
'\n' +
'**1. Create internal/api/checks.go** -- Check CRUD handlers:\n' +
'- POST /api/v1/checks -- create check definition\n' +
'  - Body: name, description, check_type (enum), config (JSONB -- varies by type), interval_seconds, timeout_seconds, enabled\n' +
'  - Validate config against check_type schema:\n' +
'    * ping: {host, count=3, timeout_ms=3000}\n' +
'    * http: {url, method=GET, expected_status=200, expected_body?, timeout_ms=5000, follow_redirects=true}\n' +
'    * tcp: {host, port, timeout_ms=5000}\n' +
'    * dns: {hostname, expected_ips?, nameserver?}\n' +
'    * cpu: {threshold_percent=90, duration_seconds=60}\n' +
'    * memory: {threshold_percent=90}\n' +
'    * disk: {path=/, threshold_percent=90}\n' +
'    * service: {service_name, expected_state=running}\n' +
'    * script: {runtime, script_body, timeout_seconds=30}\n' +
'    * custom: {command, args[], timeout_seconds=30}\n' +
'  - Store in check_definitions table, return created check with ID\n' +
'- GET /api/v1/checks -- list checks (paginated, filter by type, enabled, search by name)\n' +
'- GET /api/v1/checks/{id} -- single check with assignment count\n' +
'- PUT /api/v1/checks/{id} -- update check (name, config, interval, timeout, enabled)\n' +
'- DELETE /api/v1/checks/{id} -- soft-delete if no active assignments; hard error with count if assigned\n' +
'- POST /api/v1/checks/{id}/run-now -- queue check for immediate execution on all assigned agents\n' +
'\n' +
'**2. Create internal/api/check_assignments.go** -- Assignment management:\n' +
'- POST /api/v1/checks/{id}/assign -- assign check to agent(s) by agent_id or site_id (batch)\n' +
'- DELETE /api/v1/checks/{id}/assign/{agent_id} -- remove assignment\n' +
'- GET /api/v1/checks/{id}/assignments -- list assigned agents with last result\n' +
'- POST /api/v1/checks/assign-bulk -- assign a check to multiple agents/sites at once\n' +
'\n' +
'**3. Create internal/api/check_store.go** -- PostgreSQL queries:\n' +
'- InsertCheck, GetCheck, ListChecks (with filters + pagination), UpdateCheck, DeleteCheck\n' +
'- AssignCheck, RemoveAssignment, ListAssignments, GetAssignmentsForAgent\n' +
'- Validate check_type belongs to allowed list\n' +
'\n' +
'**4. Update internal/api/routes.go** -- register all check routes under /api/v1/checks\n' +
'\n' +
'**5. Update pkg/models/models.go** -- ensure CheckDefinition, CheckAssignment match DB schema from migration 0003\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created and any errors.',
{ label: 'check-crud', phase: 'CRUD', model: 'sonnet' }
)

// Phase 2: Built-in check library end-to-end (sonnet -- wiring)
phase('Library')
var library = await agent(
'Wire the built-in check library end-to-end at ' + repoRoot + '.\n' +
'The agent check runners already exist in pkg/agent/checkers/.\n' +
'The server-side check dispatcher already exists in internal/events/checkdispatcher.go.\n' +
'Now connect them:\n' +
'\n' +
'**1. Create internal/checklib/library.go** -- server-side check library catalog:\n' +
'- CheckTemplate struct: name, check_type, description, category (connectivity, performance, availability, custom)\n' +
'- BuiltInChecks() returns []CheckTemplate with all 5 built-in types:\n' +
'  * Ping Check: ICMP echo to host, default 3 pings, 3s timeout\n' +
'  * CPU Usage: warn at 80%, critical at 90%\n' +
'  * Memory Usage: warn at 80%, critical at 90%\n' +
'  * Disk Usage: warn at 80%, critical at 90%, check /\n' +
'  * Service Status: check if systemd service is active\n' +
'- Add GET /api/v1/checks/library endpoint returning the catalog\n' +
'- Add POST /api/v1/checks/library/{template_id}/create to instantiate a template\n' +
'\n' +
'**2. Create internal/checklib/seeder.go** -- seed built-in checks on first server start\n' +
'\n' +
'**3. Wire dispatch** -- enhance internal/events/checkdispatcher.go:\n' +
'- When check created/assigned, publish to oap.agents.<agent_id>.checks for each assigned agent\n' +
'- Verify payload format matches what the agent expects\n' +
'\n' +
'**4. Update cmd/server/main.go** -- call seeder after DB init\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back what was wired and any errors.',
{ label: 'check-library', phase: 'Library', model: 'sonnet' }
)

// Phase 3: Enhance agent check executor (sonnet -- agent side)
phase('Executor')
var executor = await agent(
'Enhance the agent check executor at ' + repoRoot + '.\n' +
'Read existing pkg/agent/checks.go, pkg/agent/checkers/registry.go, and cmd/agent/main.go first.\n' +
'\n' +
'**1. Enhance pkg/agent/checks.go**:\n' +
'- Verify payload match between server dispatcher and agent handler\n' +
'- Add check interval tracking: skip if within interval\n' +
'- Add check timeout enforcement: context.WithTimeout\n' +
'- Add result batching: batch results for 5s before publishing\n' +
'- Add retry-on-failure: 3 retries with backoff for result publish\n' +
'- Add metrics: check_count, check_duration_ms, check_failure_count as expvar\n' +
'\n' +
'**2. Enhance pkg/agent/checkers/registry.go**:\n' +
'- Add checker timeout wrapping\n' +
'- Add CheckerMetadata: name, version, description, supported_platforms per checker\n' +
'\n' +
'**3. Enhance cmd/agent/main.go**:\n' +
'- Add --list-checkers flag: print available checkers and exit\n' +
'- Log registered checkers with metadata on startup\n' +
'\n' +
'**4. Enhance pkg/agent/checkers/service.go** -- cross-platform:\n' +
'- Detect init system (systemd, openrc, launchd, Windows Service)\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./cmd/agent/ && go vet ./...\n' +
'Test: ./bin/oap-agent -list-checkers\n' +
'Report back what was enhanced and any errors.',
{ label: 'check-executor', phase: 'Executor', model: 'sonnet' }
)

// Phase 4: Check result ingest pipeline (sonnet -- threshold evaluation)
phase('Ingest')
var ingest = await agent(
'Build the check result ingest pipeline with threshold evaluation at ' + repoRoot + '.\n' +
'Read existing internal/events/checkdispatcher.go and internal/api/agent_store.go first.\n' +
'\n' +
'**1. Create internal/checks/ingest.go** -- ResultIngestor:\n' +
'- Subscribe to oap.agents.*.results (queue group for load balancing)\n' +
'- Parse result: agent_id, check_id, check_type, status (ok/warn/crit/error), output, duration_ms, timestamp\n' +
'- Store in check_results table\n' +
'- Evaluate against alert thresholds:\n' +
'  * If status = crit: look back last N results, if N consecutive => trigger alert via NATS oap.events.alerts\n' +
'  * If status = warn: same with warn threshold\n' +
'- Emit NATS event oap.events.checks.result for WebSocket broadcast\n' +
'\n' +
'**2. Create internal/checks/threshold.go** -- ThresholdEvaluator:\n' +
'- Evaluate(result, checkDef, previousResults) -> alert_needed bool, severity\n' +
'- Configurable: consecutive_failures (default 3), lookback_window (default 5min)\n' +
'- Flapping detection: fire+clear within 2 intervals => suppress (mark as flapping)\n' +
'\n' +
'**3. Update internal/events/nats.go** -- add SubjectCheckResultPrefix, SubjectAlertEvents constants\n' +
'\n' +
'**4. Update cmd/server/main.go** -- start ResultIngestor, graceful shutdown\n' +
'\n' +
'**5. Update internal/api/agents.go** -- add GET /api/v1/agents/{id}/check-results\n' +
'Add GET /api/v1/check-results for cross-agent results (paginated, filterable)\n' +
'\n' +
'**6. Update internal/api/routes.go** -- register new routes\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created and any errors.',
{ label: 'ingest-pipeline', phase: 'Ingest', model: 'sonnet' }
)

// Phase 5: Checks dashboard (sonnet -- React)
phase('Dashboard')
var dashboard = await agent(
'Build the Checks dashboard with live status at ' + repoRoot + '.\n' +
'Read existing web/src/routes/, web/src/components/, web/src/lib/ first. DO NOT run pnpm/npm install.\n' +
'\n' +
'**1. Create web/src/routes/checks/index.tsx** -- Checks list page:\n' +
'- Filter tabs: All, OK, Warning, Critical, Disabled\n' +
'- Table: Status icon (green/orange/red/grey), Check Name, Type badge, Assigned Agents count, Last Run, Interval, Actions\n' +
'- Click row navigates to /checks/$checkId\n' +
'- Search by name, Create Check button (opens modal with form)\n' +
'- Modal form: name, type (dropdown), config fields (dynamic per type), interval, submit\n' +
'\n' +
'**2. Create web/src/routes/checks/$checkId.tsx** -- Check detail page:\n' +
'- Check info card: name, type, interval, config display, enabled toggle\n' +
'- Assigned agents table with last result status per agent\n' +
'- Result history chart (simple bar chart: green/orange/red by time buckets)\n' +
'- Recent results table (last 20)\n' +
'- Actions: Edit, Run Now, Delete, Assign Agent\n' +
'\n' +
'**3. Create web/src/lib/useChecks.ts** -- React hook:\n' +
'- Fetch checks list from GET /api/v1/checks\n' +
'- fetchCheck(id), createCheck(data), updateCheck(id, data), deleteCheck(id)\n' +
'- Subscribe to WebSocket channel checks for real-time result updates\n' +
'\n' +
'**4. Update web/src/components/sidebar.tsx**:\n' +
'- Ensure Checks nav item exists and links to /checks\n' +
'\n' +
'**5. Update web/src/routes/dashboard.tsx** -- add checks summary section:\n' +
'- Add a row of KPI cards: Total Checks, OK, Warning, Critical\n' +
'- Below the existing agent KPIs\n' +
'\n' +
'Report back files created/modified.',
{ label: 'checks-dashboard', phase: 'Dashboard', model: 'sonnet' }
)

// Phase 6: Commit, push, close issues (haiku -- mechanical)
phase('Commit')
var commit = await agent(
'Stage, commit, and push Sprint 1.1 implementation from ' + repoRoot + '.\n' +
'\n' +
'Run these commands:\n' +
'1. cd ' + repoRoot + '\n' +
'2. git status\n' +
'3. git add -A\n' +
'4. git status\n' +
'5. git commit -m "Sprint 1.1: Check CRUD API, built-in library, executor enhancements, ingest pipeline, checks dashboard" -m "- Check CRUD API with all 10 check types and per-type config validation" -m "- Built-in check library with 5 templates (ping, CPU, memory, disk, service)" -m "- Agent check executor with interval tracking, timeout, batching, retry" -m "- Check result ingest pipeline with threshold evaluation and flapping detection" -m "- Checks dashboard with live status, detail page, and check form" -m "Closes #11, #14, #15, #18, #20"\n' +
'6. git push origin main\n' +
'7. Close issues: for i in 11 14 15 18 20; do gh issue close $i -r completed; done\n' +
'\n' +
'If commit signing fails, retry with: git -c commit.gpgsign=false commit ...\n' +
'Report the commit SHA and confirmation.',
{ label: 'commit-push', phase: 'Commit', model: 'haiku' }
)

return {
  status: 'Sprint 1.1 complete',
  phases: { crud: crud, library: library, executor: executor, ingest: ingest, dashboard: dashboard, commit: commit },
}
