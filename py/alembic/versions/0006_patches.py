"""patch_catalog, patch_jobs, patch_job_targets

Revision ID: 0006_patches
Revises: 0005_policies
Create Date: 2026-06-16 00:00:05

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import JSONB, UUID


revision: str = "0006_patches"
down_revision: str | None = "0005_policies"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── patch_catalog ──────────────────────────────────────────────
    op.create_table(
        "patch_catalog",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("kb", sa.String(50), nullable=False, server_default=""),
        sa.Column("guid", sa.String(255), nullable=False, server_default=""),
        sa.Column("title", sa.Text, nullable=True, server_default=""),
        sa.Column("severity", sa.String(10), nullable=False, server_default="other"),
        sa.Column("cve_ids", JSONB, nullable=False, server_default=sa.text("'[]'::jsonb")),
        sa.Column("product", sa.String(255), nullable=True, server_default=""),
        sa.Column("classification", sa.String(100), nullable=True, server_default=""),
        sa.Column("metadata", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("is_superseded", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "severity IN ('critical', 'important', 'moderate', 'low', 'other')",
            name="ck_patch_catalog_severity",
        ),
        sa.UniqueConstraint("org_id", "kb", name="uq_patch_catalog_org_kb"),
    )
    op.create_index("ix_patch_catalog_org_severity", "patch_catalog", ["org_id", "severity"])
    op.create_index("ix_patch_catalog_product", "patch_catalog", ["product"])

    # ── patch_jobs ─────────────────────────────────────────────────
    op.create_table(
        "patch_jobs",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("description", sa.Text, nullable=True, server_default=""),
        sa.Column("status", sa.String(20), nullable=False, server_default="pending"),
        sa.Column("scheduled_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("completed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "status IN ('pending', 'scanning', 'approved', 'running', 'completed', 'failed', 'cancelled')",
            name="ck_patch_jobs_status",
        ),
    )
    op.create_index("ix_patch_jobs_org_status", "patch_jobs", ["org_id", "status"])
    op.create_index("ix_patch_jobs_scheduled", "patch_jobs", ["scheduled_at"])

    # ── patch_job_targets (per-agent install records) ─────────────
    op.create_table(
        "patch_job_targets",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("patch_job_id", UUID(as_uuid=True), sa.ForeignKey("patch_jobs.id", ondelete="CASCADE"), nullable=False),
        sa.Column("patch_catalog_id", UUID(as_uuid=True), sa.ForeignKey("patch_catalog.id", ondelete="CASCADE"), nullable=False),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("state", sa.String(20), nullable=False, server_default="scanned"),
        sa.Column("action", sa.String(20), nullable=False, server_default="inherit"),
        sa.Column("installed", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("downloaded", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column("result", sa.Text, nullable=True, server_default=""),
        sa.Column("approved_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("approved_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("date_installed", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "state IN ('scanned', 'pending_approval', 'approved', 'rejected', 'installing', 'installed', 'failed', 'reboot_required')",
            name="ck_patch_job_targets_state",
        ),
        sa.CheckConstraint(
            "action IN ('inherit', 'approve', 'ignore', 'nothing')",
            name="ck_patch_job_targets_action",
        ),
        sa.UniqueConstraint("agent_id", "patch_catalog_id", name="uq_patch_job_targets_agent_patch"),
    )
    op.create_index("ix_patch_job_targets_job", "patch_job_targets", ["patch_job_id"])
    op.create_index("ix_patch_job_targets_agent", "patch_job_targets", ["agent_id"])
    op.create_index("ix_patch_job_targets_org_state", "patch_job_targets", ["org_id", "state"])


def downgrade() -> None:
    op.drop_table("patch_job_targets")
    op.drop_table("patch_jobs")
    op.drop_table("patch_catalog")
