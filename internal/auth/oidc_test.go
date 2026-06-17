package auth

import (
	"strings"
	"testing"
	"time"
)

// newTestMinter creates a SessionMinter with a freshly generated Ed25519 key
// for use in JWT round-trip tests.
func newTestMinter(t *testing.T, expiry time.Duration) *SessionMinter {
	t.Helper()
	sm, err := NewSessionMinter("oap-test", "oap-test", expiry, "")
	if err != nil {
		t.Fatalf("NewSessionMinter: %v", err)
	}
	return sm
}

// TestSessionMintAndVerify mints a session JWT, parses it back, and asserts that
// every claim round-trips intact.
func TestSessionMintAndVerify(t *testing.T) {
	sm := newTestMinter(t, time.Hour)

	tok, err := sm.Mint(&Claims{
		Subject: "user-42",
		Email:   "user42@example.com",
		Name:    "User Forty-Two",
		Groups:  []string{"oap-admins"},
		OrgID:   "org-abc",
		SiteID:  "site-xyz",
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if tok == "" {
		t.Fatal("Mint returned empty token")
	}

	claims, err := sm.Parse(tok)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if claims.Subject != "user-42" {
		t.Errorf("Subject: got %q, want user-42", claims.Subject)
	}
	if claims.Email != "user42@example.com" {
		t.Errorf("Email: got %q", claims.Email)
	}
	if claims.Name != "User Forty-Two" {
		t.Errorf("Name: got %q", claims.Name)
	}
	if claims.OrgID != "org-abc" {
		t.Errorf("OrgID: got %q", claims.OrgID)
	}
	if claims.SiteID != "site-xyz" {
		t.Errorf("SiteID: got %q", claims.SiteID)
	}
	if claims.Role != RoleAdmin {
		t.Errorf("Role: got %q, want admin (mapped from oap-admins)", claims.Role)
	}
	if len(claims.Groups) != 1 || claims.Groups[0] != "oap-admins" {
		t.Errorf("Groups: got %v", claims.Groups)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now()) {
		t.Errorf("ExpiresAt should be in the future, got %v", claims.ExpiresAt)
	}
}

// TestExpiredToken verifies that a JWT whose ExpiresAt is in the past is
// rejected by Parse. NewSessionMinter clamps non-positive expiry to 1h so we
// mint with a short positive expiry and sleep past it.
func TestExpiredToken(t *testing.T) {
	sm := newTestMinter(t, 50*time.Millisecond)

	tok, err := sm.Mint(&Claims{
		Subject: "user-expired",
		Email:   "expired@example.com",
		OrgID:   "org-1",
		Role:    RoleViewer,
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Sleep past the expiry plus a margin so the verifier sees the token
	// as expired.
	time.Sleep(150 * time.Millisecond)

	if _, err := sm.Parse(tok); err == nil {
		t.Fatal("expected Parse to reject an expired token, got nil error")
	}
}

// TestInvalidSignature verifies that a token signed with a different key is
// rejected by Parse even if its payload is structurally valid.
func TestInvalidSignature(t *testing.T) {
	signer := newTestMinter(t, time.Hour)

	// Mint a legitimate token with signer.
	tok, err := signer.Mint(&Claims{
		Subject: "user-tamper",
		Email:   "tamper@example.com",
		OrgID:   "org-1",
		Role:    RoleViewer,
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Build an independent verifier with a fresh key. Its Parse must fail
	// because the JWT signature does not match its public key.
	verifier := newTestMinter(t, time.Hour)
	if _, err := verifier.Parse(tok); err == nil {
		t.Fatal("expected Parse to reject a token signed with a different key")
	}

	// Additionally, flip a character in the signature segment to ensure that
	// a tampered token is rejected even by the original minter.
	tampered := tamperSignature(tok)
	if _, err := signer.Parse(tampered); err == nil {
		t.Fatal("expected Parse to reject a tampered token")
	}
}

// tamperSignature flips a single character in the JWT signature segment to
// ensure the verifier catches signature mutations.
func tamperSignature(tok string) string {
	// JWT is header.payload.signature — find the last dot and mutate one byte.
	idx := strings.LastIndex(tok, ".")
	if idx < 0 || idx == len(tok)-1 {
		return tok // malformed; leave for the verifier to reject
	}
	sig := []byte(tok[idx+1:])
	if sig[0] == 'A' {
		sig[0] = 'B'
	} else {
		sig[0] = 'A'
	}
	return tok[:idx+1] + string(sig)
}
