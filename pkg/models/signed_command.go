package models

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// SignedCommand is the wire envelope for security-sensitive commands sent
// to agents over NATS (currently script runs). The original command JSON
// is carried verbatim in Payload and signed with the server's Ed25519
// session signing key, so an agent that knows the server's public key can
// prove a command originated from the platform — not merely from
// something that could reach the broker.
type SignedCommand struct {
	// Payload is the original command JSON, signed byte-for-byte.
	Payload json.RawMessage `json:"payload"`
	// Algorithm identifies the signature scheme; "ed25519".
	Algorithm string `json:"alg,omitempty"`
	// Signature is the base64 (raw URL) Ed25519 signature over Payload.
	Signature string `json:"signature,omitempty"`
}

// SignedCommandAlgorithm is the only supported value for SignedCommand.
const SignedCommandAlgorithm = "ed25519"

// SignPayload signs payload with the given Ed25519 private key and
// returns the base64 (raw URL) signature.
func SignPayload(key ed25519.PrivateKey, payload []byte) (string, error) {
	if len(key) != ed25519.PrivateKeySize {
		return "", errors.New("signpayload: invalid ed25519 private key")
	}
	sig := ed25519.Sign(key, payload)
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifySignedCommand checks the envelope's algorithm and signature
// against the trusted public key and, on success, decodes Payload into
// out (which must be a pointer).
func VerifySignedCommand(env *SignedCommand, pub ed25519.PublicKey, out any) error {
	if env == nil {
		return errors.New("signed command: nil envelope")
	}
	if env.Algorithm != SignedCommandAlgorithm {
		return fmt.Errorf("signed command: unsupported algorithm %q", env.Algorithm)
	}
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("signed command: invalid ed25519 public key")
	}
	sig, err := base64.RawURLEncoding.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("signed command: decode signature: %w", err)
	}
	if !ed25519.Verify(pub, env.Payload, sig) {
		return errors.New("signed command: signature verification failed")
	}
	if err := json.Unmarshal(env.Payload, out); err != nil {
		return fmt.Errorf("signed command: decode payload: %w", err)
	}
	return nil
}
