package patches

import (
	"log/slog"
	"sync"
	"time"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Default scheduler values.
const (
	DefaultMaxConcurrency        = 10
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
	Start     time.Time
	End       time.Time
	Reason    string
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
	Start     time.Time
	End       time.Time
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
	cfg      PatchSchedulerConfig
	deployer *PatchDeployer
	store    Store
	log      *slog.Logger

	mu        sync.Mutex
	queue     []*QueuedJob
	active    map[string]bool       // jobID -> running
	deferred  map[string]*QueuedJob // jobs waiting for blackout
	agentBusy map[string]string     // agentID -> jobID currently using it
	closed    bool
	notify    chan struct{}
}

// NewPatchScheduler creates a scheduler. The deployer is used to
// actually run the jobs. The store is used to persist job state
// updates (e.g. state transitions).
