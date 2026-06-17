"""add description column to check_definitions

Revision ID: 0010_add_description_to_checks
Revises: 0009_indexes_and_views
Create Date: 2026-06-17 00:00:10

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op


revision: str = "0010_add_description_to_checks"
down_revision: str | None = "0009_indexes_and_views"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "check_definitions",
        sa.Column("description", sa.Text(), nullable=True),
    )


def downgrade() -> None:
    op.drop_column("check_definitions", "description")
