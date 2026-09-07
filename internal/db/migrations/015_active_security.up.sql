-- 015_active_security: EDR integrations, security events, agent mapping, SIEM forwarders

CREATE TABLE IF NOT EXISTS edr_integrations (
    id                     TEXT PRIMARY KEY,
    org_id                 TEXT NOT NULL DEFAULT '',
    provider               TEXT NOT NULL,  -- crowdstrike | defender | sentinelone
    name                   TEXT NOT NULL,
    credential_ref         TEXT NOT NULL,  -- SecretBackend URI
    webhook_secret         TEXT,
    poll_interval_seconds  INTEGER NOT NULL DEFAULT 900,
    enabled                BOOLEAN NOT NULL DEFAULT true,
    last_poll_at           TIMESTAMPTZ,
    last_event_at          TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_edr_integrations_org ON edr_integrations (org_id);

CREATE TABLE IF NOT EXISTS edr_agent_mapping (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL DEFAULT '',
    provider     TEXT NOT NULL,
    edr_host_id  TEXT NOT NULL,
    agent_id     TEXT NOT NULL,
    hostname     TEXT,
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, edr_host_id)
);

CREATE INDEX IF NOT EXISTS idx_edr_agent_mapping_agent ON edr_agent_mapping (agent_id);

CREATE TABLE IF NOT EXISTS security_events (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL,
    provider_event_id  TEXT NOT NULL,
    agent_id           TEXT,
    severity           TEXT NOT NULL,  -- info | warning | critical
    tactic             TEXT,
    technique          TEXT,
    detection_type     TEXT,
    payload            JSONB NOT NULL DEFAULT '{}',
    occurred_at        TIMESTAMPTZ NOT NULL,
    ingested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ingestion_method   TEXT NOT NULL DEFAULT 'webhook',  -- webhook | pull
    UNIQUE (provider, provider_event_id)
);

CREATE INDEX IF NOT EXISTS idx_security_events_org_time ON security_events (org_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS siem_forwarders (
    id                       TEXT PRIMARY KEY,
    org_id                   TEXT NOT NULL DEFAULT '',
    name                     TEXT NOT NULL,
    siem_type                TEXT NOT NULL,  -- splunk | elastic | generic
    endpoint                 TEXT NOT NULL,
    credential_ref           TEXT NOT NULL,
    batch_size               INTEGER NOT NULL DEFAULT 100,
    batch_interval_seconds   INTEGER NOT NULL DEFAULT 10,
    enabled                  BOOLEAN NOT NULL DEFAULT true,
    last_flush_at            TIMESTAMPTZ,
    last_error               TEXT,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_siem_forwarders_org ON siem_forwarders (org_id);

CREATE TABLE IF NOT EXISTS siem_forward_queue (
    forwarder_id   TEXT NOT NULL,
    event_id       TEXT NOT NULL,
    payload        JSONB NOT NULL,
    attempts       INTEGER NOT NULL DEFAULT 0,
    next_retry_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (forwarder_id, event_id)
);
