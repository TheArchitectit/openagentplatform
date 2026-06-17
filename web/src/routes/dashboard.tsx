import { createFileRoute, Link } from '@tanstack/react-router';
import {
  Bot,
  CircleCheck,
  CircleAlert,
  Bell,
  Activity,
  ArrowUpRight,
  CheckCircle2,
  AlertTriangle,
  PauseCircle,
  CheckCheck,
  CalendarDays,
  ShieldCheck,
  Wrench,
  Shield,
  CirclePlay,
  FileCode2,
  Terminal,
  Timer,
} from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { useChecks } from '@/lib/useChecks';
import { useAlerts } from '@/lib/useAlerts';
import { usePolicies, type PolicyCategory } from '@/lib/usePolicies';
import { usePatches } from '@/lib/usePatches';
import { useScripts } from '@/lib/useScripts';

export const Route = createFileRoute('/dashboard')({
  component: DashboardPage,
});

interface Kpi {
  label: string;
  value: string;
  delta: string;
  deltaTone: 'up' | 'down' | 'neutral';
  icon: typeof Bot;
  to?: string;
}

interface ActivityItem {
  id: string;
  type: 'check' | 'alert' | 'agent' | 'patch';
  title: string;
  meta: string;
  time: string;
  icon: typeof Activity;
  tone: 'success' | 'warning' | 'info' | 'danger';
}

// Static agent/alert KPIs (checks KPIs are computed live from useChecks).
const staticKpis: Kpi[] = [
  { label: 'Total Agents', value: '128', delta: '+12 this week', deltaTone: 'up', icon: Bot, to: '/agents' },
  { label: 'Online', value: '119', delta: '93% online', deltaTone: 'neutral', icon: CircleCheck, to: '/agents' },
];

const recentActivity: ActivityItem[] = [
  { id: '1', type: 'check', title: 'Check "disk-usage" failed on agent prod-web-03', meta: 'Agent prod-web-03', time: '2m ago', icon: Activity, tone: 'danger' },
  { id: '2', type: 'agent', title: 'Agent prod-db-01 came online', meta: 'Agent prod-db-01', time: '8m ago', icon: Bot, tone: 'success' },
  { id: '3', type: 'patch', title: 'Patch KB5037000 installed on 14 agents', meta: 'Fleet rollout', time: '34m ago', icon: ArrowUpRight, tone: 'info' },
  { id: '4', type: 'alert', title: 'High CPU on agent staging-api-02', meta: 'Agent staging-api-02', time: '1h ago', icon: Bell, tone: 'warning' },
  { id: '5', type: 'check', title: 'Check "tls-cert-expiry" passed on 126 agents', meta: 'Fleet check', time: '2h ago', icon: Activity, tone: 'success' },
];

const toneClasses: Record<ActivityItem['tone'], string> = {
  success: 'bg-success/10 text-success border-success/20',
  warning: 'bg-warning/10 text-warning border-warning/20',
  danger: 'bg-danger/10 text-danger border-danger/20',
  info: 'bg-info/10 text-info border-info/20',
};

const deltaClasses: Record<Kpi['deltaTone'], string> = {
  up: 'text-success',
  down: 'text-danger',
  neutral: 'text-text-secondary',
};

function isToday(iso: string | undefined): boolean {
  if (!iso) return false;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return false;
  const now = new Date();
  return (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  );
}

function DashboardPage() {
  const { checks, isLoading: checksLoading } = useChecks();
  const { alerts, isLoading: alertsLoading } = useAlerts('all');
  const { policies, isLoading: policiesLoading, fetchComplianceSummary } = usePolicies();
  const { jobs: patchJobs, isLoading: patchesLoading } = usePatches();
  const { scripts, isLoading: scriptsLoading, total: scriptsTotal } = useScripts();

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

  // 2 static agent + 4 check + 4 alert + 4 patch = 14 cards. Render in
  // separate rows so the grid stays readable.
  const agentRow: Kpi[] = staticKpis;
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

  return (
    <div className="space-y-6" aria-busy={checksLoading || alertsLoading || policiesLoading}>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Dashboard</h1>
          <p className="text-text-secondary mt-1">Overview of your fleet, agents, and recent activity.</p>
        </div>
      </div>

      {/* Agents + Checks KPIs (static agents + live check KPIs) */}
      <div role="group" aria-label="Agent and check KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        {[...agentRow, ...checkRow].map((kpi) => (
          <KpiCard key={kpi.label} kpi={kpi} />
        ))}
      </div>

      {/* Alert KPIs */}
      <section aria-labelledby="alerts-heading">
        <h2 id="alerts-heading" className="text-sm font-semibold text-text-secondary mb-3">Alerts</h2>
        <div role="group" aria-label="Alert KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {alertRow.map((kpi) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Patch KPIs */}
      <section aria-labelledby="patches-heading">
        <div className="flex items-center justify-between mb-3">
          <h2 id="patches-heading" className="text-sm font-semibold text-text-secondary">Patches</h2>
          <Link
            to="/patches"
            aria-label="View all patches"
            className="text-xs text-text-secondary hover:text-text-primary focus:outline-none focus-visible:underline transition-colors"
          >
            View all →
          </Link>
        </div>
        <div role="group" aria-label="Patch KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {patchKpis.map((kpi) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Script KPIs */}
      <section aria-labelledby="scripts-heading">
        <div className="flex items-center justify-between mb-3">
          <h2 id="scripts-heading" className="text-sm font-semibold text-text-secondary">Scripts</h2>
          <Link
            to="/scripts"
            aria-label="View all scripts"
            className="text-xs text-text-secondary hover:text-text-primary focus:outline-none focus-visible:underline transition-colors"
          >
            View all →
          </Link>
        </div>
        <div role="group" aria-label="Script KPIs" className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {scriptKpis.map((kpi) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </section>

      {/* Policy compliance */}
      <section aria-labelledby="compliance-heading">
        <h2 id="compliance-heading" className="text-sm font-semibold text-text-secondary mb-3">Policy compliance</h2>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
          {/* Overall score card */}
          <Link
            to="/policies"
            aria-label="View policy compliance details"
            className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5 hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors block"
          >
            <div className="flex items-start justify-between">
              <div>
                <p className="text-sm text-text-secondary">Overall compliance</p>
                <p
                  className={
                    'text-3xl font-semibold mt-2 tabular-nums ' +
                    (compliance.overallPct === null
                      ? 'text-text-muted'
                      : compliance.overallPct >= 80
                      ? 'text-success'
                      : compliance.overallPct >= 60
                      ? 'text-warning'
                      : 'text-danger')
                  }
                  role="status"
                  aria-label={
                    compliance.overallPct === null
                      ? 'Overall compliance: no data'
                      : `Overall compliance: ${compliance.overallPct.toFixed(0)} percent`
                  }
                >
                  {policiesLoading && policies.length === 0
                    ? '—'
                    : compliance.overallPct === null
                    ? '—'
                    : `${compliance.overallPct.toFixed(0)}%`}
                </p>
                <p className="text-xs text-text-muted mt-3">
                  {policies.length} {policies.length === 1 ? 'policy' : 'policies'}
                  {compliance.totalAgents > 0
                    ? ` · ${compliance.compliantAgents} of ${compliance.totalAgents} agents compliant`
                    : ''}
                </p>
              </div>
              <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center" aria-hidden="true">
                <ShieldCheck className="h-4 w-4 text-text-secondary" />
              </div>
            </div>
          </Link>

          {/* Violations by category mini bar chart */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5 lg:col-span-2">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-text-primary">Violations by category</h3>
              <Link
                to="/policies"
                aria-label="View all policy violations"
                className="text-xs text-text-secondary hover:text-text-primary focus:outline-none focus-visible:underline transition-colors"
              >
                View all →
              </Link>
            </div>
            {policies.length === 0 ? (
              <div className="text-center text-xs text-text-muted py-6" role="status">
                No policies to chart yet.
              </div>
            ) : (
              <div role="list" aria-label="Violations by policy category" className="space-y-2.5">
                {(Object.keys(compliance.byCategory) as PolicyCategory[]).map((cat) => {
                  const { violations, total } = compliance.byCategory[cat];
                  const pct = total > 0 ? (violations / total) * 100 : 0;
                  if (total === 0) return null;
                  return (
                    <div key={cat} role="listitem" className="flex items-center gap-3">
                      <div className="w-24 text-xs text-text-secondary capitalize">{cat}</div>
                      <div
                        className="flex-1 h-5 rounded bg-surface-tertiary/60 overflow-hidden border border-border-subtle"
                        role="progressbar"
                        aria-valuenow={Math.round(pct)}
                        aria-valuemin={0}
                        aria-valuemax={100}
                        aria-label={`${cat} violation rate`}
                      >
                        <div
                          className="h-full bg-danger/70 transition-all"
                          style={{ width: `${pct}%` }}
                        />
                      </div>
                      <div className="w-20 text-right text-xs text-text-secondary tabular-nums" aria-label={`${violations} of ${total} policies with violations`}>
                        {violations} / {total}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </section>

      {/* Recent activity */}
      <section aria-labelledby="activity-heading" className="rounded-lg border border-border-subtle bg-surface-secondary/60">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 id="activity-heading" className="text-sm font-semibold text-text-primary">Recent activity</h2>
          <span className="text-xs text-text-muted" aria-label="Time range: last 24 hours">Last 24 hours</span>
        </div>
        <ul className="divide-y divide-border-subtle" aria-label="Recent activity feed">
          {recentActivity.map((item) => {
            const Icon = item.icon;
            return (
              <li
                key={item.id}
                className="px-5 py-3 flex items-center gap-4 hover:bg-surface-primary transition-colors"
              >
                <div
                  className={`h-8 w-8 rounded-md border flex items-center justify-center shrink-0 ${toneClasses[item.tone]}`}
                  aria-hidden="true"
                >
                  <Icon className="h-4 w-4" />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-text-primary truncate">{item.title}</p>
                  <p className="text-xs text-text-muted">{item.meta}</p>
                </div>
                <span className="text-xs text-text-muted shrink-0" aria-label={`${item.time}`}>{item.time}</span>
              </li>
            );
          })}
        </ul>
      </section>
    </div>
  );
}

function KpiCard({ kpi }: { kpi: Kpi }) {
  const Icon = kpi.icon;
  const inner = (
    <>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm text-text-secondary">{kpi.label}</p>
          <p className="text-3xl font-semibold text-text-primary mt-2" aria-label={`${kpi.label}: ${kpi.value}`}>{kpi.value}</p>
        </div>
        <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center" aria-hidden="true">
          <Icon className="h-4 w-4 text-text-secondary" />
        </div>
      </div>
      <p className={`text-xs mt-3 ${deltaClasses[kpi.deltaTone]}`} aria-label={`Status: ${kpi.delta}`}>{kpi.delta}</p>
    </>
  );
  if (kpi.to) {
    return (
      <Link
        to={kpi.to}
        aria-label={`${kpi.label}: ${kpi.value}. ${kpi.delta}. Click for details.`}
        className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5 hover:border-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors block"
      >
        {inner}
      </Link>
    );
  }
  return (
    <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5 hover:border-border-strong">
      {inner}
    </div>
  );
}
