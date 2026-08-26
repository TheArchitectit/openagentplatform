// Package scheduled - scheduler.go implements the recurring dispatcher.
//
// The scheduler follows the internal/reports convention: a 30s tick loop
// loads due tasks and fires each one idempotently (at-least-once via
// last_run_at comparison). It is the production entry point for scheduled
// automation (RMM-06).
package scheduled

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// TickInterval is how often the scheduler checks for due schedules.
const TickInterval = 30 * time.Second

// RunNowFunc is the callback invoked when a task fires. It receives the task
// and a context bounded by RunTimeout. The scheduler does not know what the
// action does; callers wire the concrete executor here.
type RunNowFunc func(ctx context.Context, task *TaskRecord) error

// Scheduler triggers scheduled tasks based on their cron expressions.
type Scheduler struct {
	store  Store
	log    *slog.Logger
	runNow RunNowFunc

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler builds a Scheduler. runNow is the executor callback; it is
// called for every due task. log may be nil.
func NewScheduler(store Store, log *slog.Logger, runNow RunNowFunc) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{store: store, log: log, runNow: runNow}
}

// Start begins the scheduler tick loop. It is safe to call multiple times;
// each call re-enters the loop. The caller is responsible for calling Stop.
func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.log.Info("scheduled automation scheduler started", "tick_interval", TickInterval)
		ticker := time.NewTicker(TickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				s.log.Info("scheduled automation scheduler stopping")
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
	return nil
}

// Stop halts the tick loop and waits for in-flight task executions.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// RunNow fires a task immediately, bypassing the schedule. Used by the API
// when a user manually triggers a task. It is idempotent: a task that already
// ran at this tick window is skipped.
func (s *Scheduler) RunNow(ctx context.Context, task *TaskRecord) error {
	now := time.Now().UTC()
	if task.LastRunAt != nil && !task.LastRunAt.Before(now) {
		s.log.Debug("scheduled: task already fired this window", "id", task.ID)
		return nil
	}
	return s.runNow(ctx, task)
}

// tick checks all enabled due tasks and fires each one.
func (s *Scheduler) tick() {
	now := time.Now().UTC()
	tasks, err := s.store.ListDueTasks(s.ctx, now)
	if err != nil {
		s.log.Warn("scheduled tick: list due failed", "err", err)
		return
	}
	for _, t := range tasks {
		s.fire(t, now)
	}
}

// fire executes a task idempotently. A task is considered fired for the
// current tick window when last_run_at is at or before the tick window start,
// so a slow executor cannot re-fire the same task in the next tick.
func (s *Scheduler) fire(task *TaskRecord, now time.Time) {
	// Idempotency guard: skip if the task already ran at this tick window.
	if task.LastRunAt != nil && !task.LastRunAt.Before(now) {
		s.log.Debug("scheduled: task already fired this window", "id", task.ID)
		return
	}
	s.log.Info("scheduled: firing task", "id", task.ID, "action", task.Action)
	err := s.runNow(s.ctx, task)
	status := "ok"
	if err != nil {
		status = "error"
		s.log.Error("scheduled: task failed", "id", task.ID, "action", task.Action, "err", err)
	}
	// Advance next_run_at. If the executor is slow, next_run_at is computed
	// from the original schedule so the task does not drift.
	next, err := computeNextRun(task.CronExpr, now)
	if err != nil {
		s.log.Error("scheduled: recompute next_run_at failed", "id", task.ID, "err", err)
	}
	if err := s.store.MarkRun(s.ctx, task.ID, now, status, next); err != nil {
		s.log.Error("scheduled: mark run failed", "id", task.ID, "err", err)
	}
}