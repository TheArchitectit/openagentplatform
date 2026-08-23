// Package licensing implements Ed25519 license validation for OpenAgentPlatform.
//
// License files are signed with Ed25519 and contain:
// - Licensed entity (company/individual name)
// - License tier (Community, Pro, Enterprise)
// - Enabled features (list of feature flags)
// - Endpoint count limit
// - Issue date and expiry date
// - Ed25519 signature
//
// The public key is embedded in the binary; the private key never ships.
// Invalid signatures cause complete license rejection.
package licensing

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"time"
)

// Tier represents the license tier level.
type Tier string

const (
	// TierCommunity is the open-source tier with no gating.
	TierCommunity Tier = "community"
	// TierPro is the commercial tier with gated features and endpoint limits.
	TierPro Tier = "pro"
	// TierEnterprise is the full-featured tier with no limits.
	TierEnterprise Tier = "enterprise"
)

// Feature represents a gated feature flag.
type Feature string

const (
	// FeatureMultiTenancy enables multi-tenant isolation.
	FeatureMultiTenancy Feature = "multi_tenancy"
	// FeatureManagedRelay enables managed A2A relay service.
	FeatureManagedRelay Feature = "managed_relay"
	// FeatureEnterpriseReporting enables enterprise reporting.
	FeatureEnterpriseReporting Feature = "enterprise_reporting"
	// FeatureBilling enables Stripe billing integration.
	FeatureBilling Feature = "billing"
	// FeatureSSO enables single sign-on.
	FeatureSSO Feature = "sso"
	// FeatureAuditExport enables audit log export.
	FeatureAuditExport Feature = "audit_export"
)

// License represents a decoded license before signature verification.
type License struct {
	// Entity is the licensed company or individual name.
	Entity string `json:"entity"`
	// Tier is the license tier (community, pro, enterprise).
	Tier Tier `json:"tier"`
	// Features is the list of enabled feature flags.
	Features []Feature `json:"features"`
	// EndpointLimit is the maximum number of endpoints (0 = unlimited).
	EndpointLimit int `json:"endpoint_limit"`
	// IssueDate is when the license was issued.
	IssueDate time.Time `json:"issue_date"`
	// ExpiryDate is when the license expires.
	ExpiryDate time.Time `json:"expiry_date"`
	// Nonce is a random value to prevent replay attacks.
	Nonce string `json:"nonce"`
}

// SignedLicense is a license with its Ed25519 signature.
type SignedLicense struct {
	// License is the license data.
	License License `json:"license"`
	// Signature is the Ed25519 signature of the license JSON.
	Signature []byte `json:"signature"`
}

// ValidationResult contains the result of license validation.
type ValidationResult struct {
	// Valid indicates if the license is valid and not expired.
	Valid bool
	// License is the validated license (nil if invalid).
	License *License
	// Error describes why validation failed (empty if valid).
	Error string
	// GracePeriod indicates if the license is in grace period.
	GracePeriod bool
	// GraceExpiry is when the grace period ends (zero if not in grace).
	GraceExpiry time.Time
}

// Validator validates Ed25519-signed licenses.
type Validator struct {
	// publicKey is the Ed25519 public key for signature verification.
	publicKey ed25519.PublicKey
	// gracePeriod is the offline grace period after expiry.
	gracePeriod time.Duration
	// now returns the current time (overridable for testing).
	now func() time.Time
}

// NewValidator creates a new license validator with the given public key.
func NewValidator(publicKey ed25519.PublicKey) *Validator {
	return &Validator{
		publicKey:   publicKey,
		gracePeriod: 30 * 24 * time.Hour, // 30 days default grace
		now:         time.Now,
	}
}

// WithGracePeriod sets the offline grace period.
func (v *Validator) WithGracePeriod(d time.Duration) *Validator {
	v.gracePeriod = d
	return v
}

// WithNow sets the time function (for testing).
func (v *Validator) WithNow(now func() time.Time) *Validator {
	v.now = now
	return v
}

// Validate validates a signed license.
func (v *Validator) Validate(signed *SignedLicense) *ValidationResult {
	if signed == nil {
		return &ValidationResult{Error: "license is nil"}
	}

	// Verify signature
	if err := v.verifySignature(signed); err != nil {
		return &ValidationResult{Error: fmt.Sprintf("invalid signature: %v", err)}
	}

	// Check expiry
	now := v.now()
	license := signed.License

	if now.After(license.ExpiryDate) {
		// Check grace period
		graceExpiry := license.ExpiryDate.Add(v.gracePeriod)
		if now.After(graceExpiry) {
			return &ValidationResult{
				Error: fmt.Sprintf("license expired on %s, grace period ended on %s",
					license.ExpiryDate.Format(time.RFC3339),
					graceExpiry.Format(time.RFC3339)),
			}
		}

		// In grace period
		return &ValidationResult{
			Valid:       true,
			License:     &license,
			GracePeriod: true,
			GraceExpiry: graceExpiry,
		}
	}

	// License is valid
	return &ValidationResult{
		Valid:   true,
		License: &license,
	}
}

// verifySignature verifies the Ed25519 signature of a signed license.
func (v *Validator) verifySignature(signed *SignedLicense) error {
	// Marshal license to canonical JSON
	licenseJSON, err := json.Marshal(signed.License)
	if err != nil {
		return fmt.Errorf("failed to marshal license: %w", err)
	}

	// Verify signature
	if !ed25519.Verify(v.publicKey, licenseJSON, signed.Signature) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// HasFeature checks if a license includes a specific feature.
func (l *License) HasFeature(feature Feature) bool {
	for _, f := range l.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// IsTierOrAbove checks if the license tier is at or above the required tier.
func (l *License) IsTierOrAbove(required Tier) bool {
	tierOrder := map[Tier]int{
		TierCommunity:  0,
		TierPro:        1,
		TierEnterprise: 2,
	}

	licenseTier, ok := tierOrder[l.Tier]
	if !ok {
		return false
	}

	requiredTier, ok := tierOrder[required]
	if !ok {
		return false
	}

	return licenseTier >= requiredTier
}

// CanAddEndpoints checks if adding the given count would exceed the limit.
func (l *License) CanAddEndpoints(currentCount, addCount int) bool {
	if l.EndpointLimit == 0 {
		return true // unlimited
	}
	return currentCount+addCount <= l.EndpointLimit
}

// EndpointLimitExceeded returns an error message if the limit would be exceeded.
func (l *License) EndpointLimitExceeded(currentCount, addCount int) error {
	if l.EndpointLimit == 0 {
		return nil
	}
	if currentCount+addCount > l.EndpointLimit {
		return fmt.Errorf("endpoint limit exceeded: license allows %d endpoints, currently %d, attempting to add %d (upgrade to Pro or Enterprise for more)",
			l.EndpointLimit, currentCount, addCount)
	}
	return nil
}

// DefaultCommunityLicense returns a default community license with no gating.
func DefaultCommunityLicense() *License {
	return &License{
		Entity:        "OpenAgentPlatform Community",
		Tier:          TierCommunity,
		Features:      []Feature{},
		EndpointLimit: 0, // unlimited
		IssueDate:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:    time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
	}
}
