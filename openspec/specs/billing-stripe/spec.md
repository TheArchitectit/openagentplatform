# Stripe Billing

> **Phase:** 6 (Commercial Tiering)
> **STATUS: COMPLETE**
> **Source:** `docs/QA_REVIEW_OPENSPEC_COVERAGE.md` §4 (previously unspec'd);
> `docs/plans/MASTER_IMPLEMENTATION_PLAN.md` §4.5
> **App Path:** `internal/billing/` (client.go, billing.go, metering.go),
> `internal/api/billing.go` (handlers), `internal/api/routes_sub.go` +
> `internal/api/routes_routes.go` (routes), `cmd/server/server_init_a2a.go` (wiring)
>
> New spec authored 2026-08-23 alongside the client.go/stripe.go merge
> (commit 66921fa). Companion specs: `commercial-licensing` (offline license
> keys + feature gating), which shares the tier definitions defined here.

---

## Description

Stripe Billing is the online monetization engine for OpenAgentPlatform's
commercial tiers. It complements the offline licensing system: license keys
(Ed25519-signed, verified locally by `internal/license`) gate features with no
network dependency, while Stripe subscriptions handle payment, tier changes,
invoices, and usage-based metering.

The capability has three layers:

1. **StripeClient** (`client.go`) — a thin wrapper over stripe-go v81 for
   customers, subscriptions, invoices, and webhook verification.
2. **BillingService** (`billing.go`) — the application-facing façade that maps
   OAP tiers to Stripe price IDs and caches per-org subscription state.
3. **MeteringService** (`metering.go`) — aggregates per-org usage records and
   reports them to Stripe as billing meter events on an hourly flush cycle.

Tier quotas are intentionally mirrored between this package's `TierCatalog`
and `internal/license/license.go` so offline enforcement and online billing
report identical limits to operators.

## User Story

**As** an operator running OpenAgentPlatform commercially,
**I want** organisations to subscribe, upgrade, downgrade, and cancel through
Stripe, with their usage metered and their tier state kept current,
**so that** billing reflects real entitlements without manual reconciliation —
and deployments that never configure Stripe lose nothing but the paid tiers.

---

## Requirements

### 1. Configuration and Secrets

1.1. The Stripe secret key MUST be read exclusively from the `STRIPE_SECRET_KEY`
environment variable. It MUST NOT be hardcoded, logged, or committed.
(`EnvStripeSecretKey` constant; enforced in `NewStripeClient`, which returns
`ErrSecretKeyMissing` when absent.)

1.2. Price IDs for the Professional and Enterprise tiers MUST come from
`STRIPE_PRO_PRICE_ID` and `STRIPE_ENT_PRICE_ID`. They are non-secret but
deployment-specific, so they are environment-resolved via `PriceIDs()`.
Missing values yield `ErrPriceIDNotResolved`.

1.3. The webhook signing secret MUST be read from `STRIPE_WEBHOOK_SECRET` at
verification time, never embedded in source.

1.4. All env-var names MUST be declared once as constants
(`EnvStripeSecretKey`, `EnvProPriceID`, `EnvEnterprisePrice`,
`EnvWebhookSecret`) rather than scattered string literals.

1.5. Billing is OPTIONAL at runtime: if `STRIPE_SECRET_KEY` is unset, server
startup logs "billing endpoints disabled" and continues — the API handlers
return 503 `billing_unavailable`. A missing key MUST NOT be fatal despite the
client constructor treating absence as an error condition for callers that
require billing.

### 2. StripeClient (client.go)

2.1. The client MUST support customer lifecycle: `CreateCustomer` (tagged with
`oap_org_id` metadata so Stripe objects can be traced back to an org),
`GetCustomer`.

2.2. The client MUST support subscription lifecycle:
- `CreateSubscription(customerID, priceID)` — single-item subscription.
- `UpdateSubscription(subscriptionID, newPriceID)` — MUST fetch the current
  subscription first and pass the existing item ID, so a tier swap edits the
  item instead of appending a second one.
- `CancelSubscription(subscriptionID)` — MUST cancel **at period end**
  (`cancel_at_period_end = true`) so service persists through the paid cycle.
  (Immediate cancellation is a bug; do not reintroduce it.)
- `GetSubscription(subscriptionID)`.

2.3. `ListInvoices(customerID, limit)` MUST list real invoices via the Stripe
API, clamping `limit` to 1..100 with a default of 20. An empty-list stub MUST
NOT be substituted.

2.4. `VerifyWebhook(payload, signatureHeader)` MUST validate the
`Stripe-Signature` header against `STRIPE_WEBHOOK_SECRET` and return the parsed
event; invalid signatures MUST be rejected (handler responds 400).

2.5. `SyncInterval` MUST be 15 minutes — the cadence of the subscription sync
loop (§3.6).

### 3. BillingService (billing.go)

3.1. `TierCatalog` MUST define quota limits per tier and stay aligned with
`internal/license`:

| Tier | MaxAgents | MaxUsers | MaxSites | MonthlyPrice |
|------|-----------|----------|----------|--------------|
| Community | 10 | 2 | 1 | $0 |
| Professional | 100 | 10 | 5 | $99 |
| Enterprise | unlimited | unlimited | unlimited | $499 |

3.2. Per-org billing state (`OrgBillingState`: Stripe customer ID,
subscription ID, price ID, tier, status, current period end) MUST be guarded
by a mutex. Current implementation holds state **in memory** — persistence to
PostgreSQL is a known limitation (see §7).

3.3. `CreateSubscription(orgID, tier)` MUST resolve the tier→price mapping via
`priceIDForTier` (Community is free with no price ID), require an existing
customer (`ErrNoCustomer` otherwise), and reject unknown tiers
(`ErrUnknownTier`).

3.4. `UpdateSubscription` / `CancelSubscription` MUST require an active
subscription and refresh cached status + period end from the Stripe response.

3.5. Cancellation semantics at the service layer MUST be period-end (matching
§2.2); the handler documents "cancels at period end".

3.6. `StartSyncLoop(ctx)` MUST poll every `SyncInterval` (15m) for every known
org, refreshing status/price/period-end from Stripe, logging sync failures
per-org without halting the loop, and stopping cleanly on context cancel.

3.7. Subscription status strings MUST pass through Stripe's own values
(`active`, `past_due`, `canceled`, …) — not a local re-encoding.

### 4. MeteringService (metering.go)

4.1. Supported metrics MUST be the named constants: `agent_count_days`,
`a2a_task_count`, `api_call_count` — matching meter-event names configured in
the Stripe dashboard. Unknown metrics MUST be rejected at record time.

4.2. `RecordUsage(orgID, metric, quantity)` MUST buffer records in memory;
buffering failures other than unknown-metric MUST NOT lose the event silently.

4.3. `Flush(ctx)` MUST aggregate pending records into one meter event per
(org, metric) pair, send them via the Stripe Billing meter-events API with
payload `{oap_org_id, value}`, and re-queue any events Stripe rejected for the
next flush.

4.4. `StartFlushLoop(ctx)` MUST flush every `MeterReportInterval` (1 hour) and
stop cleanly on context cancel.

### 5. HTTP API (internal/api/billing.go)

All routes mount under `/api/v1/billing` and REQUIRE the admin role
(`auth.RequireRole(auth.RoleAdmin)`). Every handler returns 503
`billing_unavailable` when Stripe is not wired.

5.1. Route table:

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| POST | `/billing/create-customer` | handleCreateCustomer | provisions Stripe customer for org |
| POST | `/billing/create-subscription` | handleCreateSubscription | body `{tier}`; validates against TierCatalog |
| GET | `/billing/subscription` | handleGetSubscription | cached state; 404 if none |
| POST | `/billing/cancel` | handleCancelSubscription | period-end cancel |
| GET | `/billing/invoices` | handleGetInvoices | last 20 invoices |
| GET | `/billing/usage` | handleGetUsage | month-to-date usage summary |
| POST | `/webhook` | handleBillingWebhook | unauthenticated route; signature IS the auth |

5.2. Org identity MUST derive from the authenticated request context
(`billingOrgFromRequest`), never from client-supplied body fields — an admin
can only manage their own org's billing.

5.3. The webhook endpoint mounts WITHOUT session auth (Stripe cannot
authenticate) and MUST therefore always verify the `Stripe-Signature` header
via §2.4 before processing. Missing signature → 400; failed verification →
400; success → 200 `{received: true, event_id}` within Stripe's 30-second
acknowledgement window. Currently it acknowledges only; event-type dispatch
(reaction to `invoice.payment_failed`, `customer.subscription.deleted`, …)
is a known gap (see §7).

5.4. Error responses MUST use the platform's standard JSON error envelope
with machine-readable codes (`unknown_tier`, `signature_verification_failed`,
`create_subscription_failed`, …), not raw Stripe errors — internal error
detail MUST NOT leak to clients.

### 6. Server Wiring (cmd/server/server_init_a2a.go)

6.1. `wireSupportServices` MUST construct the Stripe client first; only on
success construct BillingService + MeteringService and inject all three into
the API server (`SetBilling`), enabling the routes.

6.2. When Stripe is absent, wiring MUST log the disabled state and continue
boot — billing failure must never take down RMM/A2A functionality.

### 7. Known Limitations (honest-state notes; not COMPLETE claims)

7.1. **In-memory state** — `OrgBillingState` does not survive restarts; a
restart orphans the org↔customer mapping until re-created. Persistence
(PostgreSQL, likely under `internal/tenancy` schemas) is required before
multi-replica deployment.

7.2. **Webhook ack-only** — no event-type dispatch; subscription state relies
on the 15-minute poll rather than reacting to webhook events.

7.3. **No checkout flow** — subscriptions are created server-side by an
admin; there is no Stripe Checkout/Payment Element integration for
self-serve signup.

7.4. **Single currency/price assumption** — one price ID per tier; no
regional pricing or seat-based proration.

These limitations are tracked here so the spec stays truthful; flip them into
requirements when implemented.
