-- Platform schema, part 2: tables referenced by live store code that are
-- outside the internal/*/store*.go + internal/api/*_store.go sweep captured
-- in 001_platform_schema.sql (audit, check assignments, licensing, billing,
-- remote-session recordings, tenant configs, tenant migration tracking).
-- Same rules as 001: derived from real INSERT/SELECT column lists, no
-- speculative columns, org_id TEXT.

CREATE TABLE IF NOT EXISTS audit_events (
    event_id      TEXT PRIMARY KEY,
    prev_hash     TEXT NOT NULL DEFAULT '',
    hash          TEXT NOT NULL DEFAULT '',
    "timestamp"   TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor_type    TEXT NOT NULL DEFAULT '',
    actor_id      TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    details       JSONB,
    outcome       TEXT NOT NULL DEFAULT '',
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    org_id        TEXT NOT NULL DEFAULT '',
    site_id       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_events_org_ts ON audit_events (org_id, "timestamp" DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_action ON audit_events (action);

CREATE TABLE IF NOT EXISTS check_assignments (
    id          TEXT PRIMARY KEY,
    check_id    TEXT NOT NULL,
    agent_id    TEXT NOT NULL,
    site_id     TEXT NOT NULL DEFAULT '',
    assigned_by TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (check_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_check_assignments_agent ON check_assignments (agent_id);

CREATE TABLE IF NOT EXISTS licenses (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       TEXT NOT NULL,
    license_key  TEXT NOT NULL,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    issued_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_licenses_org ON licenses (org_id, issued_at DESC);

-- tenant_configs and tenant_migrations are created by the tenancy
-- TenantMigrator itself (migration 1 of TenantMigrations, rls_migrations_data.go)
-- when OAP_ENABLE_TENANT_MIGRATIONS=1. They are declared here as well so the
-- TenantStore/TenantConfigStore (internal/tenancy/store.go, config_store.go)
-- work without the opt-in migrator flag. DDL matches the store SQL.
CREATE TABLE IF NOT EXISTS tenant_configs (
    tenant_id  UUID NOT NULL,
    key        TEXT NOT NULL,
    value      JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);

CREATE TABLE IF NOT EXISTS org_billing_state (
    org_id             TEXT PRIMARY KEY,
    stripe_customer_id TEXT NOT NULL DEFAULT '',
    subscription_id    TEXT NOT NULL DEFAULT '',
    price_id           TEXT NOT NULL DEFAULT '',
    tier               TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    current_period_end TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS session_recordings (
    session_id    TEXT PRIMARY KEY,
    agent_id      TEXT NOT NULL DEFAULT '',
    user_id       TEXT NOT NULL DEFAULT '',
    protocol      TEXT NOT NULL DEFAULT '',
    terminal_cols INTEGER,
    terminal_rows INTEGER,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    bytes_in      BIGINT NOT NULL DEFAULT 0,
    bytes_out     BIGINT NOT NULL DEFAULT 0,
    event_count   BIGINT NOT NULL DEFAULT 0,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    content_hash  TEXT
);
CREATE INDEX IF NOT EXISTS idx_session_recordings_agent ON session_recordings (agent_id, started_at DESC);

CREATE TABLE IF NOT EXISTS session_recording_chunks (
    session_id  TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,
    event_count BIGINT NOT NULL DEFAULT 0,
    compressed  BOOLEAN NOT NULL DEFAULT true,
    PRIMARY KEY (session_id, chunk_index)
);
