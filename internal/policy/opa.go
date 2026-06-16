// Package policy - opa.go wraps the OPA Rego evaluation library as a
// in-process policy engine. Policies are compiled once and cached by
// policy ID; evaluations reuse the prepared query.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/types"
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
func (e *OPAEngine) builtinRegoOptions() []func(*rego.Rego) {
	resolver := e.builtins
	now := e.now
	opts := []func(*rego.Rego){
		// oap.agent.status(agent_id) -> string
		rego.Function1(
			&rego.Function{
				Name: "oap.agent.status",
				Decl: types.NewFunction(
					types.Args(types.S), types.S,
				),
			},
			func(bctx rego.BuiltinContext, agentID *ast.Term) (*ast.Term, error) {
				if resolver == nil {
					return nil, nil
				}
				id, ok := agentID.Value.(ast.String)
				if !ok {
					return nil, nil
				}
				s, err := resolver.AgentStatus(bctx.Context, string(id))
				if err != nil {
					return nil, nil
				}
				return ast.StringTerm(s), nil
			},
		),
		// oap.agent.has_check(agent_id, check_type) -> bool
		rego.Function2(
			&rego.Function{
				Name: "oap.agent.has_check",
				Decl: types.NewFunction(
					types.Args(types.S, types.S), types.B,
				),
			},
			func(bctx rego.BuiltinContext, agentID, checkType *ast.Term) (*ast.Term, error) {
				if resolver == nil {
					return nil, nil
				}
				id, ok1 := agentID.Value.(ast.String)
				ct, ok2 := checkType.Value.(ast.String)
				if !ok1 || !ok2 {
					return nil, nil
				}
				ok, err := resolver.AgentHasCheck(bctx.Context, string(id), string(ct))
				if err != nil {
					return nil, nil
				}
				return ast.BooleanTerm(ok), nil
			},
		),
		// oap.check.last_result(agent_id, check_id) -> object
		rego.Function2(
			&rego.Function{
				Name: "oap.check.last_result",
				Decl: types.NewFunction(
					types.Args(types.S, types.S), types.A,
				),
			},
			func(bctx rego.BuiltinContext, agentID, checkID *ast.Term) (*ast.Term, error) {
				if resolver == nil {
					return nil, nil
				}
				id, ok1 := agentID.Value.(ast.String)
				cid, ok2 := checkID.Value.(ast.String)
				if !ok1 || !ok2 {
					return nil, nil
				}
				m, err := resolver.CheckLastResult(bctx.Context, string(id), string(cid))
				if err != nil {
					return nil, nil
				}
				return goMapToObjectTerm(m)
			},
		),
		// oap.agent.patch_level(agent_id) -> string
		rego.Function1(
			&rego.Function{
				Name: "oap.agent.patch_level",
				Decl: types.NewFunction(
					types.Args(types.S), types.S,
				),
			},
			func(bctx rego.BuiltinContext, agentID *ast.Term) (*ast.Term, error) {
				if resolver == nil {
					return nil, nil
				}
				id, ok := agentID.Value.(ast.String)
				if !ok {
					return nil, nil
				}
				s, err := resolver.AgentPatchLevel(bctx.Context, string(id))
				if err != nil {
					return nil, nil
				}
				return ast.StringTerm(s), nil
			},
		),
		// oap.agent.os_version(agent_id) -> string
		rego.Function1(
			&rego.Function{
				Name: "oap.agent.os_version",
				Decl: types.NewFunction(
					types.Args(types.S), types.S,
				),
			},
			func(bctx rego.BuiltinContext, agentID *ast.Term) (*ast.Term, error) {
				if resolver == nil {
					return nil, nil
				}
				id, ok := agentID.Value.(ast.String)
				if !ok {
					return nil, nil
				}
				s, err := resolver.AgentOSVersion(bctx.Context, string(id))
				if err != nil {
					return nil, nil
				}
				return ast.StringTerm(s), nil
			},
		),
		// oap.time.now() -> number (nanoseconds since epoch)
		rego.FunctionDyn(
			&rego.Function{
				Name: "oap.time.now",
				Decl: types.NewFunction(
					nil, types.N,
				),
			},
			func(_ topdown.BuiltinContext, _ []*ast.Term) (*ast.Term, error) {
				n := strconv.FormatInt(now().UnixNano(), 10)
				return ast.NumberTerm(json.Number(n)), nil
			},
		),
		// oap.time.hours_since(timestamp) -> number
		rego.Function1(
			&rego.Function{
				Name: "oap.time.hours_since",
				Decl: types.NewFunction(
					types.Args(types.N), types.N,
				),
			},
			func(_ rego.BuiltinContext, ts *ast.Term) (*ast.Term, error) {
				nv, ok := ts.Value.(ast.Number)
				if !ok {
					return nil, nil
				}
				f, _ := nv.Float64()
				secs := now().UnixNano()/1_000_000_000 - int64(f)/1_000_000_000
				hours := float64(secs) / 3600.0
				return ast.FloatNumberTerm(hours), nil
			},
		),
	}
	return opts
}

// goMapToObjectTerm converts a Go map[string]any into an OPA object
// term. The conversion is recursive so nested maps and slices are
// preserved.
func goMapToObjectTerm(m map[string]any) (*ast.Term, error) {
	if m == nil {
		return ast.ObjectTerm(), nil
	}
	pairs := make([][2]*ast.Term, 0, len(m))
	for k, v := range m {
		t, err := goValueToTerm(v)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, [2]*ast.Term{ast.StringTerm(k), t})
	}
	return ast.ObjectTerm(pairs...), nil
}

// goValueToTerm converts an arbitrary Go value (map, slice, string,
// number, bool) into an OPA *ast.Term.
func goValueToTerm(v any) (*ast.Term, error) {
	switch val := v.(type) {
	case nil:
		return ast.NullTerm(), nil
	case bool:
		return ast.BooleanTerm(val), nil
	case string:
		return ast.StringTerm(val), nil
	case json.Number:
		return ast.NumberTerm(val), nil
	case float64:
		return ast.FloatNumberTerm(val), nil
	case int:
		return ast.IntNumberTerm(val), nil
	case int64:
		return ast.IntNumberTerm(int(val)), nil
	case map[string]any:
		return goMapToObjectTerm(val)
	case []any:
		terms := make([]*ast.Term, 0, len(val))
		for _, item := range val {
			t, err := goValueToTerm(item)
			if err != nil {
				return nil, err
			}
			terms = append(terms, t)
		}
		return ast.ArrayTerm(terms...), nil
	default:
		// Fallback: JSON round-trip. This handles time.Time, etc.
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var x any
		if err := json.Unmarshal(raw, &x); err != nil {
			return nil, err
		}
		return goValueToTerm(x)
	}
}

// ValidateRegoSyntax performs a lightweight structural check on a Rego
// source string. It is intentionally permissive (OPA itself is the
// authority on syntax); the goal is to reject obviously broken input
// early, before the engine tries to compile. Full validation happens
// in CompilePolicy.
func ValidateRegoSyntax(src string) error {
	if src == "" {
		return errors.New("empty rego source")
	}
	// Must declare a package.
	if !packageRe.MatchString(src) {
		return errors.New("rego must declare a package")
	}
	return nil
}

var packageRe = regexp.MustCompile(`(?m)^\s*package\s+[\w.]+`)

// --- Default Rego policy bodies --------------------------------------------
// These are the built-in compliance checks that ship with the
// platform. The engine seeds them into the database on first boot and
// compiles them into the cache.

const (
	RegoAntivirusInstalled = `package oap_policy

# AV-1: agent must have an AV check assigned and the latest result
# must be "pass".
default allow = false

allow {
    has_av_check
    last_av_result == "pass"
}

has_av_check {
    oap.agent.has_check(input.agent_id, "antivirus")
}

last_av_result = status {
    r := oap.check.last_result(input.agent_id, "antivirus")
    status := r.status
}

violations[{"msg": msg, "details": d}] {
    not oap.agent.has_check(input.agent_id, "antivirus")
    msg := "no antivirus check assigned"
    d := {"check": "antivirus"}
}

violations[{"msg": msg, "details": d}] {
    oap.agent.has_check(input.agent_id, "antivirus")
    r := oap.check.last_result(input.agent_id, "antivirus")
    r.status != "pass"
    msg := sprintf("antivirus check status is %v, expected pass", [r.status])
    d := {"check": "antivirus", "status": r.status}
}
`

	RegoFirewallEnabled = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "firewall")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "firewall")
    r.status != "pass"
    msg := sprintf("firewall status is %v, expected pass", [r.status])
    d := {"check": "firewall", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    not oap.check.last_result(input.agent_id, "firewall")
    msg := "firewall check has never run"
    d := {"check": "firewall"}
}
`

	RegoDiskEncryption = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "disk_encryption")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "disk_encryption")
    r.status != "pass"
    msg := sprintf("disk encryption status is %v, expected pass", [r.status])
    d := {"check": "disk_encryption", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    not oap.check.last_result(input.agent_id, "disk_encryption")
    msg := "disk encryption check has never run"
    d := {"check": "disk_encryption"}
}
`

	RegoOSPatching = `package oap_policy

# OS patches must be up-to-date: no critical patches older than 30 days.
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "os_patching")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "os_patching")
    r.status != "pass"
    msg := sprintf("os patching status is %v, expected pass", [r.status])
    d := {"check": "os_patching", "status": r.status}
}
`

	RegoPasswordPolicy = `package oap_policy

default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "password_policy")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "password_policy")
    r.status != "pass"
    msg := sprintf("password policy status is %v, expected pass", [r.status])
    d := {"check": "password_policy", "status": r.status}
}
`

	RegoScreenLock = `package oap_policy

# Screen lock timeout must be <= 15 minutes (900 seconds).
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status == "pass"
    r.value <= 900
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status != "pass"
    msg := sprintf("screen lock status is %v, expected pass", [r.status])
    d := {"check": "screen_lock", "status": r.status}
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "screen_lock")
    r.status == "pass"
    r.value > 900
    msg := sprintf("screen lock timeout is %v seconds; must be <= 900", [r.value])
    d := {"check": "screen_lock", "value": r.value}
}
`

	RegoMonitoringAgentRunning = `package oap_policy

# The OAP agent must be running on the endpoint.
default allow = false

allow {
    oap.agent.status(input.agent_id) == "online"
}

violations[{"msg": msg, "details": d}] {
    oap.agent.status(input.agent_id) != "online"
    msg := sprintf("agent status is %v, expected online", [oap.agent.status(input.agent_id)])
    d := {"agent_id": input.agent_id, "status": oap.agent.status(input.agent_id)}
}
`

	RegoNoSuspiciousServices = `package oap_policy

# No known malicious services should be detected.
default allow = false

allow {
    r := oap.check.last_result(input.agent_id, "suspicious_services")
    r.status == "pass"
}

violations[{"msg": msg, "details": d}] {
    r := oap.check.last_result(input.agent_id, "suspicious_services")
    r.status != "pass"
    msg := sprintf("suspicious services detected: %v", [r.message])
    d := {"check": "suspicious_services", "details": r.details}
}
`
)

// defaultPolicyMeta maps policy name -> metadata. The engine uses it
// to seed the built-in compliance policies on first boot.
type defaultPolicyMeta struct {
	Rego        string
	Description string
	Category    string
	Severity    string
}

// AllDefaultRegoPolicies maps policy name -> rego body. Used by
// the policy engine's startup seeder.
var AllDefaultRegoPolicies = map[string]defaultPolicyMeta{
	"antivirus_installed": {
		Rego:        RegoAntivirusInstalled,
		Description: "Agent must have an antivirus check that is currently passing.",
		Category:    "security",
		Severity:    "critical",
	},
	"firewall_enabled": {
		Rego:        RegoFirewallEnabled,
		Description: "Host firewall service must be running and passing checks.",
		Category:    "security",
		Severity:    "critical",
	},
	"disk_encryption": {
		Rego:        RegoDiskEncryption,
		Description: "Disk encryption must be active on the host.",
		Category:    "security",
		Severity:    "warning",
	},
	"os_patching": {
		Rego:        RegoOSPatching,
		Description: "Operating system must be patched; no critical patches >30 days old.",
		Category:    "compliance",
		Severity:    "warning",
	},
	"password_policy": {
		Rego:        RegoPasswordPolicy,
		Description: "System password policy must meet complexity requirements.",
		Category:    "compliance",
		Severity:    "warning",
	},
	"screen_lock": {
		Rego:        RegoScreenLock,
		Description: "Screen lock timeout must be <= 15 minutes.",
		Category:    "security",
		Severity:    "info",
	},
	"monitoring_agent_running": {
		Rego:        RegoMonitoringAgentRunning,
		Description: "OpenAgentPlatform agent must be running on the endpoint.",
		Category:    "operational",
		Severity:    "critical",
	},
	"no_suspicious_services": {
		Rego:        RegoNoSuspiciousServices,
		Description: "No known malicious services should be running on the host.",
		Category:    "security",
		Severity:    "critical",
	},
}
