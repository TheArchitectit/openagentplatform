"""add alert_suppression_windows table (RMM-02)

Revision ID: 0012_rmm02_alert_suppression_windows
Revises: 0011_rmm01_offline_silence_rule
Create Date: 2026-08-24 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

revision: str = "0012_rmm02_alert_suppression_windows"
down_revision: str | None = "0011_rmm01_offline_silence_rule"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "alert_suppression_windows",
        sa.Column("id", sa.Text(), primary_key=True),
        sa.Column("org_id", sa.Text(), nullable=False),
        sa.Column("name", sa.Text(), nullable=False),
        sa.Column("client_id", sa.Text(), nullable=True),
        sa.Column("site_id", sa.Text(), nullable=True),
        sa.Column("start", sa.DateTime(timezone=True), nullable=False),
        sa.Column("end", sa.DateTime(timezone=True), nullable=False),
        sa.Column("recurring", sa.Boolean(), nullable=False, server_default=sa.text("false")),
        sa.Column("weekdays", sa.JSON(), nullable=True),
        sa.Column("timezone", sa.Text(), nullable=False, server_default=sa.text("'UTC'")),
        sa.Column("enabled", sa.Boolean(), nullable=False, server_default=sa.text("true")),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
    )
    op.create_index(
        "ix_alert_suppression_windows_org_id",
        "alert_suppression_windows",
        ["org_id"],
    )
    # Fleet-level alert-suppression windows scope alerts by client. The
    # alerts table predates this feature, so add the column additively.
    op.add_column(
        "alerts",
        sa.Column("client_id", sa.Text(), nullable=True),
    )


def downgrade() -> None:
    op.drop_index(
        "ix_alert_suppression_windows_org_id",
        table_name="alert_suppression_windows",
    )
    op.drop_table("alert_suppression_windows")
    op.drop_column("alerts", "client_id")