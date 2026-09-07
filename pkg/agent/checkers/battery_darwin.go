//go:build darwin

package checkers

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func readBatteryOS(_ context.Context, _ string) (*batteryResult, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return nil, fmt.Errorf("battery: pmset: %w", err)
	}

	result := &batteryResult{Status: "unknown"}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Battery") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "discharging") {
			result.Status = "discharging"
		} else if strings.Contains(lower, "charging") {
			result.Status = "charging"
		} else if strings.Contains(lower, "charged") {
			result.Status = "full"
		}
		if i := strings.Index(line, "%"); i > 0 {
			before := strings.TrimSpace(line[maxInt(0, i-4):i])
			fields := strings.Fields(before)
			if len(fields) > 0 {
				if pct, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
					result.Percent = pct
				}
			}
		}
	}

	if cycleOut, err := exec.Command("system_profiler", "SPPowerDataType").Output(); err == nil {
		for _, line := range strings.Split(string(cycleOut), "\n") {
			if strings.Contains(line, "Cycle Count:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					if c, err := strconv.Atoi(fields[2]); err == nil {
						result.CycleCount = c
					}
				}
			}
		}
	}
	return result, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
