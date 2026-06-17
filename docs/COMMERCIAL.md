# Commercial

OpenAgentPlatform is source-available under the Business Source License
1.1. This document describes the licensing tiers, feature gating,
billing, and support options.

## Table of contents

1. [License overview](#license-overview)
2. [Tier comparison](#tier-comparison)
3. [Feature gating](#feature-gating)
4. [Stripe billing setup](#stripe-billing-setup)
5. [Enterprise SSO](#enterprise-sso)
6. [Support SLAs](#support-slas)
7. [FAQ](#faq)

---

## License overview

OpenAgentPlatform uses the **Business Source License 1.1 (BSL 1.1)**.

### What BSL 1.1 means

- **You can**: read, modify, and self-host OAP for non-production use
  (development, testing, evaluation)
- **You can**: contribute changes back to the project under the BSL
- **You cannot**: use OAP in production above the free tier limits
  without a commercial license
- **After 4 years** from each release, that release converts to
  **Apache License 2.0**

The full BSL text is at [LICENSE](LICENSE) or at
https://mariadb.com/bsl11/.

### Free tier (BSL)

| Limit                          | Free tier     |
|--------------------------------|---------------|
| Agents                         | Up to 10      |
| Checks per agent               | Up to 20      |
| Alert rules                    | Up to 50      |
| Script executions per month    | Up to 500     |
| Remote shell sessions          | Not included  |
| LLM agents                     | Not included  |
| Compliance policies            | Up to 5       |
| Audit log retention            | 30 days       |
| Support                        | Community (Discord) |

If your deployment exceeds any limit, you must upgrade to a commercial
license or reduce your usage.

---

## Tier comparison

| Feature                          | Free (BSL)     | Pro             | Enterprise     |
|----------------------------------|----------------|-----------------|----------------|
| **Agents**                       | 10             | 200             | Unlimited      |
| **Checks per agent**            | 20             | 100             | Unlimited      |
| **Alert rules**                  | 50             | 500             | Unlimited      |
| **Script executions / month**   | 500            | 10,000          | Unlimited      |
| **Remote shell sessions**       | -              | Yes             | Yes            |
| **Session recording**           | -              | 30 days         | 1 year         |
| **LLM agents (Ozore AI)**      | -              | Yes             | Yes            |
| **A2A framework adapters**      | 1 (Anthropic)  | 4 (Anthropic, OpenAI, AutoGen, CrewAI) | All 6 (incl. LangGraph, Semantic Kernel) |
| **Compliance policies**         | 5              | 50              | Unlimited      |
| **Custom roles**                | -              | -               | Yes            |
| **SSO (SAML / OIDC)**          | -              | -               | Yes            |
| **Audit log retention**         | 30 days        | 90 days         | 1 year         |
| **SIEM webhook**                | -              | Yes             | Yes            |
| **API rate limit**              | 100 req/min    | 1000 req/min    | Custom         |
| **Uptime SLA**                  | None           | 99.9%           | 99.99%         |
| **Support**                     | Community      | Email (24h)     | 24/7 phone     |
| **Price (annual)**              | Free           | $5,000/yr       | Contact sales  |

### Additional Enterprise features

- Multi-region deployment
- Custom data retention policies
- Dedicated support engineer
- Quarterly security review
- Custom SLA terms
- On-premise deployment consulting
- Source code escrow (optional)

---

## Feature gating

Feature gates are enforced server-side based on the active license key.

### How it works

1. The server reads the license key from `OAP_LICENSE_KEY` (or
   `LICENSE_FILE` for offline validation).
2. The license is validated against the OAP license server (or
   verified offline with a signed JWT).
3. The license specifies the tier, expiration, agent limit, and
   enabled features.
4. Server endpoints check the applicable feature gate before
   processing requests.

### Example: agent limit check

```go
// Pseudocode
func RegisterAgent(w http.ResponseWriter, r *http.Request) {
    count := CountAgents(org_id)
    limit := License.AgentLimit()
    if count >= limit {
        http.Error(w, "agent limit reached; upgrade your license", http.StatusPaymentRequired)
        return
    }
    // ... create agent
}
```

### Example: feature flag check

```go
func StartShellSession(w http.ResponseWriter, r *http.Request) {
    if !License.HasFeature("remote_shell") {
        http.Error(w, "remote shell requires Pro tier", http.StatusPaymentRequired)
        return
    }
    // ... start session
}
```

### Over-limit behavior

When a limit is reached, the API returns HTTP 402 (Payment Required)
with a JSON body indicating the exceeded limit and the upgrade URL:

```json
{
  "error": "limit_exceeded",
  "limit": "agents",
  "current": 10,
  "max": 10,
  "upgrade_url": "https://openagentplatform.io/upgrade"
}
```

### License validation

- **Online**: server calls the OAP license API every 6 hours to
  validate. Requires `OAP_LICENSE_API_URL` to be reachable.
- **Offline**: license is a signed JWT that includes tier, limits,
  and expiration. Validated locally; no network call required.
  Suitable for air-gapped deployments.

### Setting a license

Environment variable:

```bash
OAP_LICENSE_KEY=oap-pro-XXXX-XXXX-XXXX
```

Or file (for long keys):

```bash
OAP_LICENSE_FILE=/etc/oap/license.jwt
```

To obtain a license key, visit https://openagentplatform.io/pricing
or contact sales@openagentplatform.io.

---

## Stripe billing setup

The platform integrates with Stripe for subscription billing on the
Pro and Enterprise tiers.

### Prerequisites

- Stripe account (https://stripe.com)
- Two products created in Stripe: Pro and Enterprise (annual)
- API keys with read/write access

### Setup

1. In Stripe, create two products:
   - **OpenAgentPlatform Pro** -- $5,000/year
   - **OpenAgentPlatform Enterprise** -- contact sales (use quote)

2. Create prices for each product. Note the price IDs (e.g. `price_1234`).

3. In your OAP deployment, set:

```bash
STRIPE_SECRET_KEY=sk_live_...
STRIPE_PUBLISHABLE_KEY=pk_live_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_ID_PRO=price_1234
STRIPE_PRICE_ID_ENTERPRISE=price_5678
```

4. Configure a webhook in Stripe:
   - URL: `https://your-domain.com/api/v1/billing/webhook`
   - Events: `customer.subscription.created`,
     `customer.subscription.updated`,
     `customer.subscription.deleted`,
     `invoice.payment_succeeded`,
     `invoice.payment_failed`

5. Use the Stripe CLI to test locally:

```bash
stripe listen --forward-to localhost:8080/api/v1/billing/webhook
```

### Billing flow

```
┌────────┐  1. Click Upgrade  ┌────────┐  2. Create session  ┌────────┐
│ User   │ ─────────────────▶│ OAP    │ ──────────────────▶│ Stripe │
│        │                   │ server │                     │        │
│        │◀── 3. Redirect ──│        │◀── 4. Session URL ──│        │
│        │                   │        │                     │        │
│        │  5. Enter card    │        │                     │        │
│        │ ────────────────────────────────────────────────▶│        │
│        │                   │        │                     │        │
│        │                   │        │  6. Webhook         │        │
│        │                   │        │◀────────────────────│        │
│        │                   │        │  7. Activate tier   │        │
│        │                   │        │                     │        │
│        │  8. Confirmation  │        │                     │        │
│        │◀─────────────────│        │                     │        │
└────────┘                   └────────┘                     └────────┘
```

### Webhook handling

The OAP server processes Stripe webhooks:

| Event                              | Action                          |
|------------------------------------|---------------------------------|
| `customer.subscription.created`    | Activate subscription           |
| `customer.subscription.updated`    | Update tier or renewal date     |
| `customer.subscription.deleted`    | Downgrade to free tier          |
| `invoice.payment_succeeded`        | Extend license expiration       |
| `invoice.payment_failed`          | Notify user, grace period       |

Webhook signatures are verified using `STRIPE_WEBHOOK_SECRET`.

### Customer portal

Users can manage their subscription via the Stripe Customer Portal.
Link from the OAP dashboard:

```
https://your-domain.com/settings/billing
```

The portal allows users to update payment methods, change tiers, and
cancel subscriptions.

### Failed payments

- **3 days after failure**: email notification to user
- **7 days**: in-app banner
- **14 days**: downgrade to free tier
- **30 days**: subscription cancelled

---

## Enterprise SSO

Enterprise customers can configure SSO via SAML 2.0 or OIDC.

### OIDC SSO

Most common for modern identity providers (Okta, Auth0, Google Workspace).

Configuration in OAP:

```bash
SSO_ENABLED=true
SSO_OIDC_ISSUER=https://your-tenant.okta.com
SSO_OIDC_CLIENT_ID=oap-production
SSO_OIDC_CLIENT_SECRET=...
SSO_OIDC_REDIRECT_URI=https://oap.your-domain.com/auth/callback
```

Users sign in via your IdP; OAP validates the ID token and creates a
session. JIT (Just-In-Time) provisioning creates user accounts on
first login.

### SAML SSO

For SAML 2.0 (e.g. ADFS, Ping Identity):

```bash
SSO_ENABLED=true
SSO_SAML_METADATA_URL=https://idp.your-domain.com/metadata.xml
SSO_SAML_ENTITY_ID=https://oap.your-domain.com
SSO_SAML_ACS_URL=https://oap.your-domain.com/auth/saml/acs
```

The OAP server fetches the IdP metadata, validates SAML responses,
and extracts user attributes (email, name, groups).

### Group-based role mapping

Map IdP groups to OAP roles:

```bash
SSO_GROUP_MAPPING={"admins":"admin","ops-team":"operator","all-staff":"viewer"}
```

When a user logs in, their IdP groups are matched against the mapping
and the corresponding OAP role is assigned. The highest-privilege
role wins.

### Domain restriction

Restrict SSO to specific email domains:

```bash
SSO_ALLOWED_DOMAINS=your-company.com,subsidiary.com
```

Users with other domains cannot sign in via SSO and are blocked from
the platform.

### SCIM provisioning (optional)

Automatically provision and de-provision users via SCIM 2.0:

```bash
SCIM_ENABLED=true
SCIM_TOKEN=...
```

The OAP SCIM endpoint is at `/api/v1/scim/v2/`.

---

## Support SLAs

| Tier        | Channels              | Response time  | Resolution time  | Hours         |
|-------------|-----------------------|----------------|------------------|---------------|
| Free        | Discord, GitHub       | Best effort    | Best effort      | Community     |
| Pro         | Email, Discord        | 24 business    | 5 business days  | 9-5 M-F       |
| Enterprise  | Email, phone, Slack   | 1 hour         | 4 hours (P1)     | 24/7          |

### Severity levels

| Severity | Description                                  | Enterprise response |
|----------|----------------------------------------------|---------------------|
| P1       | Platform down; production data loss risk     | 1 hour              |
| P2       | Major feature broken; workaround available   | 4 hours             |
| P3       | Minor issue; non-critical                   | 1 business day      |
| P4       | Cosmetic; question; enhancement request     | 5 business days     |

### How to get support

- **Email**: support@openagentplatform.io
- **Enterprise phone**: provided in onboarding
- **Enterprise Slack**: shared channel
- **Status page**: https://status.openagentplatform.io

### Onboarding

Enterprise customers receive:

- 2-hour kickoff call
- Architecture review
- Custom deployment plan
- Security questionnaire assistance
- 30 days of post-launch support included
- Quarterly business reviews

---

## FAQ

### Can I use OAP for free in production?

Only if you stay within the free tier limits (10 agents, 500 script
executions/month, etc.). Above those limits, a commercial license is
required.

### What happens when my license expires?

- **Pro**: 14-day grace period, then downgrade to free tier
- **Enterprise**: 30-day grace period, then downgrade to free tier
- Agents above the free tier limit are marked offline but not deleted
- Data is retained; reactivation restores full access

### Can I move from Pro to Enterprise mid-year?

Yes. Pro-rated credit applies to the Enterprise annual fee. Contact
sales@openagentplatform.io for a quote.

### Do you offer non-profit or educational pricing?

Yes -- 50% discount for verified non-profits, students, and educational
institutions. Contact sales@openagentplatform.io.

### Can I get a refund?

- 30-day money-back guarantee on annual plans
- Pro-rated refunds for annual plans cancelled after 30 days
- No refunds for monthly plans

### Is there a self-hosted Enterprise option?

Yes -- Enterprise can be licensed for self-hosted deployments. Pricing
is per-agent per-year. Contact sales.

### How is the open-source community supported?

The BSL ensures the source is always readable. Community support is
via Discord and GitHub Issues. Bug fixes and security patches are
backported to the last 3 minor releases.

### Can I contribute code under the BSL?

Yes. Contributions are accepted under the project's CLA and released
under the BSL. After 4 years, contributions become Apache 2.0.

### What about source code escrow?

Source code escrow is available for Enterprise customers. We use the
Iron Mountain SaaS escrow service. Contact sales for details.

---

## Related documents

- [LICENSE](LICENSE) -- full BSL 1.1 text
- [SECURITY.md](SECURITY.md) -- security model
- [DEPLOYMENT.md](DEPLOYMENT.md) -- production deployment
- [SETUP.md](SETUP.md) -- local setup with Stripe config
