"""script_definitions, script_runs

Revision ID: 0007_scripts
Revises: 0006_patches
Create Date: 2026-06-16 00:00:06

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import JSONB, UUID


revision: str = "0007_scripts"
down_revision: str | None = "0006_patches"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── script_definitions ─────────────────────────────────────────
    op.create_table(
        "script_definitions",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("description", sa.Text, nullable=True, server_default=""),
        sa.Column("body", sa.Text, nullable=False),
        sa.Column("runtime", sa.String(20), nullable=False, server_default="shell"),
        sa.Column("arguments", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("env_vars", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("timeout_seconds", sa.Integer, nullable=False, server_default="300"),
        sa.Column("supported_platforms", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("category", sa.String(100), nullable=True, server_default=""),
        sa.Column("is_template", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("created_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "runtime IN ('powershell', 'cmd', 'python', 'shell', 'nushell', 'deno')",
            name="ck_script_definitions_runtime",
        ),
        sa.CheckConstraint("timeout_seconds > 0", name="ck_script_definitions_timeout_positive"),
    )
    op.create_index("ix_script_definitions_org", "script_definitions", ["org_id"])
    op.create_index("ix_script_definitions_org_runtime", "script_definitions", ["org_id", "runtime"])

    # ── script_runs ────────────────────────────────────────────────
    op.create_table(
        "script_runs",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("script_id", UUID(as_uuid=True), sa.ForeignKey("script_definitions.id", ondelete="SET NULL"), nullable=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("runtime", sa.String(20), nullable=False),
        sa.Column("state", sa.String(20), nullable=False, server_default="pending"),
        sa.Column("arguments", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("env_vars", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("stdout", sa.Text, nullable=True, server_default=""),
        sa.Column("stderr", sa.Text, nullable=True, server_default=""),
        sa.Column("exit_code", sa.Integer, nullable=True),
        sa.Column("execution_time", sa.Float, nullable=True, server_default="0.0"),
        sa.Column("requested_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("completed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "state IN ('pending', 'running', 'success', 'error', 'timeout', 'cancelled')",
            name="ck_script_runs_state",
        ),
        sa.CheckConstraint(
            "runtime IN ('powershell', 'cmd', 'python', 'shell', 'nushell', 'deno')",
            name="ck_script_runs_runtime",
        ),
    )
    op.create_index("ix_script_runs_org_state", "script_runs", ["org_id", "state"])
    op.create_index("ix_script_runs_agent", "script_runs", ["agent_id"])
    op.create_index("ix_script_runs_script", "script_runs", ["script_id"])
    op.create_index("ix_script_runs_created", "script_runs", ["created_at"])


def downgrade() -> None:
    op.drop_table("script_runs")
    op.drop_table("script_definitions")
