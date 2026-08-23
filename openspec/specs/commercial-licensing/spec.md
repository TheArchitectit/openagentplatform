# Commercial Licensing

> **Phase:** 5 (Production) / 6 (Commercial)
> **STATUS: COMPLETE**
> **Source:** `docs/architecture/ROADMAP_AND_SPRINTS.md` §5 (Phase 6), §8 (O7), §9 (R7)
> **License:** BSL 1.1

---

## Description

OpenAgentPlatform is licensed under the Business Source License 1.1. Source is
public and freely usable within the license's stated grant; competing hosted
offerings are restricted until the change date, after which the code converts to
an open-source license.

The commercial tier adds capabilities rather than removing them: feature gating
via Ed25519-signed license files, multi-tenancy with hard data isolation, a
managed A2A relay service, enterprise reporting, and Stripe-backed billing.

Two principles govern the gating design. First, licensing is a **business
control, not a security control** — it MUST NOT be the mechanism protecting
tenant data, and it MUST NOT be able to disable safety or audit functionality.
Second, license validation failure MUST degrade gracefully. A network partition
to a license server must never take a customer's monitoring platform offline;
that would make the licensing system the least reliable component in a product
whose entire value is reliability.

## User Story

**As** the platform vendor,
**I want** commercial features gated behind cryptographically verifiable
licenses, tenants isolated at the data layer, and usage metered into billing,
**so that** I can offer a supported commercial tier — while a customer whose
license server is briefly unreachable keeps monitoring their fleet without
interruption.

---

## Requirements

### 1. BSL 1.1 Licensing

1.1. A `LICENSE` file containing the Business Source License 1.1 text MUST be
present at the repository root.

1.2. The license MUST state the Licensor, Change Date, Change License, and any
Additional Use Grant.

1.3. A contributor license agreement MUST be established and required for
external contributions.

1.4. Source file headers MUST identify the license consistently.

1.5. Third-party dependency licenses MUST be inventoried, and license
compatibility MUST be checked in CI.

### 2. License File Format and Validation

2.1. License files MUST be signed with Ed25519.

2.2. A license MUST encode: licensed entity, tier, enabled features, endpoint
count limit, issue date, and expiry date.

2.3. Signature verification MUST use a public key embedded in the binary; the
private key MUST NEVER ship with the product.

2.4. An invalid signature MUST cause the license to be rejected entirely — a
partially trusted license MUST NOT be honored.

2.5. Validation MUST be tamper-evident: modifying any field MUST invalidate the
signature.

### 3. Feature Gating

3.1. Commercial features MUST be gated on validated license entitlements.

3.2. Gating MUST be enforced server-side. Hiding a feature in the UI MUST NOT be
the only control.

3.3. A gated feature accessed without entitlement MUST return a clear,
actionable error naming the required tier — not a generic failure.

3.4. Gating MUST NOT be able to disable security, audit, or data-integrity
functionality. Licensing controls commercial capability only.

3.5. Endpoint count limits MUST be enforced with a clear message when exceeded,
and MUST NOT silently stop monitoring already-enrolled endpoints.

### 4. License Validation Failure Behavior

4.1. Validation MUST support an offline grace period (open question O7 —
resolution required before implementation).

4.2. Inability to reach a license server MUST NOT disable core monitoring,
alerting, or endpoint management.

4.3. Expiry or validation failure MUST warn operators well in advance and
escalate visibly, rather than failing abruptly.

4.4. On grace-period exhaustion, commercial features MUST degrade to the
open-source feature set — the platform MUST NOT shut down.

4.5. License state MUST be observable so operators can see entitlements, expiry,
and grace status without contacting support.

### 5. Multi-Tenancy

5.1. A tenant model MUST be implemented, with every tenant-scoped record
attributable to exactly one tenant.

5.2. Data isolation MUST be enforced at the data layer via PostgreSQL Row Level
Security, not by application filtering alone (open question O2: RLS versus
schema-per-tenant).

5.3. Every query path MUST have an integration test asserting cross-tenant
access is impossible, per risk R7.

5.4. Tenant isolation MUST be verified by quarterly security audit.

5.5. A tenant identifier MUST be present in token claims and propagated through
all service calls.

5.6. Tenant isolation MUST NOT depend on license validation — isolation is a
security control and MUST hold regardless of license state.

### 6. Managed A2A Relay Service

6.1. A managed A2A relay MUST allow agents in separate networks to communicate
without direct connectivity.

6.2. Relayed traffic MUST be authenticated on both legs; the relay MUST NOT be
an unauthenticated open forwarder.

6.3. The relay MUST enforce per-tenant isolation of relayed traffic.

6.4. Relay usage MUST be metered for billing.

6.5. The relay MUST NOT be able to read secret values transiting it.

### 7. Enterprise Reporting

7.1. Report templates MUST be provided for compliance, patch status, alert
history, and endpoint inventory.

7.2. Scheduled report delivery MUST be supported.

7.3. Reports MUST respect the requesting user's tenant scope and RBAC
permissions — a report MUST NOT become a data-exfiltration path around
authorization.

7.4. Export formats MUST include at minimum PDF and CSV.

### 8. Billing Integration

8.1. Stripe Billing MUST be integrated.

8.2. Metered usage MUST be reported accurately, with endpoint count as the
primary metric.

8.3. Webhook handlers MUST verify Stripe signatures before acting.

8.4. Webhook handling MUST be idempotent — Stripe retries MUST NOT double-charge
or double-provision.

8.5. Billing failures MUST NOT immediately disable a customer's platform;
dunning MUST proceed through notification before any entitlement change.

8.6. Payment credentials MUST NEVER touch platform storage; Stripe-hosted flows
MUST handle card data.

### 9. Audit and Compliance

9.1. License validation events, entitlement changes, and tier changes MUST be
audited.

9.2. Tenant provisioning and deprovisioning MUST be audited.

9.3. Deprovisioning MUST have a defined data-retention and deletion policy.

### 10. Testing Requirements

10.1. Tests MUST cover: valid license accepted, tampered license rejected,
expired license handled, missing license degrades correctly, and endpoint limit
enforced.

10.2. Tests MUST assert that license failure does not disable core monitoring.

10.3. Multi-tenant isolation tests MUST be part of the standard CI suite, not a
manual pre-release check.
