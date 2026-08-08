import { createFileRoute, Link } from '@tanstack/react-router';
import {
  Bot,
  CircleCheck,
  CircleAlert,
  Bell,
  CheckCircle2,
  AlertTriangle,
  PauseCircle,
  CheckCheck,
  CalendarDays,
  Wrench,
  Shield,
  CirclePlay,
  FileCode2,
  Timer,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useChecks } from '@/lib/useChecks';
import { useAlerts } from '@/lib/useAlerts';
import { usePolicies, type PolicyCategory } from '@/lib/usePolicies';
import { usePatches } from '@/lib/usePatches';
import { useScripts } from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';
import { apiFetch } from '@/lib/api';


import { isToday, relativeTime, mapAuditToActivity, type ActivityItem, type Kpi, type AuditEventShape } from './dashboard_components'

function DashboardPage() {
  const { checks, isLoading: checksLoading } = useChecks();
  const { alerts, isLoading: alertsLoading } = useAlerts('all');
  const { policies, isLoading: policiesLoading, fetchComplianceSummary } = usePolicies();
  const { jobs: patchJobs, isLoading: patchesLoading } = usePatches();
  const { scripts, isLoading: scriptsLoading, total: scriptsTotal } = useScripts();
  const { agents, total: agentsTotal, isLoading: agentsLoading } = useAgents();

  // Live audit activity feed
  const [activityItems, setActivityItems] = useState<ActivityItem[]>([]);
  const [activityLoading, setActivityLoading] = useState(true);
  const [activityError, setActivityError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setActivityLoading(true);
    setActivityError(false);
    apiFetch<{ events?: AuditEventShape[] } | AuditEventShape[]>('/audit/events?limit=10')
      .then((res) => {
        if (cancelled) return;
        const events = Array.isArray(res) ? res : (res.events ?? []);
        setActivityItems(mapAuditToActivity(events));
      })
      .catch(() => {
        if (cancelled) return;
        setActivityError(true);
      })
      .finally(() => {
        if (cancelled) return;
        setActivityLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Live policy compliance aggregates (computed from the policy list when
  // a per-policy compliance summary endpoint is not available; falls back
  // to whatever the policies endpoint provides).
  const compliance = useMemo(() => {
    let totalAgents = 0;
    let compliantAgents = 0;
    const byCategory: Record<
      PolicyCategory,
      { violations: number; total: number }
    > = {
      security: { violations: 0, total: 0 },
      compliance: { violations: 0, total: 0 },
      configuration: { violations: 0, total: 0 },
      performance: { violations: 0, total: 0 },
      custom: { violations: 0, total: 0 },
    };
    let weighted = 0;
    let weight = 0;
    for (const p of policies) {
      const agents = p.agent_count ?? 0;
      const pct = p.compliance_pct;
      if (agents > 0 && typeof pct === 'number') {
        weighted += (pct / 100) * agents;
        weight += agents;
      }
      totalAgents = Math.max(totalAgents, agents);
      byCategory[p.category].total += 1;
      // We don't have raw violation counts in the policy list, so use
      // compliance_pct as a proxy: <100% = at least one violation
      if (typeof pct === 'number' && pct < 100) {
        byCategory[p.category].violations += 1;
        compliantAgents += Math.round((pct / 100) * agents);
      } else if (typeof pct === 'number' && pct === 100) {
        compliantAgents += agents;
      }
    }
    const overallPct = weight > 0 ? (weighted / weight) * 100 : null;
    return { overallPct, totalAgents, compliantAgents, byCategory };
  }, [policies]);

  // Try to load a server-side compliance summary in the background; if it
  // succeeds, the values below would in a future revision be wired in.
  // For now we trigger the request so the data is warm.
  useEffect(() => {
    void fetchComplianceSummary().catch(() => undefined);
  }, [fetchComplianceSummary]);

  // Live check KPIs.
  const checkKpis: Kpi[] = useMemo(() => {
    let ok = 0;
    let warn = 0;
    let crit = 0;
    let disabled = 0;
    for (const c of checks) {
      if (!c.enabled) {
        disabled += 1;
        continue;
      }
      const s = c.last_status;
      if (s === 'ok') ok += 1;
      else if (s === 'warning') warn += 1;
      else if (s === 'critical') crit += 1;
      else disabled += 1;
    }
    const failing = warn + crit;
    return [
      {
        label: 'Checks Passing',
        value: checksLoading && checks.length === 0 ? '—' : String(ok),
        delta: checks.length > 0 ? `${ok} of ${checks.length} ok` : 'No checks yet',
        deltaTone: failing === 0 ? 'up' : 'down',
        icon: CheckCircle2,
        to: '/checks',
      },
      {
        label: 'Checks Warning',
        value: checksLoading && checks.length === 0 ? '—' : String(warn),
        delta: warn === 0 ? 'No warnings' : 'Needs attention',
        deltaTone: warn === 0 ? 'neutral' : 'up',
        icon: AlertTriangle,
        to: '/checks',
      },
      {
        label: 'Checks Critical',
        value: checksLoading && checks.length === 0 ? '—' : String(crit),
        delta: crit === 0 ? 'All healthy' : 'Investigate now',
        deltaTone: crit === 0 ? 'neutral' : 'down',
        icon: CircleAlert,
        to: '/checks',
      },
      {
        label: 'Checks Disabled',
        value: checksLoading && checks.length === 0 ? '—' : String(disabled),
        delta: disabled === 0 ? 'All enabled' : 'Paused',
        deltaTone: 'neutral',
        icon: PauseCircle,
        to: '/checks',
      },
    ];
  }, [checks, checksLoading]);

  // Live alert KPIs.
  const alertKpis: Kpi[] = useMemo(() => {
    let open = 0;
    let critical = 0;
    let acknowledged = 0;
    let today = 0;
    for (const a of alerts) {
      const state = (a.state ?? '').toLowerCase();
      const severity = (a.severity ?? '').toLowerCase();
      if (state === 'open') open += 1;
      if (
        (severity === 'critical' || severity === 'emergency') &&
        (state === 'open' || state === 'acknowledged' || state === 'snoozed')
      ) {
        critical += 1;
      }
      if (state === 'acknowledged') acknowledged += 1;
      if (isToday(a.created_at)) today += 1;
    }
    const dash = alertsLoading && alerts.length === 0 ? '—' : null;
    return [
      {
        label: 'Open Alerts',
        value: dash ?? String(open),
        delta: open === 0 ? 'Inbox clear' : `${open} need${open === 1 ? 's' : ''} attention`,
        deltaTone: open === 0 ? 'up' : 'down',
        icon: Bell,
        to: '/alerts',
      },
      {
        label: 'Critical',
        value: dash ?? String(critical),
        delta: critical === 0 ? 'No critical' : 'Page on-call',
        deltaTone: critical === 0 ? 'up' : 'down',
        icon: CircleAlert,
        to: '/alerts',
      },
      {
        label: 'Acknowledged',
        value: dash ?? String(acknowledged),
        delta: acknowledged === 0 ? 'None pending ack' : 'In progress',
        deltaTone: 'neutral',
        icon: CheckCheck,
        to: '/alerts',
      },
      {
        label: 'Total Today',
        value: dash ?? String(today),
        delta: today === 0 ? 'Quiet day' : 'Last 24 hours',
        deltaTone: 'neutral',
        icon: CalendarDays,
        to: '/alerts',
      },
    ];
  }, [alerts, alertsLoading]);

  // Live agent KPIs — derived from useAgents.
  const agentKpis: Kpi[] = useMemo(() => {
    const online = agents.filter((a) => a.status === 'online').length;
    const offline = agents.filter((a) => a.status === 'offline').length;
    const total = agentsTotal;
    const pct = total > 0 ? Math.round((online / total) * 100) : 0;
    const dash = agentsLoading && agents.length === 0 ? '—' : null;
    return [
      {
        label: 'Total Agents',
        value: dash ?? String(total),
        delta: total === 0 ? 'No agents yet' : `${total} registered`,
        deltaTone: 'neutral' as const,
        icon: Bot,
        to: '/agents',
      },
      {
        label: 'Online',
        value: dash ?? String(online),
        delta: total === 0 ? '—' : `${pct}% online`,
        deltaTone: (pct >= 90 ? 'up' : pct >= 50 ? 'neutral' : 'down') as Kpi['deltaTone'],
        icon: CircleCheck,
        to: '/agents',
      },
      {
        label: 'Offline',
        value: dash ?? String(offline),
        delta: offline === 0 ? 'All online' : `${offline} need attention`,
        deltaTone: (offline === 0 ? 'up' : 'down') as Kpi['deltaTone'],
        icon: CircleAlert,
        to: '/agents',
      },
    ];
  }, [agents, agentsTotal, agentsLoading]);

  // 3 live agent + 4 check + 4 alert + 4 patch = 15 cards. Render in
  // separate rows so the grid stays readable.
  const checkRow: Kpi[] = checkKpis;
  const alertRow: Kpi[] = alertKpis;

  // Live patch KPIs.
  const patchKpis: Kpi[] = useMemo(() => {
    let total = 0;
    let critical = 0;
    let security = 0;
    let approved = 0;
    let inProgress = 0;
    let completedToday = 0;
    for (const j of patchJobs) {
      total += 1;
      const sev = (j.severity ?? '').toLowerCase();
      if (sev === 'critical' || sev === 'emergency') critical += 1;
      if (sev === 'important' || j.patch_count > 0) security += 1;
      if (j.status === 'approved') approved += 1;
      if (j.status === 'in_progress') inProgress += 1;
      if (j.status === 'completed' && isToday(j.completed_at)) completedToday += 1;
    }
    const dash = patchesLoading && patchJobs.length === 0 ? '—' : null;
    return [
      {
        label: 'Total Jobs',
        value: dash ?? String(total),
        delta: total === 0 ? 'No jobs yet' : `${total} tracked`,
        deltaTone: 'neutral',
        icon: Wrench,
        to: '/patches',
      },
      {
        label: 'Critical',
        value: dash ?? String(critical),
        delta: critical === 0 ? 'No critical' : 'Action required',
        deltaTone: critical === 0 ? 'neutral' : 'down',
        icon: Shield,
        to: '/patches',
      },
      {
        label: 'Approved',
        value: dash ?? String(approved),
        delta: approved === 0 ? 'None queued' : 'Ready to deploy',
        deltaTone: 'neutral',
        icon: CircleCheck,
        to: '/patches',
      },
      {
        label: 'In Progress',
        value: dash ?? String(inProgress),
        delta: inProgress === 0 ? 'Idle' : 'Rolling out',
        deltaTone: inProgress === 0 ? 'neutral' : 'up',
        icon: CirclePlay,
        to: '/patches',
      },
    ];
  }, [patchJobs, patchesLoading]);

  // Live script KPIs — derived from the script list and (when present)
  // per-script last_status / run_count fields. A full run-history
  // aggregation would require a separate endpoint, so we keep the
  // "today" / "in progress" buckets based on what the script records
  // report, falling back to "—" when the server hasn't supplied them.
  const scriptKpis: Kpi[] = useMemo(() => {
    let total = scriptsTotal || scripts.length;
    let succeeded = 0;
    let failed = 0;
    let running = 0;
    let totalRuns = 0;
    for (const s of scripts) {
      if (typeof s.run_count === 'number') totalRuns += s.run_count;
      if (s.last_status === 'completed') succeeded += 1;
      if (s.last_status === 'failed' || s.last_status === 'timeout') failed += 1;
      if (s.last_status === 'in_progress' || s.last_status === 'pending') running += 1;
    }
    const dash = scriptsLoading && scripts.length === 0 ? '—' : null;
    return [
      {
        label: 'Total Scripts',
        value: dash ?? String(total),
        delta: total === 0 ? 'No scripts yet' : `${total} in library`,
        deltaTone: 'neutral',
        icon: FileCode2,
        to: '/scripts',
      },
      {
        label: 'Last Run OK',
        value: dash ?? String(succeeded),
        delta: succeeded === 0 ? 'No clean runs' : 'Most recent succeeded',
        deltaTone: succeeded === 0 ? 'neutral' : 'up',
        icon: CircleCheck,
        to: '/scripts',
      },
      {
        label: 'Last Run Failed',
        value: dash ?? String(failed),
        delta: failed === 0 ? 'No failures' : 'Investigate runs',
        deltaTone: failed === 0 ? 'neutral' : 'down',
        icon: CircleAlert,
        to: '/scripts',
      },
      {
        label: 'Total Runs',
        value: dash ?? String(totalRuns),
        delta: totalRuns === 0 ? 'No runs yet' : `${running} active`,
        deltaTone: running > 0 ? 'up' : 'neutral',
        icon: Timer,
        to: '/scripts',
      },
    ];
  }, [scripts, scriptsLoading, scriptsTotal]);

  const greeting = useMemo(() => {
    const h = new Date().getHours();
    if (h < 12) return 'Good morning';
    if (h < 18) return 'Good afternoon';
    return 'Good evening';
  }, []);

