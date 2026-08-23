package licensing

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Generator generates signed license files.
// This is used by the license server, not shipped in production binaries.
type Generator struct {
	// privateKey is the Ed25519 private key for signing.
	privateKey ed25519.PrivateKey
}

// NewGenerator creates a new license generator with the given private key.
func NewGenerator(privateKey ed25519.PrivateKey) *Generator {
	return &Generator{
		privateKey: privateKey,
	}
}

// GenerateKeyPair generates a new Ed25519 key pair for license signing.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key pair: %w", err)
	}
	return pub, priv, nil
}

// GenerateLicense generates a signed license file.
func (g *Generator) GenerateLicense(entity string, tier Tier, features []Feature, endpointLimit int, duration time.Duration) (*LicenseFile, error) {
	now := time.Now().UTC()

	license := License{
		Entity:        entity,
		Tier:          tier,
		Features:      features,
		EndpointLimit: endpointLimit,
		IssueDate:     now,
		ExpiryDate:    now.Add(duration),
		Nonce:         generateNonce(),
	}

	// Sign license
	signature, err := g.signLicense(license)
	if err != nil {
		return nil, fmt.Errorf("failed to sign license: %w", err)
	}

	return &LicenseFile{
		Version:   1,
		License:   license,
		Signature: hex.EncodeToString(signature),
	}, nil
}

// signLicense signs a license with the private key.
func (g *Generator) signLicense(license License) ([]byte, error) {
	// Marshal license to canonical JSON
	licenseJSON, err := json.Marshal(license)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal license: %w", err)
	}

	// Sign
	signature := ed25519.Sign(g.privateKey, licenseJSON)
	return signature, nil
}

// generateNonce generates a random nonce for replay protection.
func generateNonce() string {
	// Use crypto/rand for production
	// For now, use timestamp-based nonce
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// LicenseFileToJSON converts a license file to JSON bytes.
func LicenseFileToJSON(licenseFile *LicenseFile) ([]byte, error) {
	return json.MarshalIndent(licenseFile, "", "  ")
}

// LicenseFileFromJSON parses a license file from JSON bytes.
func LicenseFileFromJSON(data []byte) (*LicenseFile, error) {
	var licenseFile LicenseFile
	if err := json.Unmarshal(data, &licenseFile); err != nil {
		return nil, fmt.Errorf("failed to parse license file: %w", err)
	}
	return &licenseFile, nil
}

// GenerateCommunityLicense generates a community license (no signing required).
func GenerateCommunityLicense(entity string) *LicenseFile {
	return &LicenseFile{
		Version: 1,
		License: License{
			Entity:        entity,
			Tier:          TierCommunity,
			Features:      []Feature{},
			EndpointLimit: 0,
			IssueDate:     time.Now().UTC(),
			ExpiryDate:    time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
			Nonce:         generateNonce(),
		},
		Signature: "", // Community licenses don't need signatures
	}
}

// GenerateProLicense generates a Pro license with common features.
func (g *Generator) GenerateProLicense(entity string, endpointLimit int, duration time.Duration) (*LicenseFile, error) {
	features := []Feature{
		FeatureMultiTenancy,
		FeatureBilling,
		FeatureAuditExport,
	}
	return g.GenerateLicense(entity, TierPro, features, endpointLimit, duration)
}

// GenerateEnterpriseLicense generates an Enterprise license with all features.
func (g *Generator) GenerateEnterpriseLicense(entity string, duration time.Duration) (*LicenseFile, error) {
	features := []Feature{
		FeatureMultiTenancy,
		FeatureManagedRelay,
		FeatureEnterpriseReporting,
		FeatureBilling,
		FeatureSSO,
		FeatureAuditExport,
	}
	return g.GenerateLicense(entity, TierEnterprise, features, 0, duration) // 0 = unlimited
}
