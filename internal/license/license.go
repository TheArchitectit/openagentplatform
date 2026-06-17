// Package license implements the OAP commercial license engine: key parsing,
// Ed25519 signature verification, tier-based feature gating, quota enforcement,
// and trial-license generation.
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

// Tier identifies a commercial tier.
type Tier string

const (
	TierCommunity    Tier = "community"
	TierProfessional Tier = "professional"
	TierEnterprise   Tier = "enterprise"
)

// Sentinel errors returned by license validation and feature checks.
var (
	ErrInvalidKeyFormat = errors.New("license key format invalid")
	ErrInvalidSignature = errors.New("license signature verification failed")
	ErrLicenseExpired   = errors.New("license has expired")
	ErrLicenseNotYetValid = errors.New("license is not yet valid")
	ErrFeatureNotLicensed = errors.New("feature not included in license")
)

// GracePeriod is the number of days a license remains valid after its
// nominal expiry date. This window lets operators renew without an
// immediate hard cutover.
const GracePeriod = 7 * 24 * time.Hour

// License is the deserialised, verified payload of an OAP license key.
type License struct {
	ID         string    `json:"license_id"`
	OrgID      string    `json:"org_id"`
	Tier       Tier      `json:"tier"`
	MaxAgents  int       `json:"max_agents"`
	MaxUsers   int       `json:"max_users"`
	MaxSites   int       `json:"max_sites"`
	Features   []string  `json:"features"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Signature  []byte    `json:"-"` // populated by the validator, not from JSON
}

// IsExpired reports whether the license has passed its expiry date, not
// counting the grace period. Use Validate to account for the grace window.
func (l *License) IsExpired() bool {
	if l == nil {
		return true
	}
	return time.Now().After(l.ExpiresAt)
}

// IsInGracePeriod reports whether the license expired within the last
// GracePeriod and therefore is still considered valid by Validate.
func (l *License) IsInGracePeriod() bool {
	if l == nil {
		return false
	}
	now := time.Now()
	return now.After(l.ExpiresAt) && now.Before(l.ExpiresAt.Add(GracePeriod))
}

// HasFeature reports whether the given feature name is listed in the
// license's features slice.
func (l *License) HasFeature(name string) bool {
	if l == nil {
		return false
	}
	for _, f := range l.Features {
		if f == name {
			return true
		}
	}
	return false
}

// Validate checks the Ed25519 signature and the validity window of the
// license. A license that has expired is still considered valid during
// the grace period. Tier-specific feature flags are checked separately
// via HasFeature.
func (l *License) Validate(pub ed25519.PublicKey) error {
	if l == nil {
		return errors.New("nil license")
	}
	if len(l.Signature) == 0 {
		return ErrInvalidSignature
	}
	// Re-serialise the payload (without the signature field) for
	// verification. Because Signature is excluded from JSON, the bytes
	// produced here are exactly what the signer signed.
	payload, err := licensePayloadBytes(l)
	if err != nil {
		return fmt.Errorf("license: serialise payload: %w", err)
	}
	if !ed25519.Verify(pub, payload, l.Signature) {
		return ErrInvalidSignature
	}
	now := time.Now()
	if now.Before(l.IssuedAt) {
		return ErrLicenseNotYetValid
	}
	if now.After(l.ExpiresAt.Add(GracePeriod)) {
		return ErrLicenseExpired
	}
	return nil
}

// ParseKey decodes a signed license key string ("oap_<b64>.<b64>") and
// returns the verified License. The embedded OAPLicensePublicKey is used
// for signature verification.
func ParseKey(key string) (*License, error) {
	if !strings.HasPrefix(key, "oap_") {
		return nil, fmt.Errorf("%w: missing oap_ prefix", ErrInvalidKeyFormat)
	}
	body := strings.TrimPrefix(key, "oap_")
	parts := strings.SplitN(body, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: expected payload.signature", ErrInvalidKeyFormat)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: payload decode: %v", ErrInvalidKeyFormat, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: signature decode: %v", ErrInvalidKeyFormat, err)
	}
	var lic License
	if err := json.Unmarshal(payload, &lic); err != nil {
		return nil, fmt.Errorf("%w: payload parse: %v", ErrInvalidKeyFormat, err)
	}
	lic.Signature = sig
	if err := lic.Validate(OAPLicensePublicKey); err != nil {
		return nil, err
	}
	return &lic, nil
}

// licensePayloadBytes returns the canonical JSON encoding of a license
// without the Signature field. The signer operates on these bytes.
func licensePayloadBytes(l *License) ([]byte, error) {
	type wire struct {
		ID        string    `json:"license_id"`
		OrgID     string    `json:"org_id"`
		Tier      Tier      `json:"tier"`
		MaxAgents int       `json:"max_agents"`
		MaxUsers  int       `json:"max_users"`
		MaxSites  int       `json:"max_sites"`
		Features  []string  `json:"features"`
		IssuedAt  time.Time `json:"issued_at"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	return json.Marshal(wire{
		ID:        l.ID,
		OrgID:     l.OrgID,
		Tier:      l.Tier,
		MaxAgents: l.MaxAgents,
		MaxUsers:  l.MaxUsers,
		MaxSites:  l.MaxSites,
		Features:  l.Features,
		IssuedAt:  l.IssuedAt,
		ExpiresAt: l.ExpiresAt,
	})
}
