package mesh

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
)

// ReleaseRegistry wraps the mesh Store with the Ed25519 signing/verification
// operations needed for agent self-update (RMM-07 / RMM-09). The agent binary
// is signed at build time with an Ed25519 private key; the agent verifies the
// signature against an embedded public key before applying — regardless of the
// transport that delivered it.
type ReleaseRegistry struct {
	st  Store
	log *slog.Logger
}

// NewReleaseRegistry builds a release registry over the given store.
func NewReleaseRegistry(st Store, log *slog.Logger) *ReleaseRegistry {
	if log == nil {
		log = slog.Default()
	}
	return &ReleaseRegistry{st: st, log: log}
}

// SignRelease computes the SHA-256 of binary and signs that digest with the
// given Ed25519 private key. It returns the hex-encoded sha256 and a base64
// signature. The build pipeline that produces agent binaries calls this; the
// agent only ever verifies.
func (r *ReleaseRegistry) SignRelease(binary []byte, priv ed25519.PrivateKey) (sha256Hex, sigB64 string, err error) {
	sum := sha256.Sum256(binary)
	sha256Hex = hex.EncodeToString(sum[:])
	sig := ed25519.Sign(priv, sum[:])
	sigB64 = base64.StdEncoding.EncodeToString(sig)
	return sha256Hex, sigB64, nil
}

// VerifyRelease checks both integrity (SHA-256 matches) and authenticity
// (Ed25519 signature validates against pub). It returns false on ANY failure:
// bad base64, non-hex sha256, digest mismatch, or signature mismatch. This is
// the safety gate that prevents a tampered or wrongly-signed binary from ever
// being applied.
func (r *ReleaseRegistry) VerifyRelease(binary []byte, sha256Hex, sigB64 string, pub ed25519.PublicKey) bool {
	sum := sha256.Sum256(binary)
	wantHex := hex.EncodeToString(sum[:])
	if wantHex != sha256Hex {
		r.log.Warn("release verify: sha256 mismatch", "want", wantHex, "got", sha256Hex)
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		r.log.Warn("release verify: bad signature encoding", "err", err)
		return false
	}
	if !ed25519.Verify(pub, sum[:], sig) {
		r.log.Warn("release verify: signature invalid")
		return false
	}
	return true
}

// ListPinned returns the pinned releases for an org (operator-gated rollout).
func (r *ReleaseRegistry) ListPinned(ctx context.Context, orgID string) ([]*AgentRelease, error) {
	if orgID == "" {
		return nil, fmt.Errorf("mesh: org_id required")
	}
	return r.st.ListAgentReleases(ctx, orgID, true)
}
