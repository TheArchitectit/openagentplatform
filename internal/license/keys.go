package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// OAPLicensePublicKey is the Ed25519 public key used to verify OAP
// license keys. It is embedded in the binary and cannot be replaced by
// the user. Rotate only with a coordinated binary release.
var OAPLicensePublicKey = ed25519.PublicKey{
	0x7c, 0x3a, 0x9f, 0x2b, 0xe1, 0x48, 0x5d, 0x6a,
	0x8b, 0xc4, 0x12, 0x9e, 0x3f, 0x77, 0x05, 0xa1,
	0x6b, 0x2c, 0x4d, 0x8e, 0xf9, 0x10, 0xa3, 0x55,
	0x7b, 0x1d, 0x88, 0xe2, 0x4c, 0x90, 0x3a, 0xb6,
}

// GenerateKeyPair returns a fresh Ed25519 key pair for license signing.
// The private key must be kept secret by the OAP licensing service.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("license: generate key: %w", err)
	}
	return pub, priv, nil
}

// Sign produces an Ed25519 signature over the canonical payload of the
// license and stores it in the License's Signature field. It returns a
// fully-signed key string in the "oap_<b64>.<b64>" wire format.
func Sign(lic *License, priv ed25519.PrivateKey) (string, error) {
	if lic == nil {
		return "", errors.New("license: nil license")
	}
	payload, err := licensePayloadBytes(lic)
	if err != nil {
		return "", fmt.Errorf("license: serialise payload: %w", err)
	}
	lic.Signature = ed25519.Sign(priv, payload)

	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	sigB64 := base64.RawURLEncoding.EncodeToString(lic.Signature)
	return "oap_" + payloadB64 + "." + sigB64, nil
}

// EmbedPublicKey returns the public key that is compiled into the
// binary. Exposed as a function so other packages can compare the key
// without depending on the unexported var directly.
func EmbedPublicKey() ed25519.PublicKey {
	return OAPLicensePublicKey
}

// TrialDuration is the lifetime of an auto-generated trial license.
const TrialDuration = 14 * 24 * time.Hour

// TrialFeatures are the features unlocked for a trial license, matching
// the professional tier so the customer can evaluate the full
// commercial offering.
var TrialFeatures = []string{
	"a2a_managed_relay",
	"enterprise_sso",
	"audit_export",
	"advanced_reporting",
	"custom_retention",
	"priority_support",
}

// base64RawURLEncode is a thin wrapper around base64.RawURLEncoding.EncodeToString
// kept in this file so callers do not need to import encoding/base64.
func base64RawURLEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// GenerateTrial returns a 14-day trial license for the given org.
// The license is signed with the supplied private key.
func GenerateTrial(orgID string, priv ed25519.PrivateKey) (*License, string, error) {
	if orgID == "" {
		return nil, "", errors.New("license: orgID is required")
	}
	now := time.Now()
	lic := &License{
		ID:        fmt.Sprintf("lic_trial_%d", now.UnixNano()),
		OrgID:     orgID,
		Tier:      TierProfessional,
		MaxAgents: 50,
		MaxUsers:  10,
		MaxSites:  5,
		Features:  append([]string(nil), TrialFeatures...),
		IssuedAt:  now,
		ExpiresAt: now.Add(TrialDuration),
	}
	key, err := Sign(lic, priv)
	if err != nil {
		return nil, "", err
	}
	return lic, key, nil
}
