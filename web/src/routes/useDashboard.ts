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
import { useEffect, useState } from 'react';
import { useChecks } from '@/lib/useChecks';
import { useAlerts } from '@/lib/useAlerts';
import { usePolicies } from '@/lib/usePolicies';
import { usePatches } from '@/lib/usePatches';
import { useScripts } from '@/lib/useScripts';
import { useAgents } from '@/lib/useAgents';
import { apiFetch } from '@/lib/api';

import {
  isToday,
  relativeTime,
  mapAuditToActivity,
  type ActivityItem,
  type Kpi,
  type AuditEventShape,
} from './dashboard_components';
import {
  computeCompliance,
  computeCheckKpis,
  computeAlertKpis,
  computeAgentKpis,
  computePatchKpis,
  computeScriptKpis,
} from './useDashboard.helpers';

export function useDashboard() {
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

  // Live policy compliance aggregates.
  const compliance = computeCompliance(policies);

  // Try to load a server-side compliance summary in the background; if it
  // succeeds, the values below would in a future revision be wired in.
  useEffect(() => {
    void fetchComplianceSummary().catch(() => undefined);
  }, [fetchComplianceSummary]);

  // Live check KPIs.
  const checkKpis: Kpi[] = computeCheckKpis(checks, checksLoading);

  // Live alert KPIs.
  const alertKpis: Kpi[] = computeAlertKpis(alerts, alertsLoading);

  // Live agent KPIs — derived from useAgents.
  const agentKpis: Kpi[] = computeAgentKpis(agents, agentsTotal, agentsLoading);

  // 3 live agent + 4 check + 4 alert + 4 patch = 15 cards. Render in
  // separate rows so the grid stays readable.
  const checkRow: Kpi[] = checkKpis;
  const alertRow: Kpi[] = alertKpis;

  // Live patch KPIs.
  const patchKpis: Kpi[] = computePatchKpis(patchJobs, patchesLoading);

  // Live script KPIs.
  const scriptKpis: Kpi[] = computeScriptKpis(scripts, scriptsTotal, scriptsLoading);

  const greeting = (() => {
    const h = new Date().getHours();
    if (h < 12) return 'Good morning';
    if (h < 18) return 'Good afternoon';
    return 'Good evening';
  })();

  return {
    checksLoading, alertsLoading, policiesLoading,
    greeting, agentKpis, checkRow, alertRow, patchKpis, scriptKpis,
    compliance, policies,
    activityItems, activityLoading, activityError,
  };
}
