package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func testSigningKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func signedEnvelope(t *testing.T, priv ed25519.PrivateKey, cmd any) []byte {
	t.Helper()
	payload, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sig, err := models.SignPayload(priv, payload)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	data, err := json.Marshal(models.SignedCommand{
		Payload:   payload,
		Algorithm: models.SignedCommandAlgorithm,
		Signature: sig,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return data
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestDecodeScriptCommandSigned verifies the happy path: a correctly
// signed envelope decodes to the original command when the agent holds
// the matching public key.
func TestDecodeScriptCommandSigned(t *testing.T) {
	pub, priv := testSigningKeypair(t)
	want := ScriptCommand{ScriptID: "s-1", RunID: "r-1", Runtime: "bash", Script: "echo hi"}

	data := signedEnvelope(t, priv, want)
	got, err := decodeScriptCommand(data, pub, quietLogger())
	if err != nil {
		t.Fatalf("decodeScriptCommand: %v", err)
	}
	if got.ScriptID != want.ScriptID || got.Script != want.Script || got.Runtime != want.Runtime {
		t.Errorf("decoded = %+v, want %+v", got, want)
	}
}

// TestDecodeScriptCommandTampered verifies that any payload modification
// after signing is rejected.
func TestDecodeScriptCommandTampered(t *testing.T) {
	pub, priv := testSigningKeypair(t)
	cmd := ScriptCommand{ScriptID: "s-1", Runtime: "bash", Script: "echo hi"}

	data := signedEnvelope(t, priv, cmd)
	// Flip the script body inside the signed payload.
	var env models.SignedCommand
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var mutated map[string]any
	if err := json.Unmarshal(env.Payload, &mutated); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	mutated["script"] = "rm -rf /"
	env.Payload, _ = json.Marshal(mutated)
	tampered, _ := json.Marshal(env)

	if _, err := decodeScriptCommand(tampered, pub, quietLogger()); err == nil {
		t.Fatal("tampered payload accepted; want signature failure")
	}
}

// TestDecodeScriptCommandWrongKey verifies that a signature from a
// different key is rejected (e.g. a compromised peer agent's material).
func TestDecodeScriptCommandWrongKey(t *testing.T) {
	pub, _ := testSigningKeypair(t)
	_, otherPriv := testSigningKeypair(t)

	data := signedEnvelope(t, otherPriv, ScriptCommand{ScriptID: "s-1", Script: "id"})
	if _, err := decodeScriptCommand(data, pub, quietLogger()); err == nil {
		t.Fatal("command signed by wrong key accepted")
	}
}

// TestDecodeScriptCommandUnsignedRejectedWithKey verifies fail-closed
// behaviour when enforcement is enabled: plaintext (unsigned) commands
// must not run.
func TestDecodeScriptCommandUnsignedRejectedWithKey(t *testing.T) {
	pub, _ := testSigningKeypair(t)
	plain, _ := json.Marshal(ScriptCommand{ScriptID: "s-1", Script: "id"})
	if _, err := decodeScriptCommand(plain, pub, quietLogger()); err == nil {
		t.Fatal("unsigned command accepted while signing key configured")
	}
}

// TestDecodeScriptCommandLegacyCompat verifies the transitional paths:
// without a configured key the agent still accepts both legacy plaintext
// and signed envelopes (logging that verification was skipped).
func TestDecodeScriptCommandLegacyCompat(t *testing.T) {
	_, priv := testSigningKeypair(t)
	want := ScriptCommand{ScriptID: "s-1", Script: "echo legacy"}

	plain, _ := json.Marshal(want)
	got, err := decodeScriptCommand(plain, nil, quietLogger())
	if err != nil || got.ScriptID != want.ScriptID {
		t.Errorf("legacy plaintext: got %+v, err %v", got, err)
	}

	signed := signedEnvelope(t, priv, want)
	got, err = decodeScriptCommand(signed, nil, quietLogger())
	if err != nil || got.ScriptID != want.ScriptID {
		t.Errorf("signed without key: got %+v, err %v", got, err)
	}
}

// TestParseSigningKey verifies key decoding and size validation.
func TestParseSigningKey(t *testing.T) {
	pub, priv := testSigningKeypair(t)
	b64 := base64.RawURLEncoding.EncodeToString(pub)

	got, err := ParseSigningKey(b64)
	if err != nil {
		t.Fatalf("ParseSigningKey: %v", err)
	}
	if !pub.Equal(got) {
		t.Error("parsed key differs from original")
	}
	_ = priv

	if _, err := ParseSigningKey("not-base64!!"); err == nil {
		t.Error("invalid base64 accepted")
	}
	if _, err := ParseSigningKey("AAAA"); err == nil {
		t.Error("wrong-size key accepted")
	}
}
