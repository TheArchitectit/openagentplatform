-- 013_cloud_control: cloud provider accounts, resource inventory, tag drift policies, cost snapshots

CREATE TABLE IF NOT EXISTS cloud_accounts (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,
    account_id      TEXT NOT NULL,
    display_name   TEXT NOT NULL,
    is_msp_hub     BOOLEAN NOT NULL DEFAULT false,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_seen      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cloud_accounts_org ON cloud_accounts (org_id);

CREATE TABLE IF NOT EXISTS cloud_resources (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,
    account_id      TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    region          TEXT,
    name            TEXT NOT NULL,
    status          TEXT,
    tags            JSONB NOT NULL DEFAULT '{}',
    archived_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, account_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_org ON cloud_resources (org_id);
CREATE INDEX IF NOT EXISTS idx_cloud_resources_archived ON cloud_resources (archived_at) WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS cloud_policies (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,
    account_id      TEXT NOT NULL,
    required_tags  JSONB NOT NULL DEFAULT '[]',
    tag_rules      JSONB NOT NULL DEFAULT '{}',
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cloud_policies_org ON cloud_policies (org_id);

CREATE TABLE IF NOT EXISTS cost_snapshots (
    id                  TEXT PRIMARY KEY,
    org_id              TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL,
    account_id          TEXT NOT NULL,
    billing_period      TEXT NOT NULL,
    total_cost_usd     NUMERIC(12,4) NOT NULL DEFAULT 0,
    service_costs       JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cost_snapshots_org_period ON cost_snapshots (org_id, billing_period);
