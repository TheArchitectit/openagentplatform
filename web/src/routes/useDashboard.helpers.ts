// Pure KPI / aggregation helpers for useDashboard. Extracted from
// useDashboard.ts so that file stays under the size gate. These functions
// take the raw hook data and return Kpi[] / compliance aggregates; they
// contain no React state.

import {
  CheckCircle2,
  AlertTriangle,
  CircleAlert,
  PauseCircle,
  Bell,
  CircleCheck,
  CheckCheck,
  CalendarDays,
  Bot,
  Wrench,
  Shield,
  CirclePlay,
  FileCode2,
  Timer,
} from 'lucide-react';
import type { Check } from '@/lib/useChecks';
import type { Policy } from '@/lib/usePolicies';
import type { PatchJob } from '@/lib/usePatches';
import type { Script } from '@/lib/useScripts';
import type { Agent } from '@/lib/useAgents';
import type { Alert } from '@/lib/useAlerts';
import type { PolicyCategory } from '@/lib/usePolicies';
import type { Kpi } from './dashboard_components';
import { isToday } from './dashboard_components';

export function computeCompliance(policies: Policy[]): {
  overallPct: number | null;
  totalAgents: number;
  compliantAgents: number;
  byCategory: Record<PolicyCategory, { violations: number; total: number }>;
} {
  let totalAgents = 0;
  let compliantAgents = 0;
  const byCategory: Record<PolicyCategory, { violations: number; total: number }> = {
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
    if (typeof pct === 'number' && pct < 100) {
      byCategory[p.category].violations += 1;
      compliantAgents += Math.round((pct / 100) * agents);
    } else if (typeof pct === 'number' && pct === 100) {
      compliantAgents += agents;
    }
  }
  const overallPct = weight > 0 ? (weighted / weight) * 100 : null;
  return { overallPct, totalAgents, compliantAgents, byCategory };
}

export function computeCheckKpis(checks: Check[], checksLoading: boolean): Kpi[] {
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
}

export function computeAlertKpis(alerts: Alert[], alertsLoading: boolean): Kpi[] {
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
}

export function computeAgentKpis(
  agents: Agent[],
  agentsTotal: number,
  agentsLoading: boolean
): Kpi[] {
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
}

export { computePatchKpis, computeScriptKpis } from './useDashboard.kpi-helpers';
