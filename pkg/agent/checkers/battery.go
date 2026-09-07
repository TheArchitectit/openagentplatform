// Battery health check — reads local battery telemetry from the host
// OS. Cross-platform: Linux reads /sys/class/power_supply, macOS runs
// pmset/system_profiler, Windows queries WMI. The check reports
// percent, charging state, and time-remaining, plus a power_transition
// marker when the state changes (Discharging→Charging, etc.).
package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
)

const CheckTypeBattery = "battery"

type BatteryChecker struct {
	platform string
}

func NewBatteryChecker(platform string) *BatteryChecker {
	if platform == "" {
		platform = runtime.GOOS
	}
	return &BatteryChecker{platform: platform}
}

func (c *BatteryChecker) Name() string { return CheckTypeBattery }

func (c *BatteryChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:               CheckTypeBattery,
		Version:            "1.0.0",
		Description:        "Agent-side battery health: percent, charging state, time remaining, power transition",
		SupportedPlatforms: []string{"linux", "darwin", "windows"},
	}
}

type batteryConfig struct {
	WarnThreshold int `json:"warn_threshold"`
	FailThreshold int `json:"fail_threshold"`
}

type batteryResult struct {
	Status           string `json:"status"`
	Percent          int    `json:"percent"`
	TimeRemainingMin int    `json:"time_remaining_min"`
	CycleCount       int    `json:"cycle_count,omitempty"`
	HealthPercent    int    `json:"health_percent,omitempty"`
	PowerTransition  string `json:"power_transition,omitempty"`
	PreviousStatus   string `json:"previous_status,omitempty"`
}

func (c *BatteryChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	var cfg batteryConfig
	if raw, ok := req.Options["config"]; ok {
		if b, ok := raw.(json.RawMessage); ok {
			_ = json.Unmarshal(b, &cfg)
		} else if b, ok := raw.([]byte); ok {
			_ = json.Unmarshal(b, &cfg)
		} else {
			bb, _ := json.Marshal(raw)
			_ = json.Unmarshal(bb, &cfg)
		}
	}
	if cfg.WarnThreshold == 0 {
		cfg.WarnThreshold = 20
	}
	if cfg.FailThreshold == 0 {
		cfg.FailThreshold = 5
	}

	raw, err := readBatteryOS(ctx, c.platform)
	if err != nil {
		return &Result{OK: false, Error: err.Error()}
	}

	status := "ok"
	if raw.Percent < cfg.FailThreshold {
		status = "fail"
	} else if raw.Percent < cfg.WarnThreshold {
		status = "warn"
	}

	data, _ := json.Marshal(raw)
	return &Result{
		OK:      status == "ok",
		Status:  status,
		Message: fmt.Sprintf("battery: %d%%, %s", raw.Percent, raw.Status),
		Value:   json.RawMessage(data),
	}
}
