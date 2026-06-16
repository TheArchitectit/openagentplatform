package checkers

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskChecker reports disk usage for a path (default "/").
type DiskChecker struct{}

func (d *DiskChecker) Name() string { return "disk" }

// Metadata describes the disk checker.
func (d *DiskChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:        "disk",
		Version:     "1.0.0",
		Description: "Reports disk usage percent for a given path (default \"/\").",
		SupportedPlatforms: []string{
			"linux", "darwin", "freebsd", "windows",
		},
	}
}

func (d *DiskChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	path := "/"
	if p, ok := req.Options["path"].(string); ok && p != "" {
		path = p
	}
	if req.Target != "" {
		path = req.Target
	}
	start := time.Now()
	u, err := disk.UsageWithContext(ctx, path)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	threshold := 0.0
	if t, ok := req.Options["threshold"].(float64); ok {
		threshold = t
	}
	ok := threshold == 0 || u.UsedPercent <= threshold
	return &Result{
		OK:     ok,
		Status: "ok",
		Value: map[string]interface{}{
			"path":       path,
			"disk_percent": u.UsedPercent,
			"total":      u.Total,
			"used":       u.Used,
			"free":       u.Free,
			"threshold":  threshold,
		},
		Duration: time.Since(start).Milliseconds(),
	}
}
