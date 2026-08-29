-- Platform schema: derived from the live SQL in internal/*/store*.go and
-- internal/api/*_store.go (INSERT/SELECT/UPDATE column lists as of commit
-- 12abdb4). Reality, not aspiration: every table/column here is referenced
-- by store code; nothing speculative is added.
--
-- Deployment order: deploy/postgres/init.sql (extensions) -> this file.
-- Tables use UUID PKs (gen_random_uuid(), pgcrypto from init.sql) except
-- where stores generate ids in Go. org_id is TEXT: the Go stores pass org
-- identifiers as strings (empty string means "unscoped"), NOT uuids —
-- see the RLS note in the multi-tenancy spec divergence record.

CREATE TABLE IF NOT EXISTS sites (
    id                  TEXT PRIMARY KEY,
    org_id              TEXT,
    registration_token  TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agents (
    id              TEXT PRIMARY KEY,
    site_id         TEXT,
    org_id          TEXT,
    hostname        TEXT NOT NULL DEFAULT '',
    os              TEXT,
    arch            TEXT,
    platform        TEXT,
    cpu_count       INTEGER,
    total_memory_mb BIGINT,
    total_disk_gb   BIGINT,
    agent_version   TEXT,
    status          TEXT NOT NULL DEFAULT 'offline',
    last_seen       TIMESTAMPTZ,
    tags            JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agents_org       ON agents (org_id);
CREATE INDEX IF NOT EXISTS idx_agents_site      ON agents (site_id);
CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents (last_seen);

CREATE TABLE IF NOT EXISTS tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    settings    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
-- Slug uniqueness applies only to ACTIVE tenants (spec §5.1: reject
-- duplicate active slugs). A bare UNIQUE here would burn the slug of
-- every soft-deleted tenant forever; the store's slugExists check
-- already scopes to deleted_at IS NULL, so the constraint must match.
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_slug ON tenants (slug) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS check_definitions (
    id                TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL DEFAULT '',
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    check_type        TEXT NOT NULL,
    config            JSONB,
    interval_seconds  INTEGER,
    timeout_seconds   INTEGER,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    fail_threshold    INTEGER,
    warning_threshold INTEGER,
    error_threshold   INTEGER,
    alert_severity    TEXT,
    is_template       BOOLEAN NOT NULL DEFAULT false,
    last_status       TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_check_definitions_org ON check_definitions (org_id);

CREATE TABLE IF NOT EXISTS check_results (
    id        BIGSERIAL PRIMARY KEY,
    agent_id  TEXT NOT NULL,
    check_id  TEXT NOT NULL,
    "timestamp" TIMESTAMPTZ NOT NULL DEFAULT now(),
    status    TEXT NOT NULL,
    value     TEXT,
    message   TEXT,
    metadata  JSONB
);
CREATE INDEX IF NOT EXISTS idx_check_results_agent_ts ON check_results (agent_id, "timestamp" DESC);

CREATE TABLE IF NOT EXISTS script_definitions (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    runtime         TEXT NOT NULL,
    body            TEXT NOT NULL,
    timeout_seconds INTEGER,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    tags            JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_script_definitions_org ON script_definitions (org_id);

CREATE TABLE IF NOT EXISTS script_runs (
    id           TEXT PRIMARY KEY,
    script_id    TEXT NOT NULL,
    agent_id     TEXT NOT NULL,
    status       TEXT NOT NULL,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    exit_code    INTEGER,
    stdout       TEXT,
    stderr       TEXT,
    triggered_by TEXT NOT NULL DEFAULT '',
    scheduled    BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_script_runs_script ON script_runs (script_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_script_runs_agent  ON script_runs (agent_id, created_at DESC);

CREATE TABLE IF NOT EXISTS alerts (
    id              TEXT PRIMARY KEY,
    dedup_key       TEXT,
    check_id        TEXT,
    agent_id        TEXT,
    site_id         TEXT,
    org_id          TEXT,
    client_id       TEXT,
    alert_rule_id   TEXT,
    severity        TEXT NOT NULL DEFAULT 'warning',
    state           TEXT NOT NULL DEFAULT 'pending',
    message         TEXT NOT NULL DEFAULT '',
    metadata        JSONB,
    acknowledged_by TEXT,
    snoozed_until   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    closed_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_alerts_org_state ON alerts (org_id, state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_dedup     ON alerts (dedup_key);

CREATE TABLE IF NOT EXISTS alert_rules (
    id                      TEXT PRIMARY KEY,
    org_id                  TEXT NOT NULL DEFAULT '',
    name                    TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    check_id                TEXT,
    agent_id                TEXT,
    site_id                 TEXT,
    min_severity            TEXT,
    notify_channels         JSONB,
    enabled                 BOOLEAN NOT NULL DEFAULT true,
    offline_silence_seconds INTEGER,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_rules_org ON alert_rules (org_id);

CREATE TABLE IF NOT EXISTS notification_channels (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL DEFAULT '',
    user_id    TEXT,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    config     JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_channels_org ON notification_channels (org_id);

CREATE TABLE IF NOT EXISTS alert_routing_rules (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority    INTEGER NOT NULL DEFAULT 0,
    conditions  JSONB,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_routing_rules_org ON alert_routing_rules (org_id);

CREATE TABLE IF NOT EXISTS alert_rule_routing_channels (
    rule_id    TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    PRIMARY KEY (rule_id, channel_id)
);

CREATE TABLE IF NOT EXISTS alert_rule_channels (
    alert_rule_id TEXT NOT NULL,
    channel_id    TEXT NOT NULL,
    PRIMARY KEY (alert_rule_id, channel_id)
);

CREATE TABLE IF NOT EXISTS alert_default_channels (
    org_id      TEXT PRIMARY KEY,
    channel_ids JSONB NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alert_user_preferences (
    user_id             TEXT NOT NULL,
    org_id             TEXT NOT NULL,
    quiet_hours         TEXT,
    severity_threshold  TEXT,
    channel_preferences JSONB,
    mute_all            BOOLEAN NOT NULL DEFAULT false,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org_id)
);

CREATE TABLE IF NOT EXISTS alert_global_preferences (
    org_id               TEXT PRIMARY KEY,
    quiet_hours          TEXT,
    retention_days       INTEGER,
    max_alerts_per_agent INTEGER,
    auto_resolve_seconds INTEGER,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS alert_state_history (
    id         BIGSERIAL PRIMARY KEY,
    alert_id   TEXT NOT NULL,
    from_state TEXT NOT NULL DEFAULT '',
    to_state   TEXT NOT NULL DEFAULT '',
    event      TEXT NOT NULL DEFAULT '',
    actor      TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_state_history_alert ON alert_state_history (alert_id, created_at DESC);

CREATE TABLE IF NOT EXISTS alert_notifications (
    id         TEXT PRIMARY KEY,
    alert_id   TEXT NOT NULL,
    channel    TEXT NOT NULL DEFAULT '',
    recipient  TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'pending',
    error_msg  TEXT,
    sent_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_notifications_alert ON alert_notifications (alert_id);

CREATE TABLE IF NOT EXISTS alert_suppression_windows (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL,
    client_id  TEXT NOT NULL DEFAULT '',
    site_id    TEXT NOT NULL DEFAULT '',
    start      TIMESTAMPTZ NOT NULL,
    "end"      TIMESTAMPTZ NOT NULL,
    recurring  BOOLEAN NOT NULL DEFAULT false,
    weekdays   INTEGER[],
    timezone   TEXT NOT NULL DEFAULT 'UTC',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_suppression_org ON alert_suppression_windows (org_id);

CREATE TABLE IF NOT EXISTS automated_tasks (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    cron_expr   TEXT NOT NULL,
    action      TEXT NOT NULL,
    params      JSONB,
    timezone    TEXT NOT NULL DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_status TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_automated_tasks_org   ON automated_tasks (org_id);
CREATE INDEX IF NOT EXISTS idx_automated_tasks_due    ON automated_tasks (next_run_at) WHERE enabled;

CREATE TABLE IF NOT EXISTS policies (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    rego_body        TEXT,
    enforcement_mode TEXT NOT NULL DEFAULT 'detect',
    severity         TEXT NOT NULL DEFAULT 'warning',
    category         TEXT NOT NULL DEFAULT 'configuration',
    enabled          BOOLEAN NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_policies_org ON policies (org_id);

CREATE TABLE IF NOT EXISTS policy_assignments (
    id         TEXT PRIMARY KEY,
    policy_id  TEXT NOT NULL,
    agent_id   TEXT,
    site_id    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_policy ON policy_assignments (policy_id);

CREATE TABLE IF NOT EXISTS policy_violations (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL,
    agent_id    TEXT,
    severity    TEXT NOT NULL DEFAULT 'warning',
    message     TEXT NOT NULL DEFAULT '',
    details     JSONB,
    resolved    BOOLEAN NOT NULL DEFAULT false,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_policy_violations_policy  ON policy_violations (policy_id);
CREATE INDEX IF NOT EXISTS idx_policy_violations_agent  ON policy_violations (agent_id);

CREATE TABLE IF NOT EXISTS patch_jobs (
    id                        TEXT PRIMARY KEY,
    org_id                    TEXT NOT NULL DEFAULT '',
    title                     TEXT NOT NULL,
    description               TEXT NOT NULL DEFAULT '',
    severity                  TEXT NOT NULL DEFAULT 'standard',
    state                     TEXT NOT NULL DEFAULT 'draft',
    created_by                TEXT,
    scheduled_at              TIMESTAMPTZ,
    maintenance_window_start  TIMESTAMPTZ,
    maintenance_window_end    TIMESTAMPTZ,
    approval_timeout          INTEGER,
    required_approvals        INTEGER NOT NULL DEFAULT 1,
    auto_approve_on_timeout   BOOLEAN NOT NULL DEFAULT false,
    package_name              TEXT,
    package_version           TEXT,
    rollback_version          TEXT,
    failure_reason            TEXT,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at              TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_patch_jobs_org ON patch_jobs (org_id);

CREATE TABLE IF NOT EXISTS patch_approvals (
    id            TEXT PRIMARY KEY,
    patch_job_id  TEXT NOT NULL,
    approver_id   TEXT NOT NULL DEFAULT '',
    approver_name TEXT NOT NULL DEFAULT '',
    decision      TEXT NOT NULL,
    comment       TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_patch_approvals_job ON patch_approvals (patch_job_id);

CREATE TABLE IF NOT EXISTS patch_job_targets (
    id           TEXT PRIMARY KEY,
    patch_job_id TEXT NOT NULL,
    agent_id     TEXT NOT NULL,
    hostname     TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending_approval',
    error_msg    TEXT,
    applied_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_patch_job_targets_job ON patch_job_targets (patch_job_id);

CREATE TABLE IF NOT EXISTS patch_catalog (
    org_id      TEXT NOT NULL DEFAULT '',
    kb          TEXT NOT NULL,
    title       TEXT,
    severity    TEXT,
    cve_ids     JSONB NOT NULL DEFAULT '[]',
    cvss_score  NUMERIC,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, kb)
);

CREATE TABLE IF NOT EXISTS cve_enrichment (
    id               TEXT PRIMARY KEY,
    cve_id           TEXT NOT NULL,
    source           TEXT NOT NULL,
    cvss_v3_score    NUMERIC,
    cvss_v3_severity TEXT,
    description      TEXT,
    published_date   TIMESTAMPTZ,
    last_modified    TIMESTAMPTZ,
    raw_data         JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cve_enrichment_cve ON cve_enrichment (cve_id);

CREATE TABLE IF NOT EXISTS winupdate_kb_state (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL DEFAULT '',
    agent_id   TEXT NOT NULL,
    kb         TEXT NOT NULL,
    state      TEXT NOT NULL DEFAULT 'pending',
    result     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, kb)
);
CREATE INDEX IF NOT EXISTS idx_winupdate_kb_org_agent ON winupdate_kb_state (org_id, agent_id);

CREATE TABLE IF NOT EXISTS report_templates (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL,
    template_id TEXT NOT NULL,
    format      TEXT NOT NULL DEFAULT 'pdf',
    params      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_report_templates_org ON report_templates (org_id);

CREATE TABLE IF NOT EXISTS report_schedules (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    template_id     TEXT NOT NULL,
    cron_expr       TEXT NOT NULL,
    format          TEXT NOT NULL DEFAULT 'pdf',
    params          JSONB,
    delivery_method TEXT NOT NULL DEFAULT '',
    delivery_target TEXT NOT NULL DEFAULT '',
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_run_at     TIMESTAMPTZ,
    next_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_report_schedules_org ON report_schedules (org_id);

CREATE TABLE IF NOT EXISTS report_runs (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL DEFAULT '',
    template_id     TEXT NOT NULL,
    title           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    format          TEXT NOT NULL DEFAULT 'pdf',
    data            JSONB,
    delivery_status TEXT NOT NULL DEFAULT '',
    delivery_target TEXT NOT NULL DEFAULT '',
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    duration_ms     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_report_runs_org ON report_runs (org_id);

CREATE TABLE IF NOT EXISTS mesh_peers (
    agent_id   TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL DEFAULT '',
    public_key TEXT NOT NULL DEFAULT '',
    allowed_ips TEXT NOT NULL DEFAULT '',
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now(),
    status     TEXT NOT NULL DEFAULT 'active'
);
CREATE INDEX IF NOT EXISTS idx_mesh_peers_org ON mesh_peers (org_id);

CREATE TABLE IF NOT EXISTS mesh_sessions (
    session_id  TEXT PRIMARY KEY,
    operator_id TEXT NOT NULL DEFAULT '',
    agent_id    TEXT NOT NULL,
    org_id      TEXT NOT NULL DEFAULT '',
    purpose     TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at    TIMESTAMPTZ,
    status      TEXT NOT NULL DEFAULT 'active'
);
CREATE INDEX IF NOT EXISTS idx_mesh_sessions_agent ON mesh_sessions (agent_id, started_at DESC);

CREATE TABLE IF NOT EXISTS agent_releases (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         TEXT NOT NULL DEFAULT '',
    version        TEXT NOT NULL,
    platform       TEXT NOT NULL,
    binary_sha256  TEXT NOT NULL DEFAULT '',
    signature      TEXT,
    pinned         BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_releases_org ON agent_releases (org_id);
