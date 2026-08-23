package licensing

import (
	"testing"
	"time"
)

func TestValidator_Validate_ValidLicense(t *testing.T) {
	// Generate key pair
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	// Create generator
	gen := NewGenerator(priv)

	// Generate license
	licenseFile, err := gen.GenerateProLicense("Test Company", 100, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate license: %v", err)
	}

	// Create validator
	validator := NewValidator(pub)

	// Convert to signed license
	sigBytes, _ := hexDecode(licenseFile.Signature)
	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	// Validate
	result := validator.Validate(signed)
	if !result.Valid {
		t.Errorf("expected valid license, got error: %s", result.Error)
	}
	if result.License == nil {
		t.Fatal("expected license in result")
	}
	if result.License.Entity != "Test Company" {
		t.Errorf("expected entity 'Test Company', got %q", result.License.Entity)
	}
	if result.License.Tier != TierPro {
		t.Errorf("expected tier 'pro', got %q", result.License.Tier)
	}
}

func TestValidator_Validate_InvalidSignature(t *testing.T) {
	// Generate two key pairs
	_, priv1, _ := GenerateKeyPair()
	pub2, _, _ := GenerateKeyPair()

	// Sign with key 1
	gen := NewGenerator(priv1)
	licenseFile, _ := gen.GenerateProLicense("Test", 100, 365*24*time.Hour)

	// Validate with key 2 (wrong key)
	validator := NewValidator(pub2)
	sigBytes, _ := hexDecode(licenseFile.Signature)
	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	result := validator.Validate(signed)
	if result.Valid {
		t.Error("expected invalid license due to wrong key")
	}
	if result.Error == "" {
		t.Error("expected error message")
	}
}

func TestValidator_Validate_ExpiredLicense(t *testing.T) {
	// Generate key pair
	pub, priv, _ := GenerateKeyPair()

	// Create license that expired 60 days ago (beyond 30-day grace)
	gen := NewGenerator(priv)
	licenseFile, _ := gen.GenerateLicense("Test", TierPro, []Feature{FeatureBilling}, 100, -60*24*time.Hour)

	// Create validator with 30-day grace period
	validator := NewValidator(pub).
		WithGracePeriod(30 * 24 * time.Hour).
		WithNow(func() time.Time {
			return time.Now()
		})

	sigBytes, _ := hexDecode(licenseFile.Signature)
	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	result := validator.Validate(signed)
	if result.Valid {
		t.Error("expected invalid license due to expiry beyond grace period")
	}
}

func TestValidator_Validate_GracePeriod(t *testing.T) {
	// Generate key pair
	pub, priv, _ := GenerateKeyPair()

	// Create license that expired 10 days ago
	gen := NewGenerator(priv)
	licenseFile, _ := gen.GenerateLicense("Test", TierPro, []Feature{FeatureBilling}, 100, -10*24*time.Hour)

	// Create validator with 30-day grace period
	validator := NewValidator(pub).
		WithGracePeriod(30 * 24 * time.Hour).
		WithNow(func() time.Time {
			return time.Now()
		})

	sigBytes, _ := hexDecode(licenseFile.Signature)
	signed := &SignedLicense{
		License:   licenseFile.License,
		Signature: sigBytes,
	}

	result := validator.Validate(signed)
	if !result.Valid {
		t.Errorf("expected valid license in grace period, got error: %s", result.Error)
	}
	if !result.GracePeriod {
		t.Error("expected grace period flag")
	}
}

func TestLicense_HasFeature(t *testing.T) {
	license := &License{
		Features: []Feature{FeatureMultiTenancy, FeatureBilling},
	}

	tests := []struct {
		feature  Feature
		expected bool
	}{
		{FeatureMultiTenancy, true},
		{FeatureBilling, true},
		{FeatureManagedRelay, false},
		{FeatureSSO, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.feature), func(t *testing.T) {
			if got := license.HasFeature(tt.feature); got != tt.expected {
				t.Errorf("HasFeature(%q) = %v, want %v", tt.feature, got, tt.expected)
			}
		})
	}
}

func TestLicense_IsTierOrAbove(t *testing.T) {
	tests := []struct {
		name     string
		tier     Tier
		required Tier
		expected bool
	}{
		{"community meets community", TierCommunity, TierCommunity, true},
		{"community below pro", TierCommunity, TierPro, false},
		{"pro meets pro", TierPro, TierPro, true},
		{"pro above community", TierPro, TierCommunity, true},
		{"pro below enterprise", TierPro, TierEnterprise, false},
		{"enterprise meets enterprise", TierEnterprise, TierEnterprise, true},
		{"enterprise above pro", TierEnterprise, TierPro, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			license := &License{Tier: tt.tier}
			if got := license.IsTierOrAbove(tt.required); got != tt.expected {
				t.Errorf("IsTierOrAbove(%q) = %v, want %v", tt.required, got, tt.expected)
			}
		})
	}
}

func TestLicense_CanAddEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		limit         int
		current       int
		add           int
		expected      bool
		expectedError bool
	}{
		{"unlimited", 0, 100, 50, true, false},
		{"within limit", 100, 50, 30, true, false},
		{"at limit", 100, 100, 0, true, false},
		{"exceeds limit", 100, 80, 30, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			license := &License{EndpointLimit: tt.limit}

			if got := license.CanAddEndpoints(tt.current, tt.add); got != tt.expected {
				t.Errorf("CanAddEndpoints(%d, %d) = %v, want %v", tt.current, tt.add, got, tt.expected)
			}

			err := license.EndpointLimitExceeded(tt.current, tt.add)
			if tt.expectedError && err == nil {
				t.Error("expected error for endpoint limit exceeded")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultCommunityLicense(t *testing.T) {
	license := DefaultCommunityLicense()

	if license.Entity != "OpenAgentPlatform Community" {
		t.Errorf("expected entity 'OpenAgentPlatform Community', got %q", license.Entity)
	}
	if license.Tier != TierCommunity {
		t.Errorf("expected tier 'community', got %q", license.Tier)
	}
	if len(license.Features) != 0 {
		t.Errorf("expected no features, got %d", len(license.Features))
	}
	if license.EndpointLimit != 0 {
		t.Errorf("expected unlimited endpoints, got %d", license.EndpointLimit)
	}
}
