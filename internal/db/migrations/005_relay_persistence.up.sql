-- Migration 005: managed A2A relay durable state (a2a-relay spec §8)
--
-- The relay (cmd/relay) holds live session state in process memory by
-- design; these two tables are its opt-in durable ledger (spec §8.2):
--
--   relay_connections  one row per admitted leg (establish/close write-
--                      through, spec §8.3); the billing/audit record of
--                      what was relayed, not a live session cache (§8.6).
--   relay_metrics      per-tenant lifetime aggregates rehydrated at boot
--                      (§8.5a) so TotalBytesRelayed — the billing counter —
--                      survives restarts.
--
-- Conventions as 001–004: tenant id is TEXT (no FKs — application-level
-- integrity, 001 header); all statements IF NOT EXISTS-guarded so the
-- relay's own boot-time migrate is a no-op against an already-migrated DB.
-- The relay applies this set itself at boot when -store-dsn is set (§8.2);
-- the platform server's own boot migration also picks it up harmlessly
-- (it creates the tables, but only the relay ever writes them).

CREATE TABLE IF NOT EXISTS relay_connections (
    id              BIGSERIAL PRIMARY KEY,
    conn_key        TEXT UNIQUE NOT NULL,           -- relay_{tenant}_{source}_{target}_{unixnano}
    tenant_id       TEXT NOT NULL,
    source_agent_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    established_at  TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL,
    bytes_relayed   BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'active',
    closed_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_relay_connections_tenant ON relay_connections (tenant_id);
CREATE INDEX IF NOT EXISTS idx_relay_connections_active ON relay_connections (status) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS relay_metrics (
    tenant_id           TEXT PRIMARY KEY,
    connection_count    INTEGER NOT NULL DEFAULT 0,
    total_connections   BIGINT NOT NULL DEFAULT 0,
    total_bytes_relayed BIGINT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
