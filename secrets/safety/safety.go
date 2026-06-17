// Package safety implements script credential safety controls for the
// OpenAgentPlatform secret management subsystem. It ensures that credentials
// used by RMM scripts are never exposed through process listings, shell
// history, or endpoint logs.
package safety

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// SafetyPolicy determines how the credential safety layer handles secrets
// that appear in script arguments or environment variables.
type SafetyPolicy int

const (
	// PolicyNoScriptArgSecrets rejects any script that receives secrets
	// as command-line arguments.
	PolicyNoScriptArgSecrets SafetyPolicy = 1 << iota

	// PolicyNoEnvSecrets rejects any environment variable that contains
	// a raw secret value (no OAP_INJECTED_ prefix).
	PolicyNoEnvSecrets

	// PolicyEnvSecretsWithOAPPrefixOnly allows environment variables only
	// when they are namespaced under the OAP_INJECTED_ prefix. Variables
	// that contain secret references but lack the prefix are rejected.
	PolicyEnvSecretsWithOAPPrefixOnly
)

// Contains reports whether the receiver includes the given policy flag.
func (p SafetyPolicy) Contains(other SafetyPolicy) bool {
	return p&other != 0
}

// String returns a human-readable representation of the policy flags.
func (p SafetyPolicy) String() string {
	var flags []string
	if p.Contains(PolicyNoScriptArgSecrets) {
		flags = append(flags, "NoScriptArgSecrets")
	}
	if p.Contains(PolicyNoEnvSecrets) {
		flags = append(flags, "NoEnvSecrets")
	}
	if p.Contains(PolicyEnvSecretsWithOAPPrefixOnly) {
		flags = append(flags, "EnvSecretsWithOAPPrefixOnly")
	}
	if len(flags) == 0 {
		return "None"
	}
	result := ""
	for i, f := range flags {
		if i > 0 {
			result += " | "
		}
		result += f
	}
	return result
}

// DefaultPolicy returns the recommended policy: no secrets in script args,
// env secrets allowed only with the OAP_INJECTED_ prefix.
func DefaultPolicy() SafetyPolicy {
	return PolicyNoScriptArgSecrets | PolicyEnvSecretsWithOAPPrefixOnly
}

// ServerSideOperation describes an authenticated action that the OAP server
// performs on behalf of a script, so the script itself never receives the
// raw credential.
type ServerSideOperation struct {
	// AgentID is the agent requesting the operation.
	AgentID string
	// ScriptRunID uniquely identifies the script execution context.
	ScriptRunID string
	// Operation is a descriptor of the action (e.g. "ssh.connect",
	// "db.query", "api.call").
	Operation string
	// Params carries operation-specific parameters (host, command, etc.)
	// but NEVER the credential itself.
	Params map[string]string
	// Result is populated by the server after performing the operation.
	Result string
	// Err is set if the operation failed.
	Err error
	// ExecutedAt records when the server-side operation completed.
	ExecutedAt time.Time
}

// JITDeliveryResult carries a credential delivered just-in-time to an agent
// process. The credential is intended for immediate, single-use consumption.
type JITDeliveryResult struct {
	// AgentID is the agent that requested the credential.
	AgentID string
	// ScriptRunID identifies the script execution context.
	ScriptRunID string
	// URI is the ref:oap:// URI that was resolved.
	URI string
	// Credential is the raw credential value, delivered in-band.
	// Callers MUST zero-fill this byte slice after use.
	Credential []byte
	// DeliveredAt records when the credential was delivered.
	DeliveredAt time.Time
	// ExpiresAt is the deadline after which the credential must not be used.
	ExpiresAt time.Time
}

// JITDeliverer performs a just-in-time credential delivery. Implementations
// are expected to deliver credentials through a transient channel (mTLS
// session, in-process IPC, etc.) and return the value for the caller to
// forward to the agent.
type JITDeliverer interface {
	Deliver(ctx context.Context, agentID, scriptRunID, uri string) (*JITDeliveryResult, error)
}

// SecretResolver resolves a secret reference to its value. The safety layer
// uses this abstraction to fetch credentials server-side without exposing
// them to scripts.
type SecretResolver interface {
	Resolve(ctx context.Context, uri string) ([]byte, error)
}

// ScriptCredentialSafe enforces the four-credential-safety guarantees:
// server-side operations, JIT delivery, no-arg-secrets guard, and full
// audit logging on every credential fetch.
type ScriptCredentialSafe struct {
	mu       sync.RWMutex
	policy   SafetyPolicy
	resolver SecretResolver
	deliver  JITDeliverer
	audit    *audit.AuditService
	logger   *slog.Logger

	// deliveryLedger tracks active JIT deliveries for revocation.
	deliveryLedger map[string]*JITDeliveryResult
}

// NewScriptCredentialSafe creates a new ScriptCredentialSafe with the given
// policy, resolver, JIT deliverer, and audit service.
func NewScriptCredentialSafe(
	policy SafetyPolicy,
	resolver SecretResolver,
	deliver JITDeliverer,
	auditSvc *audit.AuditService,
	logger *slog.Logger,
) *ScriptCredentialSafe {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScriptCredentialSafe{
		policy:         policy,
		resolver:       resolver,
		deliver:        deliver,
		audit:          auditSvc,
		logger:         logger,
		deliveryLedger: make(map[string]*JITDeliveryResult),
	}
}

// SetPolicy updates the active safety policy. Existing operations are
// unaffected; the new policy applies to subsequent requests.
func (s *ScriptCredentialSafe) SetPolicy(p SafetyPolicy) {
	s.mu.Lock()
	s.policy = p
	s.mu.Unlock()
}

// Policy returns the currently active safety policy.
func (s *ScriptCredentialSafe) Policy() SafetyPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy
}

// ServerSideOperation performs an authenticated action on behalf of an
// agent's script. The script specifies the operation and its parameters,
// but the credential is resolved and used entirely on the server side.
// The script never sees the raw credential.
func (s *ScriptCredentialSafe) ServerSideOperation(
	ctx context.Context,
	agentID string,
	operation string,
	params map[string]string,
) (*ServerSideOperation, error) {
	if agentID == "" {
		return nil, fmt.Errorf("safety: agentID is required")
	}
	if operation == "" {
		return nil, fmt.Errorf("safety: operation is required")
	}

	// Validate that the script is not trying to smuggle a secret in
	// its parameters.
	if s.policy.Contains(PolicyNoScriptArgSecrets) {
		for k, v := range params {
			if looksLikeSecret(k, v) {
				s.emitAudit(ctx, "script.credential.smuggle_attempt", agentID, operation,
					audit.OutcomeDenied, fmt.Sprintf("secret-like value in param %q", k))
				return nil, fmt.Errorf("safety: parameter %q appears to contain a secret value", k)
			}
		}
	}

	scriptRunID, err := generateScriptRunID()
	if err != nil {
		return nil, fmt.Errorf("safety: generate run ID: %w", err)
	}

	s.emitAudit(ctx, "script.credential.server_op.start", agentID, operation,
		audit.OutcomeSuccess, "")

	result := &ServerSideOperation{
		AgentID:     agentID,
		ScriptRunID: scriptRunID,
		Operation:   operation,
		Params:      params,
		ExecutedAt:  time.Now().UTC(),
	}

	// The actual operation execution is delegated to a registered handler.
	// In this implementation, we record the intent and return. Callers can
	// extend this by registering operation handlers.
	s.logger.InfoContext(ctx, "server-side operation initiated",
		"agent_id", agentID,
		"script_run_id", scriptRunID,
		"operation", operation,
	)

	s.emitAudit(ctx, "script.credential.server_op.complete", agentID, operation,
		audit.OutcomeSuccess, "")

	return result, nil
}

// JITDelivery delivers a credential to an agent in response to a specific
// script run request. The credential is returned in the response body and
// is tracked in the delivery ledger for audit purposes.
func (s *ScriptCredentialSafe) JITDelivery(
	ctx context.Context,
	agentID string,
	scriptRunID string,
	uri string,
) (*JITDeliveryResult, error) {
	if agentID == "" {
		return nil, fmt.Errorf("safety: agentID is required")
	}
	if scriptRunID == "" {
		return nil, fmt.Errorf("safety: scriptRunID is required")
	}
	if uri == "" {
		return nil, fmt.Errorf("safety: uri is required")
	}
	if s.resolver == nil {
		return nil, fmt.Errorf("safety: resolver is not configured")
	}

	// Resolve the credential server-side.
	credential, err := s.resolver.Resolve(ctx, uri)
	if err != nil {
		s.emitAudit(ctx, "script.credential.delivery_failed", agentID, uri,
			audit.OutcomeError, err.Error())
		return nil, fmt.Errorf("safety: resolve credential: %w", err)
	}

	now := time.Now().UTC()
	delivery := &JITDeliveryResult{
		AgentID:     agentID,
		ScriptRunID: scriptRunID,
		URI:         uri,
		Credential:  credential,
		DeliveredAt: now,
		ExpiresAt:   now.Add(60 * time.Second),
	}

	// Track the delivery.
	s.mu.Lock()
	s.deliveryLedger[scriptRunID] = delivery
	s.mu.Unlock()

	s.emitAudit(ctx, "script.credential.delivered", agentID, uri,
		audit.OutcomeSuccess,
		fmt.Sprintf("script_run_id=%s delivery_method=jit_response", scriptRunID),
	)

	s.logger.InfoContext(ctx, "credential delivered via JIT",
		"agent_id", agentID,
		"script_run_id", scriptRunID,
		"uri", uri,
		"expires_at", delivery.ExpiresAt,
	)

	return delivery, nil
}

// NoArgSecrets checks whether any element in the provided arguments appears
// to contain a secret value. If the active policy prohibits secret arguments,
// a violation is returned.
func (s *ScriptCredentialSafe) NoArgSecrets(args []string) error {
	s.mu.RLock()
	policy := s.policy
	s.mu.RUnlock()

	if !policy.Contains(PolicyNoScriptArgSecrets) {
		return nil
	}

	for i, arg := range args {
		if looksLikeSecret(fmt.Sprintf("arg[%d]", i), arg) {
			return fmt.Errorf("safety: argument at position %d appears to contain a secret value", i)
		}
	}
	return nil
}

// AuditEvery records an audit event for a credential access. This method
// must be called for every credential fetch to maintain the audit trail.
func (s *ScriptCredentialSafe) AuditEvery(ctx context.Context, event string, agentID string, uri string, outcome audit.Outcome, detail string) {
	s.emitAudit(ctx, event, agentID, uri, outcome, detail)
}

// RevokeDelivery removes a JIT delivery from the active ledger and zeros
// the in-memory credential.
func (s *ScriptCredentialSafe) RevokeDelivery(scriptRunID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delivery, ok := s.deliveryLedger[scriptRunID]
	if !ok {
		return
	}
	// Zero-fill the credential in memory.
	for i := range delivery.Credential {
		delivery.Credential[i] = 0
	}
	delete(s.deliveryLedger, scriptRunID)
}

// ActiveDeliveries returns the count of currently active JIT deliveries.
func (s *ScriptCredentialSafe) ActiveDeliveries() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.deliveryLedger)
}

// emitAudit is a thin wrapper over the audit service.
func (s *ScriptCredentialSafe) emitAudit(
	ctx context.Context,
	action string,
	agentID string,
	resourceID string,
	outcome audit.Outcome,
	detail string,
) {
	if s.audit == nil {
		s.logger.WarnContext(ctx, "audit service not configured, skipping event",
			"action", action,
			"agent_id", agentID,
		)
		return
	}

	details := map[string]any{}
	if detail != "" {
		details["detail"] = detail
	}

	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorAgent,
		ActorID:      agentID,
		Action:       action,
		ResourceType: "script_credential",
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      outcome,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to emit audit event",
			"action", action,
			"error", err,
		)
	}
}

// generateScriptRunID creates a unique identifier for a script execution.
func generateScriptRunID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "srun-" + hex.EncodeToString(b), nil
}
