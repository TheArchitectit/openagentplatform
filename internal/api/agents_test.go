package api

import (
	"log/slog"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestVerifyRegistrationToken covers both the modern bcrypt-hashed path and
// the legacy plaintext fallback. The handler previously compared the stored
// DB value directly with subtleCompare — so a DB read exposed an enroll-able
// token. verifyRegistrationToken now detects bcrypt hashes and uses
// CompareHashAndPassword, while keeping a constant-time fallback for legacy
// plaintext rows (with a warning).
func TestVerifyRegistrationToken(t *testing.T) {
	log := slog.New(slog.NewTextHandler(newDiscardWriter(), nil))
	const plain = "super-secret-token"

	t.Run("bcrypt hash matches supplied token", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt hash: %v", err)
		}
		if !verifyRegistrationToken(string(hash), plain, log) {
			t.Error("expected bcrypt-hashed token to verify")
		}
	})

	t.Run("bcrypt hash rejects wrong token", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt hash: %v", err)
		}
		if verifyRegistrationToken(string(hash), "wrong-token", log) {
			t.Error("expected wrong token to be rejected against bcrypt hash")
		}
	})

	t.Run("legacy plaintext matches via constant-time fallback", func(t *testing.T) {
		if !verifyRegistrationToken(plain, plain, log) {
			t.Error("expected legacy plaintext token to verify via fallback")
		}
	})

	t.Run("legacy plaintext rejects mismatch", func(t *testing.T) {
		if verifyRegistrationToken(plain, "wrong-token", log) {
			t.Error("expected wrong plaintext token to be rejected")
		}
	})

	t.Run("empty values are rejected", func(t *testing.T) {
		if verifyRegistrationToken("", plain, log) {
			t.Error("expected empty stored to reject")
		}
		if verifyRegistrationToken(plain, "", log) {
			t.Error("expected empty supplied to reject")
		}
	})
}

// TestHashRegistrationToken verifies the helper produces a bcrypt hash that
// verifyRegistrationToken accepts, so callers (site creation/rotation) store
// hashes instead of plaintext.
func TestHashRegistrationToken(t *testing.T) {
	const token = "enroll-me"
	hash, err := HashRegistrationToken(token)
	if err != nil {
		t.Fatalf("HashRegistrationToken: %v", err)
	}
	if !isBcryptHash(hash) {
		t.Errorf("expected bcrypt hash, got %q", hash)
	}
	log := slog.New(slog.NewTextHandler(newDiscardWriter(), nil))
	if !verifyRegistrationToken(hash, token, log) {
		t.Error("hashed token did not verify against its plaintext")
	}
	// The hash must NOT equal the plaintext (the whole point).
	if hash == token {
		t.Error("hash equals plaintext — not actually hashed")
	}
}

// newDiscardWriter returns an io.Writer that discards, for test loggers.
func newDiscardWriter() *discardWriter { return &discardWriter{} }

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
