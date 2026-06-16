"""clients, sites, agents, agent_tags

Revision ID: 0002_clients_sites_agents
Revises: 0001_orgs_and_users
Create Date: 2026-06-16 00:00:01

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import ARRAY, ENUM, JSONB, UUID


revision: str = "0002_clients_sites_agents"
down_revision: str | None = "0001_orgs_and_users"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


# ── agent_state enum ───────────────────────────────────────────────
agent_state_enum = ENUM(
    "pending",
    "online",
    "offline",
    "degraded",
    "uninstalled",
    name="agent_state",
    create_type=True,
)


def upgrade() -> None:
    # ── clients ─────────────────────────────────────────────────────
    op.create_table(
        "clients",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("contact_email", sa.String(255), nullable=True),
        sa.Column("contact_phone", sa.String(50), nullable=True),
        sa.Column("metadata", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("is_active", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("org_id", "name", name="uq_client_org_name"),
    )
    op.create_index("ix_clients_org", "clients", ["org_id"])

    # ── sites ───────────────────────────────────────────────────────
    op.create_table(
        "sites",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("client_id", UUID(as_uuid=True), sa.ForeignKey("clients.id", ondelete="CASCADE"), nullable=True),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("region", sa.String(100), nullable=True),
        sa.Column("address", sa.Text, nullable=True),
        sa.Column("metadata", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("is_active", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("org_id", "client_id", "name", name="uq_site_org_client_name"),
    )
    op.create_index("ix_sites_org", "sites", ["org_id"])
    op.create_index("ix_sites_client", "sites", ["client_id"])

    # ── agents (uses agent_state enum) ──────────────────────────────
    op.create_table(
        "agents",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("site_id", UUID(as_uuid=True), sa.ForeignKey("sites.id", ondelete="CASCADE"), nullable=True),
        sa.Column("client_id", UUID(as_uuid=True), sa.ForeignKey("clients.id", ondelete="SET NULL"), nullable=True),
        sa.Column("agent_id", sa.String(255), nullable=False),
        sa.Column("hostname", sa.String(255), nullable=False),
        sa.Column("platform", sa.String(20), nullable=False, server_default="unknown"),
        sa.Column("status", agent_state_enum, nullable=False, server_default="pending"),
        sa.Column("last_seen", sa.DateTime(timezone=True), nullable=True),
        sa.Column("operating_system", sa.String(255), nullable=True, server_default=""),
        sa.Column("goarch", sa.String(10), nullable=True, server_default=""),
        sa.Column("total_ram", sa.Integer, nullable=True, server_default="0"),
        sa.Column("disks", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("services", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("wmi_detail", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("public_ip", sa.String(45), nullable=True),
        sa.Column("boot_time", sa.DateTime(timezone=True), nullable=True),
        sa.Column("logged_in_username", sa.String(255), nullable=True, server_default=""),
        sa.Column("needs_reboot", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("inventory", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("tags", ARRAY(sa.String), nullable=False, server_default=sa.text("'{}'::varchar[]")),
        sa.Column("mesh_token", sa.String(255), nullable=True, server_default=""),
        sa.Column("version", sa.String(50), nullable=False, server_default="0.0.0"),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("deleted_at", sa.DateTime(timezone=True), nullable=True),
        sa.UniqueConstraint("org_id", "agent_id", name="uq_agent_org_agent_id"),
        sa.CheckConstraint(
            "platform IN ('windows', 'linux', 'macos', 'unknown')",
            name="ck_agents_platform",
        ),
    )
    op.create_index("ix_agents_org_status", "agents", ["org_id", "status"])
    op.create_index("ix_agents_org_client_site", "agents", ["org_id", "client_id", "site_id"])
    op.create_index("ix_agents_org_platform", "agents", ["org_id", "platform"])
    op.create_index("ix_agents_org_last_seen", "agents", ["org_id", "last_seen"])
    op.create_index("ix_agents_tags", "agents", ["tags"], postgresql_using="gin")
    op.create_index("ix_agents_site", "agents", ["site_id"])

    # ── agent_tags (normalized tag table for rich queries) ─────────
    op.create_table(
        "agent_tags",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("tag", sa.String(100), nullable=False),
        sa.Column("color", sa.String(7), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("agent_id", "tag", name="uq_agent_tag"),
    )
    op.create_index("ix_agent_tags_tag", "agent_tags", ["org_id", "tag"])


def downgrade() -> None:
    op.drop_table("agent_tags")
    op.drop_table("agents")
    op.drop_table("sites")
    op.drop_table("clients")
    op.execute("DROP TYPE IF EXISTS agent_state")
