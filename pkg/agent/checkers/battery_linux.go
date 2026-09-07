//go:build linux

package checkers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func readBatteryOS(_ context.Context, _ string) (*batteryResult, error) {
	const path = "/sys/class/power_supply/BAT0/uevent"
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("battery: open %s: %w", path, err)
	}
	defer f.Close()

	result := &batteryResult{Status: "unknown"}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "POWER_SUPPLY_STATUS":
			lower := strings.ToLower(v)
			prev := result.Status
			result.Status = lower
			if lower == "discharging" && prev != "" && prev != "discharging" {
				result.PreviousStatus = prev
				result.PowerTransition = "discharging"
			} else if lower == "charging" && prev == "discharging" {
				result.PreviousStatus = prev
				result.PowerTransition = "charging"
			} else if lower == "full" && prev == "charging" {
				result.PreviousStatus = prev
				result.PowerTransition = "full"
			}
		case "POWER_SUPPLY_CAPACITY":
			if pct, err := strconv.Atoi(v); err == nil {
				result.Percent = pct
			}
		case "POWER_SUPPLY_TIME_TO_EMPTY_NOW":
			if t, err := strconv.Atoi(v); err == nil {
				result.TimeRemainingMin = t / 60
			}
		case "POWER_SUPPLY_ENERGY_FULL_DESIGN":
			if design, err := strconv.Atoi(v); err == nil && design > 0 {
				if cur, err := readIntFile("/sys/class/power_supply/BAT0/charge_now"); err == nil {
					result.HealthPercent = cur * 100 / design
				}
			}
		}
	}
	return result, scanner.Err()
}

func readIntFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var n int
	_, err = fmt.Fscanf(f, "%d", &n)
	return n, err
}
