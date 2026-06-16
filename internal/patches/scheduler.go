// Package patches - scheduler.go implements the PatchScheduler. The
// scheduler queues approved patch jobs for execution, enforces
// maintenance windows, detects conflicts (no two deployments may
// target the same agent at the same time), respects blackout
// periods, and limits concurrency.
package patches

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Default scheduler values.
const (
	DefaultMaxConcurrency     = 10
	DefaultBlackoutCheckInterval = 30 * time.Second
)

// SchedulerPriority ranks jobs in the queue. Higher priority jobs run
// first. Critical > High > Normal > Low.
type SchedulerPriority int

const (
	PriorityLow      SchedulerPriority = 0
	PriorityNormal   SchedulerPriority = 10
	PriorityHigh     SchedulerPriority = 50
	PriorityCritical SchedulerPriority = 100
)

// PriorityFor returns the scheduler priority for a patch severity.
func PriorityFor(sev models.PatchSeverity) SchedulerPriority {
	switch sev {
	case models.PatchSeverityCritical:
		return PriorityCritical
	case models.PatchSeverityMajorOS:
		return PriorityHigh
	case models.PatchSeverityStandard:
		return PriorityNormal
	}
	return PriorityLow
}

// BlackoutWindow is a time range during which no deployments may
// run. Multiple blackouts may be configured.
type BlackoutWindow struct {
	Start    time.Time
	End      time.Time
	Reason   string
	Recurring bool // if true, the window recurs weekly (same weekday)
}

// InWindow returns true if the given time falls within the blackout.
func (b BlackoutWindow) InWindow(t time.Time) bool {
	if b.Recurring {
		// Weekly recurrence: match day-of-week and time-of-day.
		if t.Weekday() != b.Start.Weekday() {
			return false
		}
		startTOD := timeOfDay(b.Start)
		endTOD := timeOfDay(b.End)
		tod := timeOfDay(t)
		if startTOD <= endTOD {
			return tod >= startTOD && tod < endTOD
		}
		// Window crosses midnight.
		return tod >= startTOD || tod < endTOD
	}
	return !t.Before(b.Start) && t.Before(b.End)
}

// timeOfDay strips the date component from a time.
func timeOfDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second
}

// QueuedJob is a patch job waiting to be dispatched.
type QueuedJob struct {
	Job         *models.PatchJob
	Priority    SchedulerPriority
	ScheduledAt time.Time
	// NotBefore is the earliest time the job may start. For jobs with
	// an explicit schedule this equals ScheduledAt; for maintenance-
	// window-only jobs it is the window start.
	NotBefore time.Time
	// Targets lists the agent ids this job will deploy to. Populated
	// at enqueue time from the job's Targets slice.
	Targets []string
	// enqueuedAt records when the job was added to the queue. Used
	// for FIFO ordering within a priority band.
	enqueuedAt time.Time
}

// PatchSchedulerConfig is the configurable behaviour for the
// scheduler.
type PatchSchedulerConfig struct {
	// MaxConcurrency is the maximum number of deployments that may
	// run simultaneously. Default 10.
	MaxConcurrency int
	// DefaultMaintenanceWindow, if set, is used to compute NotBefore
	// for jobs that do not specify a ScheduledAt but do specify a
	// maintenance window.
	DefaultMaintenanceWindow *MaintenanceWindow
	// Blackouts is the set of blackout windows to enforce.
	Blackouts []BlackoutWindow
	// BlackoutCheckInterval is the cadence at which the scheduler
	// re-evaluates blackouts for jobs in the deferred state. Default
	// 30s.
	BlackoutCheckInterval time.Duration
	// Logger is the slog logger. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// MaintenanceWindow represents a recurring or one-shot window during
// which patch deployments are permitted.
type MaintenanceWindow struct {
	Start    time.Time
	End      time.Time
	Recurring bool
	// Weekdays, if non-empty, restricts the window to those days.
	Weekdays []time.Weekday
}

// NextOccurrence returns the next time the window is open on or
// after the given reference time. For non-recurring windows this is
// the window start if it is in the future, otherwise zero.
func (w *MaintenanceWindow) NextOccurrence(after time.Time) time.Time {
	if w == nil {
		return time.Time{}
	}
	if !w.Recurring {
		if w.Start.After(after) {
			return w.Start
		}
		return time.Time{}
	}
	// Find the next weekday match.
	for d := 0; d < 14; d++ {
		candidate := after.AddDate(0, 0, d)
		if len(w.Weekdays) > 0 {
			ok := false
			for _, wd := range w.Weekdays {
				if candidate.Weekday() == wd {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		// Build candidate start/end for this day.
		start := time.Date(candidate.Year(), candidate.Month(), candidate.Day(),
			w.Start.Hour(), w.Start.Minute(), w.Start.Second(), 0, w.Start.Location())
		end := time.Date(candidate.Year(), candidate.Month(), candidate.Day(),
			w.End.Hour(), w.End.Minute(), w.End.Second(), 0, w.End.Location())
		if end.Before(start) {
			end = end.Add(24 * time.Hour)
		}
		if !after.After(start) && !after.After(end) {
			return start
		}
	}
	return time.Time{}
}

// PatchScheduler queues and dispatches patch deployments.
type PatchScheduler struct {
	cfg    PatchSchedulerConfig
	deployer *PatchDeployer
	store  Store
	log    *slog.Logger

	mu       sync.Mutex
	queue    []*QueuedJob
	active   map[string]bool          // jobID -> running
	deferred map[string]*QueuedJob    // jobs waiting for blackout
	agentBusy map[string]string       // agentID -> jobID currently using it
	closed   bool
	notify   chan struct{}
}

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
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &PatchScheduler{
		cfg:       cfg,
		deployer:  deployer,
		store:     store,
		log:       cfg.Logger,
		active:    make(map[string]bool),
		deferred:  make(map[string]*QueuedJob),
		agentBusy: make(map[string]string),
		notify:    make(chan struct{}, 1),
	}
}

// Close stops the scheduler's dispatch loop. In-flight deployments
// are not cancelled; the caller must wait for them or cancel their
// context.
func (s *PatchScheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.signal()
}

// Enqueue adds a job to the scheduler. If the job's NotBefore falls
// inside a blackout window, it is placed in the deferred set and
// dispatched automatically when the blackout ends. Otherwise it is
// placed in the priority queue.
func (s *PatchScheduler) Enqueue(job *models.PatchJob) error {
	if job == nil {
		return errors.New("patches: scheduler: nil job")
	}
	if job.ID == "" {
		return errors.New("patches: scheduler: job ID required")
	}
	qj := &QueuedJob{
		Job:        job,
		Priority:   PriorityFor(job.Severity),
		enqueuedAt: time.Now(),
	}
	if job.ScheduledAt != nil {
		qj.ScheduledAt = *job.ScheduledAt
		qj.NotBefore = *job.ScheduledAt
	} else if s.cfg.DefaultMaintenanceWindow != nil {
		next := s.cfg.DefaultMaintenanceWindow.NextOccurrence(time.Now())
		if !next.IsZero() {
			qj.NotBefore = next
		}
	}
	for _, t := range job.Targets {
		qj.Targets = append(qj.Targets, t.AgentID)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("patches: scheduler: closed")
	}
	// Check for blackouts.
	if s.inBlackout(qj.NotBefore) {
		s.deferred[job.ID] = qj
		s.log.Info("patch scheduler: deferred (blackout)",
			"job_id", job.ID, "not_before", qj.NotBefore)
	} else {
		s.queue = append(s.queue, qj)
		s.sortQueueLocked()
		s.log.Info("patch scheduler: enqueued",
			"job_id", job.ID,
			"priority", qj.Priority,
			"targets", len(qj.Targets),
		)
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

// Cancel removes a job from the queue or the deferred set. It has no
// effect on jobs already in progress.
func (s *PatchScheduler) Cancel(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, qj := range s.queue {
		if qj.Job.ID == jobID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			s.log.Info("patch scheduler: cancelled from queue", "job_id", jobID)
			return nil
		}
	}
	if _, ok := s.deferred[jobID]; ok {
		delete(s.deferred, jobID)
		s.log.Info("patch scheduler: cancelled from deferred", "job_id", jobID)
		return nil
	}
	return fmt.Errorf("patches: scheduler: job %s not found", jobID)
}

// QueueLen returns the number of jobs waiting in the priority queue
// (not including deferred jobs).
func (s *PatchScheduler) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// DeferredLen returns the number of jobs currently in the deferred
// (blackout) set.
func (s *PatchScheduler) DeferredLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.deferred)
}

// ActiveLen returns the number of jobs currently in progress.
func (s *PatchScheduler) ActiveLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

// Run starts the dispatch loop. It blocks until ctx is cancelled or
// Close is called. The loop re-evaluates deferred jobs on
// BlackoutCheckInterval and dispatches ready jobs up to MaxConcurrency.
func (s *PatchScheduler) Run(ctx context.Context) {
	blackoutTicker := time.NewTicker(s.cfg.BlackoutCheckInterval)
	defer blackoutTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
			s.dispatchReady(ctx)
		case <-blackoutTicker.C:
			s.releaseFromBlackout(ctx)
			s.dispatchReady(ctx)
		}
	}
}

// signal nudges the dispatch loop.
func (s *PatchScheduler) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// sortQueueLocked sorts the queue by descending priority, then by
// earliest enqueue time. Must be called with s.mu held.
func (s *PatchScheduler) sortQueueLocked() {
	sort.SliceStable(s.queue, func(i, j int) bool {
		if s.queue[i].Priority != s.queue[j].Priority {
			return s.queue[i].Priority > s.queue[j].Priority
		}
		return s.queue[i].enqueuedAt.Before(s.queue[j].enqueuedAt)
	})
}

// inBlackout returns true if the given time falls inside any
// configured blackout window. Must be called with s.mu held.
func (s *PatchScheduler) inBlackout(t time.Time) bool {
	for _, b := range s.cfg.Blackouts {
		if b.InWindow(t) {
			return true
		}
	}
	return false
}

// releaseFromBlackout moves deferred jobs whose NotBefore is no
// longer in a blackout into the main queue.
func (s *PatchScheduler) releaseFromBlackout(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, qj := range s.deferred {
		if s.inBlackout(now) {
			// Still in a blackout; keep deferred.
			continue
		}
		delete(s.deferred, id)
		s.queue = append(s.queue, qj)
		s.log.Info("patch scheduler: released from blackout",
			"job_id", id)
	}
	if len(s.deferred) == 0 {
		s.sortQueueLocked()
		s.signal()
	}
}

// dispatchReady launches all eligible jobs up to the concurrency
// limit. A job is eligible when:
//   - its NotBefore has passed,
//   - it is not currently in a blackout,
//   - it does not conflict with an in-flight job (shared target agent),
//   - we are below MaxConcurrency.
func (s *PatchScheduler) dispatchReady(ctx context.Context) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	dispatched := 0
	remaining := make([]*QueuedJob, 0, len(s.queue))
	for _, qj := range s.queue {
		if dispatched+len(s.active) >= s.cfg.MaxConcurrency {
			remaining = append(remaining, qj)
			continue
		}
		if qj.NotBefore.After(now) {
			remaining = append(remaining, qj)
			continue
		}
		if s.inBlackout(now) {
			remaining = append(remaining, qj)
			continue
		}
		// Conflict detection: do not dispatch if any target agent
		// is already busy with another active job.
		if s.hasAgentConflict(qj) {
			remaining = append(remaining, qj)
			continue
		}
		// Mark active and reserve agents.
		s.active[qj.Job.ID] = true
		for _, aid := range qj.Targets {
			s.agentBusy[aid] = qj.Job.ID
		}
		dispatched++
		// Launch dispatch in a goroutine.
		go s.runJob(ctx, qj)
	}
	s.queue = remaining
	s.sortQueueLocked()
	s.mu.Unlock()
}

// hasAgentConflict returns true if any of the job's target agents
// are already in use by another active job. Must be called with
// s.mu held.
func (s *PatchScheduler) hasAgentConflict(qj *QueuedJob) bool {
	for _, aid := range qj.Targets {
		if _, busy := s.agentBusy[aid]; busy {
			return true
		}
	}
	return false
}

// runJob executes a single queued job using the deployer. It
// performs the state transition to in_progress, runs the deployment,
// then transitions to completed or failed. On exit it releases the
// agent reservations and signals the loop.
func (s *PatchScheduler) runJob(ctx context.Context, qj *QueuedJob) {
	defer func() {
		s.mu.Lock()
		delete(s.active, qj.Job.ID)
		for _, aid := range qj.Targets {
			if s.agentBusy[aid] == qj.Job.ID {
				delete(s.agentBusy, aid)
			}
		}
		s.mu.Unlock()
		s.signal()
	}()

	job := qj.Job
	// Transition to in_progress via the approval workflow (system event).
	workflow := NewApprovalWorkflow()
	if _, err := workflow.Transition(ctx, TransitionInput{
		Job:   job,
		Event: EventStart,
	}); err != nil {
		s.log.Warn("patch scheduler: start transition failed",
			"job_id", job.ID, "err", err)
	} else if s.store != nil {
		if err := s.store.UpdatePatchJob(ctx, job); err != nil {
			s.log.Warn("patch scheduler: persist start failed",
				"job_id", job.ID, "err", err)
		}
	}

	// Build deploy targets from job.Targets.
	targets := make([]DeployTarget, 0, len(job.Targets))
	for _, t := range job.Targets {
		targets = append(targets, DeployTarget{
			AgentID:  t.AgentID,
			Hostname: t.Hostname,
		})
	}

	result, derr := s.deployer.Deploy(ctx, job, targets)
	if derr != nil {
		s.log.Warn("patch scheduler: deploy error",
			"job_id", job.ID, "err", derr)
		// Mark job as failed.
		_, _ = workflow.Transition(ctx, TransitionInput{
			Job:           job,
			Event:         EventFail,
			FailureReason: derr.Error(),
		})
		if s.store != nil {
			_ = s.store.UpdatePatchJob(ctx, job)
		}
		return
	}

	// Determine terminal event.
	if result.Aborted {
		_, _ = workflow.Transition(ctx, TransitionInput{
			Job:           job,
			Event:         EventFail,
			FailureReason: result.AbortReason,
		})
	} else if result.Failed > 0 && result.Succeeded == 0 {
		_, _ = workflow.Transition(ctx, TransitionInput{
			Job:           job,
			Event:         EventFail,
			FailureReason: fmt.Sprintf("%d/%d targets failed", result.Failed, result.Total),
		})
	} else {
		_, _ = workflow.Transition(ctx, TransitionInput{
			Job:   job,
			Event: EventComplete,
		})
	}
	if s.store != nil {
		if err := s.store.UpdatePatchJob(ctx, job); err != nil {
			s.log.Warn("patch scheduler: persist terminal state failed",
				"job_id", job.ID, "err", err)
		}
	}
	s.log.Info("patch scheduler: job done",
		"job_id", job.ID,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"aborted", result.Aborted,
	)
}
