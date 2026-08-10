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

export const Route = createFileRoute('/dashboard')({
  component: DashboardPage,
});


export interface Kpi {
  label: string;
  value: string;
  delta: string;
  deltaTone: 'up' | 'down' | 'neutral';
  icon: typeof Bot;
  to?: string;
}

export interface ActivityItem {
  id: string;
  type: 'agent' | 'check' | 'alert' | 'patch' | 'login';
  title: string;
  meta: string;
  time: string;
  tone: 'success' | 'warning' | 'danger' | 'info';
  actor: string;
}

const deltaClasses: Record<Kpi['deltaTone'], string> = {
  up: 'text-emerald-400',
  down: 'text-red-400',
  neutral: 'text-gray-400',
};

export function isToday(iso: string | undefined): boolean {
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

// ---------------------------------------------------------------------------
// Audit event -> ActivityItem mapping
// ---------------------------------------------------------------------------

export interface AuditEventShape {
  id: string;
  actor_id?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  outcome?: string;
  timestamp?: string;
  details?: Record<string, unknown>;
}

export function relativeTime(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const diffMs = Date.now() - d.getTime();
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  return `${days}d ago`;
}

export function mapAuditToActivity(events: AuditEventShape[]): ActivityItem[] {
  return events.map((ev) => {
    const action = (ev.action ?? '').toLowerCase();
    const outcome = (ev.outcome ?? 'success').toLowerCase();

    // Map action -> ActivityItem type
    let type: ActivityItem['type'] = 'agent';
    if (action.includes('login') || action.includes('auth')) {
      type = 'login';
    } else if (action.includes('check') || action.includes('api_call')) {
      type = 'check';
    } else if (action.includes('alert')) {
      type = 'alert';
    } else if (action.includes('patch') || action.includes('deploy')) {
      type = 'patch';
    } else {
      type = 'agent';
    }

    // Map outcome -> tone
    let tone: ActivityItem['tone'] = 'info';
    if (outcome === 'success') tone = 'success';
    else if (outcome === 'failure' || outcome === 'error') tone = 'danger';
    else if (outcome === 'denied') tone = 'warning';

    const actor = ev.actor_id ?? 'system';
    const resource = ev.resource_type
      ? `${ev.resource_type}${ev.resource_id ? ` (${ev.resource_id})` : ''}`
      : ev.resource_id ?? '';

    const title = resource
      ? `${actor} ${ev.action ?? 'unknown'} ${resource}`
      : `${actor} ${ev.action ?? 'unknown'}`;

    // Meta: extra details from the event if present
    const detailStr = ev.details
      ? Object.entries(ev.details)
          .map(([k, v]) => `${k}: ${String(v)}`)
          .join(', ')
      : '';

    return {
      id: ev.id,
      type,
      title,
      meta: detailStr || (ev.resource_type ?? ''),
      time: relativeTime(ev.timestamp),
      tone,
      actor,
    };
  });
}

export function KpiCard({ kpi }: { kpi: Kpi }) {
  const Icon = kpi.icon;
  const inner = (
    <>
      <div className="flex items-start justify-between">
        <div>
          <p className="text-sm text-gray-400">{kpi.label}</p>
          <p className="text-3xl font-bold text-white mt-2" aria-label={`${kpi.label}: ${kpi.value}`}>{kpi.value}</p>
        </div>
        <div className="h-9 w-9 rounded-lg bg-blue-500/10 p-2 flex items-center justify-center" aria-hidden="true">
          <Icon className="h-4 w-4 text-blue-500" />
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
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 hover:border-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors block"
      >
        {inner}
      </Link>
    );
  }
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-900 p-5 hover:border-slate-700">
      {inner}
    </div>
  );
}
