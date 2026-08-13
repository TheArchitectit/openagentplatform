// Display helpers and pure formatters for check detail views.
// Extracted from check_detail_components.tsx to keep that file focused on
// the page-level components (modals, charts).

import {
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  Globe,
  Network,
  HardDrive,
  Cpu,
  MemoryStick,
  ServerCog,
  FileCode2,
  ScrollText,
  ShieldCheck,
  Radio,
} from 'lucide-react';
import type { Check, CheckStatus, CheckType } from '@/lib/useChecks';

export const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

export const statusColor: Record<CheckStatus, string> = {
  ok: 'text-green-400',
  warning: 'text-yellow-400',
  critical: 'text-red-400',
  disabled: 'text-gray-400',
};

export const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-green-500/10 text-green-400 border-green-800',
  warning: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  critical: 'bg-red-500/10 text-red-400 border-red-800',
  disabled: 'bg-slate-500/10 text-gray-300 border-slate-700',
};

export const typeIcon: Record<CheckType, typeof Globe> = {
  http: Globe,
  tcp: Network,
  ping: Radio,
  disk_usage: HardDrive,
  memory_usage: MemoryStick,
  cpu_usage: Cpu,
  process: ServerCog,
  service: ServerCog,
  tls_cert: ShieldCheck,
  script: FileCode2,
  log_watch: ScrollText,
};

export const typeLabel: Record<CheckType, string> = {
  http: 'HTTP',
  tcp: 'TCP',
  ping: 'Ping',
  disk_usage: 'Disk Usage',
  memory_usage: 'Memory Usage',
  cpu_usage: 'CPU Usage',
  process: 'Process',
  service: 'Service',
  tls_cert: 'TLS Certificate',
  script: 'Script',
  log_watch: 'Log Watch',
};

export function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

export function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

export function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

export function deriveStatus(c: Check | null | undefined): CheckStatus {
  if (!c) return 'disabled';
  if (!c.enabled) return 'disabled';
  return (c.last_status ?? 'disabled') as CheckStatus;
}

import { useMemo } from 'react';
import type { CheckResult } from '@/lib/useChecks';

export function ResultBarChart({ results }: { results: CheckResult[] }) {
  const bars = useMemo(() => {
    if (results.length === 0) return [] as { status: CheckStatus; label: string }[];
    const sorted = [...results]
      .filter((r) => !!r.timestamp)
      .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
    return sorted.map((r) => {
      const ts = new Date(r.timestamp);
      const hh = ts.getHours().toString().padStart(2, '0');
      const mm = ts.getMinutes().toString().padStart(2, '0');
      return {
        status: (r.status as CheckStatus) ?? 'disabled',
        label: `${hh}:${mm}`,
      };
    });
  }, [results]);

  if (bars.length === 0) {
    return (
      <div className="text-center text-gray-400 text-sm py-8">No results to chart yet.</div>
    );
  }

  return (
    <div className="flex items-end gap-1 h-32">
      {bars.map((b, i) => {
        const color =
          b.status === 'ok'
            ? 'bg-green-500'
            : b.status === 'warning'
            ? 'bg-yellow-500'
            : b.status === 'critical'
            ? 'bg-red-500'
            : 'bg-slate-700';
        const showLabel = bars.length <= 6 || i === 0 || i === Math.floor(bars.length / 2) || i === bars.length - 1;
        return (
          <div key={i} className="flex-1 flex flex-col items-center justify-end gap-1 min-w-0">
            <div
              className={'w-full rounded-t ' + color}
              style={{ height: b.status === 'disabled' ? '4px' : '100%' }}
              title={b.status}
            />
            {showLabel && (
              <span className="text-[10px] text-gray-400 truncate w-full text-center">{b.label}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

const TYPE_ICON_MAP: Record<string, string> = {
  http: '🌐',
  dns: '🔍',
  tcp: '🔌',
  ping: '📡',
  custom: '⚙️',
};

export function TypeIcon({ type, className }: { type?: string; className?: string }) {
  const icon = TYPE_ICON_MAP[type ?? ''] ?? '📋';
  return <span className={className ?? 'text-lg'}>{icon}</span>;
}
