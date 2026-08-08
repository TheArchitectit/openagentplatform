import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
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
import { useFocusTrap, useEscapeKey } from '@/lib/a11y';

export const Route = createFileRoute('/checks/')({
  component: ChecksListPage,
});


import { CreateCheckModal, type CheckTypeDef, type ConfigField } from './check_components'

type Filter = 'all' | 'ok' | 'warning' | 'critical' | 'disabled';

const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

const statusColor: Record<CheckStatus, string> = {
  ok: 'text-green-400',
  warning: 'text-yellow-400',
  critical: 'text-red-400',
  disabled: 'text-gray-400',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-green-500/10 border-green-800',
  warning: 'bg-yellow-500/10 border-yellow-800',
  critical: 'bg-red-500/10 border-red-800',
  disabled: 'bg-slate-500/10 border-slate-700',
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
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
            <Activity className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Checks</h1>
            <p className="text-gray-300 text-sm mt-0.5">Health checks running across your fleet.</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-green-500' : status === 'connecting' ? 'bg-yellow-500' : 'bg-slate-500')
            }
            role="status"
            aria-label={`WebSocket connection: ${status}`}
          />
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            aria-label="Refresh checks"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <Plus className="h-4 w-4" aria-hidden="true" />
            <span>Create Check</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div
          role="tablist"
          aria-label="Filter checks by status"
          className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 flex-wrap"
        >
          {(['all', 'ok', 'warning', 'critical', 'disabled'] as Filter[]).map((f) => (
            <button
              key={f}
              type="button"
              role="tab"
              aria-selected={filter === f}
              onClick={() => setFilter(f)}
              className={
                'px-3 h-8 rounded text-sm capitalize transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ' +
                (filter === f
                  ? 'bg-slate-800 text-white'
                  : 'text-gray-300 hover:text-white')
              }
            >
              {f}
              <span className="ml-2 text-xs text-gray-400" aria-hidden="true">{counts[f]}</span>
              <span className="sr-only">({counts[f]} checks)</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search check name"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search check name…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
      </div>

      {/* Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Health checks" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                <th className="px-4 py-3 w-10" scope="col">Status</th>
                <th className="px-4 py-3" scope="col">Name</th>
                <th className="px-4 py-3" scope="col">Type</th>
                <th className="px-4 py-3 text-right" scope="col">Agents</th>
                <th className="px-4 py-3" scope="col">Last Run</th>
                <th className="px-4 py-3" scope="col">Interval</th>
                <th className="px-4 py-3 text-right" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && checks.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-gray-400" role="status" aria-live="polite">
                    <div className="inline-flex items-center gap-2">
                      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
                      <span>Loading checks…</span>
                    </div>
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-red-400" role="alert">
                    Failed to load checks: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-gray-400" role="status">
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
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          void navigate({ to: '/checks/$checkId', params: { checkId: c.id } });
                        }
                      }}
                      tabIndex={0}
                      className="hover:bg-slate-800/40 cursor-pointer transition-colors focus:outline-none focus-visible:bg-slate-800/60"
                    >
                      <td className="px-4 py-3">
                        <Icon className={'h-4 w-4 ' + statusColor[k]} aria-label={`Status: ${k}`} />
                      </td>
                      <td className="px-4 py-3">
                        <span className="text-white font-medium">{c.name}</span>
                      </td>
                      <td className="px-4 py-3">
                        <span className={'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md border text-xs ' + statusBg[k]}>
                          <TypeIcon className="h-3 w-3" aria-hidden="true" />
                          {typeLabel[c.type] ?? c.type}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right tabular-nums text-white">
                        {c.assigned_agents ?? 0}
                      </td>
                      <td className="px-4 py-3 text-gray-300">{formatTime(c.last_run, now)}</td>
                      <td className="px-4 py-3 text-gray-300">{formatInterval(c.interval_secs)}</td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1" role="group" aria-label={`Actions for check ${c.name}`}>
                          <button
                            type="button"
                            onClick={() => {
                              void onRunNow(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-gray-300 hover:bg-slate-700 border border-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                            aria-label={`Run check ${c.name} now`}
                          >
                            Run
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              void onDelete(c);
                            }}
                            className="px-2 h-7 rounded text-xs text-red-400 hover:bg-red-500/10 border border-red-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                            aria-label={`Delete check ${c.name}`}
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

