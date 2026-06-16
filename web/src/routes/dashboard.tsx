import { createFileRoute } from '@tanstack/react-router';
import {
  Bot,
  CircleCheck,
  CircleAlert,
  Bell,
  Activity,
  ArrowUpRight,
} from 'lucide-react';

export const Route = createFileRoute('/dashboard')({
  component: DashboardPage,
});

interface Kpi {
  label: string;
  value: string;
  delta: string;
  deltaTone: 'up' | 'down' | 'neutral';
  icon: typeof Bot;
}

const kpis: Kpi[] = [
  { label: 'Total Agents', value: '128', delta: '+12 this week', deltaTone: 'up', icon: Bot },
  { label: 'Online', value: '119', delta: '93% online', deltaTone: 'neutral', icon: CircleCheck },
  { label: 'Failing Checks', value: '4', delta: '-2 vs yesterday', deltaTone: 'down', icon: CircleAlert },
  { label: 'Open Alerts', value: '7', delta: '+3 today', deltaTone: 'up', icon: Bell },
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
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-slate-100">Dashboard</h1>
          <p className="text-slate-400 mt-1">Overview of your fleet, agents, and recent activity.</p>
        </div>
      </div>

      {/* KPI cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {kpis.map((kpi) => {
          const Icon = kpi.icon;
          return (
            <div
              key={kpi.label}
              className="rounded-lg border border-slate-800 bg-slate-900/60 p-5 hover:border-slate-700 transition-colors"
            >
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
