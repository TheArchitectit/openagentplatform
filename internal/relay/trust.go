package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

// This file implements the ISSUED-IDENTITY + ENTITLEMENT verification layer
// (RELAY-02 ADR §1–§2). It is the "not an open forwarder" property: every leg
// must present a CA-signed identity (mTLS, verified by the TLS stack via
// --trust-ca) AND a signed bearer token whose claims match the requested
// target, AND a persisted entitlement grant. Anything missing closes the leg.
//
// Trust config layout (decided 2026-08-24): SPLIT files. The mTLS CA pool is
// loaded separately via --trust-ca (shared with the RELAY-04 admin listener).
// This file (--trust-config) carries ONLY the TokenVerificationKey and the
// entitlement records.

const (
	// tokenClockSkew tolerates ±1m of clock drift when checking token expiry
	// (RELAY-02 §1.2).
	tokenClockSkew = time.Minute
	// defaultJtiTTL bounds how long a seen jti is remembered for replay
	// prevention (RELAY-02 §1.3: TTL = token lifetime).
	defaultJtiTTL = 24 * time.Hour
)

// Entitlement is a single source→target grant (RELAY-02 §2.1). A value of "*"
// for SourceAgentID or TargetAgentID matches any agent within the tenant.
type Entitlement struct {
	TenantID      string `json:"tenant_id"        yaml:"tenant_id"`
	SourceAgentID string `json:"source_agent_id"  yaml:"source_agent_id"`
	TargetAgentID string `json:"target_agent_id"  yaml:"target_agent_id"`
	Action        string `json:"action"           yaml:"action"`
}

// TrustConfig is the verified-identity configuration loaded from
// --trust-config. The mTLS CA pool is separate (--trust-ca): this file holds
// only the token-signing public key and entitlement grants.
type TrustConfig struct {
	Version        int                `json:"version"             yaml:"version"`
	PlatformKeyB64 string             `json:"platform_public_key" yaml:"platform_public_key"`
	Entitlements   []Entitlement      `json:"entitlements"        yaml:"entitlements"`
	Federation     *FederationSection `json:"federation"     yaml:"federation"`

	verifyKey ed25519.PublicKey // decoded PlatformKeyB64
}

// FederationSection extends the trust config with discovery federation peers
// (RELAY-05 ADR §2.4). Absent (nil) means no federation peers are configured.
type FederationSection struct {
	Peers            []FederationPeerConfig `json:"peers"             yaml:"peers"`
	PullInterval     string                 `json:"pull_interval"     yaml:"pull_interval"` // e.g. "5m"
	StartupReconcile bool                   `json:"startup_reconcile" yaml:"startup_reconcile"`
}

// FederationPeerConfig names one peer relay and its gRPC endpoint (ADR §2.4).
type FederationPeerConfig struct {
	RelayID  string `json:"relay_id" yaml:"relay_id"`
	Endpoint string `json:"endpoint" yaml:"endpoint"`
}

// DefaultFederationPullInterval is the pull cadence used when the config
// does not set one (ADR §2.3: 5 minutes).
const DefaultFederationPullInterval = 5 * time.Minute

// PullIntervalDuration resolves the configured pull interval, falling back to
// DefaultFederationPullInterval when unset, zero, or malformed.
func (f *FederationSection) PullIntervalDuration() time.Duration {
	if f == nil || f.PullInterval == "" {
		return DefaultFederationPullInterval
	}
	d, err := time.ParseDuration(f.PullInterval)
	if err != nil || d <= 0 {
		return DefaultFederationPullInterval
	}
	return d
}

// LoadTrustConfig reads and validates the trust config file. Fail-closed: a
// missing file, malformed YAML, or a bad public key is an error — there is no
// allow-by-default fallback (RELAY-02 §3 trust-config reload failure).
func LoadTrustConfig(path string) (*TrustConfig, error) {
	if path == "" {
		return nil, errors.New("relay: trust config path is required for admission")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("relay: read trust config: %w", err)
	}
	var tc TrustConfig
	if err := yaml.Unmarshal(raw, &tc); err != nil {
		return nil, fmt.Errorf("relay: parse trust config: %w", err)
	}
	if tc.Version != 1 {
		return nil, fmt.Errorf("relay: unsupported trust config version %d", tc.Version)
	}
	key, err := decodeEd25519Key(tc.PlatformKeyB64)
	if err != nil {
		return nil, fmt.Errorf("relay: platform_public_key: %w", err)
	}
	tc.verifyKey = key
	return &tc, nil
}

// decodeEd25519Key accepts standard or URL-safe base64 (padded or unpadded) of
// the 32-byte Ed25519 public key.
func decodeEd25519Key(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		raw, err := enc.DecodeString(s)
		if err != nil {
			continue
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("expected %d bytes, got %d", ed25519.PublicKeySize, len(raw))
		}
		return ed25519.PublicKey(raw), nil
	}
	return nil, fmt.Errorf("invalid base64 key")
}

// CheckEntitlement reports whether source→target is granted within tenant.
// Default-deny: no matching grant means false (RELAY-02 §2.2). "*" wildcards
// match any agent in the tenant (RELAY-02 §2.1).
func (t *TrustConfig) CheckEntitlement(tenantID, source, target string) bool {
	for _, e := range t.Entitlements {
		if e.TenantID != tenantID {
			continue
		}
		if e.Action != "" && e.Action != "relay" {
			continue
		}
		if e.SourceAgentID != source && e.SourceAgentID != "*" {
			continue
		}
		if e.TargetAgentID != target && e.TargetAgentID != "*" {
			continue
		}
		return true
	}
	return false
}

// tokenClaims is the signed bearer-token payload (RELAY-02 §1.2):
// `agentID | targetAgentID | tenantID | iat | exp`.
type tokenClaims struct {
	AgentID  string
	TargetID string
	TenantID string
	IssuedAt int64
	ExpiryAt int64
}

// VerifyToken verifies the Ed25519 signature over the token payload and checks
// that the claims match the verified mTLS principal, the requested target, and
// the cert-derived tenant, and that the token is not expired. Returns nil when
// the token is valid, or a non-nil error naming the failure.
func (t *TrustConfig) VerifyToken(token, principal, target, tenantID string, now time.Time) error {
	claims, err := t.decodeAndVerify(token)
	if err != nil {
		return err
	}
	switch {
	case claims.ExpiryAt <= claims.IssuedAt:
		return errors.New("token_invalid_expiry")
	case claims.AgentID != principal:
		return errors.New("token_agent_mismatch")
	case claims.TargetID != target:
		return errors.New("token_target_mismatch")
	case claims.TenantID != tenantID:
		return errors.New("token_tenant_mismatch")
	case now.Add(tokenClockSkew).Unix() >= claims.ExpiryAt:
		return errors.New("token_expired")
	case claims.IssuedAt-now.Unix() > int64(tokenClockSkew/time.Second):
		return errors.New("token_not_yet_valid")
	}
	return nil
}

// decodeAndVerify splits base64url(payload).base64url(sig), verifies the
// signature against the trust config's platform key, and parses the claims.
func (t *TrustConfig) decodeAndVerify(token string) (tokenClaims, error) {
	var c tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, errors.New("token_malformed")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, errors.New("token_malformed")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, errors.New("token_malformed")
	}
	if !ed25519.Verify(t.verifyKey, payload, sig) {
		return c, errors.New("token_signature_invalid")
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 5 {
		return c, errors.New("token_malformed")
	}
	iat, err1 := strconv.ParseInt(strings.TrimSpace(fields[3]), 10, 64)
	exp, err2 := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64)
	if err1 != nil || err2 != nil {
		return c, errors.New("token_malformed")
	}
	return tokenClaims{
		AgentID:  strings.TrimSpace(fields[0]),
		TargetID: strings.TrimSpace(fields[1]),
		TenantID: strings.TrimSpace(fields[2]),
		IssuedAt: iat,
		ExpiryAt: exp,
	}, nil
}

// jtiCache is a bounded LRU of seen token nonces (RELAY-02 §1.3). A repeated
// jti is rejected. Entries age out after defaultJtiTTL.
type jtiCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newJtiCache() *jtiCache {
	return &jtiCache{entries: make(map[string]time.Time)}
}

// seen records the nonce and reports whether it was already present.
func (j *jtiCache) seen(jti string, now time.Time) bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	// Opportunistically evict expired entries to bound memory.
	for k, seenAt := range j.entries {
		if now.Sub(seenAt) > defaultJtiTTL {
			delete(j.entries, k)
		}
	}

	if _, ok := j.entries[jti]; ok {
		return true
	}
	j.entries[jti] = now
	return false
}
