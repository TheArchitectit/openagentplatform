-- Migration 004: first-boot organization bootstrap (auth-rbac spec §14)
--
-- The identity layer is external (OIDC/dex) while tenancy is internal: on a
-- fresh database no org exists and no IdP subject is bound to one, so every
-- org-scoped route 400s with "org context required" (AI04_DEPLOY_NOTES §5.1).
-- These two tables make the first-admin flow possible:
--
--   user_org_bindings  maps an IdP subject to its org + effective role;
--                      consulted at login (spec §14.4) when the ID token
--                      carries no org_id claim.
--   app_state          single-row key/value control state; the
--                      'bootstrap_complete' row is the one-time latch that
--                      permanently closes the bootstrap endpoint (§14.3).
--
-- org_id is TEXT (the tenant UUID rendered as a string), matching every
-- other org-scoped column in 001 — the stores pass strings, and the schema
-- deliberately carries no FKs (application-level integrity, 001 header).

CREATE TABLE IF NOT EXISTS user_org_bindings (
    subject    TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_org_bindings_org ON user_org_bindings (org_id);

CREATE TABLE IF NOT EXISTS app_state (
    key    TEXT PRIMARY KEY,
    value  TEXT NOT NULL,
    set_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
