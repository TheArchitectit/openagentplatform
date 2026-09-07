package models

import "testing"

func TestPowerEventTypeConstants(t *testing.T) {
	cases := map[PowerEventType]string{
		PowerEventOnBattery:   "on_battery",
		PowerEventOnLine:      "on_line",
		PowerEventLowBattery:  "low_battery",
		PowerEventBatteryCrit: "battery_critical",
		PowerEventCharging:    "charging",
		PowerEventDischarging: "discharging",
		PowerEventFull:        "full",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("PowerEventType = %q, want %q", got, want)
		}
	}
}

func TestPowerSourceConstants(t *testing.T) {
	if PowerSourceUPS != "ups" {
		t.Errorf("PowerSourceUPS = %q, want ups", PowerSourceUPS)
	}
	if PowerSourceBattery != "battery" {
		t.Errorf("PowerSourceBattery = %q, want battery", PowerSourceBattery)
	}
}

func TestPowerStateLogBatteryPercent(t *testing.T) {
	pct := 75
	log := PowerStateLog{
		OrgID:          "org1",
		AgentID:        "agent-1",
		Source:         PowerSourceUPS,
		EventType:      PowerEventOnBattery,
		CurrentStatus:  "OB",
		BatteryPercent: &pct,
	}
	if log.BatteryPercent == nil || *log.BatteryPercent != 75 {
		t.Errorf("BatteryPercent = %v, want pointer to 75", log.BatteryPercent)
	}
}
