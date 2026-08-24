"""add winupdate_kb_state table (RMM-03)

Revision ID: 0013_rmm03_winupdate_kb_state
Revises: 0012_rmm02_alert_suppression_windows
Create Date: 2026-08-24 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects.postgresql import UUID

from alembic import op

revision: str = "0013_rmm03_winupdate_kb_state"
down_revision: str | None = "0012_rmm02_alert_suppression_windows"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "winupdate_kb_state",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column(
            "org_id",
            UUID(as_uuid=True),
            sa.ForeignKey("organizations.id", ondelete="CASCADE"),
            nullable=False,
        ),
        sa.Column(
            "agent_id",
            UUID(as_uuid=True),
            sa.ForeignKey("agents.id", ondelete="CASCADE"),
            nullable=False,
        ),
        sa.Column("kb", sa.String(50), nullable=False),
        sa.Column(
            "state",
            sa.String(20),
            nullable=False,
            server_default=sa.text("'scanned'"),
        ),
        sa.Column("result", sa.Text(), nullable=False, server_default=sa.text("''")),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            server_default=sa.func.now(),
            nullable=False,
        ),
        sa.Column(
            "updated_at",
            sa.DateTime(timezone=True),
            server_default=sa.func.now(),
            nullable=False,
        ),
        # Canonical 8-state vocabulary, shared with patch_job_targets.
        sa.CheckConstraint(
            "state IN ('scanned', 'pending_approval', 'approved', 'rejected', "
            "'installing', 'installed', 'failed', 'reboot_required')",
            name="ck_winupdate_kb_state_state",
        ),
        sa.UniqueConstraint("agent_id", "kb", name="uq_winupdate_kb_state_agent_kb"),
    )
    op.create_index(
        "ix_winupdate_kb_state_org_state",
        "winupdate_kb_state",
        ["org_id", "state"],
    )
    op.create_index(
        "ix_winupdate_kb_state_agent",
        "winupdate_kb_state",
        ["agent_id"],
    )


def downgrade() -> None:
    op.drop_index("ix_winupdate_kb_state_agent", table_name="winupdate_kb_state")
    op.drop_index("ix_winupdate_kb_state_org_state", table_name="winupdate_kb_state")
    op.drop_table("winupdate_kb_state")
