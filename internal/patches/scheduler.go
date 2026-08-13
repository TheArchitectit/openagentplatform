package patches

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// NewPatchScheduler creates a scheduler. The deployer is used to
// actually run the jobs. The store is used to persist job state
// updates (e.g. state transitions).
func NewPatchScheduler(cfg PatchSchedulerConfig, deployer *PatchDeployer, store Store) *PatchScheduler {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = DefaultMaxConcurrency
	}
	if cfg.BlackoutCheckInterval <= 0 {
		cfg.BlackoutCheckInterval = DefaultBlackoutCheckInterval
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &PatchScheduler{
		cfg:      cfg,
		deployer: deployer,
		store:    store,
		log:      log,
		queue:    make([]*QueuedJob, 0),
		active:   make(map[string]bool),
		deferred: make(map[string]*QueuedJob),
		agentBusy: make(map[string]string),
		notify:    make(chan struct{}, 1),
	}
}

// Enqueue adds a job to the scheduler queue. It returns immediately and
// the job will be dispatched when it is runnable (respecting concurrency,
// blackouts, and maintenance windows).
func (s *PatchScheduler) Enqueue(job *models.PatchJob, priority SchedulerPriority) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	targets := make([]string, 0, len(job.Targets))
	for _, t := range job.Targets {
		targets = append(targets, t.AgentID)
	}
	notBefore := time.Now()
	if job.ScheduledAt != nil && job.ScheduledAt.After(notBefore) {
		notBefore = *job.ScheduledAt
	}
	if mw := s.cfg.DefaultMaintenanceWindow; mw != nil && notBefore.Before(mw.Start) {
		if nxt := mw.NextOccurrence(notBefore); !nxt.IsZero() && nxt.After(notBefore) {
			notBefore = nxt
		}
	}
	qj := &QueuedJob{
		Job:         job,
		Priority:    priority,
		ScheduledAt: notBefore,
		NotBefore:   notBefore,
		Targets:     targets,
		enqueuedAt:  time.Now(),
	}
	s.queue = append(s.queue, qj)
	s.signal()
}

// signal wakes the Run loop if it is waiting. Safe to call while
// holding or not holding s.mu (non-blocking send).
func (s *PatchScheduler) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// inBlackout reports whether now falls inside any configured blackout.
func (s *PatchScheduler) inBlackout(now time.Time) bool {
	for _, b := range s.cfg.Blackouts {
		if b.InWindow(now) {
			return true
		}
	}
	return false
}

// Run is the scheduler main loop. It must be started as a goroutine.
func (s *PatchScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.BlackoutCheckInterval)
	defer ticker.Stop()

	for {
		s.dispatch(ctx)

		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.notify:
		}
	}
}

// dispatch evaluates the queue and deferred map and starts any jobs that
// are runnable now. Dispatching (calling the deployer) happens outside
// the lock.
func (s *PatchScheduler) dispatch(ctx context.Context) {
	for {
		s.mu.Lock()
		runnable := s.nextRunnableLocked(time.Now())
		if runnable == nil {
			s.mu.Unlock()
			return
		}

		jobID := runnable.Job.ID
		s.active[jobID] = true
		for _, a := range runnable.Targets {
			s.agentBusy[a] = jobID
		}
		s.removeFromQueueLocked(jobID)
		delete(s.deferred, jobID)
		s.mu.Unlock()

		go s.runJob(ctx, runnable)
	}
}

// nextRunnableLocked returns the highest priority queued/deferred job
// that may run now, or nil. Caller must hold s.mu.
func (s *PatchScheduler) nextRunnableLocked(now time.Time) *QueuedJob {
	if len(s.active) >= s.cfg.MaxConcurrency {
		return nil
	}
	if s.inBlackout(now) {
		return nil
	}

	candidates := append([]*QueuedJob{}, s.queue...)
	for _, d := range s.deferred {
		candidates = append(candidates, d)
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].enqueuedAt.Before(candidates[j].enqueuedAt)
	})

	for _, c := range candidates {
		// Skip jobs whose NotBefore has not arrived.
		if c.NotBefore.After(now) {
			continue
		}
		// Skip if any target agent is already busy.
		busy := false
		for _, a := range c.Targets {
			if _, ok := s.agentBusy[a]; ok {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		return c
	}
	return nil
}

// removeFromQueueLocked drops the job with the given id from the queue.
// Caller must hold s.mu.
func (s *PatchScheduler) removeFromQueueLocked(jobID string) {
	out := s.queue[:0]
	for _, q := range s.queue {
		if q.Job.ID != jobID {
			out = append(out, q)
		}
	}
	s.queue = out
}

// runJob executes a single job via the deployer, then releases its
// resources and notifies the loop to re-evaluate.
func (s *PatchScheduler) runJob(ctx context.Context, job *QueuedJob) {
	jobID := job.Job.ID
	targets := make([]DeployTarget, 0, len(job.Targets))
	for _, a := range job.Targets {
		targets = append(targets, DeployTarget{AgentID: a})
	}

	// Mark running in the store if possible (outside the lock).
	s.setJobState(job.Job, "running")

	result, err := s.deployer.Deploy(ctx, job.Job, targets)
	if err != nil {
		s.log.Error("patch scheduler: deploy failed",
			"job_id", jobID, "err", err)
		s.setJobState(job.Job, "failed")
	} else if result != nil && result.Aborted {
		s.log.Warn("patch scheduler: deploy aborted",
			"job_id", jobID, "reason", result.AbortReason)
		s.setJobState(job.Job, "failed")
	} else {
		s.log.Info("patch scheduler: deploy complete",
			"job_id", jobID,
			"succeeded", result.Succeeded,
			"failed", result.Failed)
		s.setJobState(job.Job, "succeeded")
	}

	// Release resources outside the lock.
	s.mu.Lock()
	delete(s.active, jobID)
	for _, a := range job.Targets {
		if s.agentBusy[a] == jobID {
			delete(s.agentBusy, a)
		}
	}
	s.mu.Unlock()

	s.signal()
}

// setJobState updates the job state in the store if a store is wired up.
// It never fails loudly and is safe to call without holding s.mu.
func (s *PatchScheduler) setJobState(job *models.PatchJob, state string) {
	if s.store == nil || job == nil {
		return
	}
	job.State = state
	now := time.Now()
	job.UpdatedAt = now
	if state == "succeeded" || state == "failed" {
		ca := now
		job.CompletedAt = &ca
	}
	if err := s.store.UpdatePatchJob(context.Background(), job); err != nil {
		s.log.Warn("patch scheduler: failed to persist job state",
			"job_id", job.ID, "state", state, "err", err)
	}
}

// zeroOr guards against nil result deref via a helper.

// Close shuts down the scheduler, causing Run to exit. It is safe to
// call more than once.
func (s *PatchScheduler) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.signal()
}
