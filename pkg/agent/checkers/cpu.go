package checkers

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// CPUChecker reports current CPU utilization percent.
type CPUChecker struct{}

func (c *CPUChecker) Name() string { return "cpu" }

func (c *CPUChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	interval := 1 * time.Second
	if v, ok := req.Options["interval_sec"].(float64); ok && v > 0 {
		interval = time.Duration(v) * time.Second
	}
	start := time.Now()
	percents, err := cpu.PercentWithContext(ctx, interval, false)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	if len(percents) == 0 {
		return &Result{OK: false, Error: "no cpu samples", Duration: time.Since(start).Milliseconds()}
	}
	p := percents[0]
	threshold := 0.0
	if v, ok := req.Options["threshold"].(float64); ok {
		threshold = v
	}
	ok := threshold == 0 || p <= threshold
	return &Result{
		OK:       ok,
		Status:   "ok",
		Value:    map[string]interface{}{"cpu_percent": p, "threshold": threshold},
		Duration: time.Since(start).Milliseconds(),
	}
}
