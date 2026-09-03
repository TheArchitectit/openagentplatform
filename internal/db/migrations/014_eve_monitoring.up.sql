-- 014_eve_monitoring: hypervisor clusters, VM/CT resources, and cluster events
--
-- Conventions as 001-013: org_id TEXT NOT NULL DEFAULT '', no FKs, all
-- statements IF NOT EXISTS-guarded. The relay (server-side cluster
-- connector) applies these itself when an EVE cluster is registered.

CREATE TABLE IF NOT EXISTS hypervisor_clusters (
    id                TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL DEFAULT '',
    provider          TEXT NOT NULL,  -- proxmox | libvirt | vsphere
    name              TEXT NOT NULL,
    endpoint          TEXT NOT NULL,  -- URL or libvirt URI
    credential_ref    TEXT NOT NULL,  -- SecretBackend URI
    primary_agent_id  TEXT,
    tags              JSONB NOT NULL DEFAULT '[]',
    enabled           BOOLEAN NOT NULL DEFAULT true,
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_clusters_org ON hypervisor_clusters (org_id);

CREATE TABLE IF NOT EXISTS hypervisor_resources (
    id                  TEXT PRIMARY KEY,
    org_id              TEXT NOT NULL DEFAULT '',
    cluster_id          TEXT NOT NULL,
    resource_id         TEXT NOT NULL,  -- VM/CT ID in hypervisor's native ID space
    resource_type       TEXT NOT NULL,  -- vm | ct | node | storage | network
    name                TEXT NOT NULL,
    status              TEXT,
    parent_resource_id  TEXT,
    cpu_count           INTEGER,
    memory_mb           BIGINT,
    disk_gb             BIGINT,
    last_seen           TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cluster_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_resources_org ON hypervisor_resources (org_id);
CREATE INDEX IF NOT EXISTS idx_hypervisor_resources_archived ON hypervisor_resources (archived_at) WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS hypervisor_events (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL DEFAULT '',
    cluster_id   TEXT NOT NULL,
    event_type   TEXT NOT NULL,  -- ha_failover | vm_started | vm_stopped | storage_warning | backup_failed
    payload      JSONB NOT NULL DEFAULT '{}',
    occurred_at  TIMESTAMPTZ NOT NULL,
    ingested_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_hypervisor_events_cluster ON hypervisor_events (cluster_id, occurred_at DESC);
