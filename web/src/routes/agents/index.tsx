import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import {
  Bot,
  RefreshCw,
  Search,
  CircleCheck,
  CircleX,
  CircleAlert,
  ChevronLeft,
  ChevronRight,
} from 'lucide-react';
import { useAgents, type Agent } from '@/lib/useAgents';

export const Route = createFileRoute('/agents/')({
  component: AgentsListPage,
});

type StatusFilter = 'all' | 'online' | 'offline' | 'error';
type StatusKind = 'online' | 'offline' | 'error';

const PAGE_SIZE = 50;

function deriveStatus(a: Agent, now: number): StatusKind {
  if (a.status === 'offline' || a.status === 'error') return a.status as StatusKind;
  const last = a.last_seen ? new Date(a.last_seen).getTime() : 0;
  const ageSec = last > 0 ? (now - last) / 1000 : Number.POSITIVE_INFINITY;
  if (ageSec > 300) return 'offline';
  return 'online';
}

function statusColor(kind: StatusKind): string {
  switch (kind) {
    case 'online':
      return 'bg-emerald-500';
    case 'error':
      return 'bg-rose-500';
    case 'offline':
    default:
      return 'bg-slate-500';
  }
}

function formatLastSeen(iso: string, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

function pct(n: number | undefined): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '—';
  return `${n.toFixed(1)}%`;
}

function AgentsListPage() {
  const navigate = useNavigate();
  const { agents, isLoading, error, refresh, status } = useAgents();
  const [filter, setFilter] = useState<StatusFilter>('all');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const [updatedAt, setUpdatedAt] = useState<number | null>(null);

  // Re-render once a second so "last seen" ages naturally and the
  // "updated X seconds ago" label stays current.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  useEffect(() => {
    if (!isLoading) setUpdatedAt(Date.now());
  }, [agents, isLoading]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return agents.filter((a) => {
      const k = deriveStatus(a, now);
      if (filter === 'online' && k !== 'online') return false;
      if (filter === 'offline' && k !== 'offline') return false;
      if (filter === 'error' && k !== 'error') return false;
      if (q && !a.hostname.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [agents, filter, query, now]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const currentPage = Math.min(page, totalPages - 1);
  const paged = filtered.slice(currentPage * PAGE_SIZE, (currentPage + 1) * PAGE_SIZE);

  const counts = useMemo(() => {
    const c: Record<StatusFilter, number> = { all: agents.length, online: 0, offline: 0, error: 0 };
    for (const a of agents) {
      const k = deriveStatus(a, now);
      c[k] = (c[k] ?? 0) + 1;
    }
    return c;
  }, [agents, now]);

  const updatedAgo = updatedAt ? Math.max(0, Math.floor((now - updatedAt) / 1000)) : null;

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <Bot className="h-4 w-4 text-slate-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Agents</h1>
            <p className="text-slate-400 text-sm mt-0.5">
              Endpoints reporting to this site.
              <span className="ml-2 text-slate-500">
                {updatedAgo !== null ? `Updated ${updatedAgo}s ago` : '—'}
              </span>
            </p>
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
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800">
          {(['all', 'online', 'offline', 'error'] as StatusFilter[]).map((f) => (
            <button
              key={f}
              type="button"
              onClick={() => {
                setFilter(f);
                setPage(0);
              }}
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
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(0);
            }}
            placeholder="Search hostname…"
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
                <th className="px-4 py-3">Hostname</th>
                <th className="px-4 py-3">Site</th>
                <th className="px-4 py-3">OS</th>
                <th className="px-4 py-3">Last seen</th>
                <th className="px-4 py-3 text-right">CPU</th>
                <th className="px-4 py-3 text-right">Memory</th>
                <th className="px-4 py-3 text-right">Disk</th>
                <th className="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && agents.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-slate-500">
                    Loading agents…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-rose-400">
                    Failed to load agents: {error.message}
                  </td>
                </tr>
              ) : paged.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-slate-500">
                    No agents match the current filter.
                  </td>
                </tr>
              ) : (
                paged.map((a) => {
                  const k = deriveStatus(a, now);
                  return (
                    <tr
                      key={a.id}
                      onClick={() => {
                        void navigate({ to: '/agents/$agentId', params: { agentId: a.id } });
                      }}
                      className="hover:bg-slate-800/40 cursor-pointer transition-colors"
                    >
                      <td className="px-4 py-3">
                        <span
                          className={'inline-block h-2.5 w-2.5 rounded-full ' + statusColor(k)}
                          title={k}
                        />
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <Bot className="h-4 w-4 text-slate-500" />
                          <span className="text-slate-100 font-medium">{a.hostname || a.id}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-slate-300">{a.site_id || '—'}</td>
                      <td className="px-4 py-3 text-slate-300">{a.os || '—'}</td>
                      <td className="px-4 py-3 text-slate-400">{formatLastSeen(a.last_seen, now)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-200">{pct(a.cpu_percent)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-200">{pct(a.mem_percent)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-slate-200">{pct(a.disk_percent)}</td>
                      <td className="px-4 py-3 text-right">
                        <div className="inline-flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                          {k === 'online' ? (
                            <CircleCheck className="h-4 w-4 text-emerald-500" />
                          ) : k === 'error' ? (
                            <CircleAlert className="h-4 w-4 text-rose-500" />
                          ) : (
                            <CircleX className="h-4 w-4 text-slate-500" />
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        <div className="px-4 py-3 border-t border-slate-800 flex items-center justify-between text-sm">
          <div className="text-slate-500">
            Showing{' '}
            <span className="text-slate-300">
              {filtered.length === 0 ? 0 : currentPage * PAGE_SIZE + 1}
            </span>
            –
            <span className="text-slate-300">
              {Math.min((currentPage + 1) * PAGE_SIZE, filtered.length)}
            </span>{' '}
            of <span className="text-slate-300">{filtered.length}</span>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={currentPage === 0}
              className="h-8 w-8 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-slate-300 disabled:opacity-40 hover:bg-slate-700 transition-colors"
            >
              <ChevronLeft className="h-4 w-4" />
            </button>
            <span className="px-2 text-slate-400 tabular-nums">
              {currentPage + 1} / {totalPages}
            </span>
            <button
              type="button"
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={currentPage >= totalPages - 1}
              className="h-8 w-8 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-slate-300 disabled:opacity-40 hover:bg-slate-700 transition-colors"
            >
              <ChevronRight className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
