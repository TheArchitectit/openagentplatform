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
} from 'lucide-react';
import { useMemo } from 'react';
import { useChecks } from '@/lib/useChecks';

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

// Static agent/alert KPIs (checks KPIs are computed live from useChecks).
const staticKpis: Kpi[] = [
  { label: 'Total Agents', value: '128', delta: '+12 this week', deltaTone: 'up', icon: Bot, to: '/agents' },
  { label: 'Online', value: '119', delta: '93% online', deltaTone: 'neutral', icon: CircleCheck, to: '/agents' },
  { label: 'Open Alerts', value: '7', delta: '+3 today', deltaTone: 'up', icon: Bell, to: '/alerts' },
];

interface ActivityItem {
  id: string;
  type: 'check' | 'alert' | 'agent' | 'patch';
  title: string;
  meta: string;
  time: string;
  icon: typeof Activity;
  tone: 'success' | 'warning' | 'info' | 'danger';
}

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

function DashboardPage() {
  const { checks, isLoading: checksLoading } = useChecks();

  // Compute live check KPIs from the loaded checks list.
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

  const allKpis: Kpi[] = [...staticKpis, ...checkKpis];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
          <p className="text-slate-400 mt-1">Overview of your fleet, agents, and recent activity.</p>
        </div>
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-7 gap-4">
        {allKpis.map((kpi) => {
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
                key={kpi.label}
                to={kpi.to}
                className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 transition-colors block"
              >
                {inner}
              </Link>
            );
          }
          return (
            <div
              key={kpi.label}
              className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 transition-colors"
            >
              {inner}
            </div>
          );
        })}
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
