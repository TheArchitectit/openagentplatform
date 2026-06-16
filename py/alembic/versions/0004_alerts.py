"""alert_rules, alert_state_machine, alert_notifications, notification_channels

Revision ID: 0004_alerts
Revises: 0003_checks
Create Date: 2026-06-16 00:00:03

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import ARRAY, JSONB, UUID


revision: str = "0004_alerts"
down_revision: str | None = "0003_checks"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── notification_channels ───────────────────────────────────────
    op.create_table(
        "notification_channels",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("channel_type", sa.String(20), nullable=False),
        sa.Column("config", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("is_active", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "channel_type IN ('email', 'slack', 'webhook', 'pagerduty', 'sms', 'teams', 'discord')",
            name="ck_notification_channels_type",
        ),
    )
    op.create_index("ix_notification_channels_org", "notification_channels", ["org_id"])

    # ── alert_rules ────────────────────────────────────────────────
    op.create_table(
        "alert_rules",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("name", sa.String(255), nullable=False),
        sa.Column("check_id", UUID(as_uuid=True), sa.ForeignKey("check_definitions.id", ondelete="CASCADE"), nullable=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=True),
        sa.Column("severity", sa.String(10), nullable=False, server_default="warning"),
        sa.Column("condition", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("channel_ids", ARRAY(UUID(as_uuid=True)), nullable=False, server_default=sa.text("'{}'::uuid[]")),
        sa.Column("enabled", sa.Boolean, nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "severity IN ('critical', 'high', 'medium', 'low', 'info')",
            name="ck_alert_rules_severity",
        ),
    )
    op.create_index("ix_alert_rules_org", "alert_rules", ["org_id"])
    op.create_index("ix_alert_rules_check", "alert_rules", ["check_id"])
    op.create_index("ix_alert_rules_agent", "alert_rules", ["agent_id"])

    # ── alert_state_machine (active alert instances) ───────────────
    op.create_table(
        "alert_state_machine",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("org_id", UUID(as_uuid=True), sa.ForeignKey("organizations.id", ondelete="CASCADE"), nullable=False),
        sa.Column("rule_id", UUID(as_uuid=True), sa.ForeignKey("alert_rules.id", ondelete="SET NULL"), nullable=True),
        sa.Column("check_id", UUID(as_uuid=True), sa.ForeignKey("check_definitions.id", ondelete="SET NULL"), nullable=True),
        sa.Column("agent_id", UUID(as_uuid=True), sa.ForeignKey("agents.id", ondelete="CASCADE"), nullable=False),
        sa.Column("severity", sa.String(10), nullable=False, server_default="info"),
        sa.Column("state", sa.String(20), nullable=False, server_default="new"),
        sa.Column("dedup_key", sa.String(255), nullable=True, server_default=""),
        sa.Column("message", sa.Text, nullable=True, server_default=""),
        sa.Column("context", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("fired_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("acknowledged_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("acknowledged_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("resolved_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("resolved_by", UUID(as_uuid=True), sa.ForeignKey("users.id", ondelete="SET NULL"), nullable=True),
        sa.Column("snooze_until", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "state IN ('new', 'acknowledged', 'in_progress', 'resolved', 'snoozed', 'closed')",
            name="ck_alert_state_machine_state",
        ),
        sa.CheckConstraint(
            "severity IN ('critical', 'high', 'medium', 'low', 'info')",
            name="ck_alert_state_machine_severity",
        ),
    )
    op.create_index("ix_alert_state_org_state_fired", "alert_state_machine", ["org_id", "state", sa.text("fired_at DESC")])
    op.create_index("ix_alert_state_org_severity_fired", "alert_state_machine", ["org_id", "severity", sa.text("fired_at DESC")])
    op.create_index("ix_alert_state_dedup", "alert_state_machine", ["dedup_key"])
    op.create_index("ix_alert_state_agent", "alert_state_machine", ["agent_id"])
    op.create_index("ix_alert_state_rule", "alert_state_machine", ["rule_id"])

    # ── alert_notifications (delivery log) ─────────────────────────
    op.create_table(
        "alert_notifications",
        sa.Column("id", UUID(as_uuid=True), primary_key=True),
        sa.Column("alert_id", UUID(as_uuid=True), sa.ForeignKey("alert_state_machine.id", ondelete="CASCADE"), nullable=False),
        sa.Column("channel_id", UUID(as_uuid=True), sa.ForeignKey("notification_channels.id", ondelete="SET NULL"), nullable=True),
        sa.Column("status", sa.String(20), nullable=False, server_default="pending"),
        sa.Column("payload", JSONB, nullable=False, server_default=sa.text("'{}'::jsonb")),
        sa.Column("response", sa.Text, nullable=True, server_default=""),
        sa.Column("sent_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("retry_count", sa.Integer, nullable=False, server_default="0"),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.CheckConstraint(
            "status IN ('pending', 'sent', 'delivered', 'failed', 'bounced')",
            name="ck_alert_notifications_status",
        ),
    )
    op.create_index("ix_alert_notifications_alert", "alert_notifications", ["alert_id"])
    op.create_index("ix_alert_notifications_channel", "alert_notifications", ["channel_id"])
    op.create_index("ix_alert_notifications_status", "alert_notifications", ["status"])


def downgrade() -> None:
    op.drop_table("alert_notifications")
    op.drop_table("alert_state_machine")
    op.drop_table("alert_rules")
    op.drop_table("notification_channels")
