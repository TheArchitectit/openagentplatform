# React Frontend

> **Phase:** 4 (Frontend)
> **STATUS: PARTIAL**
> **Source:** `docs/architecture/ROADMAP_AND_SPRINTS.md` §5 (Phase 4)
> **App Path:** `web/`

---

## Description

The frontend is a React 19 single-page application: 20 pages across 10 feature
modules, covering the dashboard, agent management, monitoring, patches, remote
access, script editing, the A2A dashboard, the policy editor, and settings.

It is the surface through which technicians actually do the work, so its
defining requirement is liveness. An RMM console showing stale data is worse
than one showing no data, because an operator will act on it. Real-time updates
arrive over WebSocket; polling is a fallback, not the design.

The app shell — routing via TanStack Router, server state via TanStack Query,
and OIDC login redirect — is implemented and functional. Individual feature
dashboards remain incomplete.

> **Current state:** The app shell loads, the sidebar renders, and login
> redirects correctly. However, **the dashboard routes currently return 404** —
> the feature pages are not yet wired into the router. Additionally, Phase 2 A2A
> has a three-way contract divergence between the Go gateway, the Python adapter
> service, and this frontend; the A2A dashboard MUST be built against the
> generated A2A types rather than a hand-written parallel client.

## User Story

**As** a technician triaging an incident,
**I want** a single console showing live endpoint status, alerts, checks,
patches, and running agent tasks, with a terminal and script editor at hand,
**so that** I can diagnose and remediate without switching tools — and trust that
what I'm looking at reflects the fleet right now, not thirty seconds ago.

---

## Requirements

### 1. Application Shell

1.1. The app MUST be a React 19 SPA.

1.2. Routing MUST use TanStack Router; server state MUST use TanStack Query.

1.3. The shell MUST render a persistent sidebar navigation and redirect
unauthenticated users to login.

1.4. Every route declared in navigation MUST resolve to a rendered page. A
navigable link that resolves to 404 MUST be treated as a defect, not an
unimplemented feature — unbuilt destinations MUST NOT appear in navigation.

1.5. Route-level error boundaries MUST prevent one failing page from blanking
the entire app.

### 2. Feature Modules

2.1. Ten feature modules comprising 20 pages MUST be implemented:

| Module | Coverage |
|--------|----------|
| Dashboard | Fleet overview, health summary, recent activity |
| Agent Management | Endpoint list, endpoint detail, tags, grouping |
| Monitoring | Checks list, check detail, alert inbox, alert detail |
| Patches | Patch inventory, approval queue, deployment status, compliance scorecard |
| Remote Access | Terminal session, desktop viewer, session history |
| Script Editor | Script library, Monaco editor, execution form, live output |
| A2A Dashboard | Task list, task detail, agent registry, cost view |
| Policy Editor | Policy library, rule editor, scope assignment, preview |
| Settings | Users, RBAC, SSO, API keys |
| Auth | Login, OIDC callback |

2.2. Each module MUST be independently code-split so initial bundle size does
not grow linearly with feature count.

### 3. Endpoint Management

3.1. The endpoint list MUST show hostname, status, and last-seen time.

3.2. It MUST update live via WebSocket as agents come online and go offline.

3.3. It MUST support filtering by status, platform, client, site, and tags.

3.4. It MUST be paginated; the full fleet MUST NOT be fetched into the browser
at once.

### 4. Monitoring Views

4.1. The checks dashboard MUST show a table of checks with status indicators and
MUST auto-refresh at least every 30 seconds.

4.2. The alert inbox MUST support acknowledge, resolve, and snooze actions.

4.3. The alert detail page MUST show a timeline of the alert's state
transitions.

4.4. Alert actions MUST reflect server-side authorization — a user lacking
permission MUST receive a clear error, and the UI MUST NOT assume success.

### 5. Patch Management Views

5.1. A compliance scorecard MUST summarize patch state across the fleet.

5.2. Per-agent patch status MUST be viewable.

5.3. The approval queue MUST support approve and reject with the acting user
recorded.

5.4. Reboot prompts MUST surface agents reporting `needs_reboot`.

5.5. Deployment progress MUST update live, including per-agent failures.

### 6. Script Editor

6.1. The editor MUST use Monaco with syntax highlighting for all supported
runtimes.

6.2. An execution form MUST allow selecting target agents, runtime, arguments,
environment, and timeout.

6.3. Output MUST stream to a live console as it arrives, not appear only on
completion.

6.4. A long-running or high-volume output stream MUST NOT freeze the UI;
rendering MUST be virtualized or throttled.

### 7. Remote Access

7.1. Terminal sessions MUST use xterm.js; desktop viewing MUST use noVNC.

7.2. Keystrokes MUST transmit to the endpoint and the remote desktop MUST be
viewable.

7.3. Session recordings MUST be available for playback.

7.4. Active session state MUST be visible, including who is connected.

### 8. A2A Dashboard

8.1. A2A tasks MUST be listable with state, agent, and timestamps, filterable by
state and agent.

8.2. Task detail MUST show the state-transition history, messages, and
artifacts.

8.3. Artifacts MUST be rendered per MIME type where feasible, and downloadable
otherwise.

8.4. Pending HITL approval requests MUST be surfaced prominently with approve
and reject actions.

8.5. Per-task and aggregate cost MUST be displayed.

8.6. The A2A client MUST be generated from or validated against the
authoritative A2A protobuf types. A hand-maintained parallel type definition
MUST NOT be introduced, as this is the source of the existing three-way
divergence.

### 9. Policy Editor

9.1. Policies MUST be creatable with rules, and assignable to client, site, or
agent scope.

9.2. The editor MUST preview which agents a policy will affect before it is
saved.

9.3. Enforcement mode (`inherit` / `enforce` / `exclude`) and priority MUST be
editable.

### 10. Settings

10.1. User management, role assignment, SSO configuration, and API key
management MUST be provided.

10.2. API keys MUST be displayed in full exactly once at creation, with a clear
warning that they cannot be retrieved again.

10.3. Settings pages MUST be restricted to authorized roles, enforced
server-side.

### 11. Real-Time Updates

11.1. Live updates MUST be delivered over WebSocket.

11.2. The client MUST reconnect automatically with backoff on disconnect.

11.3. Stale data MUST be visually distinguishable when the connection is lost —
the UI MUST NOT present disconnected data as current.

11.4. On reconnect, affected queries MUST be refetched so no update gap
persists.

11.5. Polling MAY be used as a fallback but MUST NOT be the primary mechanism.

### 12. Authentication Integration

12.1. Login MUST redirect through OIDC and handle the callback.

12.2. Access tokens MUST be refreshed transparently before expiry.

12.3. A `401` MUST redirect to login; a `403` MUST show an authorization error
without logging the user out.

12.4. Tokens MUST NOT be stored where XSS can trivially exfiltrate them.

### 13. Accessibility

13.1. The UI MUST meet WCAG 3.0+ compliance, per project guardrails.

13.2. All interactive controls MUST be keyboard reachable and operable.

13.3. Status MUST NOT be conveyed by color alone — icon or text MUST accompany
it, since check and alert severity are core to the product.

13.4. Live-updating regions MUST use appropriate ARIA live semantics without
flooding screen readers.

13.5. Focus MUST be managed on route change and modal open/close.

### 14. Performance

14.1. Route-based code splitting and tree-shaking MUST be applied.

14.2. A bundle-size performance budget MUST be enforced in CI, per risk R6.

14.3. Large tables MUST be virtualized rather than rendering all rows.

### 15. Ethical Engagement

15.1. The UI MUST NOT implement dark patterns, per project guardrails.

15.2. Destructive actions — script execution, patch deployment, endpoint
deletion, remote session start — MUST require explicit confirmation stating the
scope and blast radius.

15.3. Confirmation dialogs MUST NOT pre-select the destructive option.
