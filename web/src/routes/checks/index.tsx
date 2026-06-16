import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import {
  Activity,
  RefreshCw,
  Search,
  Plus,
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  X,
  Loader2,
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
import { toast } from 'sonner';
import { useChecks, type Check, type CheckStatus, type CheckType } from '@/lib/useChecks';

export const Route = createFileRoute('/checks/')({
  component: ChecksListPage,
});

// ---------------------------------------------------------------------------
// Status / type display
// ---------------------------------------------------------------------------

type Filter = 'all' | 'ok' | 'warning' | 'critical' | 'disabled';

const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

const statusColor: Record<CheckStatus, string> = {
  ok: 'text-emerald-500',
  warning: 'text-amber-500',
  critical: 'text-rose-500',
  disabled: 'text-slate-500',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-emerald-500/10 border-emerald-500/30',
  warning: 'bg-amber-500/10 border-amber-500/30',
  critical: 'bg-rose-500/10 border-rose-500/30',
  disabled: 'bg-slate-500/10 border-slate-500/30',
};

const typeIcon: Record<CheckType, typeof Globe> = {
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

const typeLabel: Record<CheckType, string> = {
  http: 'HTTP',
  tcp: 'TCP',
  ping: 'Ping',
  disk_usage: 'Disk',
  memory_usage: 'Memory',
  cpu_usage: 'CPU',
  process: 'Process',
  service: 'Service',
  tls_cert: 'TLS Cert',
  script: 'Script',
  log_watch: 'Log Watch',
};

function deriveStatus(c: Check): CheckStatus {
  if (!c.enabled) return 'disabled';
  return (c.last_status ?? 'disabled') as CheckStatus;
}

function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

function ChecksListPage() {
  const navigate = useNavigate();
  const { checks, isLoading, error, refresh, status, createCheck, deleteCheck, runCheck } = useChecks();
  const [filter, setFilter] = useState<Filter>('all');
  const [query, setQuery] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [createOpen, setCreateOpen] = useState(false);

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return checks.filter((c) => {
      const k = deriveStatus(c);
      if (filter !== 'all' && k !== filter) return false;
      if (q && !c.name.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [checks, filter, query]);

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: checks.length, ok: 0, warning: 0, critical: 0, disabled: 0 };
    for (const x of checks) {
      const k = deriveStatus(x);
      c[k] = (c[k] ?? 0) + 1;
    }
    return c;
  }, [checks]);

  const onDelete = async (c: Check) => {
    if (!confirm(`Delete check "${c.name}"?`)) return;
    try {
      await deleteCheck(c.id);
      toast.success(`Deleted "${c.name}"`);
    } catch (e) {
      toast.error(`Delete failed: ${(e as Error).message}`);
    }
  };

  const onRunNow = async (c: Check) => {
    try {
      await runCheck(c.id);
      toast.success(`Triggered "${c.name}"`);
    } catch (e) {
      toast.error(`Run failed: ${(e as Error).message}`);
    }
  };

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <Activity className="h-4 w-4 text-slate-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Checks</h1>
            <p className="text-slate-400 text-sm mt-0.5">Health checks running across your fleet.</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-emerald-500' : status === 'connecting' ? 'bg-amber-500' : 'bg-slate-500')
            }
            title={`WebSocket: ${status}`}
          />
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-slate-200 disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white transition-colors"
          >
            <Plus className="h-4 w-4" />
            <span>Create Check</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 flex-wrap">
          {(['all', 'ok', 'warning', 'critical', 'disabled'] as Filter[]).map((f) => (
            <button
              key={f}
              type="button"
              onClick={() => setFilter(f)}
              className={
                'px-3 h-8 rounded text-sm capitalize transition-colors ' +
                (filter === f
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:text-slate-200')
              }
            >
              {f}
              <span className="ml-2 text-xs text-slate-500">{counts[f]}</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search check name…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-4 py-3 w-10">Status</th>
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Type</th>
                <th className="px-4 py-3 text-right">Agents</th>
                <th className="px-4 py-3">Last Run</th>
                <th className="px-4 py-3">Interval</th>
                <th className="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && checks.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-slate-500">
                    <div className="inline-flex items-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>Loading checks…</span>
                    </div>
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-rose-400">
                    Failed to load checks: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-slate-500">
                    No checks match the current filter.
                  </td>
                </tr>
              ) : (
                filtered.map((c) => {
                  const k = deriveStatus(c);
                  const Icon = statusIcon[k];
                  const TypeIcon = typeIcon[c.type] ?? Activity;
                  return (
                    <tr
                      key={c.id}
                      onClick={() => {
                        void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
                      }}
                      className="hover:bg-slate-800/40 cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3">
                        <Icon className={'h-4 w-4 ' + statusColor[k]} title={k} />
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-slate-100 font-medium">{c.name}</span>
                      </td>
                      <td className="px-4 py-3">
                        <span className={'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md border text-xs ' + statusBg[k]}>
                          <TypeIcon className="h-3 w-3" />
                          {typeLabel[c.type] ?? c.type}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-200">
                        {c.assigned_agents ?? 0}
                      </td>
                      <td className="px-4 py-3 text-slate-400">{formatTime(c.last_run, now)}</td>
                      <td className="px-4 py-3 text-slate-300">{formatInterval(c.interval_secs)}</td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1">
                          <button
                            type="button"
                            onClick={() => {
                              void onRunNow(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-slate-300 hover:bg-slate-700 border border-slate-700"
                            title="Run now"
                          >
                            Run
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              void onDelete(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-rose-300 hover:bg-rose-500/10 border border-rose-500/30"
                            title="Delete"
                          >
                            Delete
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {createOpen && (
        <CreateCheckModal
          onClose={() => setCreateOpen(false)}
          onSubmit={async (input) => {
            try {
              const c = await createCheck(input);
              toast.success(`Created "${c.name}"`);
              setCreateOpen(false);
              void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
            } catch (e) {
              toast.error(`Create failed: ${(e as Error).message}`);
            }
          }}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Create modal
// ---------------------------------------------------------------------------

interface CheckTypeDef {
  label: string;
  icon: typeof Globe;
  defaults: Record<string, unknown>;
  fields: ConfigField[];
}

interface ConfigField {
  key: string;
  label: string;
  type: 'text' | 'number' | 'select';
  placeholder?: string;
  options?: { value: string; label: string }[];
  required?: boolean;
}

const checkTypeDefs: Record<CheckType, CheckTypeDef> = {
  http: {
    label: 'HTTP',
    icon: Globe,
    defaults: { url: '', method: 'GET', expected_status: 200, timeout_secs: 10 },
    fields: [
      { key: 'url', label: 'URL', type: 'text', placeholder: 'https://example.com/health', required: true },
      { key: 'method', label: 'Method', type: 'select', options: [
        { value: 'GET', label: 'GET' },
        { value: 'POST', label: 'POST' },
        { value: 'HEAD', label: 'HEAD' },
      ]},
      { key: 'expected_status', label: 'Expected status code', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  tcp: {
    label: 'TCP',
    icon: Network,
    defaults: { host: '', port: 443, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  ping: {
    label: 'Ping',
    icon: Radio,
    defaults: { host: '', count: 3, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'count', label: 'Packet count', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  disk_usage: {
    label: 'Disk Usage',
    icon: HardDrive,
    defaults: { path: '/', warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'path', label: 'Path', type: 'text', placeholder: '/', required: true },
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  memory_usage: {
    label: 'Memory Usage',
    icon: MemoryStick,
    defaults: { warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  cpu_usage: {
    label: 'CPU Usage',
    icon: Cpu,
    defaults: { warn_pct: 80, crit_pct: 95, window_secs: 30 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
      { key: 'window_secs', label: 'Sample window (s)', type: 'number' },
    ],
  },
  process: {
    label: 'Process',
    icon: ServerCog,
    defaults: { name: '', expected: 'running' },
    fields: [
      { key: 'name', label: 'Process name', type: 'text', placeholder: 'nginx', required: true },
      { key: 'expected', label: 'Expected state', type: 'select', options: [
        { value: 'running', label: 'Running' },
        { value: 'stopped', label: 'Stopped' },
      ]},
    ],
  },
  service: {
    label: 'Service',
    icon: ServerCog,
    defaults: { name: '' },
    fields: [
      { key: 'name', label: 'Service name', type: 'text', placeholder: 'nginx.service', required: true },
    ],
  },
  tls_cert: {
    label: 'TLS Certificate',
    icon: ShieldCheck,
    defaults: { host: '', port: 443, warn_days: 30, crit_days: 7 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number' },
      { key: 'warn_days', label: 'Warn (days remaining)', type: 'number' },
      { key: 'crit_days', label: 'Critical (days remaining)', type: 'number' },
    ],
  },
  script: {
    label: 'Script',
    icon: FileCode2,
    defaults: { script_id: '', timeout_secs: 30 },
    fields: [
      { key: 'script_id', label: 'Script ID', type: 'text', placeholder: 'script-uuid', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  log_watch: {
    label: 'Log Watch',
    icon: ScrollText,
    defaults: { path: '', pattern: '' },
    fields: [
      { key: 'path', label: 'Log path', type: 'text', placeholder: '/var/log/syslog', required: true },
      { key: 'pattern', label: 'Regex pattern', type: 'text', required: true },
    ],
  },
};

const allCheckTypes: CheckType[] = [
  'http', 'tcp', 'ping', 'disk_usage', 'memory_usage', 'cpu_usage',
  'process', 'service', 'tls_cert', 'script', 'log_watch',
];

interface CreateCheckModalProps {
  onClose: () => void;
  onSubmit: (input: { name: string; type: CheckType; config: Record<string, unknown>; interval_secs: number }) => Promise<void>;
}

function CreateCheckModal({ onClose, onSubmit }: CreateCheckModalProps) {
  const [name, setName] = useState('');
  const [type, setType] = useState<CheckType>('http');
  const [interval, setInterval] = useState(60);
  const [config, setConfig] = useState<Record<string, unknown>>({ ...checkTypeDefs.http.defaults });
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const onChangeType = (next: CheckType) => {
    setType(next);
    setConfig({ ...checkTypeDefs[next].defaults });
  };

  const setField = (key: string, value: unknown) => {
    setConfig((prev) => ({ ...prev, [key]: value }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (interval < 10) {
      setError('Interval must be at least 10 seconds');
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit({ name: name.trim(), type, config, interval_secs: interval });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setSubmitting(false);
    }
  };

  const def = checkTypeDefs[type];

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-lg rounded-lg border border-slate-800 bg-slate-900 shadow-xl">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100">Create Check</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-slate-400 hover:text-slate-100 hover:bg-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div>
            <label className="block text-xs text-slate-400 mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Disk usage on prod"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Type</label>
            <select
              value={type}
              onChange={(e) => onChangeType(e.target.value as CheckType)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            >
              {allCheckTypes.map((t) => (
                <option key={t} value={t}>
                  {checkTypeDefs[t].label}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Interval (seconds, min 10)</label>
            <input
              type="number"
              value={interval}
              min={10}
              onChange={(e) => setInterval(Number(e.target.value) || 60)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
          </div>

          <div className="rounded-md border border-slate-800 bg-slate-950/40 p-3 space-y-3">
            <p className="text-xs text-slate-500 uppercase tracking-wider">Config ({def.label})</p>
            {def.fields.map((f) => (
              <div key={f.key}>
                <label className="block text-xs text-slate-400 mb-1">{f.label}</label>
                {f.type === 'select' ? (
                  <select
                    value={String(config[f.key] ?? '')}
                    onChange={(e) => setField(f.key, e.target.value)}
                    className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                  >
                    {f.options?.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    type={f.type}
                    value={String(config[f.key] ?? '')}
                    placeholder={f.placeholder}
                    onChange={(e) => setField(f.key, f.type === 'number' ? Number(e.target.value) : e.target.value)}
                    className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                  />
                )}
              </div>
            ))}
          </div>

          {error && (
            <div className="rounded-md border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-xs text-rose-300">
              {error}
            </div>
          )}

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-slate-200 hover:bg-slate-700 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={submitting}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 text-sm text-white disabled:opacity-50 transition-colors"
            >
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{submitting ? 'Creating…' : 'Create Check'}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
