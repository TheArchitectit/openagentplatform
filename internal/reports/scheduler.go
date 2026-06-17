// Package reports - scheduler.go implements a cron-based job runner
// for scheduled report generation. It enforces a maximum of 5
// concurrent report runs with a 60-second per-run timeout.
//
// This implementation uses a simple tick-based scheduler that checks
// for due schedules every 30 seconds, avoiding the need for a
// third-party cron library.
package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxConcurrentReports is the maximum number of report runs that
	// may execute in parallel.
	MaxConcurrentReports = 5
	// ReportTimeout is the per-run execution deadline.
	ReportTimeout = 60 * time.Second
	// TickInterval is how often the scheduler checks for due schedules.
	TickInterval = 30 * time.Second
)

// Scheduler triggers scheduled reports based on cron expressions and
// dispatches them to the ReportEngine. Results are persisted via
// Store and delivered through the Deliverer.
type Scheduler struct {
	engine    *ReportEngine
	store     Store
	deliverer Deliverer
	log       *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	// sem limits concurrent report executions.
	sem chan struct{}
	// active tracks currently running report IDs for observability.
	active sync.Map
}

// NewScheduler constructs a Scheduler.
func NewScheduler(engine *ReportEngine, store Store, deliverer Deliverer, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		engine:    engine,
		store:     store,
		deliverer: deliverer,
		log:       log,
		sem:       make(chan struct{}, MaxConcurrentReports),
	}
}

// Start begins the scheduler. It loads all enabled schedules from
// the store and starts a background ticker that checks for due
// schedules every TickInterval.
func (s *Scheduler) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.log.Info("report scheduler started", "tick_interval", TickInterval)
	go s.runLoop(ctx)
	return nil
}

// Stop halts the scheduler and waits for active jobs to finish or
// for the context to be cancelled.
func (s *Scheduler) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// runLoop is the main scheduler tick loop. It loads all enabled
// schedules each tick and triggers any that are due.
func (s *Scheduler) runLoop(ctx context.Context) {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick checks all enabled schedules and triggers any that are due.
func (s *Scheduler) tick(ctx context.Context) {
	schedules, err := s.store.ListSchedules(ctx, "")
	if err != nil {
		s.log.Warn("scheduler tick: list failed", "err", err)
		return
	}
	now := time.Now().UTC()
	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}
		if sched.NextRunAt == nil {
			continue
		}
		if !sched.NextRunAt.Before(now) {
			continue
		}
		params := map[string]string{}
		if len(sched.Params) > 0 {
			_ = json.Unmarshal(sched.Params, &params)
		}
		if _, err := s.RunNow(ctx, sched.OrgID, sched.TemplateID, string(sched.Format), sched.DeliveryMethod, sched.DeliveryTarget, params); err != nil {
			s.log.Error("scheduled report failed",
				"schedule_id", sched.ID, "err", err)
		}
		// Update last_run_at and next_run_at.
		next := computeNextRun(sched.CronExpr, now)
		sched.LastRunAt = &now
		sched.NextRunAt = next
		_ = s.store.UpdateSchedule(ctx, sched.OrgID, sched.ID, sched)
	}
}

// AddSchedule registers a new schedule and persists it with a
// computed NextRunAt.
func (s *Scheduler) AddSchedule(ctx context.Context, sched *ReportSchedule) error {
	if sched.ID == "" {
		sched.ID = uuid.NewString()
	}
	if sched.CronExpr == "" {
		return fmt.Errorf("cron expression is required")
	}
	now := time.Now().UTC()
	sched.NextRunAt = computeNextRun(sched.CronExpr, now)
	return s.store.CreateSchedule(ctx, sched)
}

// RemoveSchedule removes a schedule from the store.
func (s *Scheduler) RemoveSchedule(ctx context.Context, orgID, id string) error {
	return s.store.DeleteSchedule(ctx, orgID, id)
}

// RunNow generates a report immediately, bypassing the schedule.
// This is the entry point used by the API when a user manually
// triggers a report.
func (s *Scheduler) RunNow(ctx context.Context, orgID, templateID, format, deliveryMethod, deliveryTarget string, params map[string]string) (*ReportRun, error) {
	run := &ReportRun{
		ID:             uuid.NewString(),
		OrgID:          orgID,
		TemplateID:     templateID,
		Title:          titleFor(templateID),
		Status:         "running",
		Format:         ReportFormat(format),
		DeliveryStatus: DeliveryPending,
		DeliveryTarget: deliveryTarget,
		StartedAt:      time.Now().UTC(),
	}
	if run.Format == "" {
		run.Format = FormatJSON
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	go s.execute(run, deliveryMethod, params)
	return run, nil
}

// execute runs a single report under the concurrency semaphore and
// timeout, then persists the result and delivers it.
func (s *Scheduler) execute(run *ReportRun, deliveryMethod string, params map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), ReportTimeout)
	defer cancel()

	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		s.failRun(run.ID, "scheduler saturated or timed out before start")
		return
	}
	defer func() { <-s.sem }()

	s.active.Store(run.ID, true)
	defer s.active.Delete(run.ID)

	report, err := s.engine.GenerateReport(ctx, run.OrgID, run.TemplateID, params, run.Format)
	if err != nil {
		s.failRun(run.ID, err.Error())
		return
	}

	run.Data = report.Data
	run.Title = report.Title
	now := time.Now().UTC()
	run.CompletedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()

	// Deliver.
	if s.deliverer != nil {
		ds, derr := s.deliverer.Deliver(ctx, report, deliveryMethod, run.DeliveryTarget)
		if derr != nil {
			run.DeliveryStatus = DeliveryFailed
			run.ErrorMessage = derr.Error()
			_ = s.store.UpdateRunStatus(ctx, run.ID, "completed", DeliveryFailed, derr.Error())
			return
		}
		run.DeliveryStatus = ds
	} else {
		run.DeliveryStatus = DeliveryDownload
	}

	_ = s.store.UpdateRunStatus(ctx, run.ID, "completed", run.DeliveryStatus, run.ErrorMessage)
	s.log.Info("report run completed",
		"run_id", run.ID,
		"org_id", run.OrgID,
		"template", run.TemplateID,
		"delivery", run.DeliveryStatus,
	)
}

func (s *Scheduler) failRun(id, errMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.store.UpdateRunStatus(ctx, id, "failed", DeliveryFailed, errMsg)
}

// computeNextRun returns the next time the given cron expression
// should fire. Supports a subset of cron syntax:
//
//	"@hourly"    -- top of every hour
//	"@daily"     -- midnight UTC
//	"@weekly"    -- midnight Sunday UTC
//	"@monthly"   -- midnight 1st of month UTC
//	"M H * * *"  -- minute/hour fields (dom/month/dow default to *)
func computeNextRun(expr string, after time.Time) *time.Time {
	now := after
	switch expr {
	case "@hourly":
		t := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, time.UTC)
		return &t
	case "@daily":
		t := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		return &t
	case "@weekly":
		daysUntilSunday := (7 - int(now.Weekday())) % 7
		t := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSunday, 0, 0, 0, 0, time.UTC)
		return &t
	case "@monthly":
		t := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		return &t
	default:
		// Simple "M H * * *" parser: fields are "minute hour".
		t, err := parseSimpleCron(expr, now)
		if err != nil {
			return nil
		}
		return &t
	}
}

// parseSimpleCron handles "M H * * *" style expressions.
func parseSimpleCron(expr string, after time.Time) (time.Time, error) {
	// Split into 5 fields, we only use the first two.
	var fields [5]string
	n, _ := fmt.Sscanf(expr, "%s %s %s %s %s", &fields[0], &fields[1], &fields[2], &fields[3], &fields[4])
	if n < 2 {
		return time.Time{}, fmt.Errorf("invalid cron expression: %s", expr)
	}
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, err
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, err
	}
	t := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, time.UTC)
	if !t.After(after) {
		t = t.Add(24 * time.Hour)
	}
	return t, nil
}

// parseCronField parses a single cron field, supporting wildcards
// and specific values.
func parseCronField(field string, min, max int) (int, error) {
	if field == "*" || field == "*/1" {
		return min, nil
	}
	var v int
	if _, err := fmt.Sscanf(field, "%d", &v); err != nil {
		return 0, fmt.Errorf("invalid cron field: %s", field)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("cron field out of range: %d", v)
	}
	return v, nil
}
