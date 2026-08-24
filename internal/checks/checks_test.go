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
