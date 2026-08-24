"""add offline_silence_seconds column to alert_rules (RMM-01)

Revision ID: 0011_rmm01_offline_silence_rule
Revises: 0010_add_description_to_checks
Create Date: 2026-08-24 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa

from alembic import op

revision: str = "0011_rmm01_offline_silence_rule"
down_revision: str | None = "0010_add_description_to_checks"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "alert_rules",
        sa.Column("offline_silence_seconds", sa.Integer(), nullable=True),
    )


def downgrade() -> None:
    op.drop_column("alert_rules", "offline_silence_seconds")
