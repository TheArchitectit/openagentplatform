# Commercial & Licensing Architecture

## Overview

OpenAgentPlatform (OAP) is released under the **Business Source License 1.1 (BSL 1.1)**. This section explains the license choice, what it permits, what it prohibits, how the source code is organized, and how the commercial tiers are structured on top of the open-core codebase.

### BSL 1.1 Rationale

We evaluated four licenses before settling on BSL 1.1:

| License | Verdict | Reason |
|---|---|---|
| **AGPL v3** | Rejected | Too restrictive for enterprise procurement. Many Fortune 500 legal teams flag AGPL as a red flag because it requires source disclosure for any network-accessed derivative, including internal modifications. This blocks adoption at large managed-service-provider (MSP) and large-enterprise customers regardless of actual risk. |
| **Apache 2.0 + CLA** | Rejected | Apache 2.0 alone is the ideal permissiveness target, but it does not prevent hyperscalers from forking OAP and offering it as a managed service without contributing back. Adding a CLA on top adds administrative overhead (contributor agreement signing, tracking, corporate-clause handling) without solving the managed-service problem. |
| **Elastic License v2** | Rejected | Functionally similar to BSL, but its additional-use grant is less clearly scoped. The Elastic License prohibits "competing offerings" in language that is broader and less self-defining than BSL's "non-production use" grant. This creates more ambiguity for legitimate use cases (e.g., a customer building an internal integration tool that happens to use OAP APIs). |
| **SSPL v1** | Rejected | Not OSI-recognized, and its copyleft-by-saas effect is too aggressive: it requires the entire service stack (not just OAP) to be open-sourced if offered as a network-accessible service. This is a legal liability for downstream integrators. |
| **BSL 1.1 (chosen)** | Accepted | Provides a clear Additional Use Grant, an automatic conversion date, and a well-understood legal structure (derived from MPL 2.0's framework). BSL is OSI-recognized as a source-available license and is used in production by CockroachDB, Sentry, MariaDB, and others. |

**Why BSL strikes the right balance for OAP:**

- **Open for self-hosted use.** Any organization can run OAP on their own infrastructure, modify the source, and distribute modifications internally without paying us. This includes MSPs running OAP for a single client.
- **Protected against hyperscaler free-riding.** A cloud provider cannot take OAP, rebrand it, and offer it as a managed multi-tenant SaaS without a commercial agreement. This is the core protection: it prevents the "AWS forks Sentry" problem.
- **Automatic open-sourcing.** After the change date, all code converts to Apache 2.0. This gives the community a guaranteed long-term open outcome and removes the "vendor trap" concern.

---

## BSL 1.1 License Terms

### Plain-Language Summary

BSL 1.1 is structured as: take the Apache 2.0 license, add a "Additional Use Grant" limitation, and set a future change date. Before the change date, the code is source-available but not open-source. On the change date, the license automatically converts to Apache 2.0 with no further action required.

### Change Date

```
Change Date: 2030-06-15
```

Four years from the initial release. On this date, every file licensed under BSL 1.1 in the main repository automatically re-licenses to **Apache 2.0**. Subsequent versions of those files (i.e., commits after the change date) are contributed directly under Apache 2.0, with BSL 1.1 only applying to the historical snapshot. The change date can be extended for future releases at the project's discretion.

### Additional Use Grant

The official BSL 1.1 Additional Use Grant for OpenAgentPlatform reads:

> You may use the Licensed Work in production, including as a hosted service managed by you for a single third party ("Single-Tenant Deployment"), provided that:
> 1. You do not use the Licensed Work to provide a multi-tenant managed service where multiple unrelated organizations share the same OAP instance.
> 2. You do not exceed 250 monitored endpoints across all Single-Tenant Deployments of the Licensed Work operated by you or your affiliates.
> 3. You do not rebrand or white-label the Licensed Work as a competing commercial product.
>
> These limitations do not apply to development, testing, and non-production use.

The **250-endpoint cap** is the key threshold. Below it, a user can self-host OAP for free, including as a Single-Tenant Deployment for clients. Above it, a commercial license is required. This is enforced at runtime by the feature-gating system (see below), not at the legal-license level; the license terms describe the entitlement, and the software enforces it.

### What Users CAN Do (Without a Commercial License)

- Self-host OAP on their own infrastructure (bare metal, VMs, Kubernetes, on-premises).
- Run OAP as a Single-Tenant Deployment for a single client (e.g., an MSP hosting OAP for one customer).
- Modify the source code, build custom binaries, and distribute modifications internally.
- Develop plugins, custom agent adapters, and integrations.
- Use all community-tier features listed below.
- Monitor up to 250 endpoints total across all their OAP instances.

### What Users CANNOT Do (Without a Commercial License)

- Offer OAP as a multi-tenant SaaS where multiple unrelated customers share one OAP instance.
- Exceed 250 monitored endpoints.
- Use professional or enterprise tier features (multi-tenancy, managed A2A relay, enterprise reporting, RBAC, etc.) — these are gated and require a valid license.
- Rebrand OAP as a competing commercial product.
- Remove or alter the license headers, copyright notices, or telemetry/feature-gating checks (DMCA-style, enforced by the BSL terms).

---

## Code Boundary

### Open-Source in Main Repository

The main repository at `github.com/openagentplatform/openagentplatform` contains the community edition. All code under these paths is BSL 1.1 (converting to Apache 2.0 on 2030-06-15):

```
/
├── cmd/
│   ├── oap-server/              # Community binary entry point
│   └── oap-cli/                 # Community CLI
├── internal/
│   ├── api/                     # Public HTTP/gRPC API
│   ├── agents/                  # LLM agent runtime (community features)
│   ├── a2a/                     # A2A protocol implementation (local mode)
│   ├── alerting/                # Alerting engine (community features)
│   ├── authn/                   # Authentication (local users, OIDC)
│   ├── authz/                   # Authorization (basic RBAC)
│   ├── config/                  # Configuration loader
│   ├── db/                      # Database access
│   ├── endpoints/               # Endpoint management
│   ├── events/                  # Event bus (NATS client)
│   ├── license/                 # License validation engine (see below)
│   ├── llm/                     # LLM provider adapters
│   ├── policy/                  # Policy engine
│   ├── reporting/               # Reporting (community features)
│   ├── secrets/                 # Vault integration
│   └── telemetry/               # Metrics, traces, logs
├── pkg/                         # Public Go packages
├── web/                         # React/TypeScript frontend
├── deploy/                      # Docker, Helm, Terraform
├── docs/                        # Documentation
├── LICENSE                      # BSL 1.1 text
├── NOTICE                       # Attribution
└── go.mod
```

### Proprietary Under `internal/commercial/`

Code that is only available to paying customers lives in a separate directory with build tags:

```
internal/
├── commercial/                  # Build tag: //go:build commercial
│   ├── multitenancy/            # Multi-tenant data isolation
│   ├── relay/                   # Managed A2A relay client
│   ├── reporting/               # Enterprise reporting (PDF, HTML, scheduling)
│   ├── billing/                 # Stripe integration
│   ├── rbac/                    # Advanced RBAC, MFA, SSO
│   ├── marketplace/             # MCP marketplace
│   ├── adapters/                # Custom framework adapters
│   └── license/                 # License key generation, signing
```

Every file in `internal/commercial/` starts with:

```go
//go:build commercial

// Copyright 2026 OpenAgentPlatform. All rights reserved.
// This file is part of OpenAgentPlatform Enterprise Edition.
// Licensed under the OpenAgentPlatform Commercial License Agreement.
```

### Two Binaries

The build system produces two binaries:

| Binary | Build Command | Tag | Distribution |
|---|---|---|---|
| `oap-server` | `make build` | (none) | GitHub releases, Docker Hub, Helm chart |
| `oap-server-enterprise` | `make build-enterprise` | `commercial` | Customer portal download, private registry |

The Makefile target:

```makefile
# Community build
.PHONY: build
build:
	CGO_ENABLED=1 go build -tags='' -o bin/oap-server ./cmd/oap-server

# Enterprise build
.PHONY: build-enterprise
build-enterprise:
	CGO_ENABLED=1 go build -tags=commercial -o bin/oap-server-enterprise ./cmd/oap-server
	@echo "Enterprise binary built. Note: requires a valid license key to enable commercial features."

.PHONY: build-all
build-all: build build-enterprise
```

The Go build constraint `//go:build commercial` means that:

- The community binary (`oap-server`) compiles without the `internal/commercial/` tree. Any reference to a commercial package produces a compile error.
- The enterprise binary (`oap-server-enterprise`) includes everything. It detects at runtime whether a valid license key is present and enables the corresponding features.

This is enforced by the Go compiler, not by convention. A community binary cannot accidentally call into commercial code paths.

### Code Organization Convention

```go
// In a community file: internal/api/endpoints.go
package api

func (s *Server) ListEndpoints(ctx context.Context, req *ListRequest) (*ListResponse, error) {
    endpoints, err := s.store.ListEndpoints(ctx, req)
    if err != nil {
        return nil, err
    }
    return &ListResponse{Endpoints: endpoints}, nil
}

// In a community file that needs to delegate to a commercial extension: internal/api/policy.go
package api

// EnterprisePolicyHook is the hook into the commercial policy engine.
// It is nil in the community build (the symbol does not exist).
// The enterprise binary registers an implementation at init time.
var EnterprisePolicyHook func(ctx context.Context, req *PolicyRequest) (*PolicyResponse, error)
```

The community binary declares the hook variable. The enterprise binary, when built with `-tags=commercial`, has an `init()` in `internal/commercial/policy/init.go` that assigns the implementation:

```go
//go:build commercial

package policy

import (
    "github.com/openagentplatform/openagentplatform/internal/api"
    "github.com/openagentplatform/openagentplatform/internal/commercial/policy"
)

func init() {
    api.EnterprisePolicyHook = policy.Evaluate
}
```

This is the **extension hook pattern**: the community code defines the integration point as a variable, and the commercial code populates it at init time. The community binary is unaware of the implementation; the enterprise binary is the union of both.

---

## Feature Gating

The feature-gating system controls which capabilities are available based on the active license. It is implemented in `internal/license/`.

### Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  License Key    │────>│  Validator       │────>│  Feature Gate   │
│  (signed JSON)  │     │  (EdDSA verify)  │     │  (per-feature)  │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                         │
                                                         v
                                                  ┌─────────────────┐
                                                  │  Enforcement    │
                                                  │  - HTTP 402     │
                                                  │  - CLI errors   │
                                                  │  - gRPC codes   │
                                                  └─────────────────┘
```

### License Key Format

A license key is a base64url-encoded JSON object signed with EdDSA Ed25519:

```json
{
  "license_id": "lic_2xK9pQ3mNvR8wL5j",
  "customer_id": "cus_NwR7vT2b",
  "customer_name": "Acme MSP, Inc.",
  "tier": "professional",
  "issued_at": "2026-01-15T00:00:00Z",
  "expires_at": "2027-01-15T00:00:00Z",
  "max_endpoints": 10000,
  "max_agent_processes": 25,
  "features": {
    "multi_tenancy": true,
    "managed_relay": true,
    "enterprise_reporting": true,
    "rbac_advanced": true,
    "mfa": true,
    "sso": true,
    "mcp_marketplace": false,
    "custom_adapters": false,
    "air_gapped": false
  },
  "metadata": {
    "subscription_id": "sub_1NqR8xK2",
    "stripe_customer_id": "cus_NwR7vT2b"
  }
}
```

The JSON is canonicalized (sorted keys, no whitespace) and signed. The signature is appended:

```
base64url(payload_json) + "." + base64url(signature)
```

The full key is the concatenation: `oap_<base64url(payload)>.<base64url(signature)>`.

### EdDSA Ed25519 Signing

We use EdDSA Ed25519 for license key signatures because:

- It is fast (verification is sub-millisecond).
- The signatures are small (64 bytes).
- It is deterministic: signing the same payload twice produces the same signature, simplifying reproducibility tests.
- Public keys are small (32 bytes) and easy to embed in the binary.

The private signing key is held by the OAP licensing service. The corresponding public key is embedded in both the community and enterprise binaries:

```go
// internal/license/publickey.go
package license

// OAPLicensePublicKey is the Ed25519 public key used to verify license keys.
// This key is embedded in the binary and cannot be replaced by the user.
// Rotate only with a coordinated binary release.
var OAPLicensePublicKey = []byte{
    0x7c, 0x3a, 0x9f, 0x2b, 0xe1, 0x48, 0x5d, 0x6a,
    0x8b, 0xc4, 0x12, 0x9e, 0x3f, 0x77, 0x05, 0xa1,
    0x6b, 0x2c, 0x4d, 0x8e, 0xf9, 0x10, 0xa3, 0x55,
    0x7b, 0x1d, 0x88, 0xe2, 0x4c, 0x90, 0x3a, 0xb6,
}
```

### License Validator

```go
// internal/license/validator.go
package license

import (
    "crypto/ed25519"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"
)

var (
    ErrInvalidKeyFormat    = errors.New("license key format invalid")
    ErrInvalidSignature    = errors.New("license signature verification failed")
    ErrLicenseExpired      = errors.New("license has expired")
    ErrLicenseNotYetValid  = errors.New("license is not yet valid")
    ErrFeatureNotLicensed  = errors.New("feature not included in license")
    ErrEndpointLimitReached = errors.New("endpoint limit reached")
)

// License represents a validated OAP license.
type License struct {
    LicenseID         string            `json:"license_id"`
    CustomerID        string            `json:"customer_id"`
    CustomerName      string            `json:"customer_name"`
    Tier              string            `json:"tier"`
    IssuedAt          time.Time         `json:"issued_at"`
    ExpiresAt         time.Time         `json:"expires_at"`
    MaxEndpoints      int               `json:"max_endpoints"`
    MaxAgentProcesses int               `json:"max_agent_processes"`
    Features          map[string]bool   `json:"features"`
    Metadata          map[string]string `json:"metadata"`
}

// Validator validates license keys against the embedded public key.
type Validator struct {
    publicKey ed25519.PublicKey
    now       func() time.Time
}

func NewValidator() *Validator {
    return &Validator{
        publicKey: OAPLicensePublicKey,
        now:       time.Now,
    }
}

// ParseAndValidate parses a license key string and verifies its signature.
func (v *Validator) ParseAndValidate(key string) (*License, error) {
    if !strings.HasPrefix(key, "oap_") {
        return nil, fmt.Errorf("%w: missing oap_ prefix", ErrInvalidKeyFormat)
    }
    body := strings.TrimPrefix(key, "oap_")
    parts := strings.Split(body, ".")
    if len(parts) != 2 {
        return nil, fmt.Errorf("%w: expected payload.signature", ErrInvalidKeyFormat)
    }

    payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
    if err != nil {
        return nil, fmt.Errorf("%w: payload decode: %v", ErrInvalidKeyFormat, err)
    }
    sig, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil {
        return nil, fmt.Errorf("%w: signature decode: %v", ErrInvalidKeyFormat, err)
    }

    if !ed25519.Verify(v.publicKey, payloadBytes, sig) {
        return nil, ErrInvalidSignature
    }

    var lic License
    if err := json.Unmarshal(payloadBytes, &lic); err != nil {
        return nil, fmt.Errorf("%w: payload parse: %v", ErrInvalidKeyFormat, err)
    }

    if v.now().Before(lic.IssuedAt) {
        return nil, ErrLicenseNotYetValid
    }
    if v.now().After(lic.ExpiresAt) {
        return nil, ErrLicenseExpired
    }

    return &lic, nil
}

// HasFeature reports whether a feature flag is set in this license.
func (l *License) HasFeature(name string) bool {
    if l == nil {
        return false
    }
    return l.Features[name]
}

// IsCommunity returns true if this is a community (free) license.
func (l *License) IsCommunity() bool {
    return l == nil || l.Tier == "community"
}
```

### Feature Gate

```go
// internal/license/gate.go
package license

import (
    "context"
    "errors"
    "net/http"
    "sync/atomic"
)

// Tier represents the commercial tier.
type Tier string

const (
    TierCommunity    Tier = "community"
    TierProfessional Tier = "professional"
    TierEnterprise   Tier = "enterprise"
)

// Gate enforces feature availability based on the active license.
type Gate struct {
    active   atomic.Pointer[License]
    endpoint atomic.Int64 // current monitored endpoint count
}

func NewGate() *Gate {
    g := &Gate{}
    g.active.Store(nil) // no license = community
    return g
}

// SetLicense activates a license.
func (g *Gate) SetLicense(lic *License) {
    g.active.Store(lic)
}

// ActiveLicense returns the currently active license (may be nil for community).
func (g *Gate) ActiveLicense() *License {
    return g.active.Load()
}

// SetEndpointCount updates the current monitored endpoint count.
func (g *Gate) SetEndpointCount(n int64) {
    g.endpoint.Store(n)
}

// CheckFeature returns nil if the feature is available, or a FeatureGatedError if not.
func (g *Gate) CheckFeature(name string) error {
    lic := g.active.Load()
    if lic != nil && lic.HasFeature(name) {
        return nil
    }
    return &FeatureGatedError{
        FeatureName:  name,
        TierRequired: requiredTierFor(name),
        Reason:       "feature_not_licensed",
    }
}

// CheckEndpointLimit returns an error if adding n more endpoints would exceed the licensed limit.
func (g *Gate) CheckEndpointLimit(additional int) error {
    lic := g.active.Load()
    if lic == nil {
        // Community limit: 250
        if g.endpoint.Load()+int64(additional) > 250 {
            return &FeatureGatedError{
                FeatureName:  "endpoint_limit",
                TierRequired: TierProfessional,
                Reason:       "endpoint_limit_reached",
            }
        }
        return nil
    }
    if int(g.endpoint.Load())+additional > lic.MaxEndpoints {
        return &FeatureGatedError{
            FeatureName:  "endpoint_limit",
            TierRequired: TierProfessional,
            Reason:       "endpoint_limit_reached",
        }
    }
    return nil
}

// FeatureGatedError is returned when a feature is not available under the current license.
type FeatureGatedError struct {
    FeatureName  string
    TierRequired Tier
    Reason       string
}

func (e *FeatureGatedError) Error() string {
    return fmt.Sprintf("feature %q requires %s tier: %s", e.FeatureName, e.TierRequired, e.Reason)
}

func requiredTierFor(feature string) Tier {
    switch feature {
    case "multi_tenancy", "managed_relay", "enterprise_reporting", "rbac_advanced", "mfa", "sso":
        return TierProfessional
    case "mcp_marketplace", "custom_adapters", "air_gapped":
        return TierEnterprise
    default:
        return TierCommunity
    }
}
```

### Graceful Degradation: HTTP 402

When a gated feature is accessed, the API returns HTTP 402 (Payment Required) with a structured body so frontend and CLI can display an actionable message:

```go
// internal/api/middleware/license.go
package api

import (
    "errors"
    "net/http"

    "github.com/openagentplatform/openagentplatform/internal/license"
)

// LicenseMiddleware returns HTTP 402 with structured body when a feature gate blocks a request.
func LicenseMiddleware(gate *license.Gate) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Each gated route declares its required feature via context.
            feature, _ := r.Context().Value(featureKey).(string)
            if feature != "" {
                if err := gate.CheckFeature(feature); err != nil {
                    var gated *license.FeatureGatedError
                    if errors.As(err, &gated) {
                        writeGatedResponse(w, gated)
                        return
                    }
                }
            }
            next.ServeHTTP(w, r)
        })
    }
}

func writeGatedResponse(w http.ResponseWriter, e *license.FeatureGatedError) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusPaymentRequired)
    json.NewEncoder(w).Encode(map[string]any{
        "error": "feature_gated",
        "feature_name": e.FeatureName,
        "tier_required": e.TierRequired,
        "reason": e.Reason,
        "message": fmt.Sprintf("This feature requires the %s tier. Contact sales@openagentplatform.io to upgrade.", e.TierRequired),
    })
}
```

Example response body:

```json
{
  "error": "feature_gated",
  "feature_name": "multi_tenancy",
  "tier_required": "professional",
  "reason": "feature_not_licensed",
  "message": "This feature requires the professional tier. Contact sales@openagentplatform.io to upgrade."
}
```

The frontend renders a "Upgrade to Professional" call-to-action. The CLI prints a clear error and links to the upgrade page.

### License Loading

```go
// internal/license/loader.go
package license

import (
    "fmt"
    "os"
    "strings"
)

const (
    envLicenseKey  = "OAP_LICENSE_KEY"
    fileLicenseKey = "/etc/oap/license.key"
)

// LoadLicenseFromEnvOrFile reads the license key from the OAP_LICENSE_KEY
// environment variable or, failing that, from /etc/oap/license.key.
// Returns nil license (community mode) if neither is set.
func LoadLicenseFromEnvOrFile(v *Validator) (*License, error) {
    key := os.Getenv(envLicenseKey)
    if key == "" {
        data, err := os.ReadFile(fileLicenseKey)
        if err != nil {
            if os.IsNotExist(err) {
                return nil, nil // community mode
            }
            return nil, fmt.Errorf("read license file: %w", err)
        }
        key = strings.TrimSpace(string(data))
    }
    return v.ParseAndValidate(key)
}
```

---

## Tier Definitions

### Community (BSL 1.1, Free)

The community tier is the default for the open-source `oap-server` binary. It is fully functional for self-hosted single-tenant deployments within the 250-endpoint cap.

| Feature | Community |
|---|---|
| **License** | BSL 1.1 (converts to Apache 2.0 on 2030-06-15) |
| **Tenancy** | Single-tenant only |
| **Endpoint cap** | 250 monitored endpoints |
| **LLM agent processes** | Up to 3 concurrent processes |
| **Agent frameworks** | Single framework per deployment (LangChain or AutoGen or CrewAI) |
| **A2A mode** | Local only (agents communicate within the same OAP instance) |
| **Secret management** | HashiCorp Vault integration |
| **Alerting** | Basic rules engine, email and webhook destinations |
| **Policy engine** | Basic policy templates |
| **Reporting** | Dashboard only (no scheduled reports, no PDF/HTML export) |
| **Authentication** | Local users, OIDC (Google, Microsoft, Okta) |
| **Authorization** | Basic RBAC (admin, operator, viewer) |
| **Multi-factor auth** | TOTP only |
| **SSO** | OIDC only |
| **Support** | Community forum, GitHub issues, best-effort |

### Professional (Paid)

The professional tier is the primary commercial offering, targeting MSPs and mid-to-large enterprises.

| Feature | Professional |
|---|---|
| **License** | Commercial license key, signed EdDSA |
| **Tenancy** | Multi-tenant (MSP mode), unlimited tenants |
| **Endpoint cap** | Unlimited |
| **LLM agent processes** | Up to 25 concurrent processes |
| **Agent frameworks** | Mix multiple frameworks in one deployment (LangChain + AutoGen + CrewAI simultaneously) |
| **A2A mode** | Managed A2A relay (agents across OAP instances discover and collaborate via OAP cloud) |
| **Secret management** | Vault + Infisical |
| **Alerting** | Advanced rules engine, PagerDuty, Slack, Opsgenie, Teams integrations |
| **Policy engine** | Custom policy DSL, version control, approval workflows |
| **Reporting** | Scheduled reports (daily, weekly, monthly), PDF and HTML export, cross-tenant aggregation |
| **Authentication** | Local + OIDC + SAML 2.0 |
| **Authorization** | Advanced RBAC with custom roles, resource-level permissions |
| **Multi-factor auth** | TOTP, WebAuthn, SMS |
| **SSO** | OIDC, SAML 2.0, Just-In-Time provisioning |
| **Support** | Priority support, 99.9% SLA, dedicated support engineer |

### Enterprise (Premium)

The enterprise tier adds capabilities for large organizations with custom requirements, regulatory constraints, or air-gapped deployments.

| Feature | Enterprise (everything in Professional, plus) |
|---|---|
| **LLM agent processes** | Unlimited |
| **A2A relay** | Custom A2A relay with VPC peering (customer's own cloud account), dedicated relay nodes |
| **MCP marketplace** | Access to OAP's managed MCP server marketplace, custom MCP server publishing |
| **Custom framework adapters** | Engineering team builds and maintains custom adapters for proprietary agent frameworks |
| **Air-gapped deployment** | License verification via offline key, no telemetry required, no cloud dependencies |
| **Dedicated CSM** | Dedicated Customer Success Manager, quarterly business reviews |
| **Custom SLA** | 99.99% SLA, defined remediation times, financial credits |
| **Audit log export** | SIEM integration (Splunk, Sumo Logic, Datadog), immutable audit log retention |
| **Source code escrow** | Optional, for regulated industries |

---

## Multi-Tenancy

Multi-tenancy is the core capability that differentiates the Professional tier from Community. It enables MSPs to manage multiple customer organizations from a single OAP deployment.

### Tenant Model

```sql
CREATE TABLE tenants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    slug         TEXT NOT NULL UNIQUE,           -- used in URLs, NATS subjects
    status       TEXT NOT NULL DEFAULT 'active', -- active, suspended, archived
    plan         TEXT NOT NULL,                  -- community, professional, enterprise
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    suspended_at TIMESTAMPTZ
);

CREATE INDEX idx_tenants_status ON tenants(status);
CREATE INDEX idx_tenants_slug ON tenants(slug);
```

Every other resource (endpoints, agents, policies, alerts, users) carries a `tenant_id` column. The API authenticates the request, resolves the tenant from the user's session, and applies that as a filter on all queries.

### Isolation: PostgreSQL Row-Level Security (RLS)

We use **PostgreSQL Row-Level Security** for tenant isolation. This is more maintainable than schema-per-tenant (one schema per tenant scales poorly past ~1000 tenants) and more secure than application-layer filtering (RLS is enforced by the database, not by developer discipline).

```sql
-- Enable RLS on the endpoints table
ALTER TABLE endpoints ENABLE ROW LEVEL SECURITY;

-- Policy: users can only see endpoints for their current tenant
CREATE POLICY tenant_isolation ON endpoints
    USING (tenant_id = current_setting('app.current_tenant')::uuid);

-- Force RLS even for table owners (defense in depth)
ALTER TABLE endpoints FORCE ROW LEVEL SECURITY;
```

The application sets the tenant context at the start of each transaction:

```go
// internal/db/tenant.go
package db

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

// TenantKey is the context key for the current tenant ID.
type tenantKey struct{}

// WithTenant stores the tenant ID in the context.
func WithTenant(ctx context.Context, tenantID string) context.Context {
    return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFromContext retrieves the tenant ID from the context.
func TenantFromContext(ctx context.Context) (string, bool) {
    id, ok := ctx.Value(tenantKey{}).(string)
    return id, ok
}

// WithTenantConnection returns a connection with the tenant RLS variable set.
func WithTenantConnection(ctx context.Context, pool *pgxpool.Pool) (*pgx.Conn, error) {
    tenantID, ok := TenantFromContext(ctx)
    if !ok {
        return nil, fmt.Errorf("no tenant in context")
    }
    conn, err := pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    // SET LOCAL applies only for the current transaction.
    _, err = conn.Exec(ctx, fmt.Sprintf("SET LOCAL app.current_tenant = '%s'", tenantID))
    if err != nil {
        conn.Release()
        return nil, fmt.Errorf("set tenant RLS: %w", err)
    }
    return conn.Conn(), nil
}
```

The connection pool is configured to release connections back to the pool after each transaction, so the `SET LOCAL` scope is per-transaction, not per-connection. This prevents tenant leakage across pooled connections.

### Schema-Per-Tenant Option

For high-compliance customers (financial services, healthcare), RLS may not satisfy auditors who require physically separate schemas. The enterprise tier supports optional schema-per-tenant deployment:

```go
//go:build commercial

package multitenancy

type IsolationMode string

const (
    IsolationRLS     IsolationMode = "rls"      // default, single schema, RLS-enforced
    IsolationSchema  IsolationMode = "schema"   // one schema per tenant
    IsolationDatabase IsolationMode = "database" // one database per tenant (enterprise)
)

type TenantProvisioner struct {
    mode IsolationMode
    pool *pgxpool.Pool
}

func (p *TenantProvisioner) Provision(ctx context.Context, t *Tenant) error {
    switch p.mode {
    case IsolationRLS:
        return p.provisionRLS(ctx, t)
    case IsolationSchema:
        return p.provisionSchema(ctx, t)
    case IsolationDatabase:
        return p.provisionDatabase(ctx, t)
    default:
        return fmt.Errorf("unknown isolation mode: %s", p.mode)
    }
}

func (p *TenantProvisioner) provisionSchema(ctx context.Context, t *Tenant) error {
    schema := fmt.Sprintf("tenant_%s", t.Slug)
    _, err := p.pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema))
    if err != nil {
        return err
    }
    // Run migrations within the tenant's schema
    return p.runMigrations(ctx, schema)
}
```

### Provisioning API

```protobuf
// api/proto/tenants.proto
syntax = "proto3";
package oap.tenants.v1;

service TenantService {
    rpc CreateTenant(CreateTenantRequest) returns (Tenant);
    rpc GetTenant(GetTenantRequest) returns (Tenant);
    rpc ListTenants(ListTenantsRequest) returns (ListTenantsResponse);
    rpc UpdateTenant(UpdateTenantRequest) returns (Tenant);
    rpc SuspendTenant(SuspendTenantRequest) returns (Tenant);
    rpc DeleteTenant(DeleteTenantRequest) returns (Tenant);
}

message Tenant {
    string id = 1;
    string name = 2;
    string slug = 3;
    string status = 4;
    string plan = 5;
    map<string, string> metadata = 6;
    google.protobuf.Timestamp created_at = 7;
}

message CreateTenantRequest {
    string name = 1;
    string slug = 2;
    string plan = 3;
    map<string, string> metadata = 4;
}
```

### NATS Subject Isolation

OAP uses NATS for inter-service messaging. Tenant isolation in NATS is achieved by **subject prefixing**: every subject includes the tenant slug, and tenants cannot subscribe or publish to other tenants' subjects.

```
Subject format: tenants.<tenant-slug>.<resource>.<action>

Examples:
  tenants.acme-corp.endpoints.registered
  tenants.acme-corp.alerts.fired
  tenants.globex-industries.endpoints.registered
```

A tenant-scoped NATS client is configured with a wildcard deny rule. The NATS server enforces this via its authorization system:

```go
// internal/events/tenant_nats.go
package events

import (
    "github.com/nats-io/nats.go"
)

func NewTenantClient(tenantSlug, natsURL string) (*nats.Conn, error) {
    opts := []nats.Option{
        nats.UserInfo(tenantSlug, generateTenantToken(tenantSlug)),
        // The token is signed by the OAP server and includes the allowed subject prefix.
        // The NATS server's auth resolver validates the token and enforces the prefix.
    }
    return nats.Connect(natsURL, opts...)
}
```

The token is a JWT containing the tenant slug and the subject prefix `tenants.<slug>.>`. The NATS server's `nats-account-resolver` validates the JWT on connect and rejects any subscribe/publish outside the allowed prefix.

### Stripe Billing Integration

Multi-tenancy ties into Stripe for usage-based billing. Each tenant maps to a Stripe subscription. Endpoint and agent-process overages are tracked in Stripe as metered usage:

```go
//go:build commercial

package billing

import (
    "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/subscription"
    "github.com/stripe/stripe-go/v76/usage"
)

type StripeClient struct {
    apiKey string
}

func (c *StripeClient) ReportUsage(subscriptionItemID string, quantity int64) error {
    _, err := usage.New(&stripe.UsageRecordParams{
        SubscriptionItem: stripe.String(subscriptionItemID),
        Quantity:         stripe.Int64(quantity),
        Timestamp:        stripe.Int64(time.Now().Unix()),
        Action:           stripe.String("set"),
    })
    return err
}

func (c *StripeClient) GetSubscription(subID string) (*stripe.Subscription, error) {
    return subscription.Get(subID, nil)
}
```

The OAP server reports endpoint counts and agent-process counts to Stripe on a 5-minute cadence. Stripe aggregates these for invoicing.

---

## Managed A2A Relay

The Managed A2A Relay is OAP's cloud-hosted service that lets agents in different OAP instances (on different networks) discover each other and collaborate. It is a Professional+ tier feature.

### Architecture

```
┌────────────────┐         ┌────────────────┐         ┌────────────────┐
│  OAP Instance  │         │  A2A Relay     │         │  OAP Instance  │
│  (Customer A)  │◄───────►│  (OAP Cloud)   │◄───────►│  (Customer B)  │
│  10.0.0.0/24   │  WSS    │  relay.oap.io  │  WSS    │  192.168.0.0/24│
└────────────────┘         └────────────────┘         └────────────────┘
        │                          │                          │
        │ Agent A publishes        │ Routes discovery &       │ Agent B subscribes
        │ "I can do task X"        │ task delegation          │ "Show me task X agents"
```

The relay is a stateless WebSocket gateway that:

1. Receives agent capability announcements from OAP instances.
2. Indexes them in a search-friendly store (Postgres with full-text search on capability descriptions).
3. Receives discovery queries from OAP instances and returns matching agents across all connected instances.
4. Relays task delegation messages between instances.

### Connection

Each OAP instance maintains a persistent WebSocket connection to the relay:

```go
//go:build commercial

package relay

import (
    "context"
    "crypto/tls"
    "net/http"
    "time"

    "github.com/gorilla/websocket"
)

const RelayURL = "wss://relay.oap.io/v1/connect"

type Client struct {
    licenseKey  string
    customerID  string
    conn        *websocket.Conn
    onMessage   func(msg []byte)
}

func NewClient(licenseKey, customerID string) *Client {
    return &Client{licenseKey: licenseKey, customerID: customerID}
}

func (c *Client) Connect(ctx context.Context) error {
    dialer := websocket.Dialer{
        TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13},
        HandshakeTimeout: 10 * time.Second,
    }
    headers := http.Header{}
    headers.Set("Authorization", "Bearer "+c.licenseKey)
    headers.Set("X-OAP-Customer-ID", c.customerID)

    conn, _, err := dialer.DialContext(ctx, RelayURL, headers)
    if err != nil {
        return err
    }
    c.conn = conn
    go c.readLoop()
    return nil
}

func (c *Client) readLoop() {
    for {
        _, msg, err := c.conn.ReadMessage()
        if err != nil {
            // Reconnect with exponential backoff
            time.Sleep(5 * time.Second)
            c.Connect(context.Background())
            return
        }
        c.onMessage(msg)
    }
}
```

### Cross-Network Discovery Federation

When an OAP instance needs to find an agent with capability X, it queries the relay. The relay performs a federated search across all connected instances' published capabilities:

```go
//go:build commercial

package relay

type DiscoveryQuery struct {
    Capability string   `json:"capability"`
    Tags       []string `json:"tags"`
    MaxResults int      `json:"max_results"`
}

type AgentCard struct {
    AgentID     string   `json:"agent_id"`
    CustomerID  string   `json:"customer_id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Capabilities []string `json:"capabilities"`
    Endpoint    string   `json:"endpoint"`    // WSS URL for direct delegation
    PublicKey   string   `json:"public_key"`  // for E2E encryption
}

func (c *Client) DiscoverAgents(ctx context.Context, q DiscoveryQuery) ([]AgentCard, error) {
    msg, _ := json.Marshal(map[string]any{
        "type": "discovery.query",
        "payload": q,
    })
    if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
        return nil, err
    }
    // ... wait for response
}
```

The relay never sees the agent's task contents. It only sees capability descriptions and routes the initial connection. Once two agents discover each other, they establish a direct end-to-end-encrypted connection (Noise protocol) for actual task delegation.

### Auth and Usage Metering

Every relay connection is authenticated with the customer's license key. The relay records:

- Number of agents registered per customer.
- Number of discovery queries.
- Bandwidth used (sum of message sizes).
- Number of inter-instance task delegations completed.

These are exported as a usage report to the customer's billing dashboard and to Stripe for metered billing.

```go
//go:build commercial

package relay

type UsageRecord struct {
    CustomerID        string    `json:"customer_id"`
    Timestamp         time.Time `json:"timestamp"`
    AgentsRegistered  int       `json:"agents_registered"`
    DiscoveryQueries  int64     `json:"discovery_queries"`
    BandwidthBytes    int64     `json:"bandwidth_bytes"`
    DelegationsCompleted int64  `json:"delegations_completed"`
}

func (c *Client) reportUsage(r UsageRecord) error {
    msg, _ := json.Marshal(map[string]any{
        "type": "usage.report",
        "payload": r,
    })
    return c.conn.WriteMessage(websocket.TextMessage, msg)
}
```

### Enterprise VPC Peering

Enterprise customers who cannot send traffic to a public relay endpoint (regulatory, security policy) get a dedicated relay deployed in their own VPC or on their own infrastructure, peered via AWS PrivateLink, Azure Private Link, or GCP Private Service Connect:

```
┌──────────────────────────┐         ┌──────────────────────────┐
│  Customer VPC            │         │  OAP Cloud (Enterprise)  │
│  ┌────────────────────┐  │         │  ┌────────────────────┐  │
│  │  Customer OAP      │  │ Private │  │  Dedicated Relay   │  │
│  │  Instance          │◄─┼─Link────┼─►│  (tenant-isolated)  │  │
│  └────────────────────┘  │         │  └────────────────────┘  │
└──────────────────────────┘         └──────────────────────────┘
        No internet traversal            No public IPs
```

This is provisioned by the OAP SRE team during enterprise onboarding.

---

## Enterprise Reporting

Enterprise reporting provides scheduled, exportable, cross-tenant reports for compliance, executive summaries, and SLA tracking.

### Template Engine

Reports are defined as Go templates with a structured data model:

```go
//go:build commercial

package reporting

import (
    "bytes"
    "text/template"
    "time"
)

type ReportData struct {
    Tenant        Tenant                `json:"tenant"`
    GeneratedAt   time.Time             `json:"generated_at"`
    PeriodStart   time.Time             `json:"period_start"`
    PeriodEnd     time.Time             `json:"period_end"`
    EndpointStats EndpointStats         `json:"endpoint_stats"`
    AgentStats    AgentStats            `json:"agent_stats"`
    AlertStats    AlertStats            `json:"alert_stats"`
    PolicyStats   PolicyStats           `json:"policy_stats"`
    TopIncidents  []Incident            `json:"top_incidents"`
}

const monthlyExecutiveReport = `
OAP MONTHLY EXECUTIVE REPORT
Tenant: {{.Tenant.Name}}
Period: {{.PeriodStart.Format "2006-01-02"}} to {{.PeriodEnd.Format "2006-01-02"}}

ENDPOINTS
  Total monitored:     {{.EndpointStats.Total}}
  Healthy:             {{.EndpointStats.Healthy}} ({{percent .EndpointStats.Healthy .EndpointStats.Total}})
  Degraded:            {{.EndpointStats.Degraded}} ({{percent .EndpointStats.Degraded .EndpointStats.Total}})
  Offline:             {{.EndpointStats.Offline}} ({{percent .EndpointStats.Offline .EndpointStats.Total}})

AGENTS
  Total processes:     {{.AgentStats.TotalProcesses}}
  Tasks completed:     {{.AgentStats.TasksCompleted}}
  Success rate:        {{percent .AgentStats.TasksSucceeded .AgentStats.TasksCompleted}}

ALERTS
  Total fired:         {{.AlertStats.Fired}}
  Acknowledged:        {{.AlertStats.Acknowledged}}
  Avg. MTTA:           {{.AlertStats.AvgMTTA}}
  Avg. MTTR:           {{.AlertStats.AvgMTTR}}

TOP INCIDENTS
{{range .TopIncidents}}
  - [{{.Severity}}] {{.Title}} (duration: {{.Duration}})
{{end}}
`
```

### Exporters

```go
//go:build commercial

package reporting

import (
    "github.com/jung-kurt/gofpdf"
    "github.com/yuin/goldmark"
)

type Exporter interface {
    Export(data ReportData) ([]byte, error)
    MimeType() string
    FileExtension() string
}

type PDFExporter struct{}

func (e *PDFExporter) Export(data ReportData) ([]byte, error) {
    pdf := gofpdf.New("P", "mm", "A4", "")
    pdf.AddPage()
    pdf.SetFont("Arial", "B", 16)
    pdf.Cell(40, 10, "OAP Monthly Report")
    pdf.Ln(12)
    // ... layout the structured data ...
    return pdf.Output(nil), nil
}

func (e *PDFExporter) MimeType() string         { return "application/pdf" }
func (e *PDFExporter) FileExtension() string    { return "pdf" }

type HTMLExporter struct{}

func (e *HTMLExporter) Export(data ReportData) ([]byte, error) {
    var buf bytes.Buffer
    md := goldmark.New()
    if err := md.Convert([]byte(renderTemplate(data)), &buf); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func (e *HTMLExporter) MimeType() string      { return "text/html" }
func (e *HTMLExporter) FileExtension() string { return "html" }
```

### Scheduled Delivery

```go
//go:build commercial

package reporting

type DeliveryChannel string

const (
    DeliveryEmail  DeliveryChannel = "email"
    DeliveryWebhook DeliveryChannel = "webhook"
    DeliveryS3     DeliveryChannel = "s3"
)

type Schedule struct {
    ID            string           `json:"id"`
    TenantID      string           `json:"tenant_id"`
    Name          string           `json:"name"`
    Template      string           `json:"template"`
    Cron          string           `json:"cron"`           // e.g., "0 9 1 * *" = 9am on 1st of month
    Recipients    []string         `json:"recipients"`     // emails, webhook URLs
    Channels      []DeliveryChannel `json:"channels"`
    S3Bucket      string           `json:"s3_bucket,omitempty"`
    S3Region      string           `json:"s3_region,omitempty"`
    Enabled       bool             `json:"enabled"`
}

type Scheduler struct {
    repo       ScheduleRepository
    exporters  map[string]Exporter // keyed by "pdf", "html"
    deliveries map[DeliveryChannel]Delivery
}

func (s *Scheduler) Run(ctx context.Context) {
    // Cron-driven loop. At each tick, find schedules whose cron matches the current time.
    // For each, render the report and dispatch via the configured channels.
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case t := <-ticker.C:
            schedules, _ := s.repo.DueAt(ctx, t)
            for _, sched := range schedules {
                s.execute(ctx, sched)
            }
        }
    }
}
```

### Cross-Tenant Aggregation (MSP Reports)

For MSPs, the reporting system can aggregate across all managed tenants:

```go
//go:build commercial

package reporting

func (s *Scheduler) generateMSPAggregate(ctx context.Context, mspID string, period TimeRange) (ReportData, error) {
    tenants, _ := s.repo.TenantsForMSP(ctx, mspID)
    var aggregate ReportData
    aggregate.PeriodStart = period.Start
    aggregate.PeriodEnd = period.End
    for _, t := range tenants {
        td, _ := s.generateTenantReport(ctx, t, period)
        aggregate.EndpointStats.Add(td.EndpointStats)
        aggregate.AgentStats.Add(td.AgentStats)
        aggregate.AlertStats.Add(td.AlertStats)
    }
    aggregate.Tenant.Name = fmt.Sprintf("MSP Aggregate (%d tenants)", len(tenants))
    return aggregate, nil
}
```

---

## Billing

### License Key Generation

The license key is generated by the OAP billing service (a separate internal system, not part of the open-source codebase) when a customer signs up or renews. Generation uses EdDSA:

```go
//go:build commercial

package license

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "sort"
    "time"
)

type Generator struct {
    privateKey ed25519.PrivateKey
}

func NewGenerator(privateKey ed25519.PrivateKey) *Generator {
    return &Generator{privateKey: privateKey}
}

func (g *Generator) Generate(req GenerateRequest) (string, error) {
    payload := License{
        LicenseID:         generateID("lic"),
        CustomerID:        req.CustomerID,
        CustomerName:      req.CustomerName,
        Tier:              req.Tier,
        IssuedAt:          time.Now().UTC(),
        ExpiresAt:         req.ExpiresAt,
        MaxEndpoints:      req.MaxEndpoints,
        MaxAgentProcesses: req.MaxAgentProcesses,
        Features:          defaultFeaturesFor(req.Tier),
        Metadata: map[string]string{
            "subscription_id":      req.SubscriptionID,
            "stripe_customer_id":   req.StripeCustomerID,
            "order_id":             req.OrderID,
        },
    }

    // Canonicalize JSON (sorted keys, no whitespace) for deterministic signing.
    payloadBytes, err := canonicalJSON(payload)
    if err != nil {
        return "", err
    }

    sig := ed25519.Sign(g.privateKey, payloadBytes)
    key := "oap_" +
        base64.RawURLEncoding.EncodeToString(payloadBytes) + "." +
        base64.RawURLEncoding.EncodeToString(sig)
    return key, nil
}

func canonicalJSON(v any) ([]byte, error) {
    // Marshal to map, sort keys, re-marshal.
    raw, err := json.Marshal(v)
    if err != nil {
        return nil, err
    }
    var m map[string]any
    if err := json.Unmarshal(raw, &m); err != nil {
        return nil, err
    }
    sorted := sortMap(m)
    return json.Marshal(sorted)
}
```

### Per-Endpoint Subscription

The Professional tier uses a **per-endpoint** subscription model:

| Endpoints | Monthly Price (USD) |
|---|---|
| 250 - 1,000 | $4 per endpoint / month |
| 1,001 - 5,000 | $3 per endpoint / month |
| 5,001 - 25,000 | $2 per endpoint / month |
| 25,001+ | Custom (Enterprise) |

Usage above the licensed endpoint count is metered and billed monthly in arrears via Stripe.

### Per-Agent-Process Pricing

LLM agent processes are billed separately because each process consumes LLM tokens:

| Concurrent Agent Processes | Monthly Price (USD) |
|---|---|
| Up to 3 (community) | Free |
| 4 - 25 (professional) | $50 per process / month |
| 26+ (enterprise) | $30 per process / month |

LLM token costs are passed through at provider list price with a 10% margin. Customers can bring their own LLM API keys to avoid the margin.

### Stripe Billing Integration

The OAP billing service integrates with Stripe Billing for checkout, subscription management, and invoicing:

```go
//go:build commercial

package billing

import (
    "github.com/stripe/stripe-go/v76"
    "github.com/stripe/stripe-go/v76/checkout/session"
    "github.com/stripe/stripe-go/v76/webhook"
)

type StripeService struct {
    apiKey      string
    webhookSecret string
    priceCatalog map[string]string // internal price ID -> Stripe price ID
}

func (s *StripeService) CreateCheckoutSession(cust CheckoutRequest) (*stripe.CheckoutSession, error) {
    params := &stripe.CheckoutSessionParams{
        Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
        LineItems: []*stripe.CheckoutSessionLineItemParams{
            {
                Price:    stripe.String(s.priceCatalog[cust.PriceID]),
                Quantity: stripe.Int64(cust.Quantity),
            },
        },
        SuccessURL: stripe.String(cust.SuccessURL),
        CancelURL:  stripe.String(cust.CancelURL),
        ClientReferenceID: stripe.String(cust.CustomerID),
    }
    return session.New(params)
}

// Webhook handler for Stripe events
func (s *StripeService) HandleWebhook(payload []byte, signature string) error {
    event, err := webhook.ConstructEvent(payload, signature, s.webhookSecret)
    if err != nil {
        return err
    }

    switch event.Type {
    case "customer.subscription.created":
        // Generate a license key and email it to the customer
        sub := event.Data.Object.(*stripe.Subscription)
        return s.onSubscriptionCreated(sub)

    case "customer.subscription.updated":
        sub := event.Data.Object.(*stripe.Subscription)
        return s.onSubscriptionUpdated(sub)

    case "customer.subscription.deleted":
        sub := event.Data.Object.(*stripe.Subscription)
        return s.onSubscriptionDeleted(sub)

    case "invoice.payment_failed":
        // Suspend the license; customer gets a grace period
        inv := event.Data.Object.(*stripe.Invoice)
        return s.onPaymentFailed(inv)
    }
    return nil
}
```

### Customer Portal

Stripe's hosted customer portal handles:

- Updating payment methods
- Viewing invoices
- Changing subscription quantity
- Cancelling subscription

OAP embeds the portal via Stripe.js. No custom billing UI is required.

### Usage Reporting Dashboard

The OAP web UI includes a **Billing** section for paid customers:

- Current endpoint count vs. licensed cap.
- Current agent-process count vs. licensed cap.
- Projected month-end overage charges.
- Historical invoices (links to Stripe-hosted invoices).
- License key display and rotation.

---

## Contributor Agreement

OpenAgentPlatform uses the **Developer Certificate of Origin (DCO)** for contributions, not a full CLA. The DCO is a lightweight sign-off mechanism that confirms the contributor has the right to submit the code.

### DCO Sign-Off

Every commit must include a `Signed-off-by:` trailer:

```
feat(agents): add LangChain adapter for Claude 3.5

Implements the LangChain adapter interface for Anthropic's
Claude 3.5 Sonnet model with tool-use support.

Signed-off-by: Jane Developer <jane@example.com>
```

The sign-off line certifies (per the DCO 1.1 text at [developercertificate.org](https://developercertificate.org/)):

> The contributor certifies that they have the right to submit the work under the project's license, and that they agree to the DCO terms.

### Enforcement

We use a CI check that verifies every commit in a pull request has a `Signed-off-by:` line:

```yaml
# .github/workflows/dco.yml
name: DCO Check
on: [pull_request]

jobs:
  dco:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Check DCO sign-off
        uses: contribution/dco-check@main
        with:
          allow-force-pushes: false
```

The DCO is enforced with a "soft fail" on the first commit (the contributor can amend) and "hard fail" on merge.

### Patent License Grant

The BSL 1.1 license (derived from MPL 2.0) includes an explicit patent license grant from each contributor to all users of the code. The grant covers any patents the contributor holds that are necessarily infringed by their contribution. This protects users from contributor patent assertions and is one of the reasons BSL was chosen over a pure Apache 2.0 base.

For commercial-tier code under the OAP Commercial License Agreement, a separate patent grant is included in the commercial license signed by the customer, covering both the open-source and proprietary components.

---

## Implementation Steps

This section lists the concrete steps to implement the Commercial & Licensing subsystem. Steps are ordered to allow incremental delivery: the open-source license and feature-gating infrastructure can be shipped first, with commercial features layered on top.

### Step 1: License Files and Headers

1.1. Create `LICENSE` at the repository root with the full BSL 1.1 text. Include the change date (2030-06-15) and the Additional Use Grant.

1.2. Create `NOTICE` with required attribution: copyright lines, third-party license attributions, and a statement that the software is licensed under BSL 1.1.

1.3. Add a license header to every Go file:

```go
// Copyright 2026 OpenAgentPlatform Contributors
// SPDX-License-Identifier: BSL-1.1
// See LICENSE and NOTICE for details.
```

1.4. Add a CI check that fails if a `.go` file in `internal/`, `cmd/`, or `pkg/` is missing the SPDX header.

1.5. Add a CI check that blocks PRs introducing files in `internal/commercial/` without the `//go:build commercial` tag.

### Step 2: Build System

2.1. Create the Makefile with `build`, `build-enterprise`, and `build-all` targets (shown above).

2.2. Add Go build tags to all files in `internal/commercial/`:

```go
//go:build commercial
```

2.3. Create `scripts/build-enterprise.sh` that wraps the enterprise build, signs the resulting binary with cosign, and uploads to the customer download portal.

2.4. Create a Dockerfile that builds both binaries in a multi-stage build:

```dockerfile
# Stage 1: community build
FROM golang:1.22 AS community
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -o /out/oap-server ./cmd/oap-server

# Stage 2: enterprise build
FROM golang:1.22 AS enterprise
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -tags=commercial -o /out/oap-server-enterprise ./cmd/oap-server

# Final image
FROM gcr.io/distroless/base-debian12
COPY --from=community /out/oap-server /usr/local/bin/oap-server
COPY --from=enterprise /out/oap-server-enterprise /usr/local/bin/oap-server-enterprise
```

2.5. Add a CI pipeline that runs `go build ./...` (community) and `go build -tags=commercial ./...` (enterprise) on every PR to ensure both build paths compile.

### Step 3: License Validation Engine

3.1. Implement `internal/license/validator.go` (shown above).

3.2. Implement `internal/license/loader.go` to load keys from env or file.

3.3. Embed the Ed25519 public key in `internal/license/publickey.go`.

3.4. Write unit tests for the validator:

- Valid key parses and validates.
- Tampered payload fails signature check.
- Tampered signature fails.
- Expired license returns `ErrLicenseExpired`.
- Not-yet-valid license returns `ErrLicenseNotYetValid`.
- Missing `oap_` prefix returns `ErrInvalidKeyFormat`.

3.5. Write a fuzz test for the validator using `go test -fuzz`:

```go
func FuzzParseAndValidate(f *testing.F) {
    f.Add("oap_eyJsaWNlbnNlX2lkIjoibGljX3Rlc3QifQ.7c4a8b...")
    f.Fuzz(func(t *testing.T, key string) {
        v := NewValidator()
        _, _ = v.ParseAndValidate(key) // must not panic
    })
}
```

### Step 4: Feature Gate

4.1. Implement `internal/license/gate.go` (shown above).

4.2. Wire the gate into the API middleware (shown above).

4.3. Annotate gated routes with the required feature:

```go
// internal/api/router.go
mux := http.NewServeMux()

// Community routes (no gating)
mux.Handle("GET /api/v1/endpoints", s.ListEndpoints)
mux.Handle("GET /api/v1/alerts", s.ListAlerts)

// Professional-gated routes
mux.Handle("POST /api/v1/tenants", withFeature("multi_tenancy", s.CreateTenant))
mux.Handle("GET /api/v1/relay/agents", withFeature("managed_relay", s.DiscoverAgents))
mux.Handle("POST /api/v1/reports/scheduled", withFeature("enterprise_reporting", s.ScheduleReport))

// Enterprise-gated routes
mux.Handle("GET /api/v1/marketplace/mcp", withFeature("mcp_marketplace", s.ListMarketplace))
```

4.4. Add frontend handling of HTTP 402 responses: detect the `error: "feature_gated"` body and render an upgrade prompt with the `tier_required` and `feature_name`.

4.5. Add CLI handling:

```
$ oap-cli tenants create --name "Acme Corp"
Error: feature "multi_tenancy" requires professional tier: feature_not_licensed
Learn more: https://openagentplatform.io/upgrade
```

### Step 5: Multi-Tenancy

5.1. Add the `tenants` table and RLS policies (shown above).

5.2. Refactor every resource table to include `tenant_id` and an RLS policy.

5.3. Update every query path to set `app.current_tenant` at transaction start (shown above).

5.4. Implement the `TenantService` gRPC interface and the provisioning API.

5.5. Write integration tests that verify tenant isolation: a query from tenant A's context cannot return rows belonging to tenant B, even if the WHERE clause is omitted.

5.6. Add NATS subject prefixing and the JWT-based authorization.

### Step 6: Managed A2A Relay

6.1. Build the relay service (Go, deployed on Kubernetes, horizontally scaled behind a load balancer).

6.2. Implement the WebSocket gateway with capability indexing in Postgres.

6.3. Implement the client library in `internal/commercial/relay/`.

6.4. Implement end-to-end encryption (Noise protocol) for inter-agent task delegation.

6.5. Implement usage metering and reporting back to the billing service.

6.6. Write load tests: 10,000 concurrent WebSocket connections, 1M discovery queries per hour, p99 latency < 50ms.

6.7. For Enterprise VPC peering, document the PrivateLink setup procedure and provide Terraform modules.

### Step 7: Enterprise Reporting

7.1. Build the template engine and the report data model (shown above).

7.2. Implement PDF and HTML exporters.

7.3. Implement the scheduler with cron-based dispatch.

7.4. Implement delivery channels: email (SMTP), webhook (HTTP POST with HMAC signature), S3 (multipart upload).

7.5. Implement MSP cross-tenant aggregation.

7.6. Build the frontend scheduling UI: a form to create a schedule (name, template, cron, recipients, channels).

### Step 8: Billing Integration

8.1. Build the OAP billing service (separate Go service, deployed in OAP's cloud).

8.2. Implement license key generation (shown above).

8.3. Set up the Stripe product and price catalog: per-endpoint tiers, per-agent-process tiers, enterprise flat-fee.

8.4. Implement the Stripe checkout flow and customer portal integration.

8.5. Implement webhook handlers for subscription lifecycle events (shown above).

8.6. Implement usage reporting from the OAP server to Stripe (5-minute cadence).

8.7. Build the customer-facing Billing dashboard in the web UI.

8.8. Build the internal admin tool for the OAP team: view all customers, manually generate/revoke license keys, override limits for support cases.

### Step 9: Documentation and Compliance

9.1. Write the public-facing pricing page on the OAP website with the tier comparison table.

9.2. Write the upgrade guide: how to obtain a license key, how to install it, how to verify the installation.

9.3. Write the air-gapped deployment guide for Enterprise customers.

9.4. Add the BSL rationale to the FAQ.

9.5. Train the support team on license key troubleshooting.

9.6. Set up the license key rotation procedure (rotating the signing key requires a coordinated binary release; document the runbook).

### Step 10: Launch

10.1. Announce the licensing model on the project blog and social media.

10.2. Offer a 60-day grace period for existing self-hosted users who may exceed 250 endpoints.

10.3. Monitor the upgrade funnel: self-hosted deployments -> commercial conversions.

10.4. Iterate on the Additional Use Grant based on real-world feedback.

---

## Summary

OpenAgentPlatform's licensing model is designed to maximize adoption at the self-hosted/Community tier while creating a sustainable commercial business at the Professional and Enterprise tiers. The BSL 1.1 license, combined with runtime feature gating and a clean build-tag boundary, achieves this without alienating the open-source community or creating legal ambiguity.

Key design decisions:

1. **BSL 1.1 over AGPL** to keep enterprise legal teams comfortable.
2. **Runtime feature gating over license enforcement in code** so the open-source binary is genuinely useful without a license.
3. **Build tags over separate repositories** for the commercial code to keep a single source of truth and avoid merge conflicts.
4. **EdDSA Ed25519 for license signing** for fast verification and small signatures.
5. **PostgreSQL RLS over schema-per-tenant by default** for operational simplicity, with schema-per-tenant available as an Enterprise option.
6. **Per-endpoint and per-agent-process pricing** aligned with how customers actually consume the product.
7. **DCO over a full CLA** to keep contribution friction low.

The result is a system that an open-source user can clone, build, and run for free, that a large enterprise can purchase with a clear SLA, and that a hyperscaler cannot repackage without a commercial agreement.
