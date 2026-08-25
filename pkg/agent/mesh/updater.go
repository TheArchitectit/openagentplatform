package mesh

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nats-io/nats.go"
)

// sha256Of returns the SHA-256 of b.
func sha256Of(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

// demoVerifyKey is a placeholder Ed25519 public key used when no production
// key is configured. PRODUCTION: load the real signing pubkey from a secret or
// build-time injection — never ship a demo constant as the trust anchor.
var demoVerifyKey ed25519.PublicKey

// UpdateNotice is the pinned-release message the control plane publishes on
// UpdateSubject(agentID). The agent verifies the signature before applying.
type UpdateNotice struct {
	AgentID string `json:"agent_id"`
	OrgID   string `json:"org_id"`
	Version string `json:"version"`
	// SHA256 is the hex-encoded SHA-256 of the release binary.
	SHA256 string `json:"sha256"`
	// Signature is the base64 Ed25519 signature over the binary's SHA-256.
	Signature string `json:"signature"`
}

// UpdateStatus is published on UpdateStatusSubject after the agent processes
// a notice. State is one of: "ignored", "staged", "refused", "error".
type UpdateStatus struct {
	AgentID    string    `json:"agent_id"`
	Version    string    `json:"version"`
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	At         time.Time `json:"at"`
}

// Updater subscribes to pinned-release notices and applies verified updates.
// It NEVER auto-reboots: a successful verify+stage emits an audit status and
// waits for the operator-gated reboot (RMM-04) to actually restart the agent.
type Updater struct {
	agentID        string
	nc             *nats.Conn
	currentVersion string
	verifyKey      ed25519.PublicKey
	log            *slog.Logger

	// Fetch retrieves the release binary for version. Injected so tests can
	// mock it without a real download. Defaults to a no-op returning an error.
	Fetch func(version string) ([]byte, error)

	// StagePath is where the verified binary is written before the operator
	// gate. Defaults to a temp path derived from the agent binary.
	StagePath string

	// PublishStatus is the function used to emit UpdateStatus messages.
	// Defaults to publishing on u.nc; override in tests to capture output.
	PublishStatus func(subject string, payload []byte) error
}

// NewUpdater builds an updater. verifyKey is the Ed25519 public key used to
// check signatures; pass demoVerifyKey only in tests / non-production.
func NewUpdater(agentID, currentVersion string, verifyKey ed25519.PublicKey, log *slog.Logger) *Updater {
	if log == nil {
		log = slog.Default()
	}
	if verifyKey == nil {
		verifyKey = demoVerifyKey
	}
	u := &Updater{
		agentID:        agentID,
		currentVersion: currentVersion,
		verifyKey:      verifyKey,
		log:            log,
	}
	u.Fetch = func(string) ([]byte, error) {
		return nil, errors.New("mesh-update: no Fetch configured")
	}
	return u
}

// Run subscribes to the update subject and processes notices until ctx is
// cancelled. It returns the NATS subscription (caller owns Unsubscribe) or an
// error if the subscribe failed.
func (u *Updater) Run(ctx context.Context, nc *nats.Conn) (*nats.Subscription, error) {
	if nc == nil {
		return nil, errors.New("mesh-update: nil nats conn")
	}
	u.nc = nc
	if u.PublishStatus == nil {
		u.PublishStatus = func(subject string, payload []byte) error {
			return nc.Publish(subject, payload)
		}
	}
	sub, err := nc.Subscribe(UpdateSubject(u.agentID), func(msg *nats.Msg) {
		u.handle(ctx, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("mesh-update: subscribe: %w", err)
	}
	u.log.Info("mesh-update: subscribed", "subject", UpdateSubject(u.agentID))
	return sub, nil
}

// handle validates and applies a single update notice.
func (u *Updater) handle(ctx context.Context, msg *nats.Msg) {
	var notice UpdateNotice
	if err := json.Unmarshal(msg.Data, &notice); err != nil {
		u.log.Warn("mesh-update: bad notice payload", "err", err)
		return
	}
	if notice.AgentID != u.agentID {
		u.log.Warn("mesh-update: notice agent mismatch",
			"subject_agent", u.agentID, "payload_agent", notice.AgentID)
		return
	}

	// No downgrade / same-version churn.
	if notice.Version == "" || !versionGreater(notice.Version, u.currentVersion) {
		u.publishStatus(ctx, notice.Version, "ignored", "version not newer")
		return
	}

	binary, err := u.Fetch(notice.Version)
	if err != nil {
		u.publishStatus(ctx, notice.Version, "error", fmt.Sprintf("fetch: %v", err))
		return
	}

	// SHA-256 + Ed25519 signature gate.
	if !u.verify(binary, notice.SHA256, notice.Signature) {
		u.publishStatus(ctx, notice.Version, "refused", "signature or sha256 mismatch")
		return
	}

	if err := u.stage(binary, notice.Version); err != nil {
		u.publishStatus(ctx, notice.Version, "error", fmt.Sprintf("stage: %v", err))
		return
	}

	// Operator-gated: do NOT reboot. Signal readiness; RMM-04 reboot
	// coordination performs the actual restart on operator command.
	u.publishStatus(ctx, notice.Version, "staged", "ready for operator-gated reboot")
	u.log.Info("mesh-update: staged verified update",
		"version", notice.Version, "agent_id", u.agentID)
}

// verify checks the SHA-256 and Ed25519 signature against the embedded key.
func (u *Updater) verify(binary []byte, sha256Hex, sigB64 string) bool {
	sum := sha256Of(binary)
	if hex.EncodeToString(sum) != sha256Hex {
		u.log.Warn("mesh-update: sha256 mismatch", "agent_id", u.agentID)
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		u.log.Warn("mesh-update: bad signature encoding", "err", err)
		return false
	}
	if !ed25519.Verify(u.verifyKey, sum, sig) {
		u.log.Warn("mesh-update: signature invalid", "agent_id", u.agentID)
		return false
	}
	return true
}

// stage writes the verified binary to a temp path and marks it executable.
// It does not replace the running binary — that happens at the operator-gated
// reboot. The temp path is recorded for the orchestrator.
func (u *Updater) stage(binary []byte, version string) error {
	path := u.StagePath
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("oap-agent-%s.staged", version))
	}
	if err := os.WriteFile(path, binary, 0o755); err != nil {
		return fmt.Errorf("write staged binary: %w", err)
	}
	u.log.Info("mesh-update: binary staged", "path", path, "version", version)
	return nil
}

// publishStatus emits an UpdateStatus on the status subject.
func (u *Updater) publishStatus(ctx context.Context, version, state, errMsg string) {
	if u.PublishStatus == nil {
		return
	}
	status := UpdateStatus{
		AgentID: u.agentID,
		Version: version,
		State:   state,
		Error:   errMsg,
		At:      time.Now().UTC(),
	}
	payload, err := json.Marshal(status)
	if err != nil {
		u.log.Warn("mesh-update: marshal status failed", "err", err)
		return
	}
	if err := u.PublishStatus(UpdateStatusSubject(u.agentID), payload); err != nil {
		u.log.Warn("mesh-update: publish status failed", "err", err)
	}
}

// versionGreater reports whether a is semantically newer than b. It does a
// simple dotted-numeric comparison; non-numeric segments compare lexically as
// a fallback. Same version → false.
func versionGreater(a, b string) bool {
	if a == b {
		return false
	}
	as := splitVersion(a)
	bs := splitVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = atoiOrZero(as[i])
		}
		if i < len(bs) {
			bv = atoiOrZero(bs[i])
		}
		if av != bv {
			return av > bv
		}
	}
	// All numeric parts equal; longer or lexically-greater wins.
	return a > b
}

func splitVersion(v string) []string {
	out := make([]string, 0, 3)
	cur := ""
	for _, r := range v {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func atoiOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
