// UPS SNMP check — queries a UPS daemon via SNMP (v2c or v3) on the
// network and reports battery level, load, input/output voltage, and
// on-battery state. No OAP agent is required on the UPS host itself;
// any agent that can reach the UPS over the network can run this check.
package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

const CheckTypeUPS = "ups_snmp"

type UPSChecker struct{}

func (c *UPSChecker) Name() string { return CheckTypeUPS }

func (c *UPSChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:               CheckTypeUPS,
		Version:            "1.0.0",
		Description:        "SNMP UPS monitoring: battery level, input voltage, load, on-battery state",
		SupportedPlatforms: []string{"any"},
	}
}

type upsConfig struct {
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	Community string   `json:"community"`
	Version   string   `json:"version"`
	OIDs      []string `json:"oids"`
}

type upsResult struct {
	UPSModel       string `json:"ups_model,omitempty"`
	UPSStatus      string `json:"ups_status"`
	BatteryStatus  string `json:"battery_status,omitempty"`
	BatteryPercent int    `json:"battery_percent"`
	LoadPercent    int    `json:"load_percent"`
	InputVoltage   int    `json:"input_voltage"`
	OutputVoltage  int    `json:"output_voltage"`
	RuntimeMinutes int    `json:"runtime_minutes"`
	LastTransfer   string `json:"last_transfer,omitempty"`
	PowerTransition string `json:"power_transition,omitempty"`
	PreviousStatus  string `json:"previous_status,omitempty"`
}

func (c *UPSChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	var cfg upsConfig
	if raw, ok := req.Options["config"]; ok {
		if b, ok := raw.(json.RawMessage); ok {
			if err := json.Unmarshal(b, &cfg); err != nil {
				return &Result{Error: "invalid config: " + err.Error()}
			}
		} else if b, ok := raw.([]byte); ok {
			if err := json.Unmarshal(b, &cfg); err != nil {
				return &Result{Error: "invalid config: " + err.Error()}
			}
		} else {
			b, _ := json.Marshal(raw)
			if err := json.Unmarshal(b, &cfg); err != nil {
				return &Result{Error: "invalid config: " + err.Error()}
			}
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 161
	}
	if cfg.Community == "" {
		cfg.Community = "public"
	}
	if cfg.Version == "" {
		cfg.Version = "2c"
	}
	if len(cfg.OIDs) == 0 {
		cfg.OIDs = standardUPSOIDs()
	}

	snmp := &gosnmp.GoSNMP{
		Target:    cfg.Host,
		Port:      uint16(cfg.Port),
		Community: cfg.Community,
		Version:   snmpVersion(cfg.Version),
		Timeout:   5 * time.Second,
	}
	if err := snmp.Connect(); err != nil {
		return &Result{Status: "error", Message: "snmp connect: " + err.Error()}
	}
	defer snmp.Close()

	resp, err := snmp.Get(cfg.OIDs)
	if err != nil {
		return &Result{Status: "error", Message: "snmp get: " + err.Error()}
	}

	result := parseUPSValues(resp.Variables)
	data, _ := json.Marshal(result)

	status := "ok"
	if result.UPSStatus == "OB" {
		status = "fail"
	} else if result.BatteryPercent < 10 {
		status = "fail"
	} else if result.BatteryPercent < 30 {
		status = "warn"
	}

	return &Result{
		OK:      status == "ok",
		Status:  status,
		Message: fmt.Sprintf("UPS %s: %d%% battery, status %s", result.UPSModel, result.BatteryPercent, result.UPSStatus),
		Value:   json.RawMessage(data),
	}
}

func standardUPSOIDs() []string {
	return []string{
		".1.3.6.1.2.1.33.1.1.2.0",  // upsIdentModel
		".1.3.6.1.2.1.33.1.2.1.0",  // upsBatteryStatus
		".1.3.6.1.2.1.33.1.4.1.0",  // upsOutputStatus
		".1.3.6.1.2.1.33.1.4.4.0",  // upsOutputLoad
		".1.3.6.1.2.1.33.1.4.6.0",  // upsOutputVoltage
		".1.3.6.1.2.1.33.1.3.3.1.3.1", // upsInputVoltage
	}
}

func snmpVersion(v string) gosnmp.SnmpVersion {
	if v == "3" {
		return gosnmp.Version3
	}
	return gosnmp.Version2c
}

func parseUPSValues(vars []gosnmp.SnmpPDU) upsResult {
	r := upsResult{}
	// RFC 1628 status detection:
	//   upsOutputStatus (1.3.6.1.2.1.33.1.4.1) → 1=unknown, 2=onLine,
	//     3=onBattery, 4=boosted, 5=bypassed, 6=reduced, 7=trimmed
	//   upsBatteryStatus (1.3.6.1.2.1.33.1.2.1) → 1=unknown, 2=normal,
	//     3=low, 4=depleted — battery health, NOT line state.
	// Use upsOutputStatus for OL/OB; use upsBatteryStatus only for
	// battery health reporting (LB/RB warnings).
	for _, v := range vars {
		switch v.Name {
		case ".1.3.6.1.2.1.33.1.1.2.0":
			if b, ok := v.Value.([]byte); ok {
				r.UPSModel = string(b)
			}
		case ".1.3.6.1.2.1.33.1.2.1.0":
			if n, ok := v.Value.(int); ok {
				r.BatteryStatus = decodeBatteryStatus(n)
			}
		case ".1.3.6.1.2.1.33.1.4.1.0":
			if n, ok := v.Value.(int); ok {
				switch n {
				case 3: // onBattery
					if r.UPSStatus != "OB" {
						r.PreviousStatus = r.UPSStatus
						r.UPSStatus = "OB"
						r.PowerTransition = "on_battery"
					}
				case 2: // onLine
					if r.UPSStatus == "OB" {
						r.PreviousStatus = "OB"
						r.UPSStatus = "OL"
						r.PowerTransition = "on_line"
					}
				default:
					// Unknown / bypassed / etc — keep prior state, no transition.
				}
			}
		case ".1.3.6.1.2.1.33.1.4.4.0":
			if n, ok := v.Value.(int); ok {
				r.LoadPercent = n
			}
		case ".1.3.6.1.2.1.33.1.4.6.0":
			if n, ok := v.Value.(int); ok {
				r.OutputVoltage = n
			}
		case ".1.3.6.1.2.1.33.1.3.3.1.3.1":
			if n, ok := v.Value.(int); ok {
				r.InputVoltage = n
			}
		}
	}
	return r
}

func decodeBatteryStatus(code int) string {
	switch code {
	case 2:
		return "OK"
	case 3:
		return "LB"
	case 4:
		return "RB"
	default:
		return "unknown"
	}
}

func init() {
	Register(CheckTypeUPS, &UPSChecker{})
}
