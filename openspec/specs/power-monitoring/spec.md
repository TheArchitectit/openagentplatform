# Power / UPS Monitoring

> **Phase:** P3 — RMM parity gap
> **Status:** DRAFT
> **Source:** `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.8 (G-PWR-001..003)
> **App Path:** `pkg/agent/checkers/ups.go`, `pkg/agent/checkers/battery.go`, `internal/power/`, `pkg/models/models_power.go`, `internal/api/power.go`
> **Depends on:** openspec/specs/rmm-core/spec.md §14

---

## Description

MSPs managing server rooms and edge deployments need to monitor UPS health, power state transitions, and laptop/edge battery status. OAP's check framework already dispatches checks and ingests results — this spec adds three power-aware check types and a power-event emission path. No new infrastructure; everything extends the existing check/alert engine.

This spec does **not** invent mechanisms. Each requirement is anchored to an existing pattern.

---

## User Story

**As** an MSP technician,
**I want** to be alerted when a UPS switches to battery, when a laptop battery drops below 20%, and when a server room power event occurs (on-battery, low-battery, on-line), so that I can respond to power incidents before they cause data loss or downtime.

---

## Requirements

### 1. UPS Network Monitoring (SNMP Check)

1.1. A new check type `"ups_snmp"` in `pkg/agent/checkers/ups.go`. The check queries a UPS daemon via SNMP (v2c and v3) on the network. It does **not** require an OAP agent installed on the UPS host — the check runs from any agent that can reach the UPS over the network.

1.2. Check configuration (in `CheckDefinition.config` JSONB):
```json
{
  "host": "192.168.1.50",
  "port": 161,
  "community": "public",
  "version": "2c",
  "oids": ["1.3.6.1.2.1.33.1.1.1.0", "1.3.6.1.2.1.33.1.2.1.0"]
}
```

For SNMP v3: `username`, `auth_protocol`, `auth_passphrase`, `priv_protocol`, `priv_passphrase` fields replace `community`.

1.3. Result fields: `ups_model`, `ups_status` (OL/OB/LB/RB), `battery_percent`, `load_percent`, `input_voltage`, `output_voltage`, `runtime_minutes`, `last_transfer_reason`.

1.4. SNMP library: `github.com/gosnmp/gosnmp` (MIT license, actively maintained). No custom SNMP stack.

1.5. Threshold evaluation uses the existing `ThresholdEvaluator`: `battery_percent < warn_threshold` (default 30) or `battery_percent < fail_threshold` (default 10) → warn/fail. `ups_status == "OB"` (on battery) → fail immediately regardless of threshold.

1.6. **OUT of scope**: UPS management (shut down servers, trigger graceful shutdown sequences). Monitoring only. Configuring UPS devices. UPS firmware updates.

### 2. Agent-Side Battery Health Checker

2.1. A new check type `"battery"` in `pkg/agent/checkers/battery.go`. The check reads local battery telemetry from the host OS. No SNMP — this is for laptops, edge devices, and mobile workstations that have a built-in battery.

2.2. Platform-specific data sources:
- **Linux**: `/sys/class/power_supply/BAT0/uevent` or `upower -i $(upower -e | grep BAT)`
- **macOS**: `pmset -g batt`
- **Windows**: WMI `Win32_Battery` class

2.3. Result fields: `status` (charging/discharging/full/unknown), `percent`, `time_remaining_minutes`, `cycle_count` (macOS/Windows only), `health_percent` (if available).

2.4. Threshold evaluation: `percent < warn_threshold` (default 20) or `percent < fail_threshold` (default 5). `status == "discharging" && percent < warn_threshold` → warn immediately (not just when the threshold is hit).

2.5. **OUT of scope**: battery calibration, battery replacement workflow, battery recycling tracking.

### 3. Power-Event Emission and Alert Rules

3.1. When the `"ups_snmp"` or `"battery"` check detects a power state transition (OL→OB, charging→discharging, or the reverse), the check result includes a `power_transition` field with the previous and current states.

3.2. The `ResultIngestor` (existing `internal/checks/ingest.go`) detects `power_transition` in the result metadata and publishes a `power_event` via `oap.events.alerts` with a `power_event` condition type. No new NATS subject.

3.3. New `AlertRule` fields for power-event alerting (additive to the existing struct):
- `power_event_types []string` — filter: `on_battery`, `low_battery`, `on_line`, `battery_critical`
- `power_source string` — filter: `ups`, `battery`, or `""` (any)

3.4. Power events are stateful: an `on_battery` alert auto-resolves when an `on_line` event arrives for the same agent/UPS. The existing alert state machine (`internal/alerts/engine_core.go`) handles this via the `resolve_on_clear` path.

3.5. **OUT of scope**: correlating power events across agents (e.g., "all agents in site X are on battery"). This is a site-level inference that can be done later via alert routing.

### 4. Data Model

No new tables for check results — the existing `check_results` table stores the UPS/battery data. Additive fields:

```
power_state_log — org_id, agent_id, source (ups/battery), event_type (on_battery/on_line/low_battery/critical),
                  previous_status, current_status, battery_percent, occurred_at
```

This table is append-only (no updates). It provides an audit trail for power incidents. RLS enabled. Standard conventions.

### 5. API Surface

New route group `/api/v1/power`:

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/events` | Admin/Technician | List power events (filterable by agent, site, type) |
| GET | `/state` | Admin/Technician | Current power state across all agents |

The check CRUD endpoints (`/api/v1/checks`) already handle creating `ups_snmp` and `battery` check definitions. The power-specific endpoints are for the event log and state summary.

### 6. Configuration and Credentials

6.1. SNMP community strings and v3 credentials are stored in the `CheckDefinition.config` JSONB (not in `SecretBackend`). Rationale: SNMP credentials are per-check, not per-org, and the config is already encrypted at rest via TLS in transit and disk encryption on the DB. For v3 auth/priv passphrases, a `credential_ref` field in the config can point to a `SecretBackend` path.

6.2. **OUT of scope**: SSH-based UPS monitoring (only SNMP), Modbus, or proprietary UPS protocols. SNMP covers 95% of enterprise UPS hardware.

---

## Cross-References

- `docs/GAP_ANALYSIS_RMM_PLATFORM.md` §2.8
- `pkg/agent/checkers/registry.go` — check registration pattern
- `internal/checks/ingest.go` — result ingestion and threshold evaluation
- `internal/alerts/engine_core.go` — alert state machine
- `internal/events/subjects.go` — NATS taxonomy
- `openspec/specs/rmm-core/spec.md` §14
