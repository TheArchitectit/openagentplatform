// Package safety — policy enforcement for script credential safety.
//
// This file implements the enforcement layer that inspects script requests
// (arguments, environment variables) and rejects those that would expose
// secrets through observable channels.
package safety

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

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
	return v.policy
}

// SetPolicy updates the active safety policy.
func (v *Validator) SetPolicy(p SafetyPolicy) {
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
	var violations []Violation

	// Check args for secret references.
	if v.policy.Contains(PolicyNoScriptArgSecrets) {
		violations = append(violations, v.checkArgs(req.Script, req.Args)...)
	}

	// Check env vars for secret values.
	if v.policy.Contains(PolicyNoEnvSecrets) {
		violations = append(violations, v.checkEnvNoSecrets(req.Script, req.Env)...)
	} else if v.policy.Contains(PolicyEnvSecretsWithOAPPrefixOnly) {
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
				Field:     field,
				Value:     redactValue(arg),
				Rule:      rule,
				Severity:  SeverityError,
				Message:   fmt.Sprintf("secret value detected in %s: rule %s", field, rule),
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
				"field":     violation.Field,
				"rule":      violation.Rule,
				"severity":  string(violation.Severity),
				"message":   violation.Message,
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

// --- pattern matching helpers ---

// envPrefix is the allowed prefix for injected secrets in env vars.
const envPrefix = "OAP_INJECTED_"

// secretNamePatterns are substrings in env var names that suggest a secret.
var secretNamePatterns = []string{
	"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY", "APIKEY",
	"PRIVATE_KEY", "PRIVATEKEY", "CREDENTIAL", "AUTH", "SESSION",
	"ENCRYPTION_KEY", "SIGNING_KEY", "CLIENT_SECRET",
}

// secretValuePatterns matches common credential value formats.
var secretValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^-----BEGIN .+ PRIVATE KEY-----`),           // PEM keys
	regexp.MustCompile(`^[A-Za-z0-9_-]{40,}\.[A-Za-z0-9_-]{40,}`),  // JWT-like
	regexp.MustCompile(`^(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}$`),  // GitHub tokens
	regexp.MustCompile(`^xox[bpoasr]-[A-Za-z0-9-]{10,}$`),           // Slack tokens
	regexp.MustCompile(`^sk-[A-Za-z0-9]{20,}$`),                     // OpenAI-style keys
	regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`),                        // AWS access keys
	regexp.MustCompile(`^AIza[0-9A-Za-z_-]{35}$`),                   // Google API keys
	regexp.MustCompile(`^glpat-[A-Za-z0-9_-]{20,}$`),                // GitLab PAT
	regexp.MustCompile(`^[A-Za-z0-9+/]{40,}={0,2}$`),               // base64 blobs >= 40 chars
}

// refPattern matches a secret reference URI like ref:oap://...
var refPattern = regexp.MustCompile(`^ref:oap://`)

// isSecretEnvVarName reports whether the name suggests a secret value.
func isSecretEnvVarName(name string) bool {
	upper := strings.ToUpper(name)
	for _, pattern := range secretNamePatterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

// envPrefixAllowed reports whether the env var name has the OAP_INJECTED_ prefix.
func envPrefixAllowed(name string) bool {
	return strings.HasPrefix(name, envPrefix)
}

// hasSecretValue reports whether the value looks like a raw secret (not a ref).
func hasSecretValue(name, value string) bool {
	if value == "" {
		return false
	}
	// A reference URI is not a raw secret value.
	if refPattern.MatchString(value) {
		return false
	}
	for _, re := range secretValuePatterns {
		if re.MatchString(value) {
			return true
		}
	}
	// If the name suggests a secret and the value is non-trivial, consider it a secret.
	if isSecretEnvVarName(name) && len(value) >= 16 {
		return true
	}
	return false
}

// matchSecretPattern returns the matching rule name and true if the argument
// looks like a secret value.
func matchSecretPattern(arg string) (string, bool) {
	if arg == "" {
		return "", false
	}
	if refPattern.MatchString(arg) {
		return "", false
	}
	for i, re := range secretValuePatterns {
		if re.MatchString(arg) {
			ruleNames := []string{
				"pem_private_key", "jwt_token", "github_token", "slack_token",
				"openai_key", "aws_access_key", "google_api_key", "gitlab_pat",
				"base64_blob",
			}
			return ruleNames[i], true
		}
	}
	return "", false
}

// looksLikeSecret is a top-level helper used by ScriptCredentialSafe to
// quickly check whether a key-value pair looks like a secret.
func looksLikeSecret(key, value string) bool {
	if value == "" {
		return false
	}
	if refPattern.MatchString(value) {
		return false
	}
	if isSecretEnvVarName(key) {
		return true
	}
	_, matched := matchSecretPattern(value)
	return matched
}

// redactValue truncates a value for safe logging, showing only the first 4
// characters followed by a redaction marker.
func redactValue(v string) string {
	if len(v) <= 4 {
		return "***"
	}
	return v[:4] + "***"
}
