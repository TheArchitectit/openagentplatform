package checks

import (
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// --- severityOf tests ---

func TestSeverityOf(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{StatusOK, SeverityOK},
		{"OK", SeverityOK},
		{StatusWarn, SeverityWarn},
		{StatusCrit, SeverityCrit},
		{StatusError, SeverityCrit},
		{"unknown", SeverityCrit},
		{"", SeverityCrit},
		{"WARNING", SeverityCrit}, // only exact match counts
	}
	for _, tt := range tests {
		t.Run("status="+tt.status, func(t *testing.T) {
			got := severityOf(tt.status)
			if got != tt.want {
				t.Errorf("severityOf(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// --- ThresholdConfig.withDefaults tests ---

func TestWithDefaults(t *testing.T) {
	cfg := ThresholdConfig{}.withDefaults()
	if cfg.ConsecutiveFailures != DefaultConsecutiveFailures {
		t.Errorf("ConsecutiveFailures = %d, want %d", cfg.ConsecutiveFailures, DefaultConsecutiveFailures)
	}
	if cfg.LookbackWindow != DefaultLookbackWindow {
		t.Errorf("LookbackWindow = %v, want %v", cfg.LookbackWindow, DefaultLookbackWindow)
	}
	if cfg.FlapIntervals != DefaultFlapIntervals {
		t.Errorf("FlapIntervals = %d, want %d", cfg.FlapIntervals, DefaultFlapIntervals)
	}
	if cfg.Now == nil {
		t.Error("Now should be set to time.Now by default")
	}
}

func TestWithDefaults_PreservesExplicitValues(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	cfg := ThresholdConfig{
		ConsecutiveFailures: 5,
		LookbackWindow:      10 * time.Minute,
		FlapIntervals:       4,
		Now:                 now,
	}.withDefaults()
	if cfg.ConsecutiveFailures != 5 {
		t.Errorf("ConsecutiveFailures = %d, want 5", cfg.ConsecutiveFailures)
	}
	if cfg.LookbackWindow != 10*time.Minute {
		t.Errorf("LookbackWindow = %v, want 10m", cfg.LookbackWindow)
	}
	if cfg.FlapIntervals != 4 {
		t.Errorf("FlapIntervals = %d, want 4", cfg.FlapIntervals)
	}
	if cfg.Now == nil {
		t.Error("Now should preserve the explicit function")
	}
}

// --- ThresholdEvaluator tests ---

func fixedTime(t time.Time) ThresholdConfig {
	return ThresholdConfig{
		ConsecutiveFailures: 3,
		LookbackWindow:      5 * time.Minute,
		FlapIntervals:       2,
		Now:                 func() time.Time { return t },
	}
}

func makeResult(agentID, checkID, status string, ts time.Time) *models.CheckResult {
	return &models.CheckResult{
		AgentID:   agentID,
		CheckID:   checkID,
		Status:    status,
		Timestamp: ts,
	}
}

func TestEvaluator_NilResult(t *testing.T) {
	now := time.Now()
	eval := NewThresholdEvaluator(fixedTime(now)).Evaluate(nil, nil, nil)
	if eval.AlertNeeded {
		t.Error("nil result should not trigger alert")
	}
	if eval.Reason != "nil result" {
		t.Errorf("reason = %q, want %q", eval.Reason, "nil result")
	}
}

func TestEvaluator_OKResult_NoAlert(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))
	result := makeResult("a1", "c1", StatusOK, now)
	eval := ev.Evaluate(result, nil, nil)
	if eval.AlertNeeded {
		t.Error("OK result should not trigger alert")
	}
	if eval.Severity != SeverityOK {
		t.Errorf("severity = %q, want %q", eval.Severity, SeverityOK)
	}
}

func TestEvaluator_BelowThreshold(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	// Only 2 consecutive failures (below threshold of 3)
	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusCrit, now)
	eval := ev.Evaluate(result, nil, prev)
	if eval.AlertNeeded {
		t.Error("should not alert with only 2 consecutive failures (1 prev + 1 current = 2)")
	}
	if eval.Reason != "below consecutive threshold" {
		t.Errorf("reason = %q, want %q", eval.Reason, "below consecutive threshold")
	}
}

func TestEvaluator_ThresholdExceeded(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	// 3 consecutive failures (threshold = 3): 2 prev + 1 current
	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusCrit, now)
	eval := ev.Evaluate(result, nil, prev)
	if !eval.AlertNeeded {
		t.Error("should alert with 3 consecutive failures")
	}
	if eval.Severity != SeverityCrit {
		t.Errorf("severity = %q, want %q", eval.Severity, SeverityCrit)
	}
	if eval.Reason != "threshold exceeded" {
		t.Errorf("reason = %q, want %q", eval.Reason, "threshold exceeded")
	}
}

func TestEvaluator_WarnSeverity(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusWarn, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusWarn, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusWarn, now)
	eval := ev.Evaluate(result, nil, prev)
	if !eval.AlertNeeded {
		t.Error("should alert on warn with enough consecutive failures")
	}
	if eval.Severity != SeverityWarn {
		t.Errorf("severity = %q, want %q", eval.Severity, SeverityWarn)
	}
}

func TestEvaluator_ErrorMapsToCrit(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusError, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusError, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusError, now)
	eval := ev.Evaluate(result, nil, prev)
	if !eval.AlertNeeded {
		t.Error("should alert on error with enough consecutive failures")
	}
	if eval.Severity != SeverityCrit {
		t.Errorf("error status should map to crit severity, got %q", eval.Severity)
	}
}

func TestEvaluator_AlreadyFiring_NoReFire(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusCrit, now)

	// First evaluation fires.
	eval1 := ev.Evaluate(result, nil, prev)
	if !eval1.AlertNeeded {
		t.Fatal("first evaluation should fire")
	}

	// Second evaluation at the same time should NOT re-fire.
	eval2 := ev.Evaluate(result, nil, prev)
	if eval2.AlertNeeded {
		t.Error("should not re-fire when already firing")
	}
	if eval2.Reason != "alert already firing" {
		t.Errorf("reason = %q, want %q", eval2.Reason, "alert already firing")
	}
}

func TestEvaluator_FlappingSuppression(t *testing.T) {
	flapSecs := int64(10)
	interval := time.Duration(flapSecs) * time.Second
	now := time.Now()

	// Use a mutable clock so we can advance time between evaluations.
	clock := now
	cfg := ThresholdConfig{
		ConsecutiveFailures: 3,
		LookbackWindow:      10 * time.Minute,
		FlapIntervals:       DefaultFlapIntervals,
		Now:                 func() time.Time { return clock },
	}
	ev := NewThresholdEvaluator(cfg)

	checkDef := &models.CheckDefinition{IntervalSeconds: int(flapSecs)}
	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * interval)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * interval)},
	}

	// 1. Fire alert.
	eval1 := ev.Evaluate(makeResult("a1", "c1", StatusCrit, now), checkDef, prev)
	if !eval1.AlertNeeded {
		t.Fatal("first fire should be needed")
	}

	// 2. Clear within flap window. recordClear uses cfg.Now(), so advance clock.
	clearTime := now.Add(interval)
	clock = clearTime
	ev.Evaluate(makeResult("a1", "c1", StatusOK, clearTime), checkDef, nil)

	// 3. Re-fire within flap window -> suppressed.
	now2 := clearTime.Add(2 * time.Second) // still within flapWindow
	clock = now2

	prevAfterClear := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * interval)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * interval)},
	}
	eval3 := ev.Evaluate(makeResult("a1", "c1", StatusCrit, now2), checkDef, prevAfterClear)
	if eval3.AlertNeeded {
		t.Error("should suppress flapping alert")
	}
	if !eval3.Suppressed {
		t.Error("Suppressed should be true for flapping detection")
	}
}

func TestEvaluator_RecoveryResetsState(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}

	// Fire.
	eval1 := ev.Evaluate(makeResult("a1", "c1", StatusCrit, now), nil, prev)
	if !eval1.AlertNeeded {
		t.Fatal("should fire")
	}

	// Clear after flap window (set clock forward past flap window).
	later := now.Add(DefaultLookbackWindow + time.Minute)
	ev.cfg.Now = func() time.Time { return later }
	ev.Evaluate(makeResult("a1", "c1", StatusOK, later), nil, nil)

	// New fire after recovery should be allowed.
	evenLater := later.Add(time.Minute)
	ev.cfg.Now = func() time.Time { return evenLater }
	prev2 := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: evenLater.Add(-3 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: evenLater.Add(-2 * time.Minute)},
	}
	eval2 := ev.Evaluate(makeResult("a1", "c1", StatusCrit, evenLater), nil, prev2)
	if !eval2.AlertNeeded {
		t.Error("should re-fire after full recovery (clear beyond flap window)")
	}
}

func TestEvaluator_LookbackWindowExcludesOld(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(ThresholdConfig{
		ConsecutiveFailures: 3,
		LookbackWindow:      2 * time.Minute,
		FlapIntervals:       DefaultFlapIntervals,
		Now:                 func() time.Time { return now },
	})

	// 3 previous failures, but the first is outside the 2-minute lookback.
	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-10 * time.Minute)}, // outside
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-3 * time.Minute)},  // outside
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-1 * time.Minute)},  // inside
	}
	result := makeResult("a1", "c1", StatusCrit, now)
	eval := ev.Evaluate(result, nil, prev)
	// Only 2 consecutive (1 prev inside window + 1 current) -> below threshold of 3
	if eval.AlertNeeded {
		t.Error("old results outside lookback should not count toward threshold")
	}
}

func TestEvaluator_OKBetweenFailures_ResetsConsecutive(t *testing.T) {
	now := time.Now()
	ev := NewThresholdEvaluator(fixedTime(now))

	prev := []models.CheckResult{
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-4 * time.Minute)},
		{AgentID: "a1", CheckID: "c1", Status: StatusOK, Timestamp: now.Add(-3 * time.Minute)}, // OK resets
		{AgentID: "a1", CheckID: "c1", Status: StatusCrit, Timestamp: now.Add(-2 * time.Minute)},
	}
	result := makeResult("a1", "c1", StatusCrit, now)
	eval := ev.Evaluate(result, nil, prev)
	// Only 2 consecutive (1 prev after OK + 1 current) -> below threshold
	if eval.AlertNeeded {
		t.Error("OK in the middle should break the consecutive failure chain")
	}
}

// --- flapWindow tests ---

func TestFlapWindow_WithCheckDef(t *testing.T) {
	ev := NewThresholdEvaluator(ThresholdConfig{FlapIntervals: 3})
	def := &models.CheckDefinition{IntervalSeconds: 10}
	got := ev.flapWindow(def, ev.cfg)
	want := 30 * time.Second
	if got != want {
		t.Errorf("flapWindow = %v, want %v", got, want)
	}
}

func TestFlapWindow_NilCheckDef(t *testing.T) {
	cfg := ThresholdConfig{LookbackWindow: 5 * time.Minute}
	ev := NewThresholdEvaluator(cfg)
	got := ev.flapWindow(nil, ev.cfg)
	if got != cfg.LookbackWindow {
		t.Errorf("flapWindow = %v, want %v", got, cfg.LookbackWindow)
	}
}

// --- stateKey test ---

func TestStateKey(t *testing.T) {
	k := stateKey("agent1", "check1")
	if k != "agent1\x00check1" {
		t.Errorf("stateKey = %q, want %q", k, "agent1\x00check1")
	}
}
