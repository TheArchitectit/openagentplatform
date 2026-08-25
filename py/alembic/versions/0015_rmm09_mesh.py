"""add mesh tunnel fabric tables (RMM-09)

Adds org-scoped tables for the WireGuard mesh control plane:
- mesh_peers: per-agent WireGuard public keys + allowed IPs
- mesh_sessions: operator-initiated tunnel sessions (audit trail)
- agent_releases: Ed25519-signed agent binaries for self-update

Revision ID: 0015_rmm09_mesh
Revises: 0014_rmm05_cve_enrichment
Create Date: 2026-08-24 00:00:00

"""

from collections.abc import Sequence

import sqlalchemy as sa
from sqlalchemy.dialects.postgresql import UUID

from alembic import op

revision: str = "0015_rmm09_mesh"
down_revision: str | None = "0014_rmm05_cve_enrichment"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "mesh_peers",
        sa.Column("agent_id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), nullable=False),
        sa.Column("public_key", sa.Text, nullable=False),
        sa.Column("allowed_ips", sa.Text, nullable=False),
        sa.Column("last_seen", sa.DateTime(timezone=True), nullable=True),
        sa.Column("status", sa.Text, nullable=False, server_default="active"),
    )
    op.create_index("ix_mesh_peers_org_id", "mesh_peers", ["org_id"])

    op.create_table(
        "mesh_sessions",
        sa.Column("session_id", UUID(as_uuid=True), primary_key=True),
        sa.Column("operator_id", UUID(as_uuid=True), nullable=False),
        sa.Column("agent_id", UUID(as_uuid=True), nullable=False),
        sa.Column("org_id", UUID(as_uuid=True), nullable=False),
        sa.Column("purpose", sa.Text, nullable=False),
        sa.Column(
            "started_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        sa.Column("ended_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("status", sa.Text, nullable=False, server_default="active"),
    )
    op.create_index(
        "ix_mesh_sessions_org_agent", "mesh_sessions", ["org_id", "agent_id"]
    )

    op.create_table(
        "agent_releases",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), nullable=False),
        sa.Column("version", sa.Text, nullable=False),
        sa.Column("platform", sa.Text, nullable=False),
        sa.Column("binary_sha256", sa.Text, nullable=False),
        sa.Column("signature", sa.Text, nullable=False),
        sa.Column("pinned", sa.Boolean, nullable=False, server_default=sa.false()),
        sa.Column(
            "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
    )
    op.create_index("ix_agent_releases_org_pinned", "agent_releases", ["org_id", "pinned"])


def downgrade() -> None:
    op.drop_index("ix_agent_releases_org_pinned", table_name="agent_releases")
    op.drop_table("agent_releases")
    op.drop_index("ix_mesh_sessions_org_agent", table_name="mesh_sessions")
    op.drop_table("mesh_sessions")
    op.drop_index("ix_mesh_peers_org_id", table_name="mesh_peers")
    op.drop_table("mesh_peers")
