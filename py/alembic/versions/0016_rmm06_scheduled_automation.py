"""add scheduled automation tables (RMM-06)

Adds org-scoped tables for cron-scheduled automated tasks:

- automated_tasks: one row per scheduled action bound to an org. Unlike the
  Policy.automated_tasks JSONB column (py/alembic/versions/0005_policies.py),
  this is a first-class table so the scheduler can query due tasks with a
  single indexed `next_run_at` lookup rather than re-parsing a JSONB array.

Cron grammar is cron-style recurrence (reuse internal/reports parseSimpleCron
+ @hourly/@daily/@weekly/@monthly aliases). The 21-bit schedule_bitmask is
rejected. See docs/sprints/RMM-06_SCHEDULED_AUTOMATION_DECISION.md.

Revision ID: 0016_rmm06_scheduled_automation
Revises: 0015_rmm09_mesh
Create Date: 2026-08-25 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects.postgresql import UUID

from alembic import op

revision: str = "0016_rmm06_scheduled_automation"
down_revision: str | None = "0015_rmm09_mesh"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "automated_tasks",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), nullable=False),
        sa.Column("name", sa.Text, nullable=False),
        sa.Column("enabled", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("cron_expr", sa.Text, nullable=False),
        sa.Column("action", sa.Text, nullable=False),
        sa.Column("params", sa.JSON, nullable=True),
        sa.Column("timezone", sa.Text, nullable=False, server_default="UTC"),
        sa.Column("next_run_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("last_run_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("last_status", sa.Text, nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False,
                  server_default=sa.func.now()),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False,
                  server_default=sa.func.now()),
    )
    op.create_index("ix_automated_tasks_org_id", "automated_tasks", ["org_id"])
    op.create_index("ix_automated_tasks_next_run_at", "automated_tasks", ["next_run_at"])


def downgrade() -> None:
    op.drop_index("ix_automated_tasks_next_run_at", "automated_tasks")
    op.drop_index("ix_automated_tasks_org_id", "automated_tasks")
    op.drop_table("automated_tasks")
