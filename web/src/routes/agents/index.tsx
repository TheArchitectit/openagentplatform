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
      return 'bg-success';
    case 'error':
      return 'bg-danger';
    case 'offline':
    default:
      return 'bg-text-muted';
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
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center" aria-hidden="true">
            <Bot className="h-4 w-4 text-text-secondary" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">Agents</h1>
            <p className="text-text-secondary text-sm mt-0.5">
              Endpoints reporting to this site.
              <span className="ml-2 text-text-muted" aria-live="polite">
                {updatedAgo !== null ? `Updated ${updatedAgo}s ago` : '—'}
              </span>
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-success' : status === 'connecting' ? 'bg-warning' : 'bg-text-muted')
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
            aria-label="Refresh agents"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div
          role="tablist"
          aria-label="Filter agents by status"
          className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle"
        >
          {(['all', 'online', 'offline', 'error'] as StatusFilter[]).map((f) => (
            <button
              key={f}
              type="button"
              role="tab"
              aria-selected={filter === f}
              onClick={() => {
                setFilter(f);
                setPage(0);
              }}
              className={
                'px-3 h-8 rounded text-sm capitalize transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent ' +
                (filter === f
                  ? 'bg-surface-tertiary text-text-primary'
                  : 'text-text-secondary hover:text-text-primary')
              }
            >
              {f}
              <span className="ml-2 text-xs text-text-muted" aria-hidden="true">{counts[f]}</span>
              <span className="sr-only">({counts[f]} agents)</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search agents by hostname"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(0);
            }}
            placeholder="Search hostname…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Agents" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                <th className="px-4 py-3 w-10" scope="col">Status</th>
                <th className="px-4 py-3" scope="col">Hostname</th>
                <th className="px-4 py-3" scope="col">Site</th>
                <th className="px-4 py-3" scope="col">OS</th>
                <th className="px-4 py-3" scope="col">Last seen</th>
                <th className="px-4 py-3 text-right" scope="col">CPU</th>
                <th className="px-4 py-3 text-right" scope="col">Memory</th>
                <th className="px-4 py-3 text-right" scope="col">Disk</th>
                <th className="px-4 py-3 text-right" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading && agents.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-text-muted" role="status" aria-live="polite">
                    Loading agents…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-danger" role="alert">
                    Failed to load agents: {error.message}
                  </td>
                </tr>
              ) : paged.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-4 py-12 text-center text-text-muted" role="status">
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
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          void navigate({ to: '/agents/$agentId', params: { agentId: a.id } });
                        }
                      }}
                      tabIndex={0}
                      className="hover:bg-surface-tertiary/40 cursor-pointer transition-colors focus:outline-none focus-visible:bg-surface-tertiary/60"
                    >
                      <td className="px-4 py-3">
                        <span
                          className={'inline-block h-2.5 w-2.5 rounded-full ' + statusColor(k)}
                          role="status"
                          aria-label={`Status: ${k}`}
                        />
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <Bot className="h-4 w-4 text-text-muted" aria-hidden="true" />
                          <span className="text-text-primary font-medium">{a.hostname || a.id}</span>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-text-secondary">{a.site_id || '—'}</td>
                      <td className="px-4 py-3 text-text-secondary">{a.os || '—'}</td>
                      <td className="px-4 py-3 text-text-secondary">{formatLastSeen(a.last_seen, now)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-text-primary">{pct(a.cpu_percent)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-text-primary">{pct(a.mem_percent)}</td>
                      <td className="px-4 py-3 text-right tabular-nums text-text-primary">{pct(a.disk_percent)}</td>
                      <td className="px-4 py-3 text-right">
                        <div className="inline-flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                          {k === 'online' ? (
                            <CircleCheck className="h-4 w-4 text-success" aria-label="Online" />
                          ) : k === 'error' ? (
                            <CircleAlert className="h-4 w-4 text-danger" aria-label="Error" />
                          ) : (
                            <CircleX className="h-4 w-4 text-text-muted" aria-label="Offline" />
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
        <div className="px-4 py-3 border-t border-border-subtle flex items-center justify-between text-sm">
          <div className="text-text-muted" aria-live="polite">
            Showing{' '}
            <span className="text-text-secondary">
              {filtered.length === 0 ? 0 : currentPage * PAGE_SIZE + 1}
            </span>
            –
            <span className="text-text-secondary">
              {Math.min((currentPage + 1) * PAGE_SIZE, filtered.length)}
            </span>{' '}
            of <span className="text-text-secondary">{filtered.length}</span>
          </div>
          <div className="flex items-center gap-1" role="navigation" aria-label="Pagination">
            <button
              type="button"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={currentPage === 0}
              aria-label="Previous page"
              className="h-8 w-8 inline-flex items-center justify-center rounded-md border border-border-strong bg-surface-tertiary text-text-secondary disabled:opacity-40 hover:bg-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
            >
              <ChevronLeft className="h-4 w-4" aria-hidden="true" />
            </button>
            <span className="px-2 text-text-secondary tabular-nums" aria-label={`Page ${currentPage + 1} of ${totalPages}`}>
              {currentPage + 1} / {totalPages}
            </span>
            <button
              type="button"
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={currentPage >= totalPages - 1}
              aria-label="Next page"
              className="h-8 w-8 inline-flex items-center justify-center rounded-md border border-border-strong bg-surface-tertiary text-text-secondary disabled:opacity-40 hover:bg-border-strong focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
            >
              <ChevronRight className="h-4 w-4" aria-hidden="true" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
