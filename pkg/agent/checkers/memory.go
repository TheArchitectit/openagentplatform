package checkers

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryChecker reports current memory utilization percent.
type MemoryChecker struct{}

func (m *MemoryChecker) Name() string { return "memory" }

func (m *MemoryChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	start := time.Now()
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	threshold := 0.0
	if t, ok := req.Options["threshold"].(float64); ok {
		threshold = t
	}
	ok := threshold == 0 || v.UsedPercent <= threshold
	return &Result{
		OK:     ok,
		Status: "ok",
		Value: map[string]interface{}{
			"mem_percent":   v.UsedPercent,
			"total":         v.Total,
			"used":          v.Used,
			"available":     v.Available,
			"threshold":     threshold,
		},
		Duration: time.Since(start).Milliseconds(),
	}
}
