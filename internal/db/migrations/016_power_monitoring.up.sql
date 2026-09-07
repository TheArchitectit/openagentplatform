-- 016_power_monitoring: append-only power state transition log

CREATE TABLE IF NOT EXISTS power_state_log (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL DEFAULT '',
    agent_id           TEXT NOT NULL,
    source             TEXT NOT NULL,  -- ups | battery
    event_type         TEXT NOT NULL,  -- on_battery | on_line | low_battery | battery_critical | charging | discharging | full
    previous_status    TEXT,
    current_status     TEXT NOT NULL,
    battery_percent    INTEGER,
    occurred_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_power_state_log_agent ON power_state_log (agent_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_power_state_log_org ON power_state_log (org_id, occurred_at DESC);
