-- Platform schema, part 3: gaps found by the 2026-08-30 ai04 test deploy —
-- tables/columns referenced by live Go store code that 001/002 missed.
-- Same rules as 001: derived from real store SQL (schema constants and
-- INSERT/SELECT column lists), no speculative columns, org_id TEXT.
--
-- Applied fresh installs in order: 001 -> 002 -> 003. All statements are
-- idempotent (IF NOT EXISTS) so re-running against a database that already
-- has them (e.g. ai04, where the a2a tables were applied manually during the
-- deploy) is a no-op.

-- ── A2A registry / task stores ───────────────────────────────────────────────
-- Without these the server fails at init: "a2a: list agent cards: ERROR:
-- relation "a2a_agent_cards" does not exist" (observed on a fresh ai04 DB).
-- The stores do not run EnsureSchema at startup, so this migration is the
-- only thing that creates these tables. DDL extracted verbatim from:
--   a2a/registry/store.go        (AgentCardSchema)
--   a2a/manager/store_types.go   (task + artifact CREATE TABLE constants)

CREATE TABLE IF NOT EXISTS a2a_agent_cards (
	url                TEXT         PRIMARY KEY,
	name               TEXT         NOT NULL DEFAULT '',
	description        TEXT         NOT NULL DEFAULT '',
	version            TEXT         NOT NULL DEFAULT '',
	provider           JSONB        NOT NULL DEFAULT '{}'::jsonb,
	skills             JSONB        NOT NULL DEFAULT '[]'::jsonb,
	streaming          BOOLEAN      NOT NULL DEFAULT FALSE,
	push_notifications BOOLEAN      NOT NULL DEFAULT FALSE,
	auth_schemes       JSONB        NOT NULL DEFAULT '[]'::jsonb,
	last_heartbeat     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_agent_cards_name ON a2a_agent_cards (name);

CREATE TABLE IF NOT EXISTS a2a_tasks (
	id             UUID         PRIMARY KEY,
	session_id     TEXT         NOT NULL DEFAULT '',
	status         TEXT         NOT NULL DEFAULT 'pending',
	messages       JSONB        NOT NULL DEFAULT '[]'::jsonb,
	metadata       JSONB        NOT NULL DEFAULT '{}'::jsonb,
	agent_card_url TEXT         NOT NULL DEFAULT '',
	version        INTEGER      NOT NULL DEFAULT 1,
	created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_session_id ON a2a_tasks (session_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_status     ON a2a_tasks (status);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_agent_card ON a2a_tasks (agent_card_url);

CREATE TABLE IF NOT EXISTS a2a_artifacts (
	id         TEXT         NOT NULL,
	task_id    UUID         NOT NULL REFERENCES a2a_tasks(id) ON DELETE CASCADE,
	parts      JSONB        NOT NULL DEFAULT '[]'::jsonb,
	metadata   JSONB        NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	PRIMARY KEY (id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_a2a_artifacts_task_id ON a2a_artifacts (task_id);

-- ── policies.deleted ─────────────────────────────────────────────────────────
-- internal/policy/store_crud.go scopes every read with "deleted = false" and
-- soft-deletes via "UPDATE policies SET deleted = true", but 001 created the
-- table without the column. Consequence on a fresh DB: the default-policy
-- seeder fails at startup (ERROR: column "deleted" does not exist), so no
-- built-in policies exist until fixed. Observed on the 2026-08-30 ai04 deploy.

ALTER TABLE policies ADD COLUMN IF NOT EXISTS deleted BOOLEAN NOT NULL DEFAULT false;
