package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"github.com/open-policy-agent/opa/v1/rego"
)

// RegoBuiltins are the OPA package paths this engine makes available
// to policy authors. They resolve to the oap.* Go functions registered
// via rego.Function in NewOPAEngine.
var RegoBuiltins = []string{
	"oap.agent.status",
	"oap.agent.has_check",
	"oap.check.last_result",
	"oap.agent.patch_level",
	"oap.agent.os_version",
	"oap.time.now",
	"oap.time.hours_since",
}

// Violation describes a single failed policy check.

type Violation struct {
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// EvalResult is the outcome of evaluating a single policy against a
// single input. Allowed is true when the policy produced no violations.
type EvalResult struct {
	Allowed    bool        `json:"allowed"`
	Violations []Violation `json:"violations,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// OPAEngine wraps the OPA Go library with a thread-safe compiled-query
// cache. It does NOT call an external OPA service; everything is
// in-process.
type OPAEngine struct {
	mu    sync.RWMutex
	cache map[string]*compiledPolicy // keyed by policyID
	log   *slog.Logger
	// now is the clock source for oap.time.now(). Tests may override.
	now func() time.Time
	// Builtin functions require a resolver that can look up agent data.
	// The engine calls these lazily during Eval; the resolver is set
	// at construction time and may be nil for policy-only evaluation.
	builtins BuiltinResolver
}

// BuiltinResolver backs the oap.* Go builtins. Each method is
// optional: returning an error causes the builtin to return undefined
// in Rego, which the policy author is expected to handle.
type BuiltinResolver interface {
	AgentStatus(ctx context.Context, agentID string) (string, error)
	AgentHasCheck(ctx context.Context, agentID, checkType string) (bool, error)
	CheckLastResult(ctx context.Context, agentID, checkID string) (map[string]any, error)
	AgentPatchLevel(ctx context.Context, agentID string) (string, error)
	AgentOSVersion(ctx context.Context, agentID string) (string, error)
}

// compiledPolicy holds the prepared Rego query for a single policy.
// The rego.PreparedEvalQuery is safe for concurrent use.
type compiledPolicy struct {
	policyID   string
	rego       string
	compiled   rego.PreparedEvalQuery
	compiledAt time.Time
}

// OPACfg configures NewOPAEngine.
type OPACfg struct {
	Logger   *slog.Logger
	Resolver BuiltinResolver
	// Now is the clock source for oap.time.now(). Defaults to time.Now.
	Now func() time.Time
}

// NewOPAEngine constructs a fresh engine with an empty cache.
func NewOPAEngine(cfg OPACfg) *OPAEngine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &OPAEngine{
		cache:    make(map[string]*compiledPolicy),
		log:      cfg.Logger,
		now:      cfg.Now,
		builtins: cfg.Resolver,
	}
}

// SetBuiltinResolver updates the resolver backing the oap.* builtins.
func (e *OPAEngine) SetBuiltinResolver(r BuiltinResolver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.builtins = r
}

// CompilePolicy parses and prepares a Rego module, then stores it in
// the cache under policyID. Re-compiling an existing policyID replaces
// the previous entry atomically.
func (e *OPAEngine) CompilePolicy(ctx context.Context, policyID, regoSrc string) error {
	if policyID == "" {
		return errors.New("opa: policyID required")
	}
	if regoSrc == "" {
		return errors.New("opa: rego body required")
	}
	if err := ValidateRegoSyntax(regoSrc); err != nil {
		return fmt.Errorf("opa: rego syntax invalid: %w", err)
	}

	opts := []func(*rego.Rego){
		rego.Module(policyID+".rego", regoSrc),
		rego.Query("data.oap_policy.allow"),
		rego.Query("data.oap_policy.violations"),
		rego.Query("data.oap_policy.deny"),
		rego.Query("data.oap_policy.compliant"),
	}
	// Register builtins if we have a resolver.
	opts = append(opts, e.builtinRegoOptions()...)

	r := rego.New(opts...)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("opa: prepare: %w", err)
	}

	e.mu.Lock()
	e.cache[policyID] = &compiledPolicy{
		policyID:   policyID,
		rego:       regoSrc,
		compiled:   pq,
		compiledAt: e.now(),
	}
	e.mu.Unlock()
	return nil
}

// InvalidateCache removes a compiled policy from the cache. Called
// when a policy's Rego body is updated or the policy is deleted.
func (e *OPAEngine) InvalidateCache(policyID string) {
	e.mu.Lock()
	delete(e.cache, policyID)
	e.mu.Unlock()
}

// CachedPolicyIDs returns the IDs of currently-compiled policies. Used
// for diagnostics and /health probes.
func (e *OPAEngine) CachedPolicyIDs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, 0, len(e.cache))
	for id := range e.cache {
		out = append(out, id)
	}
	return out
}

// Eval compiles (lazily) and evaluates a policy against input. The
// caller is expected to supply an input map that includes agent state
// plus whatever contextual data the policy needs. The function returns:
//
//   - allowed: true when no violations were produced
//   - violations: a list of structured Violation records
//
// The input shape is policy-defined; builtins are resolved via the
// BuiltinResolver set at engine construction time.
func (e *OPAEngine) Eval(ctx context.Context, policyID, regoSrc string, input map[string]any) (EvalResult, error) {
	// Get or compile the policy.
	cp, err := e.getOrCompile(ctx, policyID, regoSrc)
	if err != nil {
		return EvalResult{}, err
	}
	if input == nil {
		input = map[string]any{}
	}

	res, err := cp.compiled.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return EvalResult{}, fmt.Errorf("opa: eval: %w", err)
	}

	return interpretRegoResult(res), nil
}

// EvalCompiled evaluates an already-compiled policy (looked up by ID)
// against input. Returns ErrPolicyNotCompiled when the cache is empty.
func (e *OPAEngine) EvalCompiled(ctx context.Context, policyID string, input map[string]any) (EvalResult, error) {
	e.mu.RLock()
	cp, ok := e.cache[policyID]
	e.mu.RUnlock()
	if !ok {
		return EvalResult{}, fmt.Errorf("opa: policy %q not compiled", policyID)
	}
	if input == nil {
		input = map[string]any{}
	}
	res, err := cp.compiled.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return EvalResult{}, fmt.Errorf("opa: eval: %w", err)
	}
	return interpretRegoResult(res), nil
}

// getOrCompile returns the cached compiled policy or compiles it
// (using regoSrc) if missing.
func (e *OPAEngine) getOrCompile(ctx context.Context, policyID, regoSrc string) (*compiledPolicy, error) {
	e.mu.RLock()
	cp, ok := e.cache[policyID]
	e.mu.RUnlock()
	if ok {
		return cp, nil
	}
	if regoSrc == "" {
		return nil, fmt.Errorf("opa: policy %q not compiled and no source provided", policyID)
	}
	if err := e.CompilePolicy(ctx, policyID, regoSrc); err != nil {
		return nil, err
	}
	e.mu.RLock()
	cp = e.cache[policyID]
	e.mu.RUnlock()
	return cp, nil
}

// interpretRegoResult converts the raw OPA evaluation result into an
// EvalResult. Policies are expected to define one or more of:
//
//   - data.oap_policy.allow == true            (boolean: is it ok?)
//   - data.oap_policy.deny == true             (boolean: is it blocked?)
//   - data.oap_policy.compliant == true        (boolean alias for allow)
//   - data.oap_policy.violations == [{msg, ...}] (structured list)
//
// The mapping below is intentionally lenient: a missing definition
// defaults to "allowed", which means a policy that exposes no decisions
// is treated as compliant. Authors MUST use the oap_policy package to
// express rules.
func interpretRegoResult(res rego.ResultSet) EvalResult {
	if len(res) == 0 {
		// No expressions matched: default to allowed.
		return EvalResult{Allowed: true}
	}
	expressions := res[0].Expressions

	allowVal, hasAllow := boolFromExprs(expressions, "data.oap_policy.allow")
	denyVal, hasDeny := boolFromExprs(expressions, "data.oap_policy.deny")
	compliantVal, hasCompliant := boolFromExprs(expressions, "data.oap_policy.compliant")
	violations := violationsFromExprs(expressions, "data.oap_policy.violations")

	if hasAllow {
		return EvalResult{Allowed: allowVal, Violations: violations, Details: map[string]any{"source": "allow"}}
	}
	if hasDeny {
		return EvalResult{Allowed: !denyVal, Violations: violations, Details: map[string]any{"source": "deny"}}
	}
	if hasCompliant {
		return EvalResult{Allowed: compliantVal, Violations: violations, Details: map[string]any{"source": "compliant"}}
	}
	// If a violations list is present but no allow/deny flag, derive
	// compliance from the list.
	if len(violations) > 0 {
		return EvalResult{Allowed: false, Violations: violations, Details: map[string]any{"source": "violations"}}
	}
	// No recognised decision: treat as allowed.
	return EvalResult{Allowed: true, Details: map[string]any{"source": "default"}}
}

func boolFromExprs(exprs []*rego.ExpressionValue, key string) (bool, bool) {
	for _, e := range exprs {
		if e.Text != key {
			continue
		}
		if b, ok := e.Value.(bool); ok {
			return b, true
		}
	}
	return false, false
}

func violationsFromExprs(exprs []*rego.ExpressionValue, key string) []Violation {
	for _, e := range exprs {
		if e.Text != key {
			continue
		}
		arr, ok := e.Value.([]any)
		if !ok {
			return nil
		}
		out := make([]Violation, 0, len(arr))
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			v := Violation{}
			if s, ok := m["msg"].(string); ok {
				v.Message = s
			} else if s, ok := m["message"].(string); ok {
				v.Message = s
			}
			if d, ok := m["details"].(map[string]any); ok {
				v.Details = d
			}
			out = append(out, v)
		}
		return out
	}
	return nil
}

// builtinRegoOptions builds the rego.Function registrations for every
// oap.* builtin the engine supports. Functions whose resolver returns
// an error produce undefined in Rego (the standard OPA pattern), so
// policy authors can use default rules to handle missing data.
