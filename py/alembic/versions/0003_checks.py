"""check_definitions, check_assignments, check_results (timescaledb hypertable)

Revision ID: 0003_checks
Revises: 0002_clients_sites_agents
Create Date: 2026-06-16 00:00:02

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import JSONB, UUID


revision: str = "0003_checks"
down_revision: str | None = "0002_clients_sites_agents"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── check_definitions (polymorphic via check_type + config JSONB) ─
    op.create_table(
        "check_definitions",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("check_type", sa.String(20), nullable=False),
        sa.Column("interval_seconds", sa.Integer, nullable=False, server_default="300"),
        sa.Column("timeout_seconds", sa.Integer, nullable=False, server_default="120"),
        sa.Column("config", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("fail_threshold", sa.Integer, nullable=False, server_default="1"),
        sa.Column("warning_threshold", sa.Float, nullable=True),
        sa.Column("error_threshold", sa.Float, nullable=True),
        sa.Column("alert_severity", sa.String(10), nullable=False, server_default="warning"),
        sa.Column("is_template", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("last_status", sa.String(20), nullable=True, server_default="pending"),
        sa.Column("enabled", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "check_type IN ('ping', 'cpu', 'memory', 'disk', 'service', 'script', 'event_log', 'process', 'wmi', 'custom')",
            name="ck_check_definitions_type",
        ),
        sa.CheckConstraint("interval_seconds >= 30", name="ck_check_definitions_interval_min"),
        sa.CheckConstraint("timeout_seconds <= 3600", name="ck_check_definitions_timeout_max"),
    )
    op.create_index("ix_check_definitions_org_type", "check_definitions", ["org_id", "check_type"])
    op.create_index("ix_check_definitions_org_template", "check_definitions", ["org_id", "is_template"])
    op.create_index("ix_check_definitions_org_status", "check_definitions", ["org_id", "last_status"])

    # ── check_assignments (agent ↔ check junction) ─────────────────
    op.create_table(
        "check_assignments",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("check_id", UUID(as_uuid=True), sa.ForeignKey("check_definitions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("is_enabled", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("next_run_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("last_run_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("override_config", JSONB, nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("agent_id", "check_id", name="uq_check_assignment_agent_check"),
    )
    op.create_index("ix_check_assignments_agent", "check_assignments", ["agent_id"])
    op.create_index("ix_check_assignments_check", "check_assignments", ["check_id"])
    op.create_index("ix_check_assignments_next_run", "check_assignments", ["next_run_at"])

    # ── check_results (timescaledb hypertable) ──────────────────────
    op.create_table(
        "check_results",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("check_assignment_id", UUID(as_uuid=True), sa.ForeignKey("check_assignments.id", ondelete="CASCADE"), nullable=False),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("check_id", UUID(as_uuid=True), sa.ForeignKey("check_definitions.id", ondelete="CASCADE"), nullable=False),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("status", sa.String(20), nullable=False),
        sa.Column("value", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("duration_ms", sa.Integer, nullable=False, server_default="0"),
        sa.Column("execution_start", sa.DateTime(timezone=True), nullable=False),
        sa.Column("execution_end", sa.DateTime(timezone=True), nullable=False),
        sa.Column("error_message", sa.Text, nullable=True, server_default=""),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "status IN ('passing', 'failing', 'warning', 'pending', 'paused', 'error')",
            name="ck_check_results_status",
        ),
        sa.CheckConstraint("duration_ms >= 0", name="ck_check_results_duration_nonneg"),
    )
    op.create_index("ix_check_results_agent_check_time", "check_results", ["agent_id", "check_id", sa.text("execution_start DESC")])
    op.create_index("ix_check_results_org_status_time", "check_results", ["org_id", "status", sa.text("execution_start DESC")])
    op.create_index("ix_check_results_assignment", "check_results", ["check_assignment_id"])

    # Convert check_results to a TimescaleDB hypertable on execution_start.
    # If timescaledb extension is unavailable this is a no-op.
    op.execute(
        "SELECT create_hypertable("
        "'check_results', 'execution_start',"
        " chunk_time_interval => INTERVAL '1 day',"
        " if_not_exists => TRUE)"
    )


def downgrade() -> None:
    op.drop_table("check_results")
    op.drop_table("check_assignments")
    op.drop_table("check_definitions")
