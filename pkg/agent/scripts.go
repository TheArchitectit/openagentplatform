// Package agent — script execution wiring. The script handler subscribes
// to NATS subjects that carry ScriptCommand payloads, dispatches each
// command to the executor package, streams output line-by-line, and
// honours cancellation requests received on a sibling subject.
package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/openagentplatform/openagentplatform/pkg/agent/executor"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// ScriptCommand arrives on the agent's scripts subject.
type ScriptCommand struct {
	ScriptID     string            `json:"script_id"`
	RunID        string            `json:"run_id,omitempty"` // optional client-supplied correlation id
	Runtime      string            `json:"runtime"`          // bash, sh, powershell, pwsh, python, node, cmd
	Script       string            `json:"script"`           // inline source
	URL          string            `json:"url,omitempty"`    // optional URL to download
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
	TimeoutSec   int               `json:"timeout_sec,omitempty"`
	Sandbox      bool              `json:"sandbox,omitempty"`
}

// ScriptOutputChunk is streamed to the output subject.
type ScriptOutputChunk struct {
	RunID      string `json:"run_id,omitempty"`
	ScriptID   string `json:"script_id"`
	AgentID    string `json:"agent_id"`
	Stream     string `json:"stream"` // "stdout" | "stderr" | "exit" | "error"
	Data       string `json:"data,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// ScriptsSubject returns the NATS subject for incoming script commands.
func ScriptsSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts", agentID)
}

// ScriptsOutputSubject returns the per-run output subject used for
// line-by-line streaming.
func ScriptsOutputSubject(agentID, runID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts.%s.output", agentID, runID)
}

// ScriptsResultSubject returns the NATS subject for the final result of a
// script run (exit code + truncated output).
func ScriptsResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts.result", agentID)
}

// ScriptsCancelSubject returns the NATS subject used to cancel a running
// script by run_id. The payload is a JSON object containing run_id.
func ScriptsCancelSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.scripts.cancel", agentID)
}

// runRegistry tracks in-flight script runs so a later cancel message can
// find and kill the right process. Keys are run_id; values are the
// cancel function for the run's context.
type runRegistry struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func newRunRegistry() *runRegistry {
	return &runRegistry{running: make(map[string]context.CancelFunc)}
}

func (r *runRegistry) add(id string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.running[id]; ok {
		// Defensive: cancel any leftover before replacing.
		prev()
	}
	r.running[id] = cancel
}

func (r *runRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, id)
}

func (r *runRegistry) cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.running[id]
	if !ok {
		return false
	}
	cancel()
	return true
}

// ParseSigningKey decodes the base64 (raw URL) Ed25519 public key the
// server delivers at registration (signing_key).
func ParseSigningKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("signing key: decode: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("signing key: expected %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// decodeScriptCommand extracts a ScriptCommand from a NATS message.
//
// When verifyKey is non-nil the command MUST arrive inside a valid signed
// envelope (models.SignedCommand): script bodies are arbitrary code
// executed at this agent's privilege, so broker reachability alone must
// not be enough to run code here. When verifyKey is nil — legacy agents
// that never received a signing key — both forms are accepted and any
// present signature is ignored.
func decodeScriptCommand(data []byte, verifyKey ed25519.PublicKey, log *slog.Logger) (ScriptCommand, error) {
	var env models.SignedCommand
	if err := json.Unmarshal(data, &env); err == nil && env.Signature != "" {
		if verifyKey == nil {
			log.Warn("scripts: signed command received but no signing key configured; accepting without verification")
			var plain ScriptCommand
			if err := json.Unmarshal(env.Payload, &plain); err != nil {
				return ScriptCommand{}, fmt.Errorf("scripts: decode signed payload: %w", err)
			}
			return plain, nil
		}
		var cmd ScriptCommand
		if err := models.VerifySignedCommand(&env, verifyKey, &cmd); err != nil {
			return ScriptCommand{}, err
		}
		return cmd, nil
	}
	if verifyKey != nil {
		return ScriptCommand{}, errors.New("scripts: unsigned command rejected (signing key configured)")
	}
	var cmd ScriptCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return ScriptCommand{}, fmt.Errorf("scripts: decode: %w", err)
	}
	return cmd, nil
}

// RunScriptsHandler subscribes to the scripts subject and executes each
// request, streaming stdout/stderr to the per-run output subject and
// publishing the final result. It also subscribes to a cancel subject
// so the platform can abort a running script. verifyKey may be nil to
// run without signature enforcement.
func RunScriptsHandler(ctx context.Context, agentID string, defaultTimeoutSec int, nc *NATSClient, verifyKey ed25519.PublicKey, log *slog.Logger) (*nats.Subscription, error) {
	registry := newRunRegistry()
	subject := ScriptsSubject(agentID)

	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		cmd, err := decodeScriptCommand(msg.Data, verifyKey, log)
		if err != nil {
			log.Warn("scripts: rejected command",
				"err", err,
				"subject", subject,
				"data_len", len(msg.Data))
			return
		}
		if cmd.RunID == "" {
			cmd.RunID = uuid.NewString()
		}
		if cmd.ScriptID == "" {
			cmd.ScriptID = uuid.NewString()
		}
		log.Info("script received",
			"script_id", cmd.ScriptID,
			"run_id", cmd.RunID,
			"runtime", cmd.Runtime,
		)
		go runScript(ctx, agentID, &cmd, defaultTimeoutSec, nc, registry, log)
	})
	if err != nil {
		return nil, err
	}

	// Cancellation subscription: a separate handler so cancellation is
	// always reactive even when a script is still spinning up.
	cancelSubject := ScriptsCancelSubject(agentID)
	cancelSub, cancelErr := nc.Subscribe(cancelSubject, func(msg *nats.Msg) {
		var p struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			log.Warn("scripts: bad cancel payload", "err", err)
			return
		}
		if registry.cancel(p.RunID) {
			log.Info("script cancel requested", "run_id", p.RunID)
		} else {
			log.Warn("script cancel: run_id not found", "run_id", p.RunID)
		}
	})
	if cancelErr != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("scripts cancel subscribe: %w", cancelErr)
	}
	// We return the primary subscription; the cancel sub is detached
	// (its lifetime is tied to nc.Close).
	_ = cancelSub

	log.Info("scripts handler subscribed", "subject", subject, "cancel_subject", cancelSubject)
	return sub, nil
}

func runScript(parent context.Context, agentID string, cmd *ScriptCommand, defaultTimeout int, nc *NATSClient, registry *runRegistry, log *slog.Logger) {
	timeoutSec := cmd.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeout
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	registry.add(cmd.RunID, cancel)
	defer registry.remove(cmd.RunID)

	publish := func(chunk ScriptOutputChunk) {
		if chunk.Timestamp == 0 {
			chunk.Timestamp = time.Now().Unix()
		}
		chunk.AgentID = agentID
		chunk.ScriptID = cmd.ScriptID
		chunk.RunID = cmd.RunID
		data, err := json.Marshal(chunk)
		if err != nil {
			log.Warn("script chunk marshal failed", "err", err)
			return
		}
		subject := ScriptsResultSubject(agentID)
		// Per-run output subject is also published to for fine-grained
		// streaming; the result subject receives the same chunks for
		// consumers that want a single feed.
		if err := nc.Publish(parent, subject, data); err != nil {
			log.Warn("script chunk publish failed", "subject", subject, "err", err)
		}
		if out := ScriptsOutputSubject(agentID, cmd.RunID); out != subject {
			if err := nc.Publish(parent, out, data); err != nil {
				log.Warn("script chunk publish failed", "subject", out, "err", err)
			}
		}
	}

	opts := executor.Options{
		Runtime:      executor.Runtime(cmd.Runtime),
		Script:       cmd.Script,
		Args:         cmd.Args,
		Env:          cmd.Env,
		Dependencies: cmd.Dependencies,
		Timeout:      time.Duration(timeoutSec) * time.Second,
		OutputCallback: func(stream, line string) {
			publish(ScriptOutputChunk{Stream: stream, Data: line})
		},
		Sandbox: executor.EnvSandbox{Enabled: cmd.Sandbox},
	}

	start := time.Now()
	res, err := executor.ExecuteWith(ctx, executor.Default(), opts)
	if err != nil {
		// Differentiate timeout/cancel from hard errors.
		if res != nil && res.TimedOut {
			publish(ScriptOutputChunk{Stream: "error", Data: "script timed out"})
		} else if res != nil && res.Cancelled {
			publish(ScriptOutputChunk{Stream: "error", Data: "script cancelled"})
		} else if res != nil && res.Error != "" {
			publish(ScriptOutputChunk{Stream: "error", Data: res.Error})
		} else {
			publish(ScriptOutputChunk{Stream: "error", Data: err.Error()})
		}
		publish(ScriptOutputChunk{Stream: "exit", ExitCode: -1, DurationMs: time.Since(start).Milliseconds()})
		return
	}

	publish(ScriptOutputChunk{Stream: "exit", ExitCode: res.ExitCode, DurationMs: res.Duration.Milliseconds()})
}
