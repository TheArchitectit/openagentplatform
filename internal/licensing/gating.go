package licensing

import (
	"context"
	"fmt"
	"net/http"
)

// contextKey is a private type for context keys.
type contextKey string

// LicenseContextKey is the context key for the validated license.
const LicenseContextKey contextKey = "license"

// GateConfig configures feature gating behavior.
type GateConfig struct {
	// Validator is the license validator.
	Validator *Validator
	// Loader is the license loader.
	Loader *Loader
	// RequiredFeatures maps feature names to their required tiers.
	RequiredFeatures map[Feature]Tier
}

// DefaultGateConfig returns the default gate configuration.
func DefaultGateConfig(validator *Validator, loader *Loader) *GateConfig {
	return &GateConfig{
		Validator: validator,
		Loader:    loader,
		RequiredFeatures: map[Feature]Tier{
			FeatureMultiTenancy:         TierPro,
			FeatureManagedRelay:         TierEnterprise,
			FeatureEnterpriseReporting:  TierEnterprise,
			FeatureBilling:              TierPro,
			FeatureSSO:                  TierEnterprise,
			FeatureAuditExport:          TierPro,
		},
	}
}

// Gater enforces feature gating based on license entitlements.
type Gater struct {
	config    *GateConfig
	validator *Validator
	loader    *Loader
}

// NewGater creates a new feature gater.
func NewGater(config *GateConfig) *Gater {
	return &Gater{
		config:    config,
		validator: config.Validator,
		loader:    config.Loader,
	}
}

// RequireFeature returns an HTTP middleware that requires a specific feature.
func (g *Gater) RequireFeature(feature Feature) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get license from context
			license := GetLicenseFromContext(r.Context())
			if license == nil {
				http.Error(w, `{"error":"license not loaded"}`, http.StatusInternalServerError)
				return
			}

			// Check if feature is enabled
			if !license.HasFeature(feature) {
				requiredTier := g.config.RequiredFeatures[feature]
				http.Error(w, fmt.Sprintf(`{"error":"feature %q requires %s tier or higher","required_tier":"%s","current_tier":"%s"}`,
					feature, requiredTier, requiredTier, license.Tier), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireTier returns an HTTP middleware that requires a minimum tier.
func (g *Gater) RequireTier(tier Tier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get license from context
			license := GetLicenseFromContext(r.Context())
			if license == nil {
				http.Error(w, `{"error":"license not loaded"}`, http.StatusInternalServerError)
				return
			}

			// Check tier
			if !license.IsTierOrAbove(tier) {
				http.Error(w, fmt.Sprintf(`{"error":"requires %s tier or higher","current_tier":"%s"}`,
					tier, license.Tier), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// LicenseMiddleware returns an HTTP middleware that loads and validates the license.
func (g *Gater) LicenseMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Load license
			result, err := g.loader.Load()
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"failed to load license: %v"}`, err), http.StatusInternalServerError)
				return
			}

			// Check if license is valid
			if !result.Valid {
				http.Error(w, fmt.Sprintf(`{"error":"invalid license: %s"}`, result.Error), http.StatusForbidden)
				return
			}

			// Add license to context
			ctx := context.WithValue(r.Context(), LicenseContextKey, result.License)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetLicenseFromContext retrieves the license from the context.
func GetLicenseFromContext(ctx context.Context) *License {
	license, _ := ctx.Value(LicenseContextKey).(*License)
	return license
}

// CheckFeature checks if a feature is enabled in the current license.
// Returns an error if the feature is not available.
func CheckFeature(ctx context.Context, feature Feature, requiredTier Tier) error {
	license := GetLicenseFromContext(ctx)
	if license == nil {
		return fmt.Errorf("license not loaded")
	}

	if !license.HasFeature(feature) {
		return fmt.Errorf("feature %q requires %s tier or higher (current: %s)",
			feature, requiredTier, license.Tier)
	}

	return nil
}

// CheckTier checks if the current license meets the required tier.
func CheckTier(ctx context.Context, required Tier) error {
	license := GetLicenseFromContext(ctx)
	if license == nil {
		return fmt.Errorf("license not loaded")
	}

	if !license.IsTierOrAbove(required) {
		return fmt.Errorf("requires %s tier or higher (current: %s)", required, license.Tier)
	}

	return nil
}

// CheckEndpointLimit checks if adding endpoints would exceed the limit.
func CheckEndpointLimit(ctx context.Context, currentCount, addCount int) error {
	license := GetLicenseFromContext(ctx)
	if license == nil {
		return fmt.Errorf("license not loaded")
	}

	return license.EndpointLimitExceeded(currentCount, addCount)
}
