"""add cve_enrichment table (RMM-05)

Adds a cve_enrichment table for NVD-sourced CVE metadata (scores,
descriptions) and a cvss_score column on patch_catalog.

Revision ID: 0014_rmm05_cve_enrichment
Revises: 0013_rmm03_winupdate_kb_state
Create Date: 2026-08-24 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects.postgresql import JSONB, UUID

from alembic import op

revision: str = "0014_rmm05_cve_enrichment"
down_revision: str | None = "0013_rmm03_winupdate_kb_state"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "cve_enrichment",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("cve_id", sa.String(20), nullable=False),
        sa.Column("source", sa.String(20), nullable=False, server_default="nvd"),
        sa.Column("cvss_v3_score", sa.Float, nullable=True),
        sa.Column("cvss_v3_severity", sa.String(10), nullable=True),
        sa.Column("description", sa.Text, nullable=True, server_default=""),
        sa.Column("published_date", sa.DateTime(timezone=True), nullable=True),
        sa.Column("last_modified", sa.DateTime(timezone=True), nullable=True),
        sa.Column("raw_data", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column(
            "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        sa.Column(
            "updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        sa.UniqueConstraint("cve_id", name="uq_cve_enrichment_cve_id"),
    )
    op.create_index("ix_cve_enrichment_cve_id", "cve_enrichment", ["cve_id"])
    op.create_index("ix_cve_enrichment_cvss_severity", "cve_enrichment", ["cvss_v3_severity"])

    # Add cvss_score to patch_catalog for quick lookup without joining.
    op.add_column("patch_catalog", sa.Column("cvss_score", sa.Float, nullable=True))
    op.create_index("ix_patch_catalog_cvss_score", "patch_catalog", ["cvss_score"])


def downgrade() -> None:
    op.drop_index("ix_patch_catalog_cvss_score", table_name="patch_catalog")
    op.drop_column("patch_catalog", "cvss_score")
    op.drop_index("ix_cve_enrichment_cvss_severity", table_name="cve_enrichment")
    op.drop_index("ix_cve_enrichment_cve_id", table_name="cve_enrichment")
    op.drop_table("cve_enrichment")