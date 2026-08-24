# Notifications

> **Phase:** 1 (Core RMM — Sprint 1.2 Alerts, story 1.2.2 "Notification channels (email, Slack, webhook)")
> **STATUS: PARTIAL**
> **Source:** authored 2026-08-23 from code (audit docs/QA_REVIEW_OPENSPEC_COVERAGE.md §4)
> **App Path:** internal/notify/

---

## Description

The notifications capability delivers alert messages out of OpenAgentPlatform
to operator-owned channels. It consists of a channel abstraction
(`Notifier` + `NotifierRegistry` in `internal/notify/notifier.go`), three
built-in channel implementations — SMTP email (`email.go`), Slack incoming
webhook (`slack.go`), and a generic HTTP webhook with HMAC signing
(`webhook.go`) — and a concurrent fan-out dispatcher with per-channel
exponential-backoff retry (`Dispatch` in `notifier.go`).

The package is consumed from two places:

1. **Alert engine** (`internal/alerts/engine_notifiers.go`) — when an alert
   transitions, `AlertEngine.dispatchNotifications` resolves the target
   channels (routing table first, then the rule's own channels), filters
   them through per-user notification preferences, calls
   `notify.Dispatch`, and persists one `models.NotificationRecord` per
   channel outcome.
2. **REST API** (`internal/api/notifications.go`,
   `notifications_helpers.go`, routes in `routes_sub.go`) — CRUD for
   `/api/v1/notification-channels`, validated against the registry on
   create/update, plus a `/test` endpoint that sends a synthetic alert
   through a single channel.

All HTTP-based delivery (Slack and webhook) is protected by a two-layer
SSRF guard: config-time URL validation and a dial-time re-check of the
resolved IP, which together defeat DNS-rebinding attacks.

## User Story

**As** a platform operator,
**I want** alerts raised by the check pipeline to reach me on the channel I
already monitor — email, Slack, or an arbitrary HTTP webhook — with my own
delivery preferences honored,
**so that** I can respond to failing checks without watching the dashboard,
and I can verify a new channel works with a test notification before relying
on it.

---

## Requirements

### 1. Channel Model and Registry

1.1. A channel instance MUST be represented by `notify.NotificationChannel`
(`notifier.go`): `ID`, `OrgID`, optional `UserID`, `Name`, `Type`
(`"email"`, `"slack"`, `"webhook"` — constants `ChannelEmail`,
`ChannelSlack`, `ChannelWebhook`), `Enabled`, and a type-specific
`Config json.RawMessage` blob, plus `CreatedAt`/`UpdatedAt` timestamps.

1.2. Every channel implementation MUST satisfy the `Notifier` interface:
`Notify(ctx, alert *models.Alert, channel NotificationChannel) error` and
`ValidateConfig(config json.RawMessage) error`. `ValidateConfig` is called
on channel create/update and again immediately before each dispatch.

1.3. `NotifierRegistry` MUST map channel-type strings to `Notifier`
implementations behind a `sync.RWMutex`, exposing `Register`, `Get`, and
`SupportedTypes`. `InitDefaultRegistry()` MUST return a registry
pre-populated with `EmailNotifier`, `SlackNotifier`, and
`WebhookNotifier`; additional providers (e.g. PagerDuty) MUST be addable
via `Register` without touching the dispatcher.

### 2. Fan-Out Dispatch with Retry

2.1. `notify.Dispatch` (`notifier.go`) MUST deliver one alert to all
channels concurrently (one goroutine per channel, joined with a
`sync.WaitGroup`) and return one `DispatchResult` per channel in input
order (`ChannelID`, `ChannelType`, `Attempt`, `Status`, `Err`).

2.2. Disabled channels (`Enabled == false`) MUST be skipped without
invoking any notifier; their result MUST carry `Status: "skipped"` and an
error so callers can audit the skip.

2.3. Each channel MUST be retried independently up to
`MaxRetryAttempts = 3` total attempts with exponential backoff starting at
`BaseBackoff = 1s` and doubling between attempts (1s, then 2s). Channels
with no registered notifier or a config that fails `ValidateConfig` MUST
fail immediately without attempting delivery.

2.4. Every delivery attempt MUST run under its own sub-context bounded by
`DispatchTimeout = 30s`. Cancellation of the caller's context MUST abort
pending retries and all in-flight deliveries; the result MUST carry
`ctx.Err()`.

2.5. Delivery MUST be logged at `Info` level on success and `Warn` level
per failed attempt, including `alert_id`, `channel_id`, `channel_type`,
and `attempt`.

### 3. Email Channel (SMTP)

3.1. `EmailConfig` (`email.go`) MUST require a non-empty `Host`, a `Port`
in 1–65535, a non-empty `FromAddress`, and at least one `ToAddresses`
entry; `Validate()` MUST reject configs missing any of these.

3.2. The email notifier MUST support three transport modes selected by
config: implicit TLS (`UseTLS`, `tls.Dial` — SMTPS, typically port 465),
STARTTLS upgrade (`UseStartTLS` over a plaintext dial), and plaintext
SMTP. When `Username` is set, `smtp.PlainAuth` MUST be used.

3.3. The message MUST be a `multipart/alternative` MIME message
(`buildMIMEMessage`) containing both an HTML body rendered from
`emailHTMLTemplate` and a plain-text fallback from `emailTextTemplate`.

3.4. The email MUST include a severity color-coded header using
`emailSeverityColors` (info `#3b82f6`, warning `#f59e0b`, critical
`#ef4444`, emergency `#7f1d1d`; unknown severities gray `#6b7280`), the
alert metadata (severity, check, agent, state, RFC3339 UTC timestamp,
message), and a "View Alert" link to
`{PlatformURL}/alerts/{alert.ID}`. When `PlatformURL` is unset it MUST
default to `https://localhost:8443`.

3.5. The default subject MUST be `[<SEVERITY>] Alert: <CheckID>` when
`EmailConfig.Subject` is empty.

3.6. Delivery MUST be implemented on the Go standard library only
(`net/smtp`, `crypto/tls`) with no external SMTP dependencies.

### 4. Slack Channel

4.1. `SlackConfig` (`slack.go`) MUST require a `WebhookURL` with an
http(s) scheme that additionally passes the SSRF checks of requirement 6;
optional fields `Channel`, `Username`, `IconEmoji`, and `PlatformURL`
override delivery presentation.

4.2. The message MUST be posted as JSON containing a color-coded
attachment (`slackSeverityColor`: info blue, warning yellow, critical red,
emergency bright red, unknown gray) with a leading severity emoji
(`slackSeverityEmoji`: `:information_source:`, `:warning:`,
`:rotating_light:`, `:sos:`, fallback `:bell:`), fields for Check, Agent,
Severity, State, Timestamp, and Output, and a "View Alert" action button
linking to `{PlatformURL}/alerts/{alert.ID}` (default
`https://localhost:8443`).

4.3. The alert message body MUST be truncated to 500 characters in the
attachment text. The Agent field MUST use
`alert.Metadata["hostname"]` when present and non-empty, falling back to
`alert.AgentID`.

4.4. Any non-2xx response MUST be treated as delivery failure, with the
first 1024 bytes of the response body included in the returned error.
When no custom `HTTPClient` is configured, the notifier MUST use the
SSRF-hardened client from requirement 6 with a 10s timeout.

### 5. Generic Webhook Channel

5.1. `WebhookConfig` (`webhook.go`) MUST require an http(s) `URL`
passing the SSRF checks of requirement 6. `Method` MUST default to POST
and MUST be restricted to POST or PUT. `TimeoutSeconds` MUST be ≥ 0;
when unset, delivery MUST use a 10s timeout.

5.2. The request body MUST be rendered from `BodyTemplate` (Go
`text/template`, parse-validated at config time) or, when empty, from the
built-in JSON payload `defaultWebhookBody` containing alert_id, severity,
state, check_id, agent_id, message, timestamp, platform_url, and
alert_url. Templates MUST have access to a `quote` function that
JSON-encodes strings.

5.3. When `Secret` is configured, the exact rendered body MUST be signed
with HMAC-SHA256 and published in the `X-OAP-Signature` header as
`sha256=<lowercase hex>` (`signHMAC`). Requests MUST always carry
`User-Agent: OpenAgentPlatform-Webhook/1.0` and default
`Content-Type: application/json` (overridable via `Headers`).

5.4. Any non-2xx response MUST fail the attempt with the first 1024 bytes
of the response body in the error.

### 6. SSRF Protection

6.1. `validateWebhookURL` (`webhook.go`) MUST reject webhook/Slack URLs
whose host is a blocked hostname (`isBlockedHostname`: `localhost`,
`localhost.localdomain`, `ip6-localhost`, `ip6-loopback`, `metadata`,
`metadata.google.internal`), an IP literal in a blocked range, or a
hostname that resolves to any blocked IP.

6.2. `isBlockedIP` MUST block loopback, link-local unicast/multicast,
interface-local multicast, private (RFC1918/ULA), unspecified addresses,
and the cloud instance-metadata endpoint `169.254.169.254`.

6.3. Hostnames that fail DNS resolution at config time MUST be allowed
through validation; the authoritative guard is `webhookDialContext`, which
MUST re-resolve the host at connect time and refuse the dial if the
resolved IP is blocked — defeating DNS-rebinding between validation and
delivery.

6.4. The dial-time check MUST verify ALL resolved IPs, not only the first,
to prevent multi-record bypass (first record public, later records
private). `webhookHTTPClient` MUST install this dialer on the transport
for both the webhook and Slack notifiers.

### 7. Alert Engine Integration

7.1. `AlertEngine.dispatchNotifications`
(`internal/alerts/engine_notifiers.go`) MUST be invoked on alert
transitions (open, escalation, and recovery paths in
`engine_handlers.go`). Alerts without an `AlertRuleID` MUST be skipped
with a debug log, since channel configuration hangs off the rule.

7.2. Channel resolution (`resolveChannels`) MUST consult the routing
engine first when one is configured (`router.Route` with org, agent,
site, check, severity context), falling back to the rule's own
`notify_channels` when routing produces no set or errors.

7.3. Per-user preferences (`applyPreferences`) MUST then filter channels:
org-wide channels (`UserID == ""`) always pass through; user-owned
channels MUST be dropped when the channel-type toggle is off or the
preference evaluation (quiet hours, severity threshold, mute) says not to
notify. Preference load failures MUST be permissive (deliver anyway).

7.4. One `models.NotificationRecord` per channel result MUST be persisted
via `InsertNotificationRecord` with status `sent`/`failed`, the error
message, and `SentAt` on success.

### 8. Channel Management API

8.1. The API MUST expose under `/api/v1/notification-channels`
(`internal/api/routes_sub.go`):

| Method | Path | Purpose | Required Role |
|--------|------|---------|---------------|
| GET | `/` | List channels visible to the user (org-wide + own) | authenticated |
| POST | `/` | Create channel | admin, technician |
| GET | `/{id}` | Get channel (org membership enforced) | authenticated |
| PUT | `/{id}` | Update name/enabled/config | admin, technician |
| DELETE | `/{id}` | Delete channel | admin, technician |
| POST | `/{id}/test` | Send synthetic test alert through the channel | admin, technician |

8.2. Create and update MUST validate the type-specific config by calling
the registered notifier's `ValidateConfig` before persisting; unknown
channel types MUST be rejected with 400.

8.3. The `/test` endpoint MUST dispatch a synthetic alert (ID
`test-<channel id>`, severity `info`, message clearly marked as a test)
through `notify.Dispatch` with a deadline of
`DispatchTimeout × (MaxRetryAttempts + 1)`, persist a notification record
for audit, and return 502 with the error when delivery fails.

## Known Limitations

- **Alert-driven dispatch is not wired in production.**
  `cmd/server/server_init.go` constructs the alert engine without a
  `NotifierRegistry` (`alerts.Config.NotifierRegistry` unset), and
  `apiServer.SetNotifierRegistry` is never called anywhere. Because
  `dispatchNotifications` returns immediately when the registry is nil,
  no alert-triggered notification is actually sent by the production
  server despite the engine code and tests being complete. Channel CRUD
  still works because the API falls back to `notify.InitDefaultRegistry()`
  for config validation, but the `/test` endpoint returns 503
  (`notifier_registry_not_configured`) because it requires the explicitly
  wired registry.
- `EmailNotifier` declares `Content-Transfer-Encoding: quoted-printable`
  in the MIME parts but writes the bodies unencoded, and uses a fixed
  boundary string (`oap-boundary`).
- SMTP delivery runs in a goroutine that is abandoned on context
  cancellation (`sendMail`); the connection lingers until it times out
  naturally.
- `SlackNotifier` posts legacy message `attachments` despite code
  comments describing the payload as Block Kit.
- `WebhookConfig.MaxRetries` is accepted in config but ignored — retry
  policy is owned exclusively by `notify.Dispatch`.
- `validateWebhookURL` performs its config-time DNS lookup with the
  default resolver and no timeout; a slow resolver delays channel
  create/update.
