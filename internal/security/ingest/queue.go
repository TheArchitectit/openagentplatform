// Package ingest implements the async ingest queue for EDR events.
package ingest

import (
	"context"
	"log/slog"
	"sync/atomic"
)

const (
	DefaultBufferSize = 10000
	DefaultWorkers   = 16
)

type IngestJob struct {
	IntegrationID string
	Provider      string
	Event         []byte
	Source        string // "webhook" | "pull"
}

type JobHandler func(context.Context, IngestJob) error

// Queue is the buffered async ingest queue. A webhook handler enqueues
// jobs via Submit; workers process them concurrently. When the buffer is
// full, Submit returns false and the caller returns 503 with Retry-After.
type Queue struct {
	jobs    chan IngestJob
	workers int
	handler JobHandler
	log     *slog.Logger
	depth   atomic.Int64
}

// NewQueue returns a Queue with the default buffer size and worker count.
func NewQueue(handler JobHandler, log *slog.Logger) *Queue {
	return NewQueueWithSize(handler, DefaultBufferSize, DefaultWorkers, log)
}

// NewQueueWithSize returns a Queue with explicit buffer size and worker count.
func NewQueueWithSize(handler JobHandler, bufferSize, workers int, log *slog.Logger) *Queue {
	if log == nil {
		log = slog.Default()
	}
	return &Queue{
		jobs:    make(chan IngestJob, bufferSize),
		workers: workers,
		handler: handler,
		log:     log,
	}
}

// Start launches worker goroutines. Returns immediately; runs until ctx is done.
func (q *Queue) Start(ctx context.Context) {
	for i := 0; i < q.workers; i++ {
		go q.worker(ctx)
	}
}

// Submit enqueues a job. Returns false if the buffer is full (caller
// should respond 503 + Retry-After).
func (q *Queue) Submit(_ context.Context, job IngestJob) bool {
	select {
	case q.jobs <- job:
		q.depth.Add(1)
		return true
	default:
		return false
	}
}

func (q *Queue) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-q.jobs:
			q.depth.Add(-1)
			if err := q.handler(ctx, job); err != nil {
				q.log.Warn("security: ingest job failed",
					"provider", job.Provider,
					"source", job.Source,
					"err", err)
			}
		}
	}
}

// Depth returns the current buffer depth (for monitoring/alerting).
func (q *Queue) Depth() int64 {
	return q.depth.Load()
}
