export const meta = {
  name: 'sprint-12-impl',
  description: 'Implement Sprint 1.2: Alert rule engine, notification channels, alert inbox, alert preferences',
  phases: [
    { title: 'Engine', detail: 'Alert rule engine with state machine' },
    { title: 'Channels', detail: 'Notification channels: email, Slack, webhook' },
    { title: 'Inbox', detail: 'Alert inbox and detail page (React)' },
    { title: 'Preferences', detail: 'Alert preferences and routing configuration' },
    { title: 'Commit', detail: 'Stage, commit, push, close Sprint 1.2 issues' },
  ],
}

log('Sprint 1.2 implementation starting -- Alerts')

var repoRoot = '/mnt/data/git/openagentplatform'

// Phase 1: Alert rule engine with state machine (sonnet -- core logic)
phase('Engine')
var engine = await agent(
'Build the alert rule engine with state machine at ' + repoRoot + '.\n' +
'Read existing internal/checks/ingest.go, internal/checks/threshold.go, internal/api/agent_store.go, pkg/models/models.go first.\n' +
'\n' +
'**1. Create internal/alerts/engine.go** -- AlertEngine:\n' +
'- Subscribe to NATS subject oap.events.alerts (triggered by check ingest pipeline)\n' +
'- Alert states: pending, open, acknowledged, snoozed, resolved, closed (6 states)\n' +
'- State transitions via NATS messages:\n' +
'  * check_failure -> create pending alert (or escalate existing)\n' +
'  * acknowledge -> pending/open -> acknowledged (by user)\n' +
'  * snooze -> pending/open -> snoozed (with duration)\n' +
'  * check_recovery -> any state -> resolved (auto-resolve)\n' +
'  * close -> resolved -> closed (manual or timeout)\n' +
'  * snooze_expired -> snoozed -> open (auto on expiry)\n' +
'- Deduplication: alert_dedup_key (check_id + agent_id + alert_rule_id) prevents duplicate alerts\n' +
'- Escalation: if alert stays pending > 5min, auto-escalate to open\n' +
'- Suppression: if check flapping (N open/resolve cycles in time window), suppress\n' +
'- Severity levels: info, warning, critical, emergency\n' +
'\n' +
'**2. Create internal/alerts/statemachine.go** -- State machine:\n' +
'- ValidTransitions map defining legal moves for each state\n' +
'- Transition(ctx, event) validates and executes the transition\n' +
'- TransitionAudit records every state change with timestamp and actor\n' +
'- StateHistory returns timeline for a given alert\n' +
'\n' +
'**3. Create internal/alerts/store.go** -- PostgreSQL queries:\n' +
'- InsertAlert, GetAlert, ListAlerts (filterable by state, severity, agent_id, site_id, time range), UpdateAlertState\n' +
'- GetAlertRules, CreateAlertRule, UpdateAlertRule, DeleteAlertRule\n' +
'- InsertNotificationRecord, GetNotificationHistory per alert\n' +
'\n' +
'**4. Update internal/api/routes.go** -- register alert routes:\n' +
'- GET /api/v1/alerts -- list alerts\n' +
'- GET /api/v1/alerts/{id} -- single alert with history\n' +
'- POST /api/v1/alerts/{id}/acknowledge\n' +
'- POST /api/v1/alerts/{id}/snooze -- body: {duration_minutes}\n' +
'- POST /api/v1/alerts/{id}/resolve\n' +
'- POST /api/v1/alerts/{id}/close\n' +
'- GET /api/v1/alert-rules -- list rules\n' +
'- POST /api/v1/alert-rules -- create rule\n' +
'- PUT /api/v1/alert-rules/{id} -- update rule\n' +
'- DELETE /api/v1/alert-rules/{id}\n' +
'\n' +
'**5. Update pkg/models/models.go** -- ensure Alert, AlertRule, AlertStateMachine, NotificationRecord match DB schema\n' +
'\n' +
'**6. Update cmd/server/main.go** -- start AlertEngine on server startup, wire dependencies\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created and any errors.',
{ label: 'alert-engine', phase: 'Engine', model: 'sonnet' }
)

// Phase 2: Notification channels (sonnet -- email, Slack, webhook)
phase('Channels')
var channels = await agent(
'Build notification channels for OpenAgentPlatform at ' + repoRoot + '.\n' +
'Read existing internal/alerts/engine.go first.\n' +
'\n' +
'**1. Create internal/notify/notifier.go** -- Notifier interface:\n' +
'- type Notifier interface { Notify(ctx, alert, channel) error; ValidateConfig(config) error }\n' +
'- NotifierRegistry: map[channel_type]Notifier\n' +
'- Dispatch(ctx, alert, channels []NotificationChannel) -- fan out to all channels concurrently\n' +
'- Retry with exponential backoff (3 attempts)\n' +
'\n' +
'**2. Create internal/notify/email.go** -- EmailNotifier:\n' +
'- SMTP config: host, port, username, password, from_address, tls\n' +
'- HTML template: alert severity header, check details, agent info, action links\n' +
'- Plain text fallback\n' +
'- Uses Go stdlib net/smtp (no external deps)\n' +
'\n' +
'**3. Create internal/notify/slack.go** -- SlackNotifier:\n' +
'- Webhook URL config\n' +
'- Slack Block Kit message format:\n' +
'  * Color-coded attachment (info=blue, warning=yellow, crit=red, emergency=ff0000)\n' +
'  * Fields: check name, agent hostname, severity, timestamp, output summary\n' +
'  * Link button to alert detail in platform\n' +
'- HTTP POST with JSON, timeout 10s\n' +
'\n' +
'**4. Create internal/notify/webhook.go** -- WebhookNotifier:\n' +
'- Generic webhook: URL, method (POST/PUT), headers (custom), body_template (Go template)\n' +
'- Signature: HMAC-SHA256 header X-OAP-Signature for verification\n' +
'- Configurable retry and timeout\n' +
'\n' +
'**5. Update internal/alerts/engine.go** -- integrate notifier dispatch:\n' +
'- When alert state changes to open or critical, look up notification channels for the alert rule\n' +
'- Call notify.Dispatch with all matching channels\n' +
'- Log success/failure per channel\n' +
'\n' +
'**6. Create internal/api/notifications.go** -- Channel CRUD:\n' +
'- GET /api/v1/notification-channels -- list user/org channels\n' +
'- POST /api/v1/notification-channels -- create channel (type, name, config)\n' +
'- GET /api/v1/notification-channels/{id}\n' +
'- PUT /api/v1/notification-channels/{id}\n' +
'- DELETE /api/v1/notification-channels/{id}\n' +
'- POST /api/v1/notification-channels/{id}/test -- send test notification\n' +
'\n' +
'**7. Update internal/api/routes.go** -- register notification channel routes\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created and any errors.',
{ label: 'notify-channels', phase: 'Channels', model: 'sonnet' }
)

// Phase 3: Alert inbox and detail page (sonnet -- React)
phase('Inbox')
var inbox = await agent(
'Build the Alert inbox and detail page at ' + repoRoot + '.\n' +
'Read existing web/src/routes/, web/src/components/, web/src/lib/ first. DO NOT run pnpm/npm install.\n' +
'\n' +
'**1. Create web/src/routes/alerts/index.tsx** -- Alert inbox:\n' +
'- Filter tabs: All, Critical, Warning, Info, Acknowledged, Snoozed, Resolved\n' +
'- Table: Severity icon (color-coded), Alert Title, Agent, Check, State badge, Created, Actions\n' +
'- Click row navigates to /alerts/$alertId\n' +
'- Batch actions: Select multiple + Acknowledge All / Resolve All\n' +
'- Inline acknowledge/snooze/resolve buttons per row\n' +
'- Real-time updates via WebSocket channel alerts\n' +
'- Sound notification on new critical alert (optional, via browser Notification API)\n' +
'\n' +
'**2. Create web/src/routes/alerts/$alertId.tsx** -- Alert detail:\n' +
'- Header: alert title, severity badge, state badge, timestamps (created, last state change)\n' +
'- Action bar: Acknowledge, Snooze (duration picker), Resolve, Close\n' +
'- Details card: check name, agent hostname, check output (monospace), metrics\n' +
'- State timeline: vertical timeline of all state transitions with timestamps\n' +
'- Notification history: which channels were notified, delivery status\n' +
'- Related alerts: other alerts for same check or agent\n' +
'\n' +
'**3. Create web/src/lib/useAlerts.ts** -- React hook:\n' +
'- Fetch alerts list with filters\n' +
'- acknowledgeAlert, snoozeAlert, resolveAlert, closeAlert mutations\n' +
'- Subscribe to WebSocket channel alerts for real-time updates\n' +
'\n' +
'**4. Create web/src/components/severity-badge.tsx** -- reusable severity component:\n' +
'- Color + icon: info=blue(circle-info), warning=yellow(triangle-exclamation), critical=red(circle-x), emergency=red(fire)\n' +
'\n' +
'**5. Update web/src/components/sidebar.tsx** -- ensure Alerts nav links to /alerts, add count badge\n' +
'\n' +
'**6. Update web/src/routes/dashboard.tsx** -- add alert KPI cards (Open, Critical, Acknowledged, Total Today)\n' +
'\n' +
'Report back files created/modified.',
{ label: 'alert-inbox', phase: 'Inbox', model: 'sonnet' }
)

// Phase 4: Alert preferences and routing (sonnet -- config + API)
phase('Preferences')
var preferences = await agent(
'Build alert preferences and routing configuration at ' + repoRoot + '.\n' +
'Read existing internal/alerts/engine.go, internal/alerts/store.go first.\n' +
'\n' +
'**1. Create internal/alerts/preferences.go** -- Alert preferences:\n' +
'- UserAlertPreferences struct: quiet_hours (start_time, end_time, timezone, days[]), severity_threshold, channel_preferences, mute_all\n' +
'- GlobalAlertPreferences struct: default_quiet_hours, retention_days, max_alerts_per_agent, auto_resolve_seconds\n' +
'- Evaluate whether a user should receive an alert based on preferences\n' +
'- Quiet hours check: skip notification if within quiet window\n' +
'- Severity filter: only notify if alert severity >= user threshold\n' +
'\n' +
'**2. Create internal/alerts/routing.go** -- Alert routing:\n' +
'- AlertRule -> notification channels mapping (M:N, stored in alert_rule_channels junction)\n' +
'- RoutingRules struct: conditions (match agent tags, check types, severity, site), destination channels\n' +
'- Evaluate routing for an alert: collect all matching rules, union their channel sets\n' +
'- Default routing: if no rule matches, use org-level default channel set\n' +
'\n' +
'**3. API endpoints** -- add to internal/api/alerts.go or create internal/api/alert_prefs.go:\n' +
'- GET/PUT /api/v1/alert-preferences -- user preferences\n' +
'- GET/PUT /api/v1/alert-preferences/global -- admin-only global preferences\n' +
'- GET /api/v1/alert-rules/{id}/channels -- list channels for rule\n' +
'- PUT /api/v1/alert-rules/{id}/channels -- set channels for rule\n' +
'\n' +
'**4. Update internal/alerts/engine.go** -- integrate preferences and routing:\n' +
'- Before dispatching notification, evaluate user preferences (quiet hours, severity threshold)\n' +
'- Apply routing rules to determine which channels receive the notification\n' +
'- Skip notification if all channels are suppressed\n' +
'\n' +
'**5. Update internal/api/routes.go** -- register preference routes\n' +
'\n' +
'After writing, run: cd ' + repoRoot + ' && go build ./... && go vet ./...\n' +
'Report back files created and any errors.',
{ label: 'alert-preferences', phase: 'Preferences', model: 'sonnet' }
)

// Phase 5: Commit, push, close issues (haiku -- mechanical)
phase('Commit')
var commit = await agent(
'Stage, commit, and push Sprint 1.2 implementation from ' + repoRoot + '.\n' +
'\n' +
'Run:\n' +
'1. cd ' + repoRoot + '\n' +
'2. git status\n' +
'3. git add -A\n' +
'4. git commit -m "Sprint 1.2: Alert rule engine, notification channels, alert inbox, alert preferences" -m "- Alert rule engine with 6-state machine, dedup, escalation, suppression" -m "- Notification channels: email (SMTP), Slack (Block Kit), webhook (HMAC)" -m "- Alert inbox with real-time updates, batch actions, detail page with timeline" -m "- Alert preferences: quiet hours, severity thresholds, routing rules" -m "Closes #16, #17, #19, #21"\n' +
'5. git push origin main\n' +
'6. for i in 16 17 19 21; do gh issue close $i -r completed; done\n' +
'\n' +
'If commit signing fails, retry with: git -c commit.gpgsign=false commit ...\n' +
'Report the commit SHA and confirmation.',
{ label: 'commit-push', phase: 'Commit', model: 'haiku' }
)

return {
  status: 'Sprint 1.2 complete',
  phases: { engine: engine, channels: channels, inbox: inbox, preferences: preferences, commit: commit },
}
