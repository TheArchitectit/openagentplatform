package checks

import (
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Severity mirrors the alert severity taxonomy used by models.Alert.
const (
	SeverityInfo = "info"
	SeverityWarn = "warn"
	SeverityCrit = "crit"
	SeverityOK   = "ok"
)

// Check status string constants used by the evaluator. These are the same
// values agents publish in CheckResult.Status.
const (
	StatusOK    = "ok"
	StatusWarn  = "warn"
	StatusCrit  = "crit"
	StatusError = "error"
)

// DefaultConsecutiveFailures is the number of consecutive non-OK results
// required to fire an alert. The value is intentionally low (3) so that
// real outages surface quickly while the flapping detector prevents
// thrash on transient blips.
const DefaultConsecutiveFailures = 3

// DefaultLookbackWindow is the maximum age of results considered by the
// evaluator. Anything older than this is treated as stale and excluded
// from the consecutive-failure count.
const DefaultLookbackWindow = 5 * time.Minute

// DefaultFlapIntervals is the number of check intervals an alert may
// fire-and-clear within before the evaluator suppresses further alerts.
// Flap detection prevents notification storms on checks that oscillate
// rapidly between healthy and unhealthy.
const DefaultFlapIntervals = 2

// ThresholdConfig configures a ThresholdEvaluator. Zero-valued fields are
// replaced with sensible defaults from the package-level constants.
type ThresholdConfig struct {
	// ConsecutiveFailures is the number of consecutive non-OK results
	// (crit, warn, error) required to fire an alert.
	ConsecutiveFailures int
	// LookbackWindow is the maximum age of results to consider when
	// counting consecutive failures. Older results are ignored.
	LookbackWindow time.Duration
	// FlapIntervals is the maximum number of check intervals within
	// which fire+clear may happen before suppression kicks in. The
	// effective flap window is FlapIntervals * checkInterval.
	FlapIntervals int
	// Now is the time-source used by the evaluator. Tests inject a
	// fixed clock here. Defaults to time.Now if nil.
	Now func() time.Time
}

// withDefaults returns a copy of cfg with zero-valued fields replaced by
// the package-level defaults.
func (c ThresholdConfig) withDefaults() ThresholdConfig {
	if c.ConsecutiveFailures <= 0 {
		c.ConsecutiveFailures = DefaultConsecutiveFailures
	}
	if c.LookbackWindow <= 0 {
		c.LookbackWindow = DefaultLookbackWindow
	}
	if c.FlapIntervals <= 0 {
		c.FlapIntervals = DefaultFlapIntervals
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// alertState tracks the last fired / cleared time for a single
// (agent_id, check_id) pair. The flapping detector uses the timestamps to
// decide whether to suppress a new alert.
type alertState struct {
	lastFiredAt  time.Time
	lastClearedAt time.Time
	// fireCount is the number of fires within the current flap window.
	// Reset when a stable (non-flapping) period elapses.
	fireCount int
}

// ThresholdEvaluator determines whether a new check result should produce
// an alert. It is safe for concurrent use; per-check state is guarded by
// a single mutex.
//
// The evaluator is intentionally stateful: it tracks the most recent
// fired/cleared timestamps per (agent_id, check_id) pair so it can
// suppress flapping alerts. The state is held in memory; restarts reset
// the counters, which is acceptable for a best-effort flap detector.
type ThresholdEvaluator struct {
	cfg    ThresholdConfig
	mu     sync.Mutex
	states map[string]*alertState
}

// NewThresholdEvaluator constructs an evaluator with the given config.
// Zero-valued fields are replaced with defaults.
func NewThresholdEvaluator(cfg ThresholdConfig) *ThresholdEvaluator {
	return &ThresholdEvaluator{
		cfg:    cfg.withDefaults(),
		states: make(map[string]*alertState),
	}
}

// Evaluation is the result of Evaluate. AlertNeeded is true if the new
// result should produce an alert. Severity is the alert's severity
// (warn / crit) when AlertNeeded is true, or StatusOK when it is not.
type Evaluation struct {
	AlertNeeded bool
	Severity    string
	// Suppressed is true when the alert would otherwise have fired but
	// was suppressed by the flapping detector. Callers may use this to
	// log a debug event without delivering a notification.
	Suppressed bool
	// Reason is a short, human-readable explanation of the decision.
	// Useful for logging and debugging.
	Reason string
}

// stateKey returns the map key for a (agent_id, check_id) pair.
func stateKey(agentID, checkID string) string {
	return agentID + "\x00" + checkID
}

// Evaluate decides whether `result` should produce an alert given the
// check definition and the previous N results for the same
// (agent_id, check_id) pair. The previousResults slice is ordered from
// oldest to newest; the caller is responsible for filtering to the
// lookback window before calling. Results older than the lookback window
// are excluded by this function as a safety net.
//
// checkDef may be nil; when nil, the evaluator applies the defaults
// (ConsecutiveFailures, LookbackWindow) but cannot use
// checkDef.IntervalSeconds for the flapping window. The caller should
// pass a non-nil definition whenever the check is known to the platform.
func (e *ThresholdEvaluator) Evaluate(result *models.CheckResult, checkDef *models.CheckDefinition, previousResults []models.CheckResult) Evaluation {
	if result == nil {
		return Evaluation{Reason: "nil result"}
	}

	cfg := e.cfg
	now := cfg.Now()
	severity := severityOf(result.Status)

	// Filter previous results to the lookback window. We treat the
	// current result's timestamp as the reference; if it's zero we
	// fall back to now.
	ref := result.Timestamp
	if ref.IsZero() {
		ref = now
	}
	cutoff := ref.Add(-cfg.LookbackWindow)

	// Count consecutive non-OK results ending at the current one. We walk
	// backwards through previousResults until we find an OK result or
	// hit the lookback window.
	consecutive := 0
	if severity != SeverityOK {
		consecutive = 1 // the current result itself
	}
	for i := len(previousResults) - 1; i >= 0; i-- {
		pr := previousResults[i]
		if pr.Timestamp.Before(cutoff) {
			break
		}
		if severityOf(pr.Status) == SeverityOK {
			break
		}
		consecutive++
	}

	if severity == SeverityOK {
		// The result is healthy. If we have a recorded fire time, record
		// the clear and check for flapping.
		e.recordClear(result.AgentID, result.CheckID, now, checkDef, cfg)
		return Evaluation{
			AlertNeeded: false,
			Severity:    SeverityOK,
			Reason:      "result is ok",
		}
	}

	if consecutive < cfg.ConsecutiveFailures {
		return Evaluation{
			AlertNeeded: false,
			Severity:    severity,
			Reason:      "below consecutive threshold",
		}
	}

	// Non-OK and we've crossed the consecutive-failure threshold. Decide
	// whether to fire or suppress.
	e.mu.Lock()
	defer e.mu.Unlock()

	key := stateKey(result.AgentID, result.CheckID)
	st, ok := e.states[key]
	if !ok {
		st = &alertState{}
		e.states[key] = st
	}

	flapWindow := e.flapWindow(checkDef, cfg)
	if !st.lastFiredAt.IsZero() && now.Sub(st.lastFiredAt) <= flapWindow {
		// We've already fired within the flap window. If the check
		// also cleared within that window, treat this as a flap and
		// suppress.
		if !st.lastClearedAt.IsZero() && st.lastClearedAt.After(st.lastFiredAt) {
			return Evaluation{
				AlertNeeded: false,
				Severity:    severity,
				Suppressed:  true,
				Reason:      "flapping: fire+clear within flap window",
			}
		}
		// Already firing, no clear in between: treat as a sustained
		// condition and don't re-fire. The first fire is enough.
		return Evaluation{
			AlertNeeded: false,
			Severity:    severity,
			Reason:      "alert already firing",
		}
	}

	st.lastFiredAt = now
	st.fireCount++
	return Evaluation{
		AlertNeeded: true,
		Severity:    severity,
		Reason:      "threshold exceeded",
	}
}

// recordClear is called when an OK result arrives. It updates the
// per-check alert state with the clear time so the flapping detector can
// notice a fire-then-clear-within-window sequence.
func (e *ThresholdEvaluator) recordClear(agentID, checkID string, now time.Time, checkDef *models.CheckDefinition, cfg ThresholdConfig) {
	flapWindow := e.flapWindow(checkDef, cfg)
	e.mu.Lock()
	defer e.mu.Unlock()
	key := stateKey(agentID, checkID)
	st, ok := e.states[key]
	if !ok {
		return
	}
	// Only record a clear if we've previously fired and the clear falls
	// inside the flap window. A clear after the window is just a normal
	// recovery, not a flap.
	if !st.lastFiredAt.IsZero() && now.Sub(st.lastFiredAt) <= flapWindow {
		st.lastClearedAt = now
	}
	// If the clear happens after the flap window, reset state so the
	// next fire is treated as fresh.
	if !st.lastFiredAt.IsZero() && now.Sub(st.lastFiredAt) > flapWindow {
		st.lastFiredAt = time.Time{}
		st.lastClearedAt = time.Time{}
		st.fireCount = 0
	}
}

// flapWindow returns the duration used by the flapping detector. When
// the check definition has a non-zero IntervalSeconds, the window is
// FlapIntervals * interval. Otherwise the window is the lookback window
// (best-effort fallback so suppression still works for ad-hoc checks).
func (e *ThresholdEvaluator) flapWindow(checkDef *models.CheckDefinition, cfg ThresholdConfig) time.Duration {
	if checkDef != nil && checkDef.IntervalSeconds > 0 {
		return time.Duration(checkDef.IntervalSeconds) * time.Second * time.Duration(cfg.FlapIntervals)
	}
	return cfg.LookbackWindow
}

// severityOf maps a check status string to an alert severity. Anything
// not recognised as non-OK is treated as SeverityOK.
func severityOf(status string) string {
	switch status {
	case StatusOK, "OK":
		return SeverityOK
	case StatusWarn:
		return SeverityWarn
	case StatusCrit:
		return SeverityCrit
	case StatusError:
		// Errors are treated as critical for alerting purposes.
		return SeverityCrit
	default:
		return SeverityOK
	}
}
