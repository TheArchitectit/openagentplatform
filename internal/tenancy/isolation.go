// Package tenancy provides per-tenant data isolation, quota enforcement,
// and retention-based cleanup for the openagentplatform server.
//
// The package follows a three-file split:
//
//   - isolation.go – TenantContext and middleware that enriches the
//     request context with tenant information (OrgID, tier, quotas,
//     feature flags).
//   - quotas.go    – QuotaDefinition and EnforceQuota for per-tier
//     limits (agents, users, sites, API calls, A2A calls).
//   - cleanup.go   – RetentionPurger background worker that soft-deletes
//     and hard-deletes old audit_events and check_results rows.
package tenancy

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/license"
)

// ctxKey is a private type to prevent collisions in context.Value lookups.
type ctxKey int

const (
	ctxTenantKey ctxKey = iota
)

// FeatureFlag represents a single boolean feature gate for a tenant.
type FeatureFlag struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// QuotaSnapshot is a point-in-time view of a tenant's quota limits and
// current usage.  Values are denormalised into the struct so handlers
// can read them without an extra database round-trip.
type QuotaSnapshot struct {
	MaxAgents     int `json:"max_agents"`     // 0 = unlimited
	MaxUsers      int `json:"max_users"`      // 0 = unlimited
	MaxSites      int `json:"max_sites"`      // 0 = unlimited
	MaxAPIReqPerHour  int `json:"max_api_req_per_hour"`  // 0 = unlimited
	MaxA2ACallsPerDay int `json:"max_a2a_calls_per_day"` // 0 = unlimited
}

// TenantContext carries per-tenant metadata through the request lifecycle.
type TenantContext struct {
	OrgID        string          `json:"org_id"`
	Tier         license.Tier    `json:"tier"`
	Quotas       QuotaSnapshot   `json:"quotas"`
	FeatureFlags []FeatureFlag   `json:"feature_flags"`
}

// WithTenantContext returns a new context that carries the given
// TenantContext.  Used by TenantMiddleware and tests.
func WithTenantContext(ctx context.Context, tc *TenantContext) context.Context {
	return context.WithValue(ctx, ctxTenantKey, tc)
}

// GetTenant retrieves the TenantContext previously stored in ctx by
// TenantMiddleware.  The second return value is false when no tenant
// context is present (e.g. on a public route or before middleware ran).
func GetTenant(ctx context.Context) (*TenantContext, bool) {
	tc, ok := ctx.Value(ctxTenantKey).(*TenantContext)
	return tc, ok
}

// featureFlagsForTier returns the canonical feature-flag set for a tier.
// Flags are tier-specific – higher tiers gain additional capabilities.
func featureFlagsForTier(tier license.Tier) []FeatureFlag {
	common := []FeatureFlag{
		{Name: "audit_log", Enabled: true},
		{Name: "agent_registration", Enabled: true},
		{Name: "basic_monitoring", Enabled: true},
	}
	switch tier {
	case license.TierProfessional:
		return append(common,
			FeatureFlag{Name: "policy_engine", Enabled: true},
			FeatureFlag{Name: "patch_deployment", Enabled: true},
			FeatureFlag{Name: "custom_scripts", Enabled: true},
		)
	case license.TierEnterprise:
		return append(common,
			FeatureFlag{Name: "policy_engine", Enabled: true},
			FeatureFlag{Name: "patch_deployment", Enabled: true},
			FeatureFlag{Name: "custom_scripts", Enabled: true},
			FeatureFlag{Name: "multi_region", Enabled: true},
			FeatureFlag{Name: "sso_saml", Enabled: true},
			FeatureFlag{Name: "priority_support", Enabled: true},
		)
	default: // community
		return common
	}
}

// resolveTenant builds a TenantContext from session claims and a tier
// resolver function.  The tierResolver indirection keeps this package
// decoupled from the billing/license runtime resolution – callers can
// pass a closure that looks up the tier from a database, a license key,
// or a hard-coded default.
func resolveTenant(claims *auth.SessionClaims, tierResolver func(orgID string) license.Tier) *TenantContext {
	if claims == nil || claims.OrgID == "" {
		return nil
	}
	tier := license.TierCommunity
	if tierResolver != nil {
		tier = tierResolver(claims.OrgID)
	}
	quotas := QuotasForTier(tier)
	return &TenantContext{
		OrgID:        claims.OrgID,
		Tier:         tier,
		Quotas:       quotas,
		FeatureFlags: featureFlagsForTier(tier),
	}
}

// TenantMiddleware returns a chi-compatible middleware that loads the
// caller's tenant information from JWT claims and enriches the request
// context with a TenantContext.  It must run AFTER auth.VerifierMiddleware
// (so claims are present) and AFTER orgContextMiddleware (so OrgID is
// guaranteed non-empty).  Routes that do not require tenant context
// (e.g. public health checks) should be mounted before this middleware.
func TenantMiddleware(tierResolver func(orgID string) license.Tier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.UserFromContext(r.Context())
			if !ok || claims == nil || claims.OrgID == "" {
				http.Error(w, `{"error":"no tenant context"}`, http.StatusForbidden)
				return
			}
			tc := resolveTenant(claims, tierResolver)
			if tc == nil {
				http.Error(w, `{"error":"tenant resolution failed"}`, http.StatusInternalServerError)
				return
			}
			ctx := WithTenantContext(r.Context(), tc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantMiddlewareFromContext is a variant of TenantMiddleware that
// accepts an already-resolved TenantContext (useful for tests or when
// the tier is known at request-entry time).
func TenantMiddlewareFromContext(tc *TenantContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc == nil {
				http.Error(w, `{"error":"nil tenant"}`, http.StatusInternalServerError)
				return
			}
			ctx := WithTenantContext(r.Context(), tc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Ensure chi import is used (suppresses unused-import linter when this
// file is built in isolation).
var _ = chi.NewRouter
