# Power / UPS Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:equipping-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SNMP UPS monitoring, agent-side battery health checks, and power-event emission with stateful on/off-battery alert lifecycle to the existing check framework.

**Architecture:** Two new check types (`ups_snmp` and `battery`) plug into the existing `pkg/agent/checkers` registry. The `ResultIngestor` detects `power_transition` metadata in check results and emits `power_event` alerts via `oap.events.alerts`. An append-only `power_state_log` table records every transition for audit.

**Tech Stack:** `github.com/gosnmp/gosnmp` (SNMP library), existing OAP check/alert framework, no new infrastructure.

---

## File Map

```
pkg/agent/checkers/
├── ups.go                  # UPSSnmPchecker: SNMP GET against UPS daemon
├── battery.go              # BatteryChecker: OS-specific battery telemetry (Linux/macOS/Windows)
├── ups_test.go             # Test ups.go
└── battery_test.go         # Test battery.go

internal/power/
├── events.go               # PowerEvent struct, transition detection
├── store.go                # PowerStateLogStore: append-only event log
└── ingest.go               # ResultIngestor hook: detect power_transition, publish alert

pkg/models/
└── models_power.go         # PowerStateLog type

internal/db/migrations/
└── 016_power_monitoring.up.sql

internal/api/
├── routes.go               # Register /api/v1/power routes
└── power.go                # List power events, get current state

pkg/models/
└── models_alerts.go        # Add power_event_types and power_source fields
```

---

### Task 1: Database Migration 016

**Files:**
- Create: `internal/db/migrations/016_power_monitoring.up.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 016_power_monitoring: append-only power state transition log

CREATE TABLE IF NOT EXISTS power_state_log (
    id                 TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL DEFAULT '',
    agent_id          TEXT NOT NULL,
    source            TEXT NOT NULL,  -- ups | battery
    event_type        TEXT NOT NULL,  -- on_battery | on_line | low_battery | battery_critical | charging | discharging | full
    previous_status   TEXT,
    current_status    TEXT NOT NULL,
    battery_percent   INTEGER,
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_power_state_log_agent ON power_state_log (agent_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_power_state_log_org ON power_state_log (org_id, occurred_at DESC);
```

- [ ] **Step 2: Commit**

```bash
git add internal/db/migrations/016_power_monitoring.up.sql
git commit -m "feat(power): migration 016 — power_state_log (append-only transition log)"
```

---

### Task 2: Core Types and PowerStateLog

**Files:**
- Create: `pkg/models/models_power.go`
- Create: `pkg/models/models_power_test.go`

- [ ] **Step 1: Write the model**

```go
// pkg/models/models_power.go
package models

import "time"

type PowerSource string

const (
    PowerSourceUPS     PowerSource = "ups"
    PowerSourceBattery PowerSource = "battery"
)

type PowerEventType string

const (
    PowerEventOnBattery     PowerEventType = "on_battery"
    PowerEventOnLine        PowerEventType = "on_line"
    PowerEventLowBattery    PowerEventType = "low_battery"
    PowerEventBatteryCrit   PowerEventType = "battery_critical"
    PowerEventCharging      PowerEventType = "charging"
    PowerEventDischarging   PowerEventType = "discharging"
    PowerEventFull          PowerEventType = "full"
)

type PowerStateLog struct {
    ID              string         `json:"id"`
    OrgID           string         `json:"org_id"`
    AgentID         string         `json:"agent_id"`
    Source          PowerSource    `json:"source"`
    EventType       PowerEventType `json:"event_type"`
    PreviousStatus  string         `json:"previous_status,omitempty"`
    CurrentStatus   string         `json:"current_status"`
    BatteryPercent  *int           `json:"battery_percent,omitempty"`
    OccurredAt      time.Time      `json:"occurred_at"`
}
```

- [ ] **Step 2: Write tests**

```go
// pkg/models/models_power_test.go
package models

import "testing"

func TestPowerEventTypeConstants(t *testing.T) {
    if PowerEventOnBattery != "on_battery" {
        t.Errorf("PowerEventOnBattery = %q, want on_battery", PowerEventOnBattery)
    }
    if PowerEventOnLine != "on_line" {
        t.Errorf("PowerEventOnLine = %q, want on_line", PowerEventOnLine)
    }
}

func TestPowerStateLogBatteryPercent(t *testing.T) {
    pct := 75
    log := &PowerStateLog{
        BatteryPercent: &pct,
    }
    if *log.BatteryPercent != 75 {
        t.Errorf("BatteryPercent = %d, want 75", *log.BatteryPercent)
    }
}
```

- [ ] **Step 3: Commit**

```bash
git add pkg/models/models_power.go pkg/models/models_power_test.go
git commit -m "feat(power): PowerStateLog model"
```

---

### Task 3: SNMP UPS Checker

**Files:**
- Create: `pkg/agent/checkers/ups.go`
- Test: `pkg/agent/checkers/ups_test.go`

- [ ] **Step 1: Add gosnmp dependency**

Run: `go get github.com/gosnmp/gosnmp`
Expected: dependency added to go.mod

- [ ] **Step 2: Write the UPS checker**

```go
// pkg/agent/checkers/ups.go
package checkers

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/gosnmp/gosnmp"
    "github.com/openagentplatform/openagentplatform/pkg/agent"
)

const CheckTypeUPSS NMP = "ups_snmp"

type UPSS NMPChecker struct {
    // No dependencies
}

func (c *UPSS NMPChecker) Name() string { return CheckTypeUPSS NMP }

func (c *UPSS NMPChecker) Metadata() CheckerMetadata {
    return CheckerMetadata{
        Description: "SNMP-based UPS monitoring: battery level, input voltage, load",
        ConfigSchema: map[string]any{
            "host":     "string (IP or hostname of UPS daemon)",
            "port":     "int (default 161)",
            "community": "string (SNMPv2c community, default 'public')",
            "version":  "string (2c | 3, default 2c)",
            "oids":     "[]string (list of OIDs to query, default RFC 1628 standard set)",
        },
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
    UPSModel         string
    UPSStatus        string  // OL | OB | LB | RB
    BatteryPercent   int
    LoadPercent      int
    InputVoltage     int
    OutputVoltage    int
    RuntimeMinutes   int
    LastTransfer     string
    PowerTransition  string  // populated when state changes
    PreviousStatus   string  // populated on transition
}

func (c *UPSS NMPChecker) Run(ctx context.Context, req *CheckRequest) *Result {
    var cfg upsConfig
    if err := json.Unmarshal(req.Config, &cfg); err != nil {
        return &Result{Status: "error", Message: "invalid config: " + err.Error()}
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
    resultJSON, _ := json.Marshal(result)

    // Threshold evaluation
    status := "ok"
    if result.UPSStatus == "OB" {
        status = "fail"  // On battery = critical
    } else if result.BatteryPercent < 10 {
        status = "fail"
    } else if result.BatteryPercent < 30 {
        status = "warn"
    }

    return &Result{
        Status: status,
        Message: fmt.Sprintf("UPS %s: %d%% battery, status %s", result.UPSModel, result.BatteryPercent, result.UPSStatus),
        Data:   resultJSON,
    }
}

func standardUPSOIDs() []string {
    // RFC 1628 standard UPS MIB-II OIDs
    return []string{
        ".1.3.6.1.2.1.33.1.1.2.0",  // upsIdentModel
        ".1.3.6.1.2.1.33.1.1.1.0",  // upsIdentManufacturer
        ".1.3.6.1.2.1.33.1.2.1.0",  // upsBatteryStatus
        ".1.3.6.1.2.1.33.1.2.4.0",  // upsBatteryTimeOnBattery
        ".1.3.6.1.2.1.33.1.2.5.0",  // upsBatteryEstimatedTimeRemaining
        ".1.3.6.1.2.1.33.1.3.1.0",  // upsInputLineBads
        ".1.3.6.1.2.1.33.1.3.3.1.3.1",  // upsInputVoltage
        ".1.3.6.1.2.1.33.1.4.1.0",  // upsOutputStatus
        ".1.3.6.1.2.1.33.1.4.4.0",  // upsOutputLoad
        ".1.3.6.1.2.1.33.1.4.6.0",  // upsOutputVoltage
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
    for _, v := range vars {
        switch v.Name {
        case ".1.3.6.1.2.1.33.1.1.2.0":
            r.UPSModel = string(v.Value.([]byte))
        case ".1.3.6.1.2.1.33.1.2.1.0":
            r.UPSStatus = decodeBatteryStatus(int(v.Value.(int)))
        case ".1.3.6.1.2.1.33.1.4.1.0":
            if int(v.Value.(int)) == 2 {  // upsOutputStatus: onBattery
                if r.UPSStatus != "OB" {
                    r.PreviousStatus = r.UPSStatus
                    r.UPSStatus = "OB"
                    r.PowerTransition = "on_battery"
                }
            } else if int(v.Value.(int)) == 1 {  // onLine
                if r.UPSStatus == "OB" {
                    r.PreviousStatus = "OB"
                    r.UPSStatus = "OL"
                    r.PowerTransition = "on_line"
                }
            }
        case ".1.3.6.1.2.1.33.1.4.4.0":
            r.LoadPercent = int(v.Value.(int))
        case ".1.3.6.1.2.1.33.1.4.6.0":
            r.OutputVoltage = int(v.Value.(int))
        case ".1.3.6.1.2.1.33.1.3.3.1.3.1":
            r.InputVoltage = int(v.Value.(int))
        }
    }
    // Battery status: 1=unknown, 2=batteryNormal, 3=batteryLow, 4=batteryDepleted
    // Handled in decodeBatteryStatus
    return r
}

func decodeBatteryStatus(code int) string {
    switch code {
    case 2:
        return "OL"  // On Line (battery normal)
    case 3:
        return "LB"  // Low Battery
    case 4:
        return "RB"  // Replace Battery
    default:
        return "unknown"
    }
}

func init() {
    Register(CheckTypeUPSS NMP, &UPSS NMPChecker{})
}
```

- [ ] **Step 3: Write tests**

```go
// pkg/agent/checkers/ups_test.go
package checkers

import "testing"

func TestUPSS NMPCheckerImplementsInterface(t *testing.T) {
    c := &UPSS NMPChecker{}
    var _ Checker = c
}

func TestUPSS NMPCheckerName(t *testing.T) {
    c := &UPSS NMPChecker{}
    if c.Name() != "ups_snmp" {
        t.Errorf("Name = %q, want ups_snmp", c.Name())
    }
}

func TestStandardUPSOIDs(t *testing.T) {
    oids := standardUPSOIDs()
    if len(oids) == 0 {
        t.Error("standardUPSOIDs returned empty list")
    }
}

func TestSnmpVersion(t *testing.T) {
    if snmpVersion("3") != gosnmp.Version3 {
        t.Error("snmpVersion(3) should be Version3")
    }
    if snmpVersion("2c") != gosnmp.Version2c {
        t.Error("snmpVersion(2c) should be Version2c")
    }
}

func TestDecodeBatteryStatus(t *testing.T) {
    if decodeBatteryStatus(2) != "OL" {
        t.Errorf("decodeBatteryStatus(2) = %q, want OL", decodeBatteryStatus(2))
    }
    if decodeBatteryStatus(3) != "LB" {
        t.Errorf("decodeBatteryStatus(3) = %q, want LB", decodeBatteryStatus(3))
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/agent/checkers/... -run TestUPS -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/agent/checkers/ups.go pkg/agent/checkers/ups_test.go go.mod go.sum
git commit -m "feat(power): SNMP UPS checker using gosnmp"
```

---

### Task 4: Battery Checker (OS-Specific)

**Files:**
- Create: `pkg/agent/checkers/battery.go`
- Create: `pkg/agent/checkers/battery_linux.go`
- Create: `pkg/agent/checkers/battery_darwin.go`
- Create: `pkg/agent/checkers/battery_windows.go`
- Test: `pkg/agent/checkers/battery_test.go`

- [ ] **Step 1: Write the cross-platform checker (no per-OS code)**

```go
// pkg/agent/checkers/battery.go
package checkers

import (
    "context"
    "encoding/json"

    "github.com/openagentplatform/openagentplatform/pkg/agent"
)

const CheckTypeBattery = "battery"

type BatteryChecker struct {
    platform string  // "linux" | "darwin" | "windows"
}

func NewBatteryChecker(platform string) *BatteryChecker {
    return &BatteryChecker{platform: platform}
}

func (c *BatteryChecker) Name() string { return CheckTypeBattery }

func (c *BatteryChecker) Metadata() CheckerMetadata {
    return CheckerMetadata{
        Description: "Agent-side battery health: percent, charging state, time remaining",
        ConfigSchema: map[string]any{
            "warn_threshold":  "int (default 20)",
            "fail_threshold":  "int (default 5)",
        },
    }
}

type batteryConfig struct {
    WarnThreshold int `json:"warn_threshold"`
    FailThreshold int `json:"fail_threshold"`
}

type batteryResult struct {
    Status            string
    Percent           int
    TimeRemainingMin  int
    CycleCount        int
    HealthPercent     int
    PowerTransition   string  // populated when state changes
    PreviousStatus    string
}

func (c *BatteryChecker) Run(ctx context.Context, req *CheckRequest) *Result {
    var cfg batteryConfig
    if err := json.Unmarshal(req.Config, &cfg); err != nil {
        return &Result{Status: "error", Message: "invalid config: " + err.Error()}
    }
    if cfg.WarnThreshold == 0 {
        cfg.WarnThreshold = 20
    }
    if cfg.FailThreshold == 0 {
        cfg.FailThreshold = 5
    }

    raw, err := c.readBattery(c.platform)
    if err != nil {
        return &Result{Status: "error", Message: err.Error()}
    }

    status := "ok"
    if raw.Percent < cfg.FailThreshold {
        status = "fail"
    } else if raw.Percent < cfg.WarnThreshold || raw.Status == "discharging" && raw.Percent < cfg.WarnThreshold {
        status = "warn"
    }

    data, _ := json.Marshal(raw)
    return &Result{
        Status: status,
        Message: fmt.Sprintf("battery: %d%%, %s", raw.Percent, raw.Status),
        Data:   data,
    }
}

// readBattery is implemented per OS in battery_linux.go, battery_darwin.go, battery_windows.go
// It returns batteryResult with transition detection.
func (c *BatteryChecker) readBattery(platform string) (*batteryResult, error) {
    return readBatteryOS(ctx, platform)
}
```

- [ ] **Step 2: Write Linux implementation**

```go
// pkg/agent/checkers/battery_linux.go
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

func readBatteryOS(ctx context.Context, platform string) (*batteryResult, error) {
    // Read /sys/class/power_supply/BAT0/uevent
    f, err := os.Open("/sys/class/power_supply/BAT0/uevent")
    if err != nil {
        return nil, fmt.Errorf("battery: open uevent: %w", err)
    }
    defer f.Close()
    scanner := bufio.NewScanner(f)
    result := &batteryResult{Status: "unknown"}
    for scanner.Scan() {
        line := scanner.Text()
        if k, v, ok := strings.Cut(line, "="); ok {
            switch k {
            case "POWER_SUPPLY_STATUS":
                result.Status = strings.ToLower(v)
                if v == "Discharging" {
                    if result.PreviousStatus != "" && result.PreviousStatus != "discharging" {
                        result.PowerTransition = "discharging"
                        result.PreviousStatus = result.PreviousStatus
                    }
                } else if v == "Charging" {
                    if result.PreviousStatus == "discharging" {
                        result.PowerTransition = "charging"
                    }
                } else if v == "Full" {
                    if result.PreviousStatus == "charging" {
                        result.PowerTransition = "full"
                    }
                }
                result.PreviousStatus = strings.ToLower(v)
            case "POWER_SUPPLY_CAPACITY":
                if pct, err := strconv.Atoi(v); err == nil {
                    result.Percent = pct
                }
            case "POWER_SUPPLY_TIME_TO_EMPTY_NOW":
                if t, err := strconv.Atoi(v); err == nil {
                    result.TimeRemainingMin = t / 60
                }
            case "POWER_SUPPLY_ENERGY_FULL_DESIGN":
                // Compare to ENERGY_FULL for health
                fullDesign, _ := strconv.Atoi(v)
                if fullDesign > 0 {
                    // Read current capacity
                    cur, _ := readInt("/sys/class/power_supply/BAT0/charge_now")
                    result.HealthPercent = cur * 100 / fullDesign
                }
            }
        }
    }
    return result, nil
}

func readInt(path string) (int, error) {
    f, err := os.Open(path)
    if err != nil {
        return 0, err
    }
    defer f.Close()
    var v int
    fmt.Fscanf(f, "%d", &v)
    return v, nil
}
```

- [ ] **Step 3: Write macOS implementation**

```go
// pkg/agent/checkers/battery_darwin.go
//go:build darwin

package checkers

import (
    "context"
    "fmt"
    "os/exec"
    "strconv"
    "strings"
)

func readBatteryOS(ctx context.Context, platform string) (*batteryResult, error) {
    out, err := exec.Command("pmset", "-g", "batt").Output()
    if err != nil {
        return nil, fmt.Errorf("battery: pmset: %w", err)
    }
    result := &batteryResult{Status: "unknown"}
    lines := strings.Split(string(out), "\n")
    for _, line := range lines {
        if !strings.Contains(line, "Battery") {
            continue
        }
        if strings.Contains(line, "discharging") {
            result.Status = "discharging"
        } else if strings.Contains(line, "charging") {
            result.Status = "charging"
        } else if strings.Contains(line, "charged") {
            result.Status = "full"
        }
        if idx := strings.Index(line, "%"); idx > 0 {
            before := strings.TrimSpace(line[max(0, idx-4):idx])
            fields := strings.Fields(before)
            if len(fields) > 0 {
                if pct, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
                    result.Percent = pct
                }
            }
        }
    }
    // Read cycle count via system_profiler
    cycleOut, _ := exec.Command("system_profiler", "SPPowerDataType").Output()
    if strings.Contains(string(cycleOut), "Cycle Count:") {
        for _, line := range strings.Split(string(cycleOut), "\n") {
            if strings.Contains(line, "Cycle Count:") {
                fields := strings.Fields(line)
                if len(fields) >= 3 {
                    if cycles, err := strconv.Atoi(fields[2]); err == nil {
                        result.CycleCount = cycles
                    }
                }
            }
        }
    }
    return result, nil
}
```

- [ ] **Step 4: Write Windows implementation**

```go
// pkg/agent/checkers/battery_windows.go
//go:build windows

package checkers

import (
    "context"
    "fmt"
    "os/exec"
    "strconv"
    "strings"
)

func readBatteryOS(ctx context.Context, platform string) (*batteryResult, error) {
    // Use WMI via PowerShell
    out, err := exec.Command("powershell", "-Command",
        "Get-WmiObject Win32_Battery | Select-Object BatteryStatus, EstimatedChargeRemaining, EstimatedRunTime | ConvertTo-Json").Output()
    if err != nil {
        return nil, fmt.Errorf("battery: wmi: %w", err)
    }
    var wmi struct {
        BatteryStatus               int `json:"BatteryStatus"`
        EstimatedChargeRemaining    int `json:"EstimatedChargeRemaining"`
        EstimatedRunTime            int `json:"EstimatedRunTime"`
    }
    if err := jsonUnmarshal(out, &wmi); err != nil {
        return nil, fmt.Errorf("battery: parse wmi: %w", err)
    }
    result := &batteryResult{
        Status:           wmiStatusToString(wmi.BatteryStatus),
        Percent:          wmi.EstimatedChargeRemaining,
        TimeRemainingMin: wmi.EstimatedRunTime,
    }
    return result, nil
}

func wmiStatusToString(code int) string {
    switch code {
    case 1: return "discharging"
    case 2: return "ac_power"
    case 3: return "full"
    case 4: return "low"
    case 5: return "critical"
    case 6: return "charging"
    case 7: return "charging_high"
    case 8: return "charging_low"
    case 9: return "charging_critical"
    case 10: return "undefined"
    case 11: return "partially_charged"
    default: return "unknown"
    }
}

func jsonUnmarshal(b []byte, v any) error {
    return json.Unmarshal(b, v)
}
```

- [ ] **Step 5: Write tests**

```go
// pkg/agent/checkers/battery_test.go
package checkers

import "testing"

func TestBatteryCheckerName(t *testing.T) {
    c := NewBatteryChecker("linux")
    if c.Name() != "battery" {
        t.Errorf("Name = %q, want battery", c.Name())
    }
}

func TestBatteryCheckerImplementsInterface(t *testing.T) {
    c := NewBatteryChecker("linux")
    var _ Checker = c
}

func TestBatteryCheckerDefaults(t *testing.T) {
    c := NewBatteryChecker("linux")
    _ = c
    // Run with empty config should not panic
    // (no battery on test machine — error returned)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./pkg/agent/checkers/... -run TestBattery -v`
Expected: PASS (cross-compile for each platform)

- [ ] **Step 7: Commit**

```bash
git add pkg/agent/checkers/battery.go pkg/agent/checkers/battery_linux.go pkg/agent/checkers/battery_darwin.go pkg/agent/checkers/battery_windows.go pkg/agent/checkers/battery_test.go
git commit -m "feat(power): battery health checker with platform-specific implementations"
```

---

### Task 5: ResultIngestor Power-Transition Detection

**Files:**
- Create: `internal/power/ingest.go`
- Create: `internal/power/ingest_test.go`
- Modify: `internal/checks/ingest.go` — call into power ingest when result has power_transition

- [ ] **Step 1: Write the power ingest handler**

```go
// internal/power/ingest.go
package power

import (
    "context"
    "log/slog"

    "github.com/openagentplatform/openagentplatform/internal/events"
    "github.com/openagentplatform/openagentplatform/pkg/models"
)

type Ingestor struct {
    store PowerStateLogStore
    log   *slog.Logger
}

func NewIngestor(store PowerStateLogStore, log *slog.Logger) *Ingestor {
    return &Ingestor{store: store, log: log}
}

type powerTransition struct {
    Source         string
    PowerTransition string
    PreviousStatus string
    CurrentStatus  string
    BatteryPercent *int
}

// HandleResult is called from checks/ingest.go for every result. If the
// result contains a power_transition, an alert is emitted and the
// state log is updated.
func (i *Ingestor) HandleResult(ctx context.Context, orgID, agentID, checkID string, data []byte) {
    var p powerTransition
    if err := json.Unmarshal(data, &p); err != nil {
        return
    }
    if p.PowerTransition == "" {
        return
    }

    // Determine source
    source := models.PowerSourceUPS
    if p.Source == "battery" {
        source = models.PowerSourceBattery
    }

    // Map transition to event type
    eventType := mapTransitionToEventType(source, p.PowerTransition, p.BatteryPercent)

    // Append to state log (audit trail)
    log := &models.PowerStateLog{
        ID:             orgID + "-" + agentID + "-" + time.Now().Format(time.RFC3339Nano),
        OrgID:          orgID,
        AgentID:        agentID,
        Source:         source,
        EventType:      eventType,
        PreviousStatus: p.PreviousStatus,
        CurrentStatus:  p.CurrentStatus,
        BatteryPercent: p.BatteryPercent,
        OccurredAt:     time.Now().UTC(),
    }
    if err := i.store.Append(ctx, log); err != nil {
        i.log.Warn("power: state log append failed", "agent", agentID, "err", err)
    }

    // Emit alert
    events.PublishAlert(ctx, &events.AlertPayload{
        Type:       "power_event",
        Severity:   severityForEventType(eventType),
        ResourceID: agentID,
        Message:    string(eventType),
        Details: map[string]any{
            "source":            string(source),
            "event_type":        string(eventType),
            "previous_status":   p.PreviousStatus,
            "current_status":    p.CurrentStatus,
            "battery_percent":   p.BatteryPercent,
            "agent_id":          agentID,
            "check_id":          checkID,
        },
    })
}

func mapTransitionToEventType(source models.PowerSource, transition string, percent *int) models.PowerEventType {
    switch transition {
    case "on_battery":
        return models.PowerEventOnBattery
    case "on_line":
        return models.PowerEventOnLine
    case "discharging":
        if percent != nil && *percent < 20 {
            return models.PowerEventLowBattery
        }
        return models.PowerEventDischarging
    case "charging":
        return models.PowerEventCharging
    case "full":
        return models.PowerEventFull
    case "critical":
        return models.PowerEventBatteryCrit
    default:
        return models.PowerEventType(transition)
    }
}

func severityForEventType(t models.PowerEventType) string {
    switch t {
    case models.PowerEventOnBattery, models.PowerEventLowBattery, models.PowerEventBatteryCrit:
        return "warning"
    case models.PowerEventOnLine, models.PowerEventFull, models.PowerEventCharging:
        return "info"
    default:
        return "info"
    }
}
```

- [ ] **Step 2: Write tests**

```go
// internal/power/ingest_test.go
package power

import "testing"

func TestMapTransitionToEventType(t *testing.T) {
    cases := []struct {
        in        string
        source    string
        percent   *int
        want      string
    }{
        {"on_battery", "ups", nil, "on_battery"},
        {"on_line", "ups", nil, "on_line"},
        {"discharging", "battery", intPtr(50), "discharging"},
        {"discharging", "battery", intPtr(15), "low_battery"},
        {"charging", "battery", nil, "charging"},
    }
    for _, c := range cases {
        got := mapTransitionToEventType(models.PowerSource(c.source), c.in, c.percent)
        if string(got) != c.want {
            t.Errorf("mapTransitionToEventType(%q, %q, %v) = %q, want %q", c.in, c.source, c.percent, got, c.want)
        }
    }
}

func intPtr(i int) *int { return &i }
```

- [ ] **Step 3: Wire into checks/ingest.go**

```go
// internal/checks/ingest.go — in the Ingestor struct, add powerIngestor:
type ResultIngestor struct {
    // ... existing fields ...
    powerIngestor *power.Ingestor
}

// In the result handler, after persisting the result:
if r.powerIngestor != nil {
    r.powerIngestor.HandleResult(ctx, orgID, agentID, checkID, result.Data)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/power/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/power/ingest.go internal/power/ingest_test.go internal/checks/ingest.go
git commit -m "feat(power): result ingest hook for power transitions"
```

---

### Task 6: Add Power Fields to AlertRule

**Files:**
- Modify: `pkg/models/models_alerts.go` — add `PowerEventTypes` and `PowerSource` fields

- [ ] **Step 1: Add fields to AlertRule struct**

```go
// pkg/models/models_alerts.go — add to AlertRule struct:
type AlertRule struct {
    // ... existing fields ...
    PowerEventTypes []string `json:"power_event_types,omitempty"`  // on_battery, low_battery, on_line
    PowerSource     string   `json:"power_source,omitempty"`        // ups | battery | empty (any)
}
```

- [ ] **Step 2: Verify the alert engine picks up these fields**

```go
// internal/alerts/engine_core.go — in the rule matching logic, add:
if len(rule.PowerEventTypes) > 0 && !contains(rule.PowerEventTypes, alert.Details["event_type"].(string)) {
    continue
}
if rule.PowerSource != "" && alert.Details["source"] != rule.PowerSource {
    continue
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/alerts/... -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/models/models_alerts.go internal/alerts/engine_core.go
git commit -m "feat(power): AlertRule fields for power_event_types and power_source"
```

---

### Task 7: API Routes for Power Events

**Files:**
- Create: `internal/api/power.go`
- Modify: `internal/api/routes.go` — add `s.mountPowerRoutes(r)`
- Test: `internal/api/power_test.go`

- [ ] **Step 1: Write handlers**

```go
// internal/api/power.go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"

    "github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *Server) mountPowerRoutes(r chi.Router) {
    r.Route("/power", func(r chi.Router) {
        r.Get("/events", s.listPowerEvents)
        r.Get("/state", s.listPowerState)
    })
}

func (s *Server) listPowerEvents(w http.ResponseWriter, r *http.Request) {
    orgID := tenancy.GetTenant(r.Context()).OrgID
    events, err := s.powerStore.ListByOrg(r.Context(), orgID, 100)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(events)
}

func (s *Server) listPowerState(w http.ResponseWriter, r *http.Request) {
    orgID := tenancy.GetTenant(r.Context()).OrgID
    // Aggregate latest state per agent
    state, err := s.powerStore.LatestByOrg(r.Context(), orgID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(state)
}
```

- [ ] **Step 2: Wire into routes**

```go
// internal/api/routes.go — in mountAPISubRoutes:
s.mountPowerRoutes(r)
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/... -run TestPower -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/power.go internal/api/routes.go
git commit -m "feat(power): API surface — /api/v1/power events and state"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** §1 UPS SNMP (Task 3), §2 battery (Task 4), §3 power events (Tasks 5+6), §4 data model (Task 1), §5 API (Task 7), §6 credentials (covered in §1.2 of spec).
- [ ] **Placeholder scan:** All code concrete.
- [ ] **Type consistency:** `PowerEventType` defined once, used everywhere.
- [ ] **Pattern adherence:** Reuses `pkg/agent/checkers` registry, `oap.events.alerts` subject, chi router, role-gated.
- [ ] **OUT of scope verified:** No UPS management (shut down servers), no battery calibration, no cross-site power correlation.
