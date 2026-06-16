"""composite indexes and materialized views for dashboard

Revision ID: 0009_indexes_and_views
Revises: 0008_audit
Create Date: 2026-06-16 00:00:08

"""
from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op


revision: str = "0009_indexes_and_views"
down_revision: str | None = "0008_audit"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    # ── Additional composite indexes for dashboard queries ─────────

    # Agents: org + online status for fleet overview
    op.create_index(
        "ix_agents_org_online_lastseen",
        "agents",
        ["org_id", "last_seen"],
        postgresql_where=sa.text("status = 'online' AND deleted_at IS NULL"),
    )

    # Sites: org + active for filtering
    op.create_index(
        "ix_sites_org_active",
        "sites",
        ["org_id"],
        postgresql_where=sa.text("is_active = true"),
    )

    # Alerts: org + open states for active alert board
    op.create_index(
        "ix_alert_state_org_active",
        "alert_state_machine",
        ["org_id", sa.text("fired_at DESC")],
        postgresql_where=sa.text("state IN ('new', 'acknowledged', 'in_progress', 'snoozed')"),
    )

    # Script runs: org + recent for activity feed
    op.create_index(
        "ix_script_runs_org_recent",
        "script_runs",
        ["org_id", sa.text("created_at DESC")],
    )

    # Patch targets: org + pending states
    op.create_index(
        "ix_patch_targets_org_pending",
        "patch_job_targets",
        ["org_id", "state"],
        postgresql_where=sa.text("state IN ('scanned', 'pending_approval', 'approved', 'installing')"),
    )

    # Policy violations: org + open
    op.create_index(
        "ix_policy_violations_org_open",
        "policy_violations",
        ["org_id", sa.text("detected_at DESC")],
        postgresql_where=sa.text("state IN ('open', 'acknowledged')"),
    )

    # ── Materialized view: agent_fleet_summary ─────────────────────
    op.execute(
        """
        CREATE MATERIALIZED VIEW mv_agent_fleet_summary AS
        SELECT
            a.org_id,
            a.client_id,
            a.site_id,
            a.status,
            a.platform,
            COUNT(*) AS agent_count,
            COUNT(*) FILTER (WHERE a.status = 'online') AS online_count,
            COUNT(*) FILTER (WHERE a.status = 'offline') AS offline_count,
            COUNT(*) FILTER (WHERE a.status = 'degraded') AS degraded_count,
            COUNT(*) FILTER (WHERE a.needs_reboot = true) AS reboot_needed_count,
            MAX(a.last_seen) AS last_seen_max,
            now() AS refreshed_at
        FROM agents a
        WHERE a.deleted_at IS NULL
        GROUP BY a.org_id, a.client_id, a.site_id, a.status, a.platform;
        """
    )
    op.execute(
        "CREATE UNIQUE INDEX uq_mv_agent_fleet_summary"
        " ON mv_agent_fleet_summary (org_id, client_id, site_id, status, platform)"
    )

    # ── Materialized view: alert_dashboard ─────────────────────────
    op.execute(
        """
        CREATE MATERIALIZED VIEW mv_alert_dashboard AS
        SELECT
            asm.org_id,
            asm.severity,
            asm.state,
            COUNT(*) AS alert_count,
            COUNT(*) FILTER (WHERE asm.state = 'new') AS new_count,
            COUNT(*) FILTER (WHERE asm.state = 'acknowledged') AS acknowledged_count,
            COUNT(*) FILTER (WHERE asm.state = 'in_progress') AS in_progress_count,
            MIN(asm.fired_at) AS oldest_fired_at,
            MAX(asm.fired_at) AS newest_fired_at,
            now() AS refreshed_at
        FROM alert_state_machine asm
        WHERE asm.state IN ('new', 'acknowledged', 'in_progress', 'snoozed')
        GROUP BY asm.org_id, asm.severity, asm.state;
        """
    )
    op.execute(
        "CREATE UNIQUE INDEX uq_mv_alert_dashboard"
        " ON mv_alert_dashboard (org_id, severity, state)"
    )

    # ── Materialized view: check_result_hourly (for timeseries dashboards) ──
    op.execute(
        """
        CREATE MATERIALIZED VIEW mv_check_result_hourly AS
        SELECT
            cr.org_id,
            cr.agent_id,
            cr.check_id,
            date_trunc('hour', cr.execution_start) AS bucket,
            COUNT(*) AS total_runs,
            COUNT(*) FILTER (WHERE cr.status = 'passing') AS passing_count,
            COUNT(*) FILTER (WHERE cr.status = 'failing') AS failing_count,
            COUNT(*) FILTER (WHERE cr.status = 'warning') AS warning_count,
            COUNT(*) FILTER (WHERE cr.status = 'error') AS error_count,
            AVG(cr.duration_ms)::float AS avg_duration_ms,
            MAX(cr.duration_ms) AS max_duration_ms,
            MIN(cr.duration_ms) AS min_duration_ms,
            now() AS refreshed_at
        FROM check_results cr
        GROUP BY cr.org_id, cr.agent_id, cr.check_id, date_trunc('hour', cr.execution_start);
        """
    )
    op.execute(
        "CREATE UNIQUE INDEX uq_mv_check_result_hourly"
        " ON mv_check_result_hourly (org_id, agent_id, check_id, bucket)"
    )

    # ── Materialized view: patch_compliance ────────────────────────
    op.execute(
        """
        CREATE MATERIALIZED VIEW mv_patch_compliance AS
        SELECT
            a.org_id,
            a.client_id,
            a.site_id,
            COUNT(DISTINCT a.id) AS total_agents,
            COUNT(DISTINCT pjt.agent_id) FILTER (
                WHERE pjt.state IN ('pending_approval', 'approved', 'installing')
            ) AS agents_with_pending_patches,
            COUNT(DISTINCT pjt.agent_id) FILTER (
                WHERE pjt.state = 'installed'
            ) AS agents_with_patches_installed,
            COUNT(DISTINCT pjt.agent_id) FILTER (
                WHERE pjt.severity = 'critical'
                AND pjt.state IN ('scanned', 'pending_approval')
            ) AS agents_with_critical_pending,
            now() AS refreshed_at
        FROM agents a
        LEFT JOIN patch_job_targets pjt ON pjt.agent_id = a.id
        WHERE a.deleted_at IS NULL
        GROUP BY a.org_id, a.client_id, a.site_id;
        """
    )
    op.execute(
        "CREATE UNIQUE INDEX uq_mv_patch_compliance"
        " ON mv_patch_compliance (org_id, client_id, site_id)"
    )


def downgrade() -> None:
    op.execute("DROP MATERIALIZED VIEW IF EXISTS mv_patch_compliance")
    op.execute("DROP MATERIALIZED VIEW IF EXISTS mv_check_result_hourly")
    op.execute("DROP MATERIALIZED VIEW IF EXISTS mv_alert_dashboard")
    op.execute("DROP MATERIALIZED VIEW IF EXISTS mv_agent_fleet_summary")

    op.drop_index("ix_policy_violations_org_open", table_name="policy_violations")
    op.drop_index("ix_patch_targets_org_pending", table_name="patch_job_targets")
    op.drop_index("ix_script_runs_org_recent", table_name="script_runs")
    op.drop_index("ix_alert_state_org_active", table_name="alert_state_machine")
    op.drop_index("ix_sites_org_active", table_name="sites")
    op.drop_index("ix_agents_org_online_lastseen", table_name="agents")
