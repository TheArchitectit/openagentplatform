# Notifications — Channel Dispatch

> **Phase:** 2 (Automation) — alert delivery path
> **STATUS: COMPLETE** — email / Slack / webhook channels, registry, retry, SSRF guard
> **Source:** authored 2026-08-25 from code (`internal/notify/`)
> **App Path:** `internal/notify/`
> **Source files:** `internal/notify/notifier.go`, `internal/notify/email.go`,
> `internal/notify/email_smtp.go`, `internal/notify/email_templates.go`,
> `internal/notify/slack.go`, `internal/notify/webhook.go`,
> `internal/notify/webhook_body.go`, `internal/notify/webhook_ssrf.go`

---

## Description

`internal/notify/` is the platform's notification dispatch system. It defines a
`Notifier` interface that every channel (email, Slack, webhook) satisfies, a
`NotifierRegistry` mapping channel-type strings to implementations, and a
`Dispatch` fan-out that delivers an alert to all configured channels concurrently
with exponential-backoff retry.

Three channels ship built-in: **email** (`email.go`, SMTP via `email_smtp.go`,
templates in `email_templates.go`), **Slack** (`slack.go`), and **webhook**
(`webhook.go`, `webhook_body.go`, SSRF guard `webhook_ssrf.go`). A webhook target
is subject to SSRF protection: only public, non-link-local, non-loopback
addresses are permitted (`webhook_ssrf_test.go` covers the rejection cases).

The system is consumed by the alert pipeline (`internal/alerts/`) after an
`Alert` transitions to a firing state. Delivery is best-effort and auditable:
every channel outcome is returned as a `DispatchResult`.

## User Story

**As** an on-call operator,
**I want** an alert to reach me on email, Slack, and a webhook simultaneously,
with retries if a channel is transiently down,
**so that** I never miss a critical event because one channel hiccuped.

---

## Requirements

### 1. Notifier Contract

1.1. `Notifier` exposes `Notify(ctx, *models.Alert, NotificationChannel) error`
and `ValidateConfig(json.RawMessage) error`. `ValidateConfig` is called on
channel create/update and on startup.

1.2. `NotificationChannel` is the serialised, validated config for one channel
instance: `ID`, `OrgID`, `UserID`, `Name`, `Type` (`"email"`/`"slack"`/`"webhook"`),
`Enabled`, and a type-specific `Config json.RawMessage` blob.

### 2. Registry

2.1. `NotifierRegistry` maps channel-type strings to `Notifier` implementations.
`Register`, `Get`, `SupportedTypes` are concurrency-safe (`sync.RWMutex`).

2.2. `InitDefaultRegistry()` pre-populates email, Slack, and webhook. Additional
channels (PagerDuty, OpsGenie) register via `Register` — the interface is the
extension point.

### 3. Dispatch & Retry

3.1. `Dispatch` delivers to all enabled channels **concurrently** (one goroutine
per channel, joined by `sync.WaitGroup`). The caller's context cancels all
in-flight deliveries.

3.2. Each channel retries up to `MaxRetryAttempts = 3` (initial + 2). Backoff is
exponential: `BaseBackoff = 1s`, then 2s, 4s
(`backoff = BaseBackoff * 2^(attempt-1)`).

3.3. Per-attempt timeout is `DispatchTimeout = 30s` via a fresh sub-context so a
hung channel cannot block the others.

3.4. Disabled channels return status `"skipped"`; channels with no registered
notifier or invalid config return status `"failed"` with `Err` set, so the
caller can audit rather than silently drop.

3.5. `DispatchResult` records `ChannelID`, `ChannelType`, `Attempt`, `Status`
(`"sent"`/`"failed"`/`"skipped"`), and `Err`.

### 4. Webhook SSRF Guard

4.1. `webhook_ssrf.go` rejects loopback (`127.0.0.0/8`, `::1`), link-local
(`169.254.0.0/16`, `fe80::/10`), and private (`RFC 1918`) targets.

4.2. DNS resolution is performed and each resolved address is checked; the URL is
only delivered if **every** resolved address is public. This prevents an
attacker from pointing a webhook at the internal metadata endpoint
(`169.254.169.254`).

### 5. Email Channel

5.1. `EmailNotifier` sends via SMTP (`email_smtp.go`) using templates in
`email_templates.go`. Config carries SMTP host/port/credentials + sender.

### 6. Slack Channel

6.1. `SlackNotifier` posts to an incoming-webhook URL from config. The URL itself
is not SSRF-guarded (it is operator-supplied configuration, not user-triggered
input), but the payload is bounded.

---

## Known Limitations

- **Best-effort only.** `Dispatch` returns per-channel results but does not
  redeliver after the process exits; a channel that is down for >4s (3 attempts)
  is marked `failed` and dropped until the next alert.
- **Slack webhook URL is not SSRF-guarded** — it is operator config, not
  request-derived input, but it should be treated as trusted.
- **No rate limiting** across channels — a firing storm fans out to every
  configured channel unthrottled.

---

## Cross-References

- `internal/alerts/` — consumer; calls `Dispatch` on alert state transition
- `notifications` spec (top-level) — broader notification config domain
- `rmm-operations` §4 — maintenance windows suppress notification fan-out
- `observability-telemetry` — `oap_alert_transitions_total` recorded upstream