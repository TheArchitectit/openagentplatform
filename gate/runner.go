package gate

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// RunMode controls whether gates execute one at a time or concurrently.
type RunMode int

const (
	Sequential RunMode = iota
	Parallel
)

// Result contains one gate's findings and execution error.
type Result struct {
	Gate     string
	Findings []Finding
	Err      error
}

// GateRunner executes an ordered collection of gates.
type GateRunner struct {
	gates []Gate
	mode  RunMode
}

// NewRunner creates a runner for the supplied gates.
func NewRunner(mode RunMode, gates ...Gate) *GateRunner {
	return &GateRunner{gates: append([]Gate(nil), gates...), mode: mode}
}

// Run checks paths with every configured gate. Results preserve gate order.
func (r *GateRunner) Run(ctx context.Context, paths []string) ([]Result, error) {
	if ctx == nil {
		return nil, errors.New("gate: nil context")
	}

	results := make([]Result, len(r.gates))
	if r.mode == Parallel {
		var wg sync.WaitGroup
		for i, checker := range r.gates {
			wg.Add(1)
			go func(index int, g Gate) {
				defer wg.Done()
				results[index] = runGate(ctx, g, paths)
			}(i, checker)
		}
		wg.Wait()
	} else {
		for i, checker := range r.gates {
			if err := ctx.Err(); err != nil {
				results[i] = Result{Err: err}
				continue
			}
			results[i] = runGate(ctx, checker, paths)
		}
	}

	var errs []error
	for _, result := range results {
		if result.Err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", result.Gate, result.Err))
		}
	}
	return results, errors.Join(errs...)
}

func runGate(ctx context.Context, checker Gate, paths []string) Result {
	if checker == nil {
		return Result{Gate: "<nil>", Err: errors.New("nil gate")}
	}
	findings, err := checker.Check(ctx, paths)
	return Result{Gate: checker.Name(), Findings: findings, Err: err}
}
