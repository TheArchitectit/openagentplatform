# Frontend Architecture

## Overview

The OpenAgentPlatform frontend is a React 19 single-page application (SPA) that replaces the existing vanilla JavaScript implementation. The legacy frontend consisted of hand-rolled DOM manipulation, ad-hoc jQuery patterns, and tightly coupled HTTP request handling. The React 19 SPA introduces a declarative component model, type-safe routing, server-state caching, and real-time data synchronization via WebSockets.

**Why this stack:**

| Requirement | Legacy Limitation | React 19 SPA Solution |
|---|---|---|
| Component reusability | Copy-paste HTML/JS fragments | Composable React components with props/children |
| Type safety | Implicit any, runtime errors | TypeScript strict mode, Zod runtime validation |
| Server state caching | Manual fetch + re-render on every page | TanStack Query v5 stale-while-revalidate |
| Routing | Hash-based anchors, full page reloads | TanStack Router file-based, type-safe params |
| Real-time updates | Polling intervals, manual DOM patching | WebSocket subscriptions with automatic cache invalidation |
| UI consistency | Inconsistent inline styles | Tailwind CSS + Shadcn/ui design system |
| Terminal/Desktop access | None or iframe hacks | xterm.js and noVNC as first-class React components |
| Code editing | `<textarea>` or external links | Monaco Editor with language services |

The SPA communicates exclusively with the Go backend REST API (`/api/v1/*`) and the WebSocket gateway (`/ws`). No direct database access occurs from the browser.

---

## Tech Stack

| Layer | Technology | Version | Purpose |
|---|---|---|---|
| Build tool | Vite | 6.x | Dev server, HMR, production bundling |
| UI library | React | 19.x | Component model, concurrent rendering |
| Language | TypeScript | 5.6.x | Static typing, IDE support |
| Router | TanStack Router | 1.x | File-based, type-safe, code-split routes |
| Data fetching | TanStack Query v5 | 5.x | Server state cache, mutations, invalidation |
| Styling | Tailwind CSS | 4.x | Utility-first CSS |
| Component primitives | Shadcn/ui | latest | Accessible, copy-paste component library |
| Terminal | xterm.js | 5.5.x | WebSocket-backed terminal emulator |
| Remote desktop | noVNC | 1.5.x | VNC/RFB client for agent desktop sessions |
| Code editor | Monaco Editor | 0.52.x | VS Code's editor as a React component |
| Client state | Zustand | 5.x | Lightweight global state for UI concerns |
| Forms | React Hook Form | 7.x | Performant form state management |
| Schema validation | Zod | 3.x | Runtime + compile-time type inference |
| HTTP client | Native fetch | — | Wrapped in a typed client module |
| Icons | Lucide React | latest | Consistent icon set |
| Date utilities | date-fns | 4.x | Date formatting, parsing, manipulation |
| Testing | Vitest + Testing Library | latest | Unit and component testing |
| Linting | ESLint + Prettier | latest | Code quality enforcement |

All versions are pinned in `package.json` and locked via `pnpm-lock.yaml` (or `bun.lockb`). Renovate bot opens weekly PRs for minor/patch updates.

---

## Project Structure

```
src/
├── main.tsx                      # Entry point, root render
├── app.tsx                       # App shell with providers
├── routeTree.gen.ts              # Auto-generated route tree (TanStack Router)
├── env.ts                        # Zod-validated environment variables
├── assets/                       # Static assets (images, fonts)
│   └── logo.svg
├── components/
│   ├── ui/                       # Shadcn/ui primitives (30+ components)
│   │   ├── button.tsx
│   │   ├── card.tsx
│   │   ├── dialog.tsx
│   │   ├── dropdown-menu.tsx
│   │   ├── input.tsx
│   │   ├── table.tsx
│   │   ├── tabs.tsx
│   │   ├── toast.tsx
│   │   ├── tooltip.tsx
│   │   └── ...                   # 30+ total
│   ├── layout/                   # App shell components
│   │   ├── app-shell.tsx         # Sidebar + topbar + content
│   │   ├── sidebar.tsx           # Collapsible navigation
│   │   ├── topbar.tsx            # Org switcher, user menu, notifications
│   │   ├── breadcrumb.tsx
│   │   └── footer.tsx
│   ├── charts/                   # Visualization components
│   │   ├── line-chart.tsx
│   │   ├── gauge.tsx
│   │   ├── sparkline.tsx
│   │   ├── status-badge.tsx
│   │   ├── bar-chart.tsx
│   │   └── heatmap.tsx
│   ├── terminal/
│   │   └── xterm-terminal.tsx    # xterm.js wrapper
│   ├── vnc/
│   │   └── novnc-viewer.tsx      # noVNC wrapper
│   ├── editor/
│   │   └── monaco-editor.tsx     # Monaco wrapper
│   ├── shared/                   # Cross-feature shared components
│   │   ├── data-table.tsx        # Generic sortable/filterable table
│   │   ├── empty-state.tsx
│   │   ├── error-boundary.tsx
│   │   ├── loading-skeleton.tsx
│   │   ├── confirm-dialog.tsx
│   │   ├── permission-gate.tsx   # RBAC-conditional render
│   │   └── page-header.tsx
│   └── feedback/
│       ├── toast-provider.tsx
│       └── alert-banner.tsx
├── hooks/                        # Custom React hooks
│   ├── use-auth.ts               # Current user, login, logout
│   ├── use-permissions.ts        # RBAC permission checks
│   ├── use-websocket.ts          # WebSocket subscription with auto-reconnect
│   ├── use-debounce.ts
│   ├── use-media-query.ts
│   ├── use-local-storage.ts
│   ├── use-clipboard.ts
│   └── use-feature-flag.ts
├── lib/                          # Core utilities
│   ├── api-client.ts             # Fetch wrapper with interceptors
│   ├── ws-client.ts              # WebSocket client with heartbeat
│   ├── auth.ts                   # JWT decode, refresh logic
│   ├── query-client.ts           # TanStack Query config
│   ├── router.ts                 # TanStack Router config
│   ├── utils.ts                  # cn() class merge, formatters
│   ├── formatters.ts             # Date, number, byte formatters
│   └── constants.ts              # App-wide constants
├── stores/                       # Zustand stores (client state)
│   ├── ui-store.ts               # Sidebar collapsed, theme, modals
│   ├── draft-store.ts            # Unsaved form drafts
│   ├── notification-store.ts     # Toast queue
│   └── ws-store.ts               # WebSocket connection state
├── types/                        # Shared TypeScript types
│   ├── api.ts                    # Request/response DTOs
│   ├── domain.ts                 # Agent, Check, Alert, Patch, Policy, Secret
│   ├── auth.ts                   # User, Role, Permission
│   ├── ws.ts                     # WebSocket message types
│   └── env.d.ts
├── schemas/                      # Zod schemas
│   ├── agent.ts
│   ├── check.ts
│   ├── alert.ts
│   ├── patch.ts
│   ├── policy.ts
│   ├── secret.ts
│   ├── script.ts
│   └── auth.ts
├── features/                     # Feature modules (10 total)
│   ├── auth/
│   │   ├── routes/               # File-based routes
│   │   │   ├── login.tsx
│   │   │   ├── sso-callback.tsx
│   │   │   └── password-reset.tsx
│   │   ├── components/
│   │   │   ├── login-form.tsx
│   │   │   └── sso-redirect.tsx
│   │   ├── hooks/
│   │   │   └── use-login.ts
│   │   └── index.ts
│   ├── dashboard/
│   │   ├── routes/
│   │   │   └── index.tsx
│   │   ├── components/
│   │   │   ├── agent-status-grid.tsx
│   │   │   ├── alert-feed.tsx
│   │   │   ├── check-health-gauge.tsx
│   │   │   ├── patch-compliance-bar.tsx
│   │   │   └── live-metric-line.tsx
│   │   ├── hooks/
│   │   │   ├── use-dashboard-stats.ts
│   │   │   └── use-live-metrics.ts
│   │   └── index.ts
│   ├── agents/
│   │   ├── routes/
│   │   │   ├── index.tsx         # AgentList
│   │   │   └── $agentId.tsx      # AgentDetail
│   │   ├── components/
│   │   │   ├── agent-list.tsx
│   │   │   ├── agent-detail.tsx
│   │   │   ├── agent-overview-tab.tsx
│   │   │   ├── agent-checks-tab.tsx
│   │   │   ├── agent-alerts-tab.tsx
│   │   │   ├── agent-patches-tab.tsx
│   │   │   ├── agent-sessions-tab.tsx
│   │   │   ├── agent-logs-tab.tsx
│   │   │   └── agent-actions-menu.tsx
│   │   ├── hooks/
│   │   │   ├── use-agents.ts
│   │   │   └── use-agent.ts
│   │   └── index.ts
│   ├── monitoring/
│   │   ├── routes/
│   │   │   ├── checks/
│   │   │   │   ├── index.tsx
│   │   │   │   └── $checkId.tsx
│   │   │   └── alerts/
│   │   │       ├── index.tsx
│   │   │       └── $alertId.tsx
│   │   ├── components/
│   │   │   ├── check-list.tsx
│   │   │   ├── check-detail.tsx
│   │   │   ├── check-time-series.tsx
│   │   │   ├── check-run-button.tsx
│   │   │   ├── alert-list.tsx
│   │   │   ├── alert-detail.tsx
│   │   │   └── alert-acknowledge-dialog.tsx
│   │   ├── hooks/
│   │   │   ├── use-checks.ts
│   │   │   ├── use-check.ts
│   │   │   ├── use-alerts.ts
│   │   │   └── use-alert.ts
│   │   └── index.ts
│   ├── patches/
│   │   ├── routes/
│   │   │   ├── compliance.tsx
│   │   │   ├── patches.tsx
│   │   │   ├── policies.tsx
│   │   │   └── deployments.tsx
│   │   ├── components/
│   │   │   ├── compliance-overview.tsx
│   │   │   ├── patch-list.tsx
│   │   │   ├── patch-policy-list.tsx
│   │   │   ├── policy-editor.tsx
│   │   │   ├── deployment-list.tsx
│   │   │   └── deployment-progress.tsx
│   │   ├── hooks/
│   │   │   ├── use-compliance.ts
│   │   │   ├── use-patches.ts
│   │   │   ├── use-policies.ts
│   │   │   └── use-deployments.ts
│   │   └── index.ts
│   ├── remote-access/
│   │   ├── routes/
│   │   │   ├── terminal.tsx
│   │   │   ├── desktop.tsx
│   │   │   └── sessions.tsx
│   │   ├── components/
│   │   │   ├── terminal-view.tsx
│   │   │   ├── desktop-view.tsx
│   │   │   ├── session-list.tsx
│   │   │   └── session-recording-player.tsx
│   │   ├── hooks/
│   │   │   ├── use-terminal-session.ts
│   │   │   ├── use-desktop-session.ts
│   │   │   └── use-sessions.ts
│   │   └── index.ts
│   ├── scripts/
│   │   ├── routes/
│   │   │   ├── index.tsx
│   │   │   └── $scriptId.tsx
│   │   ├── components/
│   │   │   ├── script-editor.tsx
│   │   │   ├── run-form.tsx
│   │   │   ├── live-output-console.tsx
│   │   │   ├── script-list.tsx
│   │   │   └── target-selector.tsx
│   │   ├── hooks/
│   │   │   ├── use-scripts.ts
│   │   │   ├── use-run-script.ts
│   │   │   └── use-run-status.ts
│   │   └── index.ts
│   ├── a2a/
│   │   ├── routes/
│   │   │   ├── index.tsx
│   │   │   ├── agents.tsx
│   │   │   ├── tasks.tsx
│   │   │   └── messages.tsx
│   │   ├── components/
│   │   │   ├── agent-card.tsx
│   │   │   ├── task-lifecycle.tsx
│   │   │   ├── message-thread.tsx
│   │   │   ├── artifact-viewer.tsx
│   │   │   └── a2a-stats-panel.tsx
│   │   ├── hooks/
│   │   │   ├── use-a2a-agents.ts
│   │   │   ├── use-a2a-tasks.ts
│   │   │   └── use-a2a-messages.ts
│   │   └── index.ts
│   ├── policies/
│   │   ├── routes/
│   │   │   ├── index.tsx
│   │   │   └── $policyId.tsx
│   │   ├── components/
│   │   │   ├── policy-editor.tsx
│   │   │   ├── policy-list.tsx
│   │   │   ├── validation-panel.tsx
│   │   │   └── policy-diff-view.tsx
│   │   ├── hooks/
│   │   │   ├── use-policies.ts
│   │   │   └── use-validate-policy.ts
│   │   └── index.ts
│   ├── secrets/
│   │   ├── routes/
│   │   │   ├── index.tsx
│   │   │   └── $secretId.tsx
│   │   ├── components/
│   │   │   ├── secret-list.tsx
│   │   │   ├── secret-detail.tsx
│   │   │   ├── secret-create-dialog.tsx
│   │   │   ├── access-log.tsx
│   │   │   └── secret-value-reveal.tsx
│   │   ├── hooks/
│   │   │   ├── use-secrets.ts
│   │   │   ├── use-secret.ts
│   │   │   └── use-secret-access-log.ts
│   │   └── index.ts
│   └── settings/
│       ├── routes/
│       │   ├── users.tsx
│       │   ├── roles.tsx
│       │   ├── sso.tsx
│       │   ├── notifications.tsx
│       │   ├── org.tsx
│       │   └── api-keys.tsx
│       ├── components/
│       │   ├── user-list.tsx
│       │   ├── role-editor.tsx
│       │   ├── sso-config-form.tsx
│       │   ├── notification-channels.tsx
│       │   ├── org-settings-form.tsx
│       │   └── api-key-list.tsx
│       ├── hooks/
│       │   ├── use-users.ts
│       │   ├── use-roles.ts
│       │   └── use-api-keys.ts
│       └── index.ts
├── styles/
│   ├── globals.css               # Tailwind base + custom properties
│   └── themes.css                # Light/dark theme variables
├── test/
│   ├── setup.ts                  # Vitest setup
│   ├── mocks/
│   │   ├── server.ts             # MSW handlers
│   │   └── data.ts
│   └── utils.tsx                 # Test render helpers
└── env.d.ts                      # Vite env types
```

The `routeTree.gen.ts` file is auto-generated by the TanStack Router Vite plugin. It is committed to the repo and regenerated on every route file change.

---

## State Management

The frontend uses a four-tier state management strategy, each tier serving a distinct concern.

### 1. Server State (TanStack Query v5)

All data fetched from the Go backend is managed by TanStack Query. The query client is configured with the following defaults:

```typescript
// src/lib/query-client.ts
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,          // 30 seconds
      gcTime: 5 * 60_000,         // 5 minutes
      retry: 3,
      retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 30_000),
      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
    },
    mutations: {
      retry: 0,
    },
  },
});
```

**Query key conventions** follow a hierarchical structure:

| Resource | Query Key Pattern |
|---|---|
| Agents list | `['agents', { filters, sort, page }]` |
| Agent detail | `['agents', agentId]` |
| Checks | `['checks', { agentId, status }]` |
| Check detail | `['checks', checkId]` |
| Alerts | `['alerts', { status, severity }]` |
| Alert detail | `['alerts', alertId]` |
| Patches | `['patches', { status }]` |
| Policies | `['policies']` |
| Secrets (list) | `['secrets']` |
| Secret detail | `['secrets', secretId]` |
| Scripts | `['scripts']` |
| Dashboard stats | `['dashboard', 'stats']` |
| A2A tasks | `['a2a', 'tasks', { status }]` |
| Users | `['users']` |
| Roles | `['roles']` |

**Mutation invalidation patterns:**

| Mutation | Invalidates |
|---|---|
| Create agent | `['agents']` |
| Update agent | `['agents']`, `['agents', agentId]` |
| Acknowledge alert | `['alerts']`, `['alerts', alertId]`, `['dashboard', 'stats']` |
| Deploy patch | `['patches', { status }]`, `['deployments']`, `['agents']` |
| Update policy | `['policies']`, `['policies', policyId]` |
| Run script | `['scripts', scriptId, 'runs']` |

### 2. Client State (Zustand)

Client state covers UI concerns that are not server-derived:

| Store | State | Persistence |
|---|---|---|
| `ui-store` | Sidebar collapsed, active modal, theme, density | `localStorage` (sidebar, theme) |
| `draft-store` | Unsaved form drafts (auto-save every 5s) | `localStorage` with TTL |
| `notification-store` | Toast queue, severity | None (session only) |
| `ws-store` | WebSocket connection status, last heartbeat | None |

```typescript
// src/stores/ui-store.ts
interface UIState {
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  activeModal: string | null;
  openModal: (id: string) => void;
  closeModal: () => void;
  theme: 'light' | 'dark' | 'system';
  setTheme: (theme: 'light' | 'dark' | 'system') => void;
}
```

### 3. Form State (React Hook Form + Zod)

All forms use React Hook Form for performance (uncontrolled inputs, minimal re-renders) paired with Zod for validation. Schemas are defined in `src/schemas/` and shared between client and server validation.

```typescript
// Example: Create agent form
const createAgentSchema = z.object({
  name: z.string().min(1).max(64),
  hostname: z.string().min(1).max(255),
  os: z.enum(['linux', 'windows', 'macos']),
  tags: z.array(z.string()).default([]),
});

const { register, handleSubmit, formState: { errors } } = useForm({
  resolver: zodResolver(createAgentSchema),
});
```

### 4. Real-Time State (WebSocket Subscriptions)

The WebSocket client maintains a persistent connection to `/ws`. Subscriptions are declared at the component level using `useWebSocket`:

```typescript
// src/hooks/use-websocket.ts
function useWebSocket<T>(channel: string, onMessage: (data: T) => void) {
  useEffect(() => {
    const unsubscribe = wsClient.subscribe(channel, onMessage);
    return unsubscribe;
  }, [channel, onMessage]);
}
```

**Message routing:**

| WS Channel | Message Type | Triggers Cache Invalidation |
|---|---|---|
| `agent.status` | `{ agentId, status, lastSeen }` | `['agents']`, `['agents', agentId]`, `['dashboard', 'stats']` |
| `check.result` | `{ checkId, status, output }` | `['checks']`, `['checks', checkId]`, `['agents', agentId, 'checks']` |
| `alert.created` | `{ alertId, severity, ... }` | `['alerts']`, `['dashboard', 'stats']` |
| `alert.updated` | `{ alertId, status }` | `['alerts']`, `['alerts', alertId]` |
| `patch.progress` | `{ deploymentId, progress }` | `['deployments']`, `['deployments', deploymentId]` |
| `terminal.output` | `{ sessionId, data }` | Direct xterm.js write (not TanStack) |
| `script.run.status` | `{ runId, status, output }` | `['scripts', scriptId, 'runs']` |
| `a2a.task.update` | `{ taskId, status, ... }` | `['a2a', 'tasks']`, `['a2a', 'tasks', taskId]` |

The WebSocket client is a singleton (`ws-client.ts`) that handles reconnection with exponential backoff, heartbeat ping/pong every 30s, and automatic resubscription on reconnect.

---

## Feature Modules

### 1. Auth Module

**Routes:** `/login`, `/sso/callback`, `/password-reset`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Login | `/login` | `LoginForm`, `SSORedirect` | Email/password submit, SSO button redirect |
| SSO Callback | `/sso/callback` | `SSOCallbackHandler` | Extract code from URL, exchange for JWT |
| Password Reset | `/password-reset` | `PasswordResetForm` | Email submit, token validation, new password |

**Data dependencies:** `POST /api/v1/auth/login`, `POST /api/v1/auth/sso/callback`, `POST /api/v1/auth/password-reset`

**Flow:**

```
User navigates to /login
  -> LoginForm renders (email, password fields)
  -> User submits -> POST /api/v1/auth/login
  -> Response: { accessToken, refreshToken, user }
  -> Store tokens in httpOnly cookies (via backend Set-Cookie)
  -> Store user in TanStack Query cache
  -> Navigate to /
```

### 2. Dashboard Module

**Routes:** `/` (index)

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Dashboard | `/` | `AgentStatusGrid`, `AlertFeed`, `CheckHealthGauge`, `PatchComplianceBar`, `LiveMetricLine` | Real-time status, click-through to details |

**Component diagram:**

```
DashboardPage
├── AgentStatusGrid          # 4-column grid: Online / Degraded / Offline / Total
│   └── StatusBadge (x4)
├── AlertFeed                 # Scrolling list of recent alerts (last 10)
│   └── AlertCard (x10)
│       └── SeverityIcon
├── CheckHealthGauge          # Circular gauge: % checks passing
├── PatchComplianceBar        # Horizontal bar: % agents patched
└── LiveMetricLine            # Multi-line chart: CPU, Memory, Network (last 1h)
```

**Data dependencies:**
- `GET /api/v1/dashboard/stats` (agent counts, alert counts)
- `GET /api/v1/alerts?limit=10&sort=-createdAt`
- `GET /api/v1/dashboard/health`
- `GET /api/v1/dashboard/compliance`
- `GET /api/v1/metrics/agents?range=1h` (time-series)

**WebSocket subscriptions:** `agent.status`, `alert.created`, `check.result`

### 3. Agent Management Module

**Routes:** `/agents`, `/agents/$agentId`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Agent List | `/agents` | `AgentList`, `AgentFilters`, `AgentBulkActions` | Filter, sort, bulk select, export CSV |
| Agent Detail | `/agents/$agentId` | `AgentDetail` with 6 tabs | View/edit/delete, navigate tabs |

**Agent Detail tabs:**

| Tab | Component | Content |
|---|---|---|
| Overview | `AgentOverviewTab` | Hostname, OS, IP, tags, status, last seen, registration date |
| Checks | `AgentChecksTab` | List of checks assigned to this agent, pass/fail counts |
| Alerts | `AgentAlertsTab` | List of alerts from this agent, acknowledge/resolve |
| Patches | `AgentPatchesTab` | Patch compliance status, missing patches, reboot required |
| Sessions | `AgentSessionsTab` | Active and historical remote access sessions |
| Logs | `AgentLogsTab` | Scrolling log viewer with filter (level, source, time range) |

**Data dependencies:**
- `GET /api/v1/agents?page=1&limit=50&sort=name&filter[status]=online`
- `GET /api/v1/agents/:id`
- `GET /api/v1/agents/:id/checks`
- `GET /api/v1/agents/:id/alerts`
- `GET /api/v1/agents/:id/patches`
- `GET /api/v1/agents/:id/sessions`
- `GET /api/v1/agents/:id/logs?level=info&since=2024-01-01T00:00:00Z`

**WebSocket subscriptions:** `agent.status` (for live status badge updates)

### 4. Monitoring Module

**Routes:** `/monitoring/checks`, `/monitoring/checks/$checkId`, `/monitoring/alerts`, `/monitoring/alerts/$alertId`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Checks List | `/monitoring/checks` | `CheckList`, `CheckFilters`, `CheckCreateButton` | Filter by agent/type/status, create check |
| Check Detail | `/monitoring/checks/$checkId` | `CheckDetail`, `CheckTimeSeries`, `CheckRunButton` | View history, run now, edit, delete |
| Alerts List | `/monitoring/alerts` | `AlertList`, `AlertFilters`, `AlertBulkActions` | Filter, bulk acknowledge, export |
| Alert Detail | `/monitoring/alerts/$alertId` | `AlertDetail`, `CheckTimeSeries`, `AlertAcknowledgeDialog` | View context, acknowledge, resolve, add comment |

**Component diagram (Check Detail):**

```
CheckDetailPage
├── CheckHeader               # Name, type, agent, status, actions menu
│   └── CheckRunButton        # "Run Now" with loading state
├── CheckConfigPanel          # Current configuration (type, params, threshold)
│   └── JsonViewer
├── CheckTimeSeries           # Line chart of results over time
│   └── LineChart
│   └── ThresholdLine         # Dashed line at threshold
└── CheckRunHistory           # Table of recent runs
    └── DataTable
        └── StatusCell
```

**Data dependencies:**
- `GET /api/v1/checks?page=1&limit=50`
- `GET /api/v1/checks/:id`
- `GET /api/v1/checks/:id/results?range=24h`
- `POST /api/v1/checks/:id/run` (mutation)
- `GET /api/v1/alerts?page=1&limit=50&filter[status]=open`
- `GET /api/v1/alerts/:id`
- `POST /api/v1/alerts/:id/acknowledge`
- `POST /api/v1/alerts/:id/resolve`

**WebSocket subscriptions:** `check.result`, `alert.created`, `alert.updated`

### 5. Patch Management Module

**Routes:** `/patches/compliance`, `/patches/patches`, `/patches/policies`, `/patches/deployments`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Compliance | `/patches/compliance` | `ComplianceOverview`, `ComplianceByAgent`, `ComplianceByPatch` | Drill-down to agent/patch detail |
| Patches | `/patches/patches` | `PatchList`, `PatchFilters` | Filter by OS/severity/status, approve/reject |
| Policies | `/patches/policies` | `PatchPolicyList`, `PolicyEditor` (reused) | Create/edit patch deployment policies |
| Deployments | `/patches/deployments` | `DeploymentList`, `DeploymentProgress` | View active deployments, cancel |

**Data dependencies:**
- `GET /api/v1/patches/compliance`
- `GET /api/v1/patches?filter[os]=linux&filter[status]=pending`
- `GET /api/v1/patch-policies`
- `GET /api/v1/patch-deployments?status=active`
- `POST /api/v1/patch-deployments` (create deployment)
- `POST /api/v1/patch-deployments/:id/cancel`

**WebSocket subscriptions:** `patch.progress` (for live deployment progress bars)

### 6. Remote Access Module

**Routes:** `/remote/terminal`, `/remote/desktop`, `/remote/sessions`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Terminal | `/remote/terminal` | `TerminalView` (xterm.js) | Connect to agent shell, send commands, disconnect |
| Desktop | `/remote/desktop` | `DesktopView` (noVNC) | Connect to agent desktop, full keyboard/mouse, disconnect |
| Sessions | `/remote/sessions` | `SessionList`, `SessionRecordingPlayer` | View active sessions, play back recordings |

**Terminal page component diagram:**

```
TerminalView
├── SessionConfigForm         # Select agent, user, shell
│   └── AgentSelect
│   └── ShellSelect
├── XtermTerminal             # xterm.js wrapper
│   ├── Terminal (xterm.js)   # The actual terminal UI
│   ├── FitAddon              # Resize handling
│   └── WebglAddon            # GPU rendering (optional)
├── SessionControls           # Connect/Disconnect/Recording toggle
│   └── ConnectionStatusBadge
└── SessionLog                # Scrolling log of session events
```

**Data dependencies:**
- `POST /api/v1/remote/terminal` (creates session, returns WebSocket URL)
- WebSocket: `wss://api/ws/terminal/:sessionId` (bidirectional: input from browser, output to terminal)
- `GET /api/v1/remote/sessions?status=active`
- `GET /api/v1/remote/sessions/:id/recording` (for playback)

**WebSocket subscriptions:** `terminal.output` (direct xterm.js write, bypasses TanStack)

**Security:** All terminal/desktop sessions are recorded by default. The frontend displays a persistent recording indicator. Sensitive commands (e.g., containing secrets) are masked in the log but visible in the encrypted recording.

### 7. Script Editor Module

**Routes:** `/scripts`, `/scripts/$scriptId`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Script List | `/scripts` | `ScriptList`, `ScriptCreateButton` | List scripts, filter by language/target, create |
| Script Editor | `/scripts/$scriptId` | `ScriptEditor` (Monaco), `RunForm`, `LiveOutputConsole` | Edit code, configure run params, execute, view output |

**Component diagram (Script Editor):**

```
ScriptEditorPage
├── PageHeader                # Name, language, save status, actions
├── EditorLayout (2-column)
│   ├── LeftColumn
│   │   ├── MonacoEditor      # Code editor with syntax highlighting
│   │   │   └── LanguageServices (Bash, PowerShell, Python)
│   │   └── EditorToolbar     # Format, Find/Replace, Word Wrap toggles
│   └── RightColumn
│       ├── RunForm           # Target agent(s), timeout, env vars
│       │   ├── TargetSelector (multi-select agents or tags)
│       │   ├── TimeoutInput
│       │   └── EnvVarEditor
│       ├── RunButton         # "Run" with confirmation
│       └── LiveOutputConsole # xterm.js or plain output viewer
│           └── OutputFilter (stdout/stderr/exit code)
```

**Data dependencies:**
- `GET /api/v1/scripts`
- `GET /api/v1/scripts/:id`
- `PUT /api/v1/scripts/:id` (save)
- `POST /api/v1/scripts/:id/run` (execute, returns runId)
- `GET /api/v1/scripts/:id/runs/:runId` (poll for status)
- WebSocket: `script.run.output` (stream output), `script.run.status` (status updates)

**Monaco configuration:**
- Language: `bash`, `powershell`, `python` (selected per script)
- Theme: matches app theme (vs-dark / vs-light)
- Autocomplete: built-in language services
- Format: Prettier-like formatting via `monaco-editor`'s built-in formatters

### 8. A2A Dashboard Module

**Routes:** `/a2a`, `/a2a/agents`, `/a2a/tasks`, `/a2a/messages`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| A2A Dashboard | `/a2a` | `AgentCards`, `TaskLifecycle`, `A2AStatsPanel` | Overview of A2A activity |
| A2A Agents | `/a2a/agents` | `AgentCard` grid, `AgentDetail` | Browse registered A2A agents |
| A2A Tasks | `/a2a/tasks` | `TaskList`, `TaskLifecycle` | View task queue, status transitions |
| A2A Messages | `/a2a/messages` | `MessageThread`, `ArtifactViewer` | View inter-agent message history |

**Data dependencies:**
- `GET /api/v1/a2a/agents`
- `GET /api/v1/a2a/tasks?status=running`
- `GET /api/v1/a2a/tasks/:id`
- `GET /api/v1/a2a/messages?conversationId=:id`
- `GET /api/v1/a2a/artifacts/:id`

**WebSocket subscriptions:** `a2a.task.update`, `a2a.message.received`

### 9. Policies Module

**Routes:** `/policies`, `/policies/$policyId`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Policy List | `/policies` | `PolicyList`, `PolicyCreateButton` | List policies, filter by type/status |
| Policy Editor | `/policies/$policyId` | `PolicyEditor`, `ValidationPanel` | Edit policy (YAML/JSON), validate, save |

**Component diagram (Policy Editor):**

```
PolicyEditorPage
├── PageHeader                # Name, type, version, save status
├── EditorLayout (2-column)
│   ├── LeftColumn
│   │   ├── PolicyEditor      # Monaco with YAML/JSON
│   │   ├── FormatToggle      # YAML <-> JSON
│   │   └── SchemaHint        # Inline schema documentation
│   └── RightColumn
│       ├── ValidationPanel   # Real-time validation results
│       │   ├── ErrorList     # Errors with line numbers
│       │   ├── WarningList   # Warnings (best practices)
│       │   └── PreviewPanel  # What this policy will do
│       ├── PolicyDiffView    # Show changes since last save
│       └── ActionButtons     # Save, Discard, Dry Run
```

**Validation flow:**
1. User types in Monaco editor
2. On change (debounced 500ms), parse with Zod schema
3. Display errors/warnings in `ValidationPanel`
4. Click "Dry Run" -> `POST /api/v1/policies/:id/dry-run` -> show affected agents/checks
5. Click "Save" -> `PUT /api/v1/policies/:id`

**Data dependencies:**
- `GET /api/v1/policies`
- `GET /api/v1/policies/:id`
- `PUT /api/v1/policies/:id`
- `POST /api/v1/policies/:id/validate` (real-time validation, debounced)
- `POST /api/v1/policies/:id/dry-run`

### 10. Secret Management Module

**Routes:** `/secrets`, `/secrets/$secretId`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Secret List | `/secrets` | `SecretList`, `SecretCreateDialog` | List secrets (values masked), filter by type, create |
| Secret Detail | `/secrets/$secretId` | `SecretDetail`, `AccessLog`, `SecretValueReveal` | View metadata, reveal value (audit-logged), view access log |

**Data dependencies:**
- `GET /api/v1/secrets?type=password&tag=db`
- `GET /api/v1/secrets/:id` (returns metadata only, never the value)
- `POST /api/v1/secrets/:id/reveal` (returns value, logged)
- `GET /api/v1/secrets/:id/access-log`
- `POST /api/v1/secrets` (create)
- `PUT /api/v1/secrets/:id` (update metadata)
- `DELETE /api/v1/secrets/:id`

**Security model:**
- Secret values are never sent to the frontend unless explicitly revealed
- Each reveal requires a reason (logged)
- Reveal actions are rate-limited (5 per hour per user)
- The `SecretValueReveal` component shows a countdown timer (30s) before auto-hiding the value

### 11. Settings Module

**Routes:** `/settings/users`, `/settings/roles`, `/settings/sso`, `/settings/notifications`, `/settings/org`, `/settings/api-keys`

**Pages:**

| Page | Path | Components | Key Interactions |
|---|---|---|---|
| Users | `/settings/users` | `UserList`, `UserInviteDialog`, `UserEditDialog` | Invite, edit, deactivate, assign roles |
| Roles | `/settings/roles` | `RoleList`, `RoleEditor` | Create/edit roles, assign permissions |
| SSO | `/settings/sso` | `SSOConfigForm` | Configure SAML/OIDC provider, test, enable |
| Notifications | `/settings/notifications` | `NotificationChannels` | Configure email, Slack, webhook channels |
| Org | `/settings/org` | `OrgSettingsForm` | Org name, logo, default settings |
| API Keys | `/settings/api-keys` | `APIKeyList`, `APIKeyCreateDialog` | Generate, revoke API keys |

**Data dependencies:**
- `GET /api/v1/users`
- `POST /api/v1/users` (invite)
- `PUT /api/v1/users/:id`
- `GET /api/v1/roles`
- `POST /api/v1/roles`
- `GET /api/v1/sso/config`
- `PUT /api/v1/sso/config`
- `GET /api/v1/notification-channels`
- `POST /api/v1/notification-channels`
- `GET /api/v1/org`
- `PUT /api/v1/org`
- `GET /api/v1/api-keys`
- `POST /api/v1/api-keys`
- `DELETE /api/v1/api-keys/:id`

---

## Shared Infrastructure

The following 9 infrastructure tasks are completed before any feature module work begins.

### Task 1: Vite + React 19 Scaffolding

- Initialize Vite project with `react-ts` template
- Configure `vite.config.ts` with path aliases (`@/` -> `src/`)
- Set up ESLint + Prettier with strict TypeScript rules
- Configure Vitest for unit/component testing
- Add Husky + lint-staged for pre-commit hooks

**Key files:**
- `vite.config.ts`
- `tsconfig.json` (strict mode, `noUncheckedIndexedAccess`)
- `.eslintrc.cjs`
- `.prettierrc`

### Task 2: Tailwind CSS + Shadcn/ui Setup

- Install Tailwind CSS 4.x with Vite plugin
- Configure `tailwind.config.ts` with custom design tokens
- Set up CSS custom properties for theming (light/dark)
- Initialize Shadcn/ui with `npx shadcn@latest init`
- Configure component aliases

**Design tokens:**

| Token | Light | Dark |
|---|---|---|
| `--background` | `0 0% 100%` | `222 47% 11%` |
| `--foreground` | `222 47% 11%` | `210 40% 98%` |
| `--primary` | `221 83% 53%` | `217 91% 60%` |
| `--muted` | `210 40% 96%` | `217 33% 17%` |
| `--border` | `214 32% 91%` | `217 33% 20%` |
| `--destructive` | `0 84% 60%` | `0 63% 40%` |
| `--success` | `142 71% 45%` | `142 71% 45%` |
| `--warning` | `38 92% 50%` | `38 92% 50%` |

### Task 3: Environment Configuration + Zod Validation

- Create `.env.example` with all required variables
- Validate at build time with Zod schema
- Type-safe access via `import { env } from '@/env'`

```typescript
// src/env.ts
const envSchema = z.object({
  VITE_API_BASE_URL: z.string().url(),
  VITE_WS_URL: z.string().url(),
  VITE_APP_VERSION: z.string(),
  VITE_SENTRY_DSN: z.string().url().optional(),
  VITE_ENABLE_MOCK: z.enum(['true', 'false']).default('false'),
});

export const env = envSchema.parse(import.meta.env);
```

### Task 4: API Client (Fetch Wrapper + Interceptors + Retry)

- Typed `fetch` wrapper with:
  - Automatic JSON serialization/deserialization
  - JWT injection from auth store
  - 401 -> refresh token flow
  - 403 -> redirect to forbidden page
  - 5xx -> exponential backoff retry (3 attempts)
  - Request/response logging in dev mode
  - Abort signal propagation for cleanup

```typescript
// src/lib/api-client.ts
class ApiClient {
  async request<T>(config: RequestConfig): Promise<T> {
    const token = authStore.getAccessToken();
    const response = await fetch(`${env.VITE_API_BASE_URL}${config.path}`, {
      ...config,
      headers: {
        'Content-Type': 'application/json',
        ...(token && { Authorization: `Bearer ${token}` }),
        ...config.headers,
      },
      signal: config.signal,
    });

    if (response.status === 401) {
      await this.refreshToken();
      return this.retry(config);
    }

    if (!response.ok) {
      throw new ApiError(response.status, await response.json());
    }

    return response.json();
  }

  get<T>(path: string, params?: Record<string, unknown>) { ... }
  post<T>(path: string, body: unknown) { ... }
  put<T>(path: string, body: unknown) { ... }
  delete<T>(path: string) { ... }
}

export const api = new ApiClient();
```

### Task 5: WebSocket Client (Reconnect + Heartbeat + Message Routing)

- Singleton WebSocket connection to `${VITE_WS_URL}/ws`
- Automatic reconnection with exponential backoff (1s, 2s, 4s, 8s, max 30s)
- Heartbeat ping every 30s, disconnect if no pong within 10s
- Channel-based pub/sub for component subscriptions
- Automatic cache invalidation on subscribed messages

```typescript
// src/lib/ws-client.ts
class WebSocketClient {
  private ws: WebSocket | null = null;
  private subscribers = new Map<string, Set<(data: unknown) => void>>();
  private reconnectAttempts = 0;
  private heartbeatInterval: number | null = null;

  connect() {
    this.ws = new WebSocket(`${env.VITE_WS_URL}/ws`);
    this.ws.onopen = this.handleOpen;
    this.ws.onmessage = this.handleMessage;
    this.ws.onclose = this.handleClose;
    this.ws.onerror = this.handleError;
  }

  subscribe(channel: string, callback: (data: unknown) => void) {
    if (!this.subscribers.has(channel)) {
      this.subscribers.set(channel, new Set());
      this.send({ action: 'subscribe', channel });
    }
    this.subscribers.get(channel)!.add(callback);
    return () => this.unsubscribe(channel, callback);
  }

  private handleMessage(event: MessageEvent) {
    const { channel, data } = JSON.parse(event.data);
    const callbacks = this.subscribers.get(channel);
    callbacks?.forEach((cb) => cb(data));

    // Auto-invalidate TanStack queries based on channel
    this.invalidateQueries(channel, data);
  }
}

export const wsClient = new WebSocketClient();
```

### Task 6: Auth Layer (JWT Decode + Refresh + RBAC)

- JWT decode using `jose` library
- Access token (15min) + refresh token (7d)
- Silent refresh 1 minute before expiry
- RBAC permission context provider
- `usePermission('agents:delete')` hook

```typescript
// src/lib/auth.ts
export function decodeJWT(token: string): JWTPayload {
  return jose.decodeJwt(token);
}

export async function refreshAccessToken(): Promise<string> {
  const refreshToken = getRefreshToken();
  const response = await fetch(`${env.VITE_API_BASE_URL}/api/v1/auth/refresh`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${refreshToken}` },
  });
  const { accessToken } = await response.json();
  setAccessToken(accessToken);
  return accessToken;
}
```

```typescript
// src/hooks/use-permissions.ts
export function usePermission(permission: string): boolean {
  const { user } = useAuth();
  return user?.permissions.includes(permission) ?? false;
}
```

### Task 7: TanStack Router + App Shell

- File-based routing with auto-generated `routeTree.gen.ts`
- Type-safe params and search params
- Auth guard at root route (redirect to `/login` if not authenticated)
- App shell: sidebar (collapsible) + topbar (org switcher, user menu) + content area
- Breadcrumb auto-generated from route hierarchy

```typescript
// src/routes/__root.tsx
export const Route = createRootRoute({
  component: RootComponent,
  beforeLoad: ({ location }) => {
    if (!authStore.isAuthenticated() && !isPublicRoute(location.pathname)) {
      throw redirect({ to: '/login' });
    }
  },
});

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <AppShell>
          <Outlet />
        </AppShell>
      </AuthProvider>
      <ToastProvider />
    </QueryClientProvider>
  );
}
```

### Task 8: Shadcn/ui Primitives (30+ Components)

The following Shadcn/ui components are installed and customized:

| Component | File | Usage |
|---|---|---|
| Button | `ui/button.tsx` | All action buttons |
| Card | `ui/card.tsx` | Content containers |
| Dialog | `ui/dialog.tsx` | Modal dialogs |
| Dropdown Menu | `ui/dropdown-menu.tsx` | Action menus |
| Input | `ui/input.tsx` | Text/number inputs |
| Label | `ui/label.tsx` | Form labels |
| Select | `ui/select.tsx` | Dropdown selects |
| Tabs | `ui/tabs.tsx` | Tabbed interfaces |
| Table | `ui/table.tsx` | Data tables |
| Toast | `ui/toast.tsx` | Notifications |
| Tooltip | `ui/tooltip.tsx` | Hover hints |
| Checkbox | `ui/checkbox.tsx` | Boolean inputs |
| Radio Group | `ui/radio-group.tsx` | Mutually exclusive options |
| Switch | `ui/switch.tsx` | Toggle inputs |
| Slider | `ui/slider.tsx` | Range inputs |
| Textarea | `ui/textarea.tsx` | Multi-line text |
| Combobox | `ui/combobox.tsx` | Searchable select |
| Date Picker | `ui/date-picker.tsx` | Date selection |
| Pagination | `ui/pagination.tsx` | List pagination |
| Badge | `ui/badge.tsx` | Status indicators |
| Avatar | `ui/avatar.tsx` | User/org icons |
| Alert | `ui/alert.tsx` | Inline messages |
| Alert Dialog | `ui/alert-dialog.tsx` | Confirmation dialogs |
| Sheet | `ui/sheet.tsx` | Side panels |
| Skeleton | `ui/skeleton.tsx` | Loading placeholders |
| Progress | `ui/progress.tsx` | Progress bars |
| Separator | `ui/separator.tsx` | Visual dividers |
| Scroll Area | `ui/scroll-area.tsx` | Custom scrollbars |
| Command | `ui/command.tsx` | Command palette |
| Popover | `ui/popover.tsx` | Floating content |
| Collapsible | `ui/collapsible.tsx` | Expandable sections |
| Form | `ui/form.tsx` | Form field wrappers |
| Navigation Menu | `ui/navigation-menu.tsx` | Top-level nav |

### Task 9: Shared Visualization Components

Custom-built on top of `recharts` (for line/bar/area charts) and custom SVG (for gauges, sparklines).

| Component | Props | Implementation |
|---|---|---|
| `LineChart` | `data`, `xKey`, `yKeys[]`, `height` | recharts `LineChart` with theme-aware colors |
| `Gauge` | `value`, `max`, `thresholds` | Custom SVG arc with color zones |
| `Sparkline` | `data`, `width`, `height` | Custom SVG path, no axes |
| `StatusBadge` | `status` | Colored badge mapping status to variant |
| `BarChart` | `data`, `xKey`, `yKey` | recharts `BarChart` |
| `Heatmap` | `data`, `xLabels`, `yLabels` | Custom SVG grid with color intensity |

**Status badge color mapping:**

| Status | Color | Variant |
|---|---|---|
| `online` / `healthy` / `passing` | Green | `success` |
| `degraded` / `warning` | Yellow | `warning` |
| `offline` / `failing` / `critical` | Red | `destructive` |
| `pending` / `unknown` | Gray | `muted` |
| `running` / `active` | Blue | `default` |

---

## Implementation Steps

The frontend is built in 20 sequential tasks. Tasks 1-9 are shared infrastructure; tasks 10-20 are feature modules.

| # | Task | Description | Dependencies | Estimated Effort |
|---|---|---|---|---|
| 1 | Vite + React 19 Scaffolding | Initialize project, configure TypeScript, ESLint, Prettier, Vitest | — | 0.5d |
| 2 | Tailwind CSS + Shadcn/ui Setup | Install Tailwind 4.x, Shadcn/ui, configure design tokens, install 30+ components | Task 1 | 1d |
| 3 | Environment Configuration + Zod Validation | Create `.env.example`, Zod schema, type-safe env access | Task 1 | 0.25d |
| 4 | API Client | Fetch wrapper, interceptors, retry, refresh token flow | Task 3 | 1d |
| 5 | WebSocket Client | Singleton WS, reconnect, heartbeat, pub/sub, cache invalidation | Task 3 | 1d |
| 6 | Auth Layer | JWT decode, refresh, RBAC context, permission hooks | Task 4 | 0.75d |
| 7 | TanStack Router + App Shell | File-based routing, auth guard, sidebar, topbar, breadcrumb | Tasks 1, 6 | 1d |
| 8 | Shadcn/ui Primitives | Install and customize 30+ components | Task 2 | 0.5d |
| 9 | Shared Visualization | LineChart, Gauge, Sparkline, StatusBadge, BarChart, Heatmap | Task 2 | 1d |
| 10 | Auth Module | Login, SSO callback, password reset pages | Tasks 6, 7 | 1d |
| 11 | Dashboard Module | AgentStatusGrid, AlertFeed, CheckHealthGauge, PatchComplianceBar, LiveMetricLine | Tasks 7, 9 | 1.5d |
| 12 | Agent Management Module | AgentList, AgentDetail with 6 tabs (Overview, Checks, Alerts, Patches, Sessions, Logs) | Tasks 7, 8, 9 | 2d |
| 13 | Monitoring Module (Checks) | CheckList, CheckDetail with CheckTimeSeries | Tasks 7, 8, 9 | 1.5d |
| 14 | Monitoring Module (Alerts) | AlertList, AlertDetail with acknowledge/resolve | Tasks 7, 8, 9 | 1.5d |
| 15 | Patch Management Module | Compliance, Patches, Policies, Deployments | Tasks 7, 8, 9 | 2d |
| 16 | Remote Access Module | Terminal (xterm.js), Desktop (noVNC), Sessions | Tasks 5, 7, 8 | 2d |
| 17 | Script Editor Module | Monaco editor, RunForm, LiveOutputConsole | Tasks 5, 7, 8 | 1.5d |
| 18 | A2A Dashboard Module | AgentCards, TaskLifecycle, Messages, Artifacts | Tasks 5, 7, 8, 9 | 1.5d |
| 19 | Policies Module | PolicyEditor (Monaco), ValidationPanel, PolicyDiffView | Tasks 7, 8, 9 | 1.5d |
| 20 | Secret Management + Settings Modules | SecretList, SecretDetail, AccessLog, Users, Roles, SSO, Notifications, Org, APIKeys | Tasks 7, 8 | 2.5d |

**Total estimated effort:** ~22 person-days

### Task Dependency Graph

```
Task 1 ─┬─> Task 2 ──> Task 8
        ├─> Task 3 ─┬─> Task 4 ──> Task 6 ─┐
        │          └─> Task 5 ─────────────┤
        └─> Task 7 ────────────────────────┤
                    Task 9 ─────────────────┤
                                            │
        ┌───────────────────────────────────┘
        ├─> Task 10 (Auth)
        ├─> Task 11 (Dashboard)
        ├─> Task 12 (Agents)
        ├─> Tasks 13, 14 (Monitoring)
        ├─> Task 15 (Patches)
        ├─> Task 16 (Remote Access)
        ├─> Task 17 (Scripts)
        ├─> Task 18 (A2A)
        ├─> Task 19 (Policies)
        └─> Task 20 (Secrets + Settings)
```

### Testing Strategy

| Layer | Tool | Coverage Target |
|---|---|---|
| Unit (hooks, utils) | Vitest | 90% |
| Component | Testing Library + Vitest | 80% |
| Integration (features) | Testing Library + MSW | 70% |
| E2E (critical paths) | Playwright | 5 happy paths |
| Visual regression | Chromatic | All Storybook stories |
| Accessibility | axe-core (via Playwright) | 0 violations |

### Performance Budget

| Metric | Target |
|---|---|
| Initial JS bundle (gzipped) | < 200 KB |
| First Contentful Paint | < 1.5s |
| Time to Interactive | < 3.0s |
| Largest Contentful Paint | < 2.5s |
| Cumulative Layout Shift | < 0.1 |
| Route transition (client-side) | < 100ms |

### Accessibility

- WCAG 2.1 AA compliance
- Full keyboard navigation
- Screen reader support (ARIA labels, live regions for alerts)
- Focus management on route changes and modal open/close
- Color contrast ratios meeting AA (4.5:1 for text, 3:1 for UI components)
- `prefers-reduced-motion` respected for animations

### Internationalization (i18n)

- `react-i18next` for translations
- Default language: English
- Supported languages: English, Spanish, French, German, Japanese
- Translation files in `src/locales/{lang}/common.json`
- Lazy-loaded per language

### Error Handling

- Global `ErrorBoundary` at root route
- Per-route `ErrorBoundary` for isolated failures
- Toast notifications for transient errors
- Inline form errors for validation failures
- Sentry integration for unhandled errors (production only)

### Build & Deployment

- Vite production build outputs to `dist/`
- Static assets served from CDN
- SPA fallback: all routes serve `index.html` (handled by CDN/nginx config)
- Cache headers: `index.html` no-cache, hashed assets 1 year
- Source maps uploaded to Sentry (not served to users)
- Bundle analysis via `rollup-plugin-visualizer` (run in CI)
