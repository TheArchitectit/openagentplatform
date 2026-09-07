//go:build windows

package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

func readBatteryOS(_ context.Context, _ string) (*batteryResult, error) {
	out, err := exec.Command("powershell", "-Command",
		"Get-WmiObject Win32_Battery | Select-Object BatteryStatus, EstimatedChargeRemaining, EstimatedRunTime | ConvertTo-Json").Output()
	if err != nil {
		return nil, fmt.Errorf("battery: wmi: %w", err)
	}
	var wmi struct {
		BatteryStatus            int `json:"BatteryStatus"`
		EstimatedChargeRemaining int `json:"EstimatedChargeRemaining"`
		EstimatedRunTime         int `json:"EstimatedRunTime"`
	}
	if err := json.Unmarshal(out, &wmi); err != nil {
		return nil, fmt.Errorf("battery: parse wmi: %w", err)
	}
	return &batteryResult{
		Status:           wmiStatusToString(wmi.BatteryStatus),
		Percent:          wmi.EstimatedChargeRemaining,
		TimeRemainingMin: wmi.EstimatedRunTime,
	}, nil
}

func wmiStatusToString(code int) string {
	switch code {
	case 1:
		return "discharging"
	case 2:
		return "ac_power"
	case 3:
		return "full"
	case 4:
		return "low"
	case 5:
		return "critical"
	case 6:
		return "charging"
	case 7:
		return "charging_high"
	case 8:
		return "charging_low"
	case 9:
		return "charging_critical"
	case 10:
		return "undefined"
	case 11:
		return "partially_charged"
	default:
		return "unknown"
	}
}
