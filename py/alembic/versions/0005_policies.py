"""policy_definitions, policy_assignments, policy_violations

Revision ID: 0005_policies
Revises: 0004_alerts
Create Date: 2026-06-16 00:00:04

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import JSONB, UUID


revision: str = "0005_policies"
down_revision: str | None = "0004_alerts"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── policy_definitions ─────────────────────────────────────────
    op.create_table(
        "policy_definitions",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("description", sa.Text, nullable=True, server_default=""),
        sa.Column("enforcement_mode", sa.String(20), nullable=False, server_default="inherit"),
        sa.Column("priority", sa.Integer, nullable=False, server_default="0"),
        sa.Column("checks", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("automated_tasks", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("win_update_policy", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("alert_routing", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("is_active", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "enforcement_mode IN ('inherit', 'enforce', 'exclude')",
            name="ck_policy_definitions_mode",
        ),
    )
    op.create_index("ix_policy_definitions_org_priority", "policy_definitions", ["org_id", "priority"])
    op.create_index("ix_policy_definitions_org_active", "policy_definitions", ["org_id", "is_active"])

    # ── policy_assignments (scope) ─────────────────────────────────
    op.create_table(
        "policy_assignments",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("policy_id", UUID(as_uuid=True), sa.ForeignKey("policy_definitions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("scope_type", sa.String(10), nullable=False),
        sa.Column("client_id", UUID(as_uuid=True), sa.ForeignKey("clients.id", ondelete="CASCADE"), nullable=True),
        sa.Column("site_id", UUID(as_uuid=True), sa.ForeignKey("sites.id", ondelete="CASCADE"), nullable=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "scope_type IN ('client', 'site', 'agent')",
            name="ck_policy_assignments_scope_type",
        ),
        sa.CheckConstraint(
            "(client_id IS NOT NULL)::int + (site_id IS NOT NULL)::int + (agent_id IS NOT NULL)::int = 1",
            name="ck_policy_assignments_xor",
        ),
    )
    op.create_index("ix_policy_assignments_policy", "policy_assignments", ["policy_id"])
    op.create_index("ix_policy_assignments_client", "policy_assignments", ["client_id"])
    op.create_index("ix_policy_assignments_site", "policy_assignments", ["site_id"])
    op.create_index("ix_policy_assignments_agent", "policy_assignments", ["agent_id"])

    # ── policy_violations ──────────────────────────────────────────
    op.create_table(
        "policy_violations",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("policy_id", UUID(as_uuid=True), sa.ForeignKey("policy_definitions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=True),
        sa.Column("rule_key", sa.String(255), nullable=False),
        sa.Column("severity", sa.String(10), nullable=False, server_default="medium"),
        sa.Column("detail", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("state", sa.String(20), nullable=False, server_default="open"),
        sa.Column("detected_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("resolved_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("resolved_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.CheckConstraint(
            "severity IN ('critical', 'high', 'medium', 'low', 'info')",
            name="ck_policy_violations_severity",
        ),
        sa.CheckConstraint(
            "state IN ('open', 'acknowledged', 'resolved', 'suppressed')",
            name="ck_policy_violations_state",
        ),
    )
    op.create_index("ix_policy_violations_org_state", "policy_violations", ["org_id", "state"])
    op.create_index("ix_policy_violations_policy", "policy_violations", ["policy_id"])
    op.create_index("ix_policy_violations_agent", "policy_violations", ["agent_id"])
    op.create_index("ix_policy_violations_detected", "policy_violations", ["detected_at"])


def downgrade() -> None:
    op.drop_table("policy_violations")
    op.drop_table("policy_assignments")
    op.drop_table("policy_definitions")
