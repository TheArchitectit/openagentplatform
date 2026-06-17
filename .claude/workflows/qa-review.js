export const meta = {
  name: 'qa-review',
  description: 'Comprehensive QA review of all 7 sprints: code quality, security, integration, coverage, docs',
  phases: [
    { title: 'Structure', detail: 'Filesystem audit and architecture conformance' },
    { title: 'Review', detail: 'Parallel domain reviews: backend, agent, frontend, infra, security, docs' },
    { title: 'CrossCut', detail: 'Integration and cross-cutting concerns' },
    { title: 'Report', detail: 'Synthesize findings into prioritized report' },
  ],
}

log('Phase 1 QA review starting -- systematic audit of all 34 delivered issues')

var repoRoot = '/mnt/data/git/openagentplatform'

var FINDING_SCHEMA = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          severity: { type: 'string', enum: ['critical', 'high', 'medium', 'low', 'info'] },
          category: { type: 'string', enum: ['bug', 'security', 'perf', 'style', 'missing', 'integration', 'docs'] },
          file: { type: 'string' },
          line: { type: 'number' },
          title: { type: 'string' },
          description: { type: 'string' },
          recommendation: { type: 'string' },
        },
        required: ['severity', 'category', 'title', 'description', 'recommendation'],
      },
    },
  },
  required: ['findings'],
}

// Phase 1: Structural audit (haiku -- cheap)
phase('Structure')
var structure = await agent(
  'Audit the filesystem structure of the OpenAgentPlatform monorepo at ' + repoRoot + '.\n' +
  '\n' +
  'Run these commands and analyze the output:\n' +
  '1. find ' + repoRoot + ' -name "*.go" | head -60 (count Go files per package)\n' +
  '2. find ' + repoRoot + ' -name "*.tsx" -o -name "*.ts" | grep -v node_modules | head -30 (TypeScript files)\n' +
  '3. find ' + repoRoot + ' -name "*.py" | head -20 (Python files)\n' +
  '4. ls -la ' + repoRoot + '/cmd/ ' + repoRoot + '/internal/ ' + repoRoot + '/pkg/ ' + repoRoot + '/web/src/\n' +
  '5. du -sh ' + repoRoot + '/cmd ' + repoRoot + '/internal ' + repoRoot + '/pkg ' + repoRoot + '/web ' + repoRoot + '/py\n' +
  '6. Go files: find ' + repoRoot + ' -maxdepth 1 -name "*.go" -o -name "go.*" | head -5\n' +
  '7. Verify no leftover stub/test placeholder files that should have been replaced\n' +
  '8. Check for any .disabled files or temporary artifacts\n' +
  '9. Verify directory structure matches architecture docs\n' +
  '\n' +
  'Report: file counts by language, package organization health, any structural issues found.',
  { label: 'structure-audit', phase: 'Structure', model: 'haiku' }
)

// Phase 2: Parallel domain reviews
phase('Review')

var backendReview = agent(
  'Review the Go backend code for OpenAgentPlatform at ' + repoRoot + '/internal/.\n' +
  '\n' +
  'Examine these files systematically (read each one):\n' +
  '- internal/api/ -- all handlers, routes, stores (checks, alerts, policies, patches, scripts, agents, audit, notifications, remote)\n' +
  '- internal/auth/ -- OIDC, middleware\n' +
  '- internal/alerts/ -- engine, statemachine, store\n' +
  '- internal/policy/ -- opa, engine, violations, collectors/*\n' +
  '- internal/patches/ -- approval, deployer, scanner, scheduler, store\n' +
  '- internal/checks/ -- ingest, threshold\n' +
  '- internal/notify/ -- notifier, email, slack, webhook\n' +
  '- internal/remote/ -- shell, auth, recorder\n' +
  '- internal/audit/ -- audit, middleware\n' +
  '- internal/events/ -- nats, heartbeat, checkdispatcher\n' +
  '- internal/config/ -- config\n' +
  '- internal/db/ -- postgres\n' +
  '- internal/schema/ -- openapi\n' +
  '- pkg/models/ -- models\n' +
  '- pkg/logger/ -- logger\n' +
  '- cmd/server/main.go\n' +
  '\n' +
  'For each package, check:\n' +
  '1. Does every exported function have a doc comment?\n' +
  '2. Are errors properly wrapped with context (%w)?\n' +
  '3. Is there proper input validation on all API handlers?\n' +
  '4. Are SQL queries using parameterized queries (no string concatenation)?\n' +
  '5. Are transactions used where needed (multi-table writes)?\n' +
  '6. Is there proper context propagation (no context.Background() in handlers)?\n' +
  '7. Are there any obvious nil pointer dereference risks?\n' +
  '8. Is error handling consistent (return err, not panic)?\n' +
  '9. Are there any import cycles?\n' +
  '10. Do route registrations use correct HTTP methods?',
  { label: 'review-backend', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

var agentReview = agent(
  'Review the Go agent code for OpenAgentPlatform at ' + repoRoot + '/pkg/agent/ and ' + repoRoot + '/cmd/agent/.\n' +
  '\n' +
  'Examine these files systematically (read each one):\n' +
  '- cmd/agent/main.go -- entry point, flag parsing, signal handling\n' +
  '- pkg/agent/config.go -- config loading\n' +
  '- pkg/agent/register.go -- registration\n' +
  '- pkg/agent/heartbeat.go -- heartbeat\n' +
  '- pkg/agent/checks.go -- check executor\n' +
  '- pkg/agent/scripts.go -- script executor\n' +
  '- pkg/agent/compliance.go -- compliance handler\n' +
  '- pkg/agent/hostinfo.go -- host info\n' +
  '- pkg/agent/nats.go -- NATS wrapper\n' +
  '- pkg/agent/checkers/*.go -- all 9 checkers + registry\n' +
  '- pkg/agent/executor/*.go -- script executors + runtimes\n' +
  '- pkg/agent/patcher/*.go -- patch scanner, installer, handler\n' +
  '- pkg/agent/shell/*.go -- shell handler\n' +
  '\n' +
  'For each file, check:\n' +
  '1. Are all NATS subjects consistently formatted?\n' +
  '2. Is graceful shutdown properly handled (goroutine cleanup, context cancellation)?\n' +
  '3. Are there any goroutine leaks (missing context.Done() checks, unbounded channel sends)?\n' +
  '4. Is error handling consistent across all checkers/executors?\n' +
  '5. Are timeouts enforced on all external operations?\n' +
  '6. Is temp file cleanup guaranteed (defer or signal handler)?\n' +
  '7. Are platform-specific build tags correctly applied?\n' +
  '8. Is the config file permission-safe (0600 for tokens)?\n' +
  '9. Are metric/expvar counters properly incremented?\n' +
  '10. Does the agent handle NATS disconnection/reconnection gracefully?',
  { label: 'review-agent', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

var frontendReview = agent(
  'Review the React frontend code for OpenAgentPlatform at ' + repoRoot + '/web/src/.\n' +
  '\n' +
  'Examine these files systematically (read each one):\n' +
  '- web/src/main.tsx, app.tsx, styles.css\n' +
  '- web/src/routes/ -- all route files (__root, index, login, dashboard, agents/*, checks/*, alerts/*, policies/*, patches/*, scripts/*, shell-recordings/*)\n' +
  '- web/src/components/ -- sidebar, header, severity-badge, policy-editor\n' +
  '- web/src/lib/ -- api, auth, websocket, useAgents, useChecks, useAlerts, usePolicies, usePatches, useScripts\n' +
  '- web/package.json, vite.config.ts, tsconfig.json, index.html\n' +
  '\n' +
  'For each file, check:\n' +
  '1. Are API calls using the fetch wrapper from lib/api.ts (not raw fetch)?\n' +
  '2. Are WebSocket subscriptions properly cleaned up on unmount?\n' +
  '3. Are loading and error states handled for every data fetch?\n' +
  '4. Are there any missing TypeScript types (any usage)?\n' +
  '5. Do route components handle the case where data is null/undefined?\n' +
  '6. Are all nav links in sidebar consistent with actual routes?\n' +
  '7. Are event handlers properly memoized (useCallback where appropriate)?\n' +
  '8. Is there any use of dangerouslySetInnerHTML that needs review?\n' +
  '9. Are form submissions properly disabled during loading?\n' +
  '10. Does the route tree match the actual file-based routes?',
  { label: 'review-frontend', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

var infraReview = agent(
  'Review the infrastructure and deployment code at ' + repoRoot + '/deploy/ and ' + repoRoot + '/.github/.\n' +
  '\n' +
  'Examine:\n' +
  '- deploy/docker-compose.yml, deploy/docker-compose.dev.yml\n' +
  '- deploy/nats/nats.conf, deploy/nats/certs/, deploy/nats/scripts/\n' +
  '- deploy/postgres/init.sql\n' +
  '- deploy/dex/config.yaml, deploy/dex/static-users.yaml\n' +
  '- .github/workflows/go.yml, python.yml, web.yml\n' +
  '- .github/CODEOWNERS\n' +
  '- Makefile\n' +
  '- .env.example\n' +
  '- .gitignore\n' +
  '\n' +
  'For each file, check:\n' +
  '1. Are Docker images pinned to specific versions (not :latest)?\n' +
  '2. Are healthchecks configured on all services in docker-compose?\n' +
  '3. Are secrets properly handled (not hardcoded, using env vars or secrets)?\n' +
  '4. Are resource limits set on containers?\n' +
  '5. Is the NATS config using proper TLS settings?\n' +
  '6. Are CI workflows using caching effectively?\n' +
  '7. Does the Makefile have proper .PHONY declarations?\n' +
  '8. Are there any credential-like values in plaintext config files?\n' +
  '9. Is the .gitignore comprehensive (no binaries, .env, node_modules, __pycache__)?\n' +
  '10. Do the Dex static passwords use bcrypt (not plaintext)?',
  { label: 'review-infra', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

var securityReview = agent(
  'Perform a security review of the OpenAgentPlatform codebase at ' + repoRoot + '.\n' +
  '\n' +
  'Search for common security issues (use grep/rg):\n' +
  '1. Hardcoded secrets: grep for private keys, tokens, passwords (look for patterns like "sk-", "Bearer", "password", "secret", "token =", "key =" in .go, .ts, .tsx files)\n' +
  '2. SQL injection: grep for fmt.Sprintf with SQL, string concatenation in queries, query building without parameterization\n' +
  '3. TLS verification: grep for InsecureSkipVerify, tls.Config with insecure settings\n' +
  '4. Path traversal: grep for filepath.Join with user input, os.Open with unsanitized paths\n' +
  '5. Command injection: grep for exec.Command with user-controlled input, os/exec without proper escaping\n' +
  '6. Auth bypass: grep for auth middleware checks, are any routes accidentally unauthenticated?\n' +
  '7. RBAC bypass: check if all mutation endpoints check org ownership (claims.OrgID)\n' +
  '8. Rate limiting: is there any rate limiting on login, API endpoints?\n' +
  '9. CSRF: are state-changing endpoints protected?\n' +
  '10. Token handling: are JWTs validated correctly (exp, nbf, iss, aud claims)?\n' +
  '\n' +
  'For each finding, provide the exact file, line, and a clear recommendation.',
  { label: 'review-security', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

var docsReview = agent(
  'Review the documentation quality at ' + repoRoot + '/docs/ and ' + repoRoot + '/README.md.\n' +
  '\n' +
  'Read:\n' +
  '- README.md\n' +
  '- docs/SETUP.md\n' +
  '- docs/ARCHITECTURE.md\n' +
  '- docs/CONTRIBUTING.md\n' +
  '- docs/API.md\n' +
  '- docs/architecture/*.md (all docs)\n' +
  '\n' +
  'Check:\n' +
  '1. Does the README quick-start actually work (commands copy-paste runnable)?\n' +
  '2. Are documented ports, URLs, and credentials consistent with actual config?\n' +
  '3. Do architecture docs accurately describe the actual code structure?\n' +
  '4. Are there any references to features not yet implemented (Phase 2+)?\n' +
  '5. Is the API doc consistent with routes registered in internal/api/routes.go?\n' +
  '6. Are there any broken internal links between docs?\n' +
  '7. Is the CONTRIBUTING guide actionable?\n' +
  '8. Are external tool versions correct (Go, Node, Docker requirements)?\n' +
  '9. Does SETUP.md cover troubleshooting for common issues?\n' +
  '10. Is the license file present and correct (BSL 1.1)?',
  { label: 'review-docs', phase: 'Review', model: 'sonnet', schema: FINDING_SCHEMA }
)

// Phase 3: Cross-cutting integration review (haiku)
phase('CrossCut')
var crossCut = await agent(
  'Perform cross-cutting integration analysis at ' + repoRoot + '.\n' +
  '\n' +
  'Check these integration points:\n' +
  '1. Are NATS subjects consistent between server publishers and agent subscribers (and vice versa)?\n' +
  '  - Search for "oap.agents." in both internal/ and pkg/agent/ -- are subjects identical?\n' +
  '2. Are API routes in internal/api/routes.go consistent with:\n' +
  '  - Web frontend API calls in web/src/lib/*.ts?\n' +
  '  - Agent API calls in pkg/agent/register.go?\n' +
  '3. Is the model layer (pkg/models/models.go) consistent with:\n' +
  '  - DB schema in py/alembic/versions/*.py (column names, types)?\n' +
  '  - API request/response structs in handlers?\n' +
  '4. Does go.mod list all packages actually imported? Run: cd ' + repoRoot + ' && go mod tidy && go build ./...\n' +
  '5. Check for dead code: any exported functions, types, or files that are never referenced?\n' +
  '6. Are WebSocket channel names consistent between server (internal/api/websocket.go) and frontend (web/src/lib/websocket.ts)?\n' +
  '7. Do all 5 built-in check types (ping, cpu, memory, disk, service) exist in both the agent checkers AND the check library catalog?\n' +
  '8. Are all 10 check types from the API config validation also present in the agent checker registry?\n' +
  '\n' +
  'Use grep, find, and go build to verify. Report any mismatches with exact file:line references.',
  { label: 'cross-cut', phase: 'CrossCut', model: 'haiku' }
)

// Phase 4: Synthesis report (sonnet)
phase('Report')
var report = await agent(
  'Synthesize all QA findings into a prioritized report. Write the report to ' + repoRoot + '/docs/QA_REVIEW.md.\n' +
  '\n' +
  'Input findings:\n' +
  'Backend review: ' + JSON.stringify(backendReview) + '\n' +
  'Agent review: ' + JSON.stringify(agentReview) + '\n' +
  'Frontend review: ' + JSON.stringify(frontendReview) + '\n' +
  'Infra review: ' + JSON.stringify(infraReview) + '\n' +
  'Security review: ' + JSON.stringify(securityReview) + '\n' +
  'Docs review: ' + JSON.stringify(docsReview) + '\n' +
  'Structure audit: ' + JSON.stringify(structure) + '\n' +
  'Cross-cut: ' + JSON.stringify(crossCut) + '\n' +
  '\n' +
  'Write a report to ' + repoRoot + '/docs/QA_REVIEW.md with this structure:\n' +
  '\n' +
  '# QA Review Report\n' +
  '> Date: 2026-06-17 | Sprints: 0.1 - 1.5 | Issues: 34\n' +
  '\n' +
  '## Executive Summary\n' +
  '- Total findings by severity (critical/high/medium/low/info)\n' +
  '- Overall health assessment (green/yellow/red)\n' +
  '- Top 3 highest-priority issues\n' +
  '\n' +
  '## Findings by Domain\n' +
  '### Backend (Go API)\n' +
  '### Agent (CLI Binary)\n' +
  '### Frontend (React)\n' +
  '### Infrastructure\n' +
  '### Security\n' +
  '### Documentation\n' +
  '### Cross-Cutting / Integration\n' +
  '\n' +
  'Each section: table of findings with severity, category, file, description, recommendation\n' +
  '\n' +
  '## Metrics\n' +
  '- File counts: Go X, TypeScript Y, Python Z, Config W\n' +
  '- Lines of code estimate\n' +
  '- Build status: go build/vet, CI config status\n' +
  '- Test coverage: any tests present?\n' +
  '\n' +
  '## Recommendations\n' +
  '- Must-fix before Phase 2 (critical + high)\n' +
  '- Should-fix (medium)\n' +
  '- Nice-to-have (low + info)\n' +
  '\n' +
  '## Sign-off\n' +
  '- [ ] All critical/high findings addressed\n' +
  '- [ ] Build passes cleanly\n' +
  '- [ ] Ready for Phase 2 (A2A + Agents)\n' +
  '\n' +
  'Use the Write tool to create the file. No placeholder text -- include every finding from every domain.\n' +
  'Report back the file path and summary statistics.',
  { label: 'synthesis-report', phase: 'Report', model: 'sonnet' }
)

return {
  status: 'QA review complete',
  phases: { structure: structure, findings: [backendReview, agentReview, frontendReview, infraReview, securityReview, docsReview], crossCut: crossCut, report: report },
}
