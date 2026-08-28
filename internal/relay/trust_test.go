package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// helperTrustYAML produces a valid trust config YAML with the given public key
// and entitlements.
func helperTrustYAML(pubKey ed25519.PublicKey, ents []Entitlement) string {
	var b strings.Builder
	b.WriteString("version: 1\n")
	b.WriteString(fmt.Sprintf("platform_public_key: %s\n", base64.RawURLEncoding.EncodeToString(pubKey)))
	b.WriteString("entitlements:\n")
	for _, e := range ents {
		b.WriteString(fmt.Sprintf("  - tenant_id: %q\n", e.TenantID))
		b.WriteString(fmt.Sprintf("    source_agent_id: %q\n", e.SourceAgentID))
		b.WriteString(fmt.Sprintf("    target_agent_id: %q\n", e.TargetAgentID))
		b.WriteString(fmt.Sprintf("    action: %q\n", e.Action))
	}
	return b.String()
}

// helperSignToken creates a signed bearer token: base64url(payload).base64url(sig).
func helperSignToken(privKey ed25519.PrivateKey, agentID, targetID, tenantID string, iat, exp int64) string {
	payload := fmt.Sprintf("%s|%s|%s|%d|%d", agentID, targetID, tenantID, iat, exp)
	sig := ed25519.Sign(privKey, []byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

func TestLoadTrustConfig_Valid(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_ = priv
	yaml := helperTrustYAML(pub, []Entitlement{
		{TenantID: "t1", SourceAgentID: "a1", TargetAgentID: "a2", Action: "relay"},
	})
	f, err := os.CreateTemp("", "trust-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	tc, err := LoadTrustConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadTrustConfig: %v", err)
	}
	if tc.Version != 1 {
		t.Errorf("Version = %d, want 1", tc.Version)
	}
	if len(tc.Entitlements) != 1 {
		t.Errorf("len(Entitlements) = %d, want 1", len(tc.Entitlements))
	}
}

func TestLoadTrustConfig_EmptyPath(t *testing.T) {
	_, err := LoadTrustConfig("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestLoadTrustConfig_BadVersion(t *testing.T) {
	f, err := os.CreateTemp("", "trust-config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("version: 2\nplatform_public_key: \"aaa\"\n")
	f.Close()

	_, err = LoadTrustConfig(f.Name())
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported-version error, got: %v", err)
	}
}

func TestDecodeEd25519Key_StdBase64(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	encoded := base64.StdEncoding.EncodeToString(pub)
	decoded, err := decodeEd25519Key(encoded)
	if err != nil {
		t.Fatalf("decodeEd25519Key: %v", err)
	}
	if !equalBytes(pub, decoded) {
		t.Error("decoded key does not match original")
	}
}

func TestDecodeEd25519Key_UrlSafeBase64(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	encoded := base64.RawURLEncoding.EncodeToString(pub)
	decoded, err := decodeEd25519Key(encoded)
	if err != nil {
		t.Fatalf("decodeEd25519Key: %v", err)
	}
	if !equalBytes(pub, decoded) {
		t.Error("decoded key does not match original")
	}
}

func TestDecodeEd25519Key_WrongLength(t *testing.T) {
	_, err := decodeEd25519Key(base64.StdEncoding.EncodeToString([]byte("short")))
	if err == nil {
		t.Fatal("expected error for wrong-length key")
	}
}

func equalBytes(a, b ed25519.PublicKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCheckEntitlement_DefaultDeny(t *testing.T) {
	tc := &TrustConfig{Entitlements: nil}
	if tc.CheckEntitlement("t1", "a1", "a2") {
		t.Fatal("expected deny with no entitlements")
	}
}

func TestCheckEntitlement_ExactMatch(t *testing.T) {
	tc := &TrustConfig{Entitlements: []Entitlement{
		{TenantID: "t1", SourceAgentID: "a1", TargetAgentID: "a2", Action: "relay"},
	}}
	if !tc.CheckEntitlement("t1", "a1", "a2") {
		t.Fatal("expected allow for exact match")
	}
	if tc.CheckEntitlement("t1", "a1", "a3") {
		t.Fatal("expected deny for wrong target")
	}
	if tc.CheckEntitlement("t2", "a1", "a2") {
		t.Fatal("expected deny for wrong tenant")
	}
}

func TestCheckEntitlement_Wildcard(t *testing.T) {
	tc := &TrustConfig{Entitlements: []Entitlement{
		{TenantID: "t1", SourceAgentID: "*", TargetAgentID: "*", Action: "relay"},
	}}
	if !tc.CheckEntitlement("t1", "anything", "anyother") {
		t.Fatal("expected allow for wildcard")
	}
}

func TestCheckEntitlement_SourceWildcard(t *testing.T) {
	tc := &TrustConfig{Entitlements: []Entitlement{
		{TenantID: "t1", SourceAgentID: "*", TargetAgentID: "a2", Action: "relay"},
	}}
	if !tc.CheckEntitlement("t1", "any-source", "a2") {
		t.Fatal("expected allow for source-wildcard")
	}
	if tc.CheckEntitlement("t1", "any-source", "a3") {
		t.Fatal("expected deny for wrong target with source-wildcard")
	}
}

func TestVerifyToken_Valid(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	iat := now.Unix() - 10
	exp := now.Unix() + 300

	token := helperSignToken(priv, "oap:agentA", "oap:agentB", "t1", iat, exp)
	if err := tc.VerifyToken(token, "oap:agentA", "oap:agentB", "t1", now); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
}

func TestVerifyToken_Expired(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	iat := now.Unix() - 600
	exp := now.Unix() - 60 // expired 1m ago

	token := helperSignToken(priv, "a1", "a2", "t1", iat, exp)
	if err := tc.VerifyToken(token, "a1", "a2", "t1", now); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestVerifyToken_AgentMismatch(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	token := helperSignToken(priv, "a1", "a2", "t1", now.Unix()-10, now.Unix()+300)
	if err := tc.VerifyToken(token, "a3", "a2", "t1", now); err == nil {
		t.Fatal("expected agent mismatch error")
	}
}

func TestVerifyToken_TargetMismatch(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	token := helperSignToken(priv, "a1", "a2", "t1", now.Unix()-10, now.Unix()+300)
	if err := tc.VerifyToken(token, "a1", "a3", "t1", now); err == nil {
		t.Fatal("expected target mismatch error")
	}
}

func TestVerifyToken_TenantMismatch(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	token := helperSignToken(priv, "a1", "a2", "t1", now.Unix()-10, now.Unix()+300)
	if err := tc.VerifyToken(token, "a1", "a2", "t2", now); err == nil {
		t.Fatal("expected tenant mismatch error")
	}
}

func TestVerifyToken_BadSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	token := helperSignToken(otherPriv, "a1", "a2", "t1", now.Unix()-10, now.Unix()+300)
	if err := tc.VerifyToken(token, "a1", "a2", "t1", now); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestVerifyToken_Malformed(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	for _, bad := range []string{"", "no.dots", "a.b.c"} {
		if err := tc.VerifyToken(bad, "a1", "a2", "t1", now); err == nil {
			t.Fatalf("expected error for malformed token %q", bad)
		}
	}
}

func TestVerifyToken_InvalidExpiry(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	tc := &TrustConfig{verifyKey: pub}

	now := time.Now()
	// exp == iat → invalid
	token := helperSignToken(priv, "a1", "a2", "t1", now.Unix(), now.Unix())
	if err := tc.VerifyToken(token, "a1", "a2", "t1", now); err == nil {
		t.Fatal("expected invalid-expiry error")
	}
}

func TestJtiCache_Replay(t *testing.T) {
	cache := newJtiCache()
	now := time.Now()

	if cache.seen("abc123", now) {
		t.Fatal("first seen should return false (not a repeat)")
	}
	if !cache.seen("abc123", now) {
		t.Fatal("second seen should return true (replay detected)")
	}
}

func TestJtiCache_DifferentJti(t *testing.T) {
	cache := newJtiCache()
	now := time.Now()

	if cache.seen("jti1", now) {
		t.Fatal("first jti should not be a repeat")
	}
	if cache.seen("jti2", now) {
		t.Fatal("different jti should not be a repeat")
	}
}

func TestJtiCache_Eviction(t *testing.T) {
	cache := newJtiCache()
	old := time.Now().Add(-25 * time.Hour)
	recent := time.Now()

	// Seed an old entry.
	cache.entries["old-jti"] = old
	// This should trigger eviction of old entries.
	cache.seen("new-jti", recent)

	cache.mu.Lock()
	_, exists := cache.entries["old-jti"]
	cache.mu.Unlock()
	if exists {
		t.Fatal("old jti entry should have been evicted")
	}
}
