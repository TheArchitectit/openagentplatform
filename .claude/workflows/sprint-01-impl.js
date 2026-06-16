export const meta = {
  name: 'sprint-01-impl',
  description: 'Implement Sprint 0.1: monorepo scaffold, CI, schema, NATS, OIDC, OpenAPI, React shell',
  phases: [
    { title: 'Scaffold', detail: 'Create monorepo structure with Go, Python, TypeScript workspaces' },
    { title: 'CI', detail: 'GitHub Actions workflows for Go, Python, TypeScript' },
    { title: 'Schema', detail: 'PostgreSQL schema with 9 base migrations' },
    { title: 'NATS', detail: 'NATS config with mTLS and SPIFFE mappings' },
    { title: 'OIDC', detail: 'OIDC auth with Dex test IdP' },
    { title: 'OpenAPI', detail: 'OpenAPI 3.1 spec generation' },
    { title: 'React', detail: 'React shell with TanStack Router and Query' },
    { title: 'Commit', detail: 'Stage, commit, and push Sprint 0.1 deliverables' },
  ],
}

log('Sprint 0.1 implementation starting')

const repoRoot = '/mnt/data/git/openagentplatform'
const dir = (p) => `${repoRoot}/${p}`

// Phase 1: Scaffold monorepo (haiku — simple structure creation)
phase('Scaffold')
const scaffold = await agent(`Create the monorepo scaffold for OpenAgentPlatform at ${repoRoot}.

Use Bash (mkdir -p, touch) and Write tools to create this structure:

\`\`\`
${repoRoot}/
├── go.work
├── go.mod
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── api/
│   │   └── handler.go
│   ├── auth/
│   │   └── oidc.go
│   ├── config/
│   │   └── config.go
│   ├── db/
│   │   └── postgres.go
│   ├── events/
│   │   └── nats.go
│   ├── api/
│   │   └── routes.go
│   └── schema/
│       └── openapi.go
├── pkg/
│   ├── logger/
│   │   └── logger.go
│   └── models/
│       └── models.go
├── py/
│   ├── pyproject.toml
│   ├── alembic.ini
│   ├── oap/
│   │   ├── __init__.py
│   │   ├── settings.py
│   │   └── db.py
│   ├── alembic/
│   │   ├── env.py
│   │   ├── script.py.mako
│   │   └── versions/
│   │       └── 0001_init.py
│   └── tests/
│       └── test_smoke.py
├── web/
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   ├── index.html
│   ├── src/
│   │   ├── main.tsx
│   │   ├── app.tsx
│   │   ├── routes/
│   │   │   ├── __root.tsx
│   │   │   ├── index.tsx
│   │   │   ├── login.tsx
│   │   │   └── dashboard.tsx
│   │   ├── components/
│   │   │   ├── sidebar.tsx
│   │   │   └── header.tsx
│   │   ├── lib/
│   │   │   ├── api.ts
│   │   │   └── auth.ts
│   │   └── styles.css
│   └── public/
│       └── favicon.svg
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.dev.yml
│   ├── nats/
│   │   ├── nats.conf
│   │   └── certs/.gitkeep
│   ├── postgres/
│   │   └── init.sql
│   └── dex/
│       ├── config.yaml
│       └── static-users.yaml
├── .github/
│   └── workflows/
│       ├── go.yml
│       ├── python.yml
│       └── web.yml
├── Makefile
├── .gitignore
├── .env.example
├── README.md
├── LICENSE
└── docs/
    ├── SETUP.md
    ├── ARCHITECTURE.md
    ├── CONTRIBUTING.md
    └── API.md
\`\`\`

Key file contents (write each one carefully):

1. **go.work** — Go workspace with members ./cmd/server, ./internal/..., ./pkg/...
2. **go.mod** — module github.com/openagentplatform/openagentplatform, go 1.23, require chi/v5, nats.go, golang-jwt, log/slog
3. **cmd/server/main.go** — minimal main that loads config and starts HTTP server on :8080
4. **internal/api/handler.go** — chi router setup, health check, swagger route
5. **internal/api/routes.go** — route registration
6. **internal/auth/oidc.go** — OIDC verifier stub using coreos/go-oidc/v3
7. **internal/config/config.go** — env-based config struct with validation
8. **internal/db/postgres.go** — pgx connection pool
9. **internal/events/nats.go** — NATS client wrapper with mTLS
10. **internal/schema/openapi.go** — serve embedded OpenAPI spec
11. **pkg/logger/logger.go** — slog JSON logger
12. **pkg/models/models.go** — core models (User, Site, Agent, Check, Alert, Policy, Patch, Script, AuditEvent)

13. **py/pyproject.toml** — uv-managed project, requires fastapi, uvicorn, sqlalchemy, alembic, asyncpg, pydantic-settings, pyjwt
14. **py/alembic.ini** — standard alembic config
15. **py/oap/__init__.py** — version
16. **py/oap/settings.py** — pydantic-settings BaseSettings
17. **py/oap/db.py** — async SQLAlchemy engine + session factory
18. **py/alembic/env.py** — async env using oap.db.Base
19. **py/alembic/script.py.mako** — standard template
20. **py/alembic/versions/0001_init.py** — creates 9 base tables
21. **py/tests/test_smoke.py** — passes if importable

22. **web/package.json** — vite, react 19, @tanstack/react-router, @tanstack/react-query, tailwindcss, lucide-react
23. **web/tsconfig.json** — strict mode, bundler resolution
24. **web/vite.config.ts** — react plugin, @tanstack/router-plugin
25. **web/index.html** — root div
26. **web/src/main.tsx** — createRoot, BrowserRouter
27. **web/src/app.tsx** — QueryClientProvider, RouterProvider
28. **web/src/routes/__root.tsx** — root layout with Sidebar
29. **web/src/routes/index.tsx** — landing/redirect to /dashboard
30. **web/src/routes/login.tsx** — login button (initiates OIDC redirect)
31. **web/src/routes/dashboard.tsx** — placeholder with welcome
32. **web/src/components/sidebar.tsx** — nav with Dashboard/Agents/Checks/Alerts/Settings
33. **web/src/components/header.tsx** — top bar with user
34. **web/src/lib/api.ts** — fetch wrapper with credentials
35. **web/src/lib/auth.ts** — token storage helpers
36. **web/src/styles.css** — tailwind directives

37. **deploy/docker-compose.yml** — postgres, nats, dex, server, web with healthchecks
38. **deploy/docker-compose.dev.yml** — dev overrides with hot-reload
39. **deploy/nats/nats.conf** — mTLS, cluster, accounts
40. **deploy/postgres/init.sql** — CREATE EXTENSION pgcrypto, uuid-ossp, timescaledb
41. **deploy/dex/config.yaml** — local issuer, static connectors

42. **.github/workflows/go.yml** — go test, go vet, go build on push/PR
43. **.github/workflows/python.yml** — uv sync, pytest, ruff
44. **.github/workflows/web.yml** — pnpm install, vitest, build

45. **Makefile** — targets: up, down, test, lint, build, migrate, seed, fmt
46. **.gitignore** — node_modules, __pycache__, .env, dist, binaries, .venv
47. **.env.example** — all required env vars with placeholders
48. **README.md** — project intro, quick start, links to docs
49. **LICENSE** — Business Source License 1.1
50. **docs/SETUP.md** — 5-minute setup walkthrough
51. **docs/ARCHITECTURE.md** — high-level system architecture
52. **docs/CONTRIBUTING.md** — PR process, coding standards
53. **docs/API.md** — API overview (defer full spec to /docs/swagger)

Create EVERY file listed. Use the Write tool for each. After writing, run:
- \`ls -la ${repoRoot}\` to confirm structure
- \`cd ${repoRoot} && go mod tidy\` if go is available
- \`cd ${repoRoot} && go build ./...\` to verify Go compiles

Do NOT run pnpm install or npm install (slow, may not be in env). Just write the files.
Do NOT initialize git (already a git repo).

Report back with a final file count and any errors.`, {
  label: 'scaffold-monorepo',
  phase: 'Scaffold',
  model: 'sonnet',
})

// Phase 2: CI workflows (sonnet — config files matter)
phase('CI')
const ci = await agent(`Verify and improve the CI workflows at ${repoRoot}/.github/workflows/.

Read each existing file (go.yml, python.yml, web.yml) and:
1. Confirm they trigger on push to main and pull_request to main
2. Add caching (Go modules, uv cache, pnpm store)
3. Add concurrency cancellation
4. Add matrix testing on multiple OS or versions where appropriate
5. Add a "lint" job separate from "test" where reasonable

For go.yml: add golangci-lint job, setup-go with cache, test with -race -coverprofile
For python.yml: use astral-sh/setup-uv@v3, run ruff check + ruff format --check + pytest
For web.yml: use pnpm/action-setup, run tsc --noEmit + vitest run + vite build

Use Edit tool to modify existing files. Do NOT create new workflow files.

Also create a CODEOWNERS file at ${repoRoot}/.github/CODEOWNERS with:
- /cmd/ @openagentplatform/backend
- /internal/ @openagentplatform/backend
- /py/ @openagentplatform/backend
- /web/ @openagentplatform/frontend
- /deploy/ @openagentplatform/infra
- /docs/ @openagentplatform/docs
- /pkg/agent/ @openagentplatform/agent

After editing, run \`ls -la ${repoRoot}/.github/workflows/ ${repoRoot}/.github/CODEOWNERS\` to confirm.

Report back what changed.`, {
  label: 'ci-workflows',
  phase: 'CI',
  model: 'sonnet',
})

// Phase 3: PostgreSQL schema (sonnet — critical data layer)
phase('Schema')
const schema = await agent(`Build out the PostgreSQL schema migrations for OpenAgentPlatform.

The initial migration is at ${repoRoot}/py/alembic/versions/0001_init.py — read it first.

Create the 9 base tables per the spec in ${repoRoot}/docs/architecture/RMM_CORE.md and ${repoRoot}/docs/architecture/AUTH_AND_RBAC.md.

The 9 migrations should be split as separate files:
1. 0001_orgs_and_users.py — orgs, users, user_org_roles (RBAC), api_keys
2. 0002_clients_sites_agents.py — clients, sites, agents (with agent_state enum), agent_tags
3. 0003_checks.py — check_definitions (polymorphic via check_type + config JSONB), check_assignments, check_results (with timescaledb hypertable)
4. 0004_alerts.py — alert_rules, alert_state_machine, alert_notifications, notification_channels
5. 0005_policies.py — policy_definitions, policy_assignments, policy_violations
6. 0006_patches.py — patch_catalog, patch_jobs, patch_job_targets
7. 0007_scripts.py — script_definitions, script_runs
8. 0008_audit.py — audit_events (append-only, hash-chained)
9. 0009_indexes_and_views.py — all the composite indexes, materialized views for dashboard

Each migration:
- Use op.create_table / op.add_column / op.create_index
- Include downgrade() in reverse
- Use proper FKs with ON DELETE CASCADE where appropriate
- For check_results, use \`op.execute("SELECT create_hypertable(...)\")\` for timescaledb
- For audit_events, add prev_hash + hash columns for tamper-evidence

Delete the old 0001_init.py stub and create all 9 files fresh.

After writing, run \`cd ${repoRoot}/py && ls alembic/versions/\` to confirm 9 files exist.

Report back file list.`, {
  label: 'pg-schema',
  phase: 'Schema',
  model: 'sonnet',
})

// Phase 4: NATS config (haiku — config-only)
phase('NATS')
const nats = await agent(`Set up NATS server configuration with mTLS and SPIFFE mappings.

Read ${repoRoot}/deploy/nats/nats.conf first.

Update it to include:
- HTTPS port monitoring: 8222
- Cluster: name=openagentplatform, listen=0.0.0.0:6222
- Accounts: OAP, Agent, A2A
- mTLS: ca_file, cert_file, key_file, verify=true
- JetStream: store_dir=/data/jetstream, max_memory=2G
- Authorization mappings: account OAP → OIDC users, account Agent → SPIFFE IDs

Create:
- ${repoRoot}/deploy/nats/certs/leaf.conf — leaf node config for agents
- ${repoRoot}/deploy/nats/scripts/gen-certs.sh — openssl script to generate ca, server cert, agent certs
- ${repoRoot}/deploy/nats/scripts/spiffe-mappings.json — SPIFFE ID → account mapping

Also create a docker-compose healthcheck for the nats container that uses \`wget\` against :8222/healthz.

Use Write/Edit tools. Do NOT execute gen-certs.sh (it would write to the filesystem).

Report back files created/modified.`, {
  label: 'nats-mtls',
  phase: 'NATS',
  model: 'haiku',
})

// Phase 5: OIDC auth (sonnet — security-critical)
phase('OIDC')
const oidc = await agent(`Implement OIDC authentication with Dex test IdP.

Read ${repoRoot}/internal/auth/oidc.go first.

Update it to:
1. Use github.com/coreos/go-oidc/v3/oidc
2. Discover issuer from config (OIDC_ISSUER_URL)
3. Verify ID token from Authorization header (Bearer)
4. Extract claims (sub, email, groups, org)
5. Map OIDC groups to RBAC roles
6. Mint internal session JWT (EdDSA, 1h expiry)
7. Provide middleware for chi router

Add a new file ${repoRoot}/internal/auth/middleware.go with the chi middleware.

Update ${repoRoot}/internal/api/routes.go to:
- Add /auth/login (redirect to Dex)
- Add /auth/callback (handle OIDC code, set session cookie)
- Add /auth/logout
- Add /auth/me (returns current user)
- All other routes use the auth middleware

Update ${repoRoot}/deploy/dex/config.yaml to:
- Issuer: http://localhost:5556/dex
- Static connectors: local users (admin@oap.local/password), local users (tech@oap.local/password)
- Storage: in-memory
- Enable password DB

Update ${repoRoot}/deploy/dex/static-users.yaml with hashed passwords (use bcrypt format) for:
- admin@oap.local (admin)
- tech@oap.local (technician)

After writing, run \`cd ${repoRoot} && go build ./...\` to verify everything compiles.

Report back what changed and any compile errors.`, {
  label: 'oidc-auth',
  phase: 'OIDC',
  model: 'sonnet',
})

// Phase 6: OpenAPI 3.1 spec (sonnet — needs careful spec authoring)
phase('OpenAPI')
const openapi = await agent(`Generate an OpenAPI 3.1 specification for the OpenAgentPlatform API.

Create ${repoRoot}/api/openapi.yaml with full spec covering:

**Authentication:**
- GET /auth/login
- GET /auth/callback
- POST /auth/logout
- GET /auth/me

**Agents (Stream B):**
- GET /api/v1/agents
- GET /api/v1/agents/{id}
- POST /api/v1/agents/{id}/command (remote shell, script run)
- GET /api/v1/agents/{id}/check-results (recent check history)
- POST /api/v1/agents/register (admin only)

**Checks (Phase 1):**
- GET /api/v1/checks
- POST /api/v1/checks
- GET /api/v1/checks/{id}
- PUT /api/v1/checks/{id}
- DELETE /api/v1/checks/{id}
- POST /api/v1/checks/{id}/run-now
- GET /api/v1/check-results

**Alerts (Phase 1):**
- GET /api/v1/alerts
- POST /api/v1/alerts/{id}/acknowledge
- POST /api/v1/alerts/{id}/resolve
- POST /api/v1/alerts/{id}/snooze
- GET /api/v1/alert-rules
- POST /api/v1/alert-rules
- PUT /api/v1/alert-rules/{id}

**Policies (Phase 1):**
- GET /api/v1/policies
- POST /api/v1/policies
- GET /api/v1/policies/{id}
- PUT /api/v1/policies/{id}
- GET /api/v1/policies/{id}/violations

**Patches (Phase 1):**
- GET /api/v1/patches
- POST /api/v1/patches/jobs
- GET /api/v1/patches/jobs/{id}
- POST /api/v1/patches/jobs/{id}/approve
- POST /api/v1/patches/jobs/{id}/reject

**Scripts (Phase 1):**
- GET /api/v1/scripts
- POST /api/v1/scripts
- GET /api/v1/scripts/{id}
- POST /api/v1/scripts/{id}/run
- GET /api/v1/scripts/{id}/runs

**Audit (Phase 0):**
- GET /api/v1/audit/events
- GET /api/v1/audit/events/{id}

For each endpoint, document:
- Summary, description
- Tags
- Path/query/header parameters with schemas
- Request body schema (if applicable)
- All response codes (200, 201, 400, 401, 403, 404, 500)
- Security requirements (bearerAuth)

Use shared component schemas:
- ErrorResponse, PaginationParams, PaginatedResponse
- Agent, Check, CheckResult, Alert, AlertRule, Policy, Patch, Script, AuditEvent

Update ${repoRoot}/internal/schema/openapi.go to:
- Use //go:embed api/openapi.yaml (move the file to ${repoRoot}/internal/api/openapi.yaml or use go:embed relative path)
- Serve at GET /docs/swagger (raw yaml)
- Serve Swagger UI at GET /docs (HTML with CDN script tag)
- Add /docs/openapi.json (convert yaml to json via ghodss/yaml or sigs.k8s.io/yaml)

After writing, run \`cd ${repoRoot} && go build ./...\` to verify the embed works.

Report back file size and any errors.`, {
  label: 'openapi-spec',
  phase: 'OpenAPI',
  model: 'sonnet',
})

// Phase 7: React shell (sonnet — substantial UI work)
phase('React')
const react = await agent(`Build the React shell with TanStack Router and Query.

Read the existing files at ${repoRoot}/web/src/ first.

Update them to:

1. **web/package.json** — ensure deps: @tanstack/react-router, @tanstack/react-router-devtools, @tanstack/react-query, react 19, react-dom 19, vite 6, @vitejs/plugin-react, tailwindcss 3, postcss, autoprefixer, lucide-react, sonner (toast)

2. **web/vite.config.ts** — add @tanstack/router-plugin/vite, dev server proxy /api → http://localhost:8080

3. **web/src/main.tsx** — createRoot, QueryClientProvider, RouterProvider, Toaster, StrictMode

4. **web/src/app.tsx** — exports router instance, sets up context providers

5. **web/src/routes/__root.tsx** — Outlet, Sidebar on left, Header on top, Toaster

6. **web/src/routes/index.tsx** — redirects to /dashboard if authenticated, else /login

7. **web/src/routes/login.tsx** — Card with logo, "Sign in with OIDC" button that hits /auth/login (full window redirect)

8. **web/src/routes/dashboard.tsx** — 4 KPI cards (Total Agents, Online, Failing Checks, Open Alerts) using placeholder data; recent activity feed

9. **web/src/components/sidebar.tsx** — nav with: Dashboard (/dashboard), Agents (/agents), Checks (/checks), Alerts (/alerts), Policies (/policies), Patches (/patches), Scripts (/scripts), Settings (/settings). Logo at top, user at bottom.

10. **web/src/components/header.tsx** — search bar, notifications icon, user avatar with dropdown (profile, logout)

11. **web/src/lib/api.ts** — fetch wrapper:
    - baseURL from import.meta.env.VITE_API_URL
    - credentials: 'include'
    - Auto-redirect to /login on 401
    - JSON parse + error throwing

12. **web/src/lib/auth.ts** — getUser(), isAuthenticated(), logout() — calls /api/v1/auth/me and /auth/logout

13. **web/src/styles.css** — tailwind directives + base styles + CSS variables for theme

Add stub routes for /agents, /checks, /alerts, /policies, /patches, /scripts, /settings that render placeholder text "Coming in Sprint X.Y".

Use Lucide icons throughout. Make it look professional with proper spacing, hover states, dark mode (CSS variables).

After writing, run \`cd ${repoRoot}/web && ls -la src/routes/ src/components/ src/lib/\` to confirm structure.

DO NOT run pnpm install or npm install.

Report back files created/modified.`, {
  label: 'react-shell',
  phase: 'React',
  model: 'sonnet',
})

// Phase 8: Commit and push (haiku — mechanical)
phase('Commit')
const commit = await agent(`Stage, commit, and push the Sprint 0.1 implementation.

Run from ${repoRoot}:

1. \`cd ${repoRoot}\`
2. \`git status\` — see what's changed
3. \`git add -A\` — stage everything
4. \`git status\` — confirm staged files
5. \`git -c user.name='openagentplatform-bot' -c user.email='bot@openagentplatform.dev' commit -m "Sprint 0.1: monorepo scaffold, CI, schema, NATS, OIDC, OpenAPI, React shell\\n\\n- Go workspace with chi router, OIDC, NATS, pgx\\n- Python venv with FastAPI, SQLAlchemy, Alembic, 9 migrations\\n- TypeScript workspace with React 19, TanStack Router/Query, Tailwind\\n- Docker compose for postgres, nats, dex, server, web\\n- GitHub Actions CI for Go, Python, TypeScript\\n- BSL 1.1 license, README, CONTRIBUTING, docs\\n\\nCloses #1, #2, #3, #4, #5, #6, #7"\`
6. \`git push origin main\`
7. \`gh issue close 1 2 3 4 5 6 7\` — close the Sprint 0.1 issues

If the commit is rejected (e.g., signing required), retry with:
\`git -c commit.gpgsign=false -c user.name='openagentplatform-bot' -c user.email='bot@openagentplatform.dev' commit ...\`

Report back the commit SHA and confirmation that push succeeded and issues closed.`, {
  label: 'commit-push',
  phase: 'Commit',
  model: 'haiku',
})

return {
  status: 'Sprint 0.1 complete',
  phases: { scaffold, ci, schema, nats, oidc, openapi, react, commit },
}
