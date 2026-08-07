package policy

import (
	"encoding/json"
	"strconv"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown"
	"github.com/open-policy-agent/opa/v1/types"
)

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
