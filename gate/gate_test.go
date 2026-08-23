package gate

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type testGate struct {
	name     string
	findings []Finding
	err      error
	delay    time.Duration
	mu       *sync.Mutex
	order    *[]string
}

func (g testGate) Name() string { return g.name }

func (g testGate) Check(ctx context.Context, _ []string) ([]Finding, error) {
	select {
	case <-time.After(g.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if g.order != nil {
		g.mu.Lock()
		*g.order = append(*g.order, g.name)
		g.mu.Unlock()
	}
	return g.findings, g.err
}

func TestRunnerSequentialPreservesOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	runner := NewRunner(Sequential,
		testGate{name: "first", mu: &mu, order: &order},
		testGate{name: "second", mu: &mu, order: &order},
	)
	results, err := runner.Run(context.Background(), []string{"file.go"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"first", "second"}) {
		t.Fatalf("order = %v", order)
	}
	if results[0].Gate != "first" || results[1].Gate != "second" {
		t.Fatalf("result order = %#v", results)
	}
}

func TestRunnerParallelRunsConcurrently(t *testing.T) {
	runner := NewRunner(Parallel,
		testGate{name: "first", delay: 80 * time.Millisecond},
		testGate{name: "second", delay: 80 * time.Millisecond},
	)
	started := time.Now()
	results, err := runner.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 150*time.Millisecond {
		t.Fatalf("parallel run took %v", elapsed)
	}
	if results[0].Gate != "first" || results[1].Gate != "second" {
		t.Fatalf("result order = %#v", results)
	}
}

func TestRunnerAggregatesErrors(t *testing.T) {
	failure := errors.New("failed")
	runner := NewRunner(Sequential, testGate{name: "bad", err: failure}, nil)
	results, err := runner.Run(context.Background(), nil)
	if !errors.Is(err, failure) {
		t.Fatalf("Run error = %v", err)
	}
	if len(results) != 2 || results[1].Gate != "<nil>" || results[1].Err == nil {
		t.Fatalf("results = %#v", results)
	}
}

func TestRunnerRejectsNilContext(t *testing.T) {
	if _, err := NewRunner(Sequential).Run(nil, nil); err == nil {
		t.Fatal("expected nil context error")
	}
}

func TestRunnerHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := NewRunner(Parallel, testGate{name: "cancelled", delay: time.Second}).Run(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v", err)
	}
	if !errors.Is(results[0].Err, context.Canceled) {
		t.Fatalf("result error = %v", results[0].Err)
	}
}
