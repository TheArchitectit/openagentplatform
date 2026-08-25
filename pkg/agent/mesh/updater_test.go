package mesh

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestUpdater_VerifyReject(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "1.0.0", pub, slog.Default())

	binary := []byte("fake-agent-binary-v2.0.0")
	sum := sha256.Sum256(binary)
	sha := hex.EncodeToString(sum[:])
	sig := ed25519.Sign(priv, sum[:])
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Valid
	if !u.verify(binary, sha, sigB64) {
		t.Fatal("verify should accept valid signature")
	}
	// Wrong SHA
	if u.verify(binary, "badsha", sigB64) {
		t.Fatal("verify should reject wrong sha")
	}
	// Bad base64
	if u.verify(binary, sha, "!!!not-base64") {
		t.Fatal("verify should reject bad base64 sig")
	}
	// Wrong key
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	u2 := NewUpdater("agent-1", "1.0.0", wrongPub, slog.Default())
	if u2.verify(binary, sha, sigB64) {
		t.Fatal("verify should reject signature from wrong key")
	}
}

func TestUpdater_IgnoreOldVersion(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "2.0.0", pub, slog.Default())

	// Same version → ignored
	notice := UpdateNotice{AgentID: "agent-1", Version: "2.0.0"}
	data, _ := json.Marshal(notice)
	msg := &nats.Msg{Data: data}

	// Use a tracking publish to check status
	got := captureStatus(t, u, msg)
	if got.State != "ignored" {
		t.Fatalf("expected ignored, got %s", got.State)
	}
}

func TestUpdater_RefuseBadSig(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "1.0.0", pub, slog.Default())
	u.Fetch = func(string) ([]byte, error) { return []byte("binary"), nil }

	notice := UpdateNotice{
		AgentID:   "agent-1",
		Version:   "2.0.0",
		SHA256:    "badsha",
		Signature: "bad-sig",
	}
	data, _ := json.Marshal(notice)
	msg := &nats.Msg{Data: data}

	got := captureStatus(t, u, msg)
	if got.State != "refused" {
		t.Fatalf("expected refused, got %s", got.State)
	}
}

func TestUpdater_StageSuccess(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "1.0.0", pub, slog.Default())

	binary := []byte("fake-agent-binary-v2.0.0")
	sum := sha256.Sum256(binary)
	sha := hex.EncodeToString(sum[:])
	sig := ed25519.Sign(priv, sum[:])
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	u.Fetch = func(string) ([]byte, error) { return binary, nil }
	stagePath := filepath.Join(t.TempDir(), "staged-binary")
	u.StagePath = stagePath

	notice := UpdateNotice{
		AgentID:   "agent-1",
		Version:   "2.0.0",
		SHA256:    sha,
		Signature: sigB64,
	}
	data, _ := json.Marshal(notice)
	msg := &nats.Msg{Data: data}

	got := captureStatus(t, u, msg)
	if got.State != "staged" {
		t.Fatalf("expected staged, got %s: %s", got.State, got.Error)
	}
	if _, err := os.Stat(stagePath); err != nil {
		t.Fatalf("staged binary not found: %v", err)
	}
}

func TestUpdater_FetchError(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "1.0.0", pub, slog.Default())
	u.Fetch = func(string) ([]byte, error) { return nil, errors.New("download failed") }

	notice := UpdateNotice{AgentID: "agent-1", Version: "2.0.0"}
	data, _ := json.Marshal(notice)
	msg := &nats.Msg{Data: data}

	got := captureStatus(t, u, msg)
	if got.State != "error" {
		t.Fatalf("expected error, got %s", got.State)
	}
}

func TestUpdater_WrongAgentID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	u := NewUpdater("agent-1", "1.0.0", pub, slog.Default())

	notice := UpdateNotice{AgentID: "agent-999", Version: "2.0.0"}
	data, _ := json.Marshal(notice)
	msg := &nats.Msg{Data: data}

	// Should silently drop (no status published)
	got := captureStatus(t, u, msg)
	if got != nil {
		t.Fatalf("expected nil status for wrong agent, got %+v", got)
	}
}

func TestVersionGreater(t *testing.T) {
	tests := []struct{ a, b string; want bool }{
		{"2.0.0", "1.0.0", true},
		{"1.0.0", "2.0.0", false},
		{"1.1.0", "1.0.0", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"2", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
	}
	for _, tt := range tests {
		if got := versionGreater(tt.a, tt.b); got != tt.want {
			t.Errorf("versionGreater(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// ── helpers ──

// captureStatus runs u.handle with an injected status publisher and returns the
// published status. Returns nil if nothing was published.
func captureStatus(t *testing.T, u *Updater, msg *nats.Msg) *UpdateStatus {
	t.Helper()
	var status *UpdateStatus
	u.PublishStatus = func(subject string, payload []byte) error {
		var s UpdateStatus
		if err := json.Unmarshal(payload, &s); err == nil {
			status = &s
		}
		return nil
	}
	u.handle(t.Context(), msg)
	return status
}
