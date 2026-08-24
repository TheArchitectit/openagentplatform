package main

// server_tier.go implements org tier resolution from a signed license
// file. The license file (OAP_LICENSE_FILE env, default
// /etc/oap/license.json) is validated with Ed25519 at startup using
// the public key from OAP_LICENSE_PUBLIC_KEY (hex-encoded). Its tier
// becomes the platform tier. Without a key or file — or on any
// validation failure — the platform runs as Community (fail-closed).

import (
	"encoding/hex"
	"log/slog"
	"os"
	"time"

	license "github.com/openagentplatform/openagentplatform/internal/license"
	"github.com/openagentplatform/openagentplatform/internal/licensing"
)

// newTierResolver loads and validates the license file at startup and
// returns a resolveOrgTier-compatible function. Never fails: any error
// falls back to Community so the server always starts.
func newTierResolver(log *slog.Logger) func(orgID string) license.Tier {
	community := func(string) license.Tier { return license.TierCommunity }

	path := os.Getenv("OAP_LICENSE_FILE")
	if path == "" {
		path = "/etc/oap/license.json"
	}
	if _, err := os.Stat(path); err != nil {
		// No license file: Community, no warning — that is the default.
		return community
	}

	keyHex := os.Getenv("OAP_LICENSE_PUBLIC_KEY")
	if keyHex == "" {
		log.Warn("license: OAP_LICENSE_PUBLIC_KEY not set, cannot verify licenses; running as Community")
		return community
	}
	pubKey, err := hex.DecodeString(keyHex)
	if err != nil || len(pubKey) != 32 {
		log.Warn("license: invalid OAP_LICENSE_PUBLIC_KEY (want 64 hex chars); running as Community")
		return community
	}

	loader := licensing.NewLoader(licensing.NewValidator(pubKey), path)
	loadResult, err := loader.Load()
	if err != nil || loadResult == nil || !loadResult.Valid || loadResult.License == nil {
		reason := "unknown"
		if loadResult != nil && loadResult.Error != "" {
			reason = loadResult.Error
		}
		log.Warn("license: validation failed, running as Community", "path", path, "reason", reason, "err", err)
		return community
	}
	if loadResult.License.ExpiryDate.Before(time.Now()) {
		log.Warn("license: expired, running as Community", "expiry", loadResult.License.ExpiryDate)
		return community
	}

	tier := mapLicensingTier(loadResult.License.Tier)
	log.Info("license loaded", "tier", string(tier), "entity", loadResult.License.Entity)
	return func(string) license.Tier { return tier }
}

// mapLicensingTier converts the licensing package's tier vocabulary to
// the tenancy/license quota vocabulary ("pro" -> "professional").
func mapLicensingTier(t licensing.Tier) license.Tier {
	switch t {
	case licensing.TierPro:
		return license.TierProfessional
	case licensing.TierEnterprise:
		return license.TierEnterprise
	default:
		return license.TierCommunity
	}
}
