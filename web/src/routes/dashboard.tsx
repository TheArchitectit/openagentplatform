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
} from 'lucide-react';
import { useMemo } from 'react';
import { useChecks } from '@/lib/useChecks';
import { useAlerts } from '@/lib/useAlerts';

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
  success: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20',
  warning: 'bg-amber-500/10 text-amber-400 border-amber-500/20',
  danger: 'bg-rose-500/10 text-rose-400 border-rose-500/20',
  info: 'bg-sky-500/10 text-sky-400 border-sky-500/20',
};

const deltaClasses: Record<Kpi['deltaTone'], string> = {
  up: 'text-emerald-400',
  down: 'text-rose-400',
  neutral: 'text-slate-400',
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

  // 2 static agent + 4 check + 4 alert = 10 cards. Render in a 4-col grid
  // for the alert row alone, and a separate row for checks.
  const agentRow: Kpi[] = staticKpis;
  const checkRow: Kpi[] = checkKpis;
  const alertRow: Kpi[] = alertKpis;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
          <p className="text-slate-400 mt-1">Overview of your fleet, agents, and recent activity.</p>
        </div>
      </div>

      {/* Agents + Checks KPIs (static agents + live check KPIs) */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        {[...agentRow, ...checkRow].map((kpi) => (
          <KpiCard key={kpi.label} kpi={kpi} />
        ))}
      </div>

      {/* Alert KPIs */}
      <div>
        <h2 className="text-sm font-semibold text-slate-300 mb-3">Alerts</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {alertRow.map((kpi) => (
            <KpiCard key={kpi.label} kpi={kpi} />
          ))}
        </div>
      </div>

      {/* Recent activity */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100">Recent activity</h2>
          <span className="text-xs text-slate-500">Last 24 hours</span>
        </div>
        <ul className="divide-y divide-slate-800">
          {recentActivity.map((item) => {
            const Icon = item.icon;
            return (
              <li
                key={item.id}
                className="px-5 py-3 flex items-center gap-4 hover:bg-slate-900 transition-colors"
              >
                <div
                  className={`h-8 w-8 rounded-md border flex items-center justify-center shrink-0 ${toneClasses[item.tone]}`}
                >
                  <Icon className="h-4 w-4" />
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-slate-200 truncate">{item.title}</p>
                  <p className="text-xs text-slate-500">{item.meta}</p>
                </div>
                <span className="text-xs text-slate-500 shrink-0">{item.time}</span>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}

function KpiCard({ kpi }: { kpi: Kpi }) {
  const Icon = kpi.icon;
  const inner = (
    <>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm text-slate-400">{kpi.label}</p>
          <p className="text-3xl font-semibold text-slate-100 mt-2">{kpi.value}</p>
        </div>
        <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
          <Icon className="h-4 w-4 text-slate-300" />
        </div>
      </div>
      <p className={`text-xs mt-3 ${deltaClasses[kpi.deltaTone]}`}>{kpi.delta}</p>
    </>
  );
  if (kpi.to) {
    return (
      <Link
        to={kpi.to}
        className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 transition-colors block"
      >
        {inner}
      </Link>
    );
  }
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 transition-colors">
      {inner}
    </div>
  );
}
