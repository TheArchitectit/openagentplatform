import { createFileRoute, Link } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import {
  ArrowLeft,
  Bot,
  Play,
  Terminal,
  ScrollText,
  Cpu,
  MemoryStick,
  HardDrive,
  Activity,
  CheckCircle2,
  XCircle,
  AlertTriangle,
} from 'lucide-react';
import { apiFetch, ApiError } from '@/lib/api';
import { getWsClient, type WsEnvelope } from '@/lib/websocket';

export const Route = createFileRoute('/agents/$agentId')({
  component: AgentDetailPage,
});

interface CheckResult {
  agent_id: string;
  check_id: string;
  timestamp: string;
  status: string;
  value: number;
  message: string;
  metadata?: Record<string, unknown>;
}

interface AgentDetail {
  id: string;
  hostname: string;
  os: string;
  arch?: string;
  platform?: string;
  agent_version: string;
  version?: string;
  status: string;
  last_seen: string;
  site_id: string;
  org_id?: string;
  cpu_count: number;
  total_memory_mb: number;
  total_disk_gb: number;
  tags?: string[];
  metadata?: Record<string, unknown>;
}

interface AgentResponse {
  agent: AgentDetail;
  check_results: CheckResult[];
}

interface LiveMetrics {
  cpu_percent?: number;
  mem_percent?: number;
  disk_percent?: number;
  uptime_secs?: number;
  lastSeen?: string;
}

function formatUptime(secs: number | undefined): string {
  if (!secs || secs <= 0) return '—';
  const d = Math.floor(secs / 86400);
  const h = Math.floor((secs % 86400) / 3600);
  const m = Math.floor((secs % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatTime(iso: string): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '—';
  return t.toLocaleString();
}

function GaugeBar({
  label,
  percent,
  icon: Icon,
  total,
  used,
}: {
  label: string;
  percent: number | undefined;
  icon: typeof Cpu;
  total?: number;
  used?: number;
}) {
  const value = Math.max(0, Math.min(100, percent ?? 0));
  const color =
    value > 85 ? 'bg-rose-500' : value > 65 ? 'bg-amber-500' : 'bg-emerald-500';

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2 text-slate-300">
          <Icon className="h-4 w-4" />
          <span className="text-sm font-medium">{label}</span>
        </div>
        <span className="text-sm tabular-nums text-slate-200">
          {percent === undefined ? '—' : `${value.toFixed(1)}%`}
        </span>
      </div>
      <div className="h-2 w-full rounded-full bg-slate-800 overflow-hidden">
        <div
          className={'h-full transition-all ' + color}
          style={{ width: `${value}%` }}
        />
      </div>
      {total !== undefined && (
        <p className="text-xs text-slate-500 mt-2">
          {used !== undefined ? `${used.toFixed(1)} / ` : ''}
          {total.toFixed(1)} GB
        </p>
      )}
    </div>
  );
}

function statusTone(s: string): { color: string; icon: typeof CheckCircle2; label: string } {
  switch (s) {
    case 'pass':
    case 'success':
    case 'ok':
      return { color: 'text-emerald-400', icon: CheckCircle2, label: s };
    case 'fail':
    case 'failed':
    case 'error':
      return { color: 'text-rose-400', icon: XCircle, label: s };
    case 'warn':
    case 'warning':
      return { color: 'text-amber-400', icon: AlertTriangle, label: s };
    default:
      return { color: 'text-slate-400', icon: Activity, label: s || 'unknown' };
  }
}

function AgentDetailPage() {
  const { agentId } = Route.useParams();
  const [data, setData] = useState<AgentResponse | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [live, setLive] = useState<LiveMetrics>({});

  useEffect(() => {
    let alive = true;
    setIsLoading(true);
    apiFetch<AgentResponse>(`/agents/${encodeURIComponent(agentId)}?check_limit=20`)
      .then((res) => {
        if (!alive) return;
        setData(res);
        setError(null);
      })
      .catch((err) => {
        if (!alive) return;
        setError(err instanceof ApiError ? err : new Error(String(err)));
      })
      .finally(() => {
        if (alive) setIsLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [agentId]);

  // Subscribe to live heartbeats for this specific agent.
  useEffect(() => {
    const ws = getWsClient();
    const unsub = ws.subscribe('agents', (env: WsEnvelope) => {
      if (env.type !== 'event' || !env.data) return;
      const hb = env.data as {
        agent_id: string;
        timestamp: string;
        cpu_percent: number;
        mem_percent: number;
        disk_percent: number;
        uptime_secs: number;
      };
      if (hb.agent_id !== agentId) return;
      setLive({
        cpu_percent: hb.cpu_percent,
        mem_percent: hb.mem_percent,
        disk_percent: hb.disk_percent,
        uptime_secs: hb.uptime_secs,
        lastSeen: hb.timestamp,
      });
    });
    return unsub;
  }, [agentId]);

  if (isLoading) {
    return (
      <div className="text-center text-slate-500 py-24">Loading agent…</div>
    );
  }
  if (error || !data) {
    return (
      <div className="space-y-4">
        <Link
          to="/agents"
          className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-200"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to agents</span>
        </Link>
        <div className="rounded-lg border border-rose-500/30 bg-rose-500/5 p-6 text-rose-300">
          Failed to load agent: {error?.message ?? 'unknown error'}
        </div>
      </div>
    );
  }

  const a = data.agent;
  const cpuPct = live.cpu_percent;
  const memPct = live.mem_percent;
  const diskPct = live.disk_percent;
  const uptime = live.uptime_secs;

  const memUsedGB =
    memPct !== undefined && a.total_memory_mb > 0
      ? (a.total_memory_mb / 1024) * (memPct / 100)
      : undefined;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/agents"
            className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center hover:bg-slate-700 transition-colors"
          >
            <ArrowLeft className="h-4 w-4 text-slate-300" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-indigo-600/20 border border-indigo-500/30 flex items-center justify-center">
            <Bot className="h-4 w-4 text-indigo-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">{a.hostname || a.id}</h1>
            <p className="text-slate-400 text-sm mt-0.5 font-mono text-xs">{a.id}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-sm text-slate-200 disabled:opacity-50"
          >
            <Play className="h-4 w-4" />
            <span>Run check</span>
          </button>
          <button
            type="button"
            disabled
            title="Remote shell — coming soon"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-sm text-slate-200 disabled:opacity-50"
          >
            <Terminal className="h-4 w-4" />
            <span>Remote shell</span>
          </button>
          <button
            type="button"
            disabled
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-sm text-slate-200 disabled:opacity-50"
          >
            <ScrollText className="h-4 w-4" />
            <span>View logs</span>
          </button>
        </div>
      </div>

      {/* Info card */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-5">
        <h2 className="text-sm font-semibold text-slate-100 mb-4">Agent info</h2>
        <dl className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4 text-sm">
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Hostname</dt>
            <dd className="text-slate-200 mt-1 break-all">{a.hostname || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">OS</dt>
            <dd className="text-slate-200 mt-1 break-all">{a.os || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Agent version</dt>
            <dd className="text-slate-200 mt-1 break-all">
              {a.agent_version || a.version || '—'}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Site</dt>
            <dd className="text-slate-200 mt-1 break-all">{a.site_id || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Last seen</dt>
            <dd className="text-slate-200 mt-1 break-all">
              {formatTime(live.lastSeen ?? a.last_seen)}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Arch</dt>
            <dd className="text-slate-200 mt-1 break-all">{a.arch || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Platform</dt>
            <dd className="text-slate-200 mt-1 break-all">{a.platform || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">CPU cores</dt>
            <dd className="text-slate-200 mt-1">{a.cpu_count || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Memory</dt>
            <dd className="text-slate-200 mt-1">
              {a.total_memory_mb ? `${(a.total_memory_mb / 1024).toFixed(1)} GB` : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Disk</dt>
            <dd className="text-slate-200 mt-1">
              {a.total_disk_gb ? `${a.total_disk_gb.toFixed(1)} GB` : '—'}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Uptime</dt>
            <dd className="text-slate-200 mt-1">{formatUptime(uptime)}</dd>
          </div>
        </dl>
      </div>

      {/* Metrics */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <GaugeBar label="CPU" percent={cpuPct} icon={Cpu} />
        <GaugeBar
          label="Memory"
          percent={memPct}
          icon={MemoryStick}
          total={a.total_memory_mb ? a.total_memory_mb / 1024 : undefined}
          used={memUsedGB}
        />
        <GaugeBar
          label="Disk"
          percent={diskPct}
          icon={HardDrive}
          total={a.total_disk_gb}
        />
      </div>

      {/* Check results */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100">Recent check results</h2>
          <span className="text-xs text-slate-500">Last 20</span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-4 py-3 w-10">Status</th>
                <th className="px-4 py-3">Check</th>
                <th className="px-4 py-3 text-right">Value</th>
                <th className="px-4 py-3">Message</th>
                <th className="px-4 py-3 text-right">Time</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {data.check_results.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-12 text-center text-slate-500">
                    No check results yet.
                  </td>
                </tr>
              ) : (
                data.check_results.slice(0, 20).map((r, i) => {
                  const t = statusTone(r.status);
                  const Icon = t.icon;
                  return (
                    <tr key={`${r.check_id}-${r.timestamp}-${i}`}>
                      <td className="px-4 py-3">
                        <Icon className={'h-4 w-4 ' + t.color} />
                      </td>
                      <td className="px-4 py-3 text-slate-200 font-mono text-xs">
                        {r.check_id}
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-300">
                        {Number.isFinite(r.value) ? r.value : '—'}
                      </td>
                      <td className="px-4 py-3 text-slate-300 truncate max-w-md">
                        {r.message || '—'}
                      </td>
                      <td className="px-4 py-3 text-right text-slate-500">
                        {formatTime(r.timestamp)}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
