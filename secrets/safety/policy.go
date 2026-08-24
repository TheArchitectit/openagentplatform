// Package safety — policy enforcement for script credential safety.
//
// This file implements the enforcement layer that inspects script requests
// (arguments, environment variables) and rejects those that would expose
// secrets through observable channels. Secret pattern matching helpers
// live in policy_patterns.go.
package safety

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// ViolationSeverity indicates whether a policy violation is a warning
// (logged but allowed) or an error (rejected with a hard error).
type ViolationSeverity string

const (
	// SeverityWarning logs the violation but does not block the request.
	SeverityWarning ViolationSeverity = "warning"
	// SeverityError blocks the request and returns a hard error.
	SeverityError ViolationSeverity = "error"
)

// Violation represents a single policy violation detected during script
// request validation.
type Violation struct {
	// Field is the name of the offending field (e.g. "args[0]", "env.PASSWORD").
	Field string
	// Value is the offending value (may be truncated for logging safety).
	Value string
	// Rule is the policy rule that was violated.
	Rule string
	// Severity indicates whether this violation blocks execution.
	Severity ViolationSeverity
	// Message provides a human-readable explanation.
	Message string
}

// ScriptRequest represents a script execution request to be validated
// against the credential safety policy.
type ScriptRequest struct {
	// Script is the script name or identifier.
	Script string
	// Args are the command-line arguments the script will receive.
	Args []string
	// Env are the environment variables the script will inherit.
	Env map[string]string
}

// Validator validates script requests against the credential safety policy.
type Validator struct {
	mu     sync.RWMutex
	policy SafetyPolicy
	audit  *audit.AuditService
	logger *slog.Logger
}

// NewValidator creates a new policy Validator.
func NewValidator(policy SafetyPolicy, auditSvc *audit.AuditService, logger *slog.Logger) *Validator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Validator{
		policy: policy,
		audit:  auditSvc,
		logger: logger,
	}
}

// Policy returns the active safety policy.
func (v *Validator) Policy() SafetyPolicy {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.policy
}

// SetPolicy updates the active safety policy.
func (v *Validator) SetPolicy(p SafetyPolicy) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.policy = p
}

// ValidateScriptRequest inspects a script request for policy violations.
// It returns a slice of Violations found. If any violation has
// SeverityError, the caller should reject the request.
//
// Validation checks:
//  1. Secrets in args are rejected (PolicyNoScriptArgSecrets).
//  2. Secrets in env vars without the OAP_INJECTED_ prefix are
//     rejected (PolicyEnvSecretsWithOAPPrefixOnly) or all env secrets
//     are rejected (PolicyNoEnvSecrets).
//  3. All violations are emitted as audit events.
func (v *Validator) ValidateScriptRequest(ctx context.Context, req ScriptRequest) []Violation {
	v.mu.RLock()
	policy := v.policy
	v.mu.RUnlock()

	var violations []Violation

	// Check args for secret references.
	if policy.Contains(PolicyNoScriptArgSecrets) {
		violations = append(violations, v.checkArgs(req.Script, req.Args)...)
	}

	// Check env vars for secret values.
	if policy.Contains(PolicyNoEnvSecrets) {
		violations = append(violations, v.checkEnvNoSecrets(req.Script, req.Env)...)
	} else if policy.Contains(PolicyEnvSecretsWithOAPPrefixOnly) {
		violations = append(violations, v.checkEnvPrefixOnly(req.Script, req.Env)...)
	}

	// Emit audit events for all violations.
	for _, violation := range violations {
		v.logViolation(ctx, req.Script, violation)
	}

	return violations
}

// checkArgs inspects each argument for patterns that suggest a secret value.
func (v *Validator) checkArgs(script string, args []string) []Violation {
	var violations []Violation

	for i, arg := range args {
		rule, matched := matchSecretPattern(arg)
		if matched {
			field := fmt.Sprintf("args[%d]", i)
			violations = append(violations, Violation{
				Field:    field,
				Value:    redactValue(arg),
				Rule:     rule,
				Severity: SeverityError,
				Message:  fmt.Sprintf("secret value detected in %s: rule %s", field, rule),
			})
		}
	}

	_ = script
	return violations
}

// checkEnvNoSecrets rejects all env vars whose names suggest they carry
// secret values, regardless of prefix.
func (v *Validator) checkEnvNoSecrets(script string, env map[string]string) []Violation {
	var violations []Violation

	for name, value := range env {
		if isSecretEnvVarName(name) && hasSecretValue(name, value) {
			violations = append(violations, Violation{
				Field:    fmt.Sprintf("env.%s", name),
				Value:    redactValue(value),
				Rule:     "env_secret_forbidden",
				Severity: SeverityError,
				Message:  fmt.Sprintf("env var %q carries a secret value but secrets in env are forbidden", name),
			})
		}
	}

	_ = script
	return violations
}

// checkEnvPrefixOnly allows env vars with the OAP_INJECTED_ prefix but
// rejects any env var whose name suggests a secret but lacks the prefix.
func (v *Validator) checkEnvPrefixOnly(script string, env map[string]string) []Violation {
	var violations []Violation

	for name, value := range env {
		if !isSecretEnvVarName(name) {
			continue
		}
		if envPrefixAllowed(name) {
			continue
		}
		// The env var name looks like a secret but lacks the OAP_INJECTED_ prefix.
		if hasSecretValue(name, value) {
			violations = append(violations, Violation{
				Field:    fmt.Sprintf("env.%s", name),
				Value:    redactValue(value),
				Rule:     "env_secret_prefix_required",
				Severity: SeverityError,
				Message:  fmt.Sprintf("env var %q carries a secret value but lacks the OAP_INJECTED_ prefix", name),
			})
		} else {
			// Name suggests a secret, but value looks like a ref — warn only.
			violations = append(violations, Violation{
				Field:    fmt.Sprintf("env.%s", name),
				Value:    redactValue(value),
				Rule:     "env_secret_suspicious_name",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("env var %q has a secret-like name but no value; consider using OAP_INJECTED_ prefix", name),
			})
		}
	}

	_ = script
	return violations
}

// HasErrors reports whether any violation in the slice has SeverityError.
func HasErrors(violations []Violation) bool {
	for _, v := range violations {
		if v.Severity == SeverityError {
			return true
		}
	}
	return false
}

// logViolation emits an audit event for a policy violation.
func (v *Validator) logViolation(ctx context.Context, script string, violation Violation) {
	outcome := audit.OutcomeDenied
	if violation.Severity == SeverityWarning {
		outcome = audit.OutcomeFailure
	}

	if v.audit != nil {
		_, _ = v.audit.Record(ctx, audit.EventInput{
			ActorType:    audit.ActorSystem,
			ActorID:      "policy-validator",
			Action:       "script.policy.violation",
			ResourceType: "script_request",
			ResourceID:   script,
			Details: map[string]any{
				"field":          violation.Field,
				"rule":           violation.Rule,
				"severity":       string(violation.Severity),
				"message":        violation.Message,
				"value_redacted": violation.Value,
			},
			Outcome: outcome,
		})
	}

	v.logger.WarnContext(ctx, "script policy violation",
		"script", script,
		"field", violation.Field,
		"rule", violation.Rule,
		"severity", string(violation.Severity),
		"message", violation.Message,
	)
}
