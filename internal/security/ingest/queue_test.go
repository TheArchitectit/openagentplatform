package ingest

import (
	"context"
	"testing"
	"time"
)

func TestQueueSubmit(t *testing.T) {
	handled := 0
	q := NewQueueWithSize(
		func(ctx context.Context, j IngestJob) error {
			handled++
			return nil
		},
		10, 2, nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	q.Start(ctx)

	for i := 0; i < 5; i++ {
		if !q.Submit(ctx, IngestJob{Provider: "crowdstrike", Source: "webhook"}) {
			t.Fatalf("Submit %d returned false", i)
		}
	}
	for i := 0; i < 100 && handled < 5; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if handled != 5 {
		t.Errorf("handled = %d, want 5", handled)
	}
}

func TestQueueFull(t *testing.T) {
	// 0 workers = the queue is purely a buffer; nothing drains it.
	// Buffer size 2 means 2 jobs can sit in the channel; the 3rd
	// must reject.
	q := NewQueueWithSize(
		func(ctx context.Context, j IngestJob) error { return nil },
		2, 0, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !q.Submit(ctx, IngestJob{}) {
		t.Fatal("submit 1 should succeed")
	}
	if !q.Submit(ctx, IngestJob{}) {
		t.Fatal("submit 2 should succeed")
	}
	if q.Submit(ctx, IngestJob{}) {
		t.Error("expected Submit to return false when buffer is full")
	}
}

func TestQueueDepth(t *testing.T) {
	q := NewQueueWithSize(func(ctx context.Context, j IngestJob) error { return nil }, 10, 1, nil)
	ctx := context.Background()
	q.Submit(ctx, IngestJob{})
	q.Submit(ctx, IngestJob{})
	if d := q.Depth(); d != 2 {
		t.Errorf("Depth = %d, want 2", d)
	}
}
