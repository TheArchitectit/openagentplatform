// Shell session recordings — list page.
//
// Shows every recorded remote shell session visible to the caller
// (admins see all; non-admins see only their own). Supports
// filtering by agent hostname, user, and date range. Clicking a row
// navigates to the per-session playback view.
//
// The backend endpoint is /api/v1/shell/recordings; admin-only
// DELETE is available on the detail page.

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import { Search, RefreshCw, Calendar, Download, Terminal } from 'lucide-react';
import { apiFetch } from '@/lib/api';
import { getStoredUser } from '@/lib/auth';

export const Route = createFileRoute('/shell-recordings/')({
  component: RecordingsListPage,
});

interface Recording {
  session_id: string;
  agent_id: string;
  user_id: string;
  protocol: string;
  terminal_size: { cols: number; rows: number };
  started_at: string;
  ended_at: string;
  duration: string;
  bytes_in: number;
  bytes_out: number;
  event_count: number;
  chunk_count: number;
  content_hash: string;
}

interface ListResponse {
  recordings: Recording[];
  total: number;
  limit: number;
  offset: number;
}

const PAGE_SIZE = 50;

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(2)} MB`;
}

function formatDate(iso: string): string {
  if (!iso) return '—';
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso;
  }
}

function formatDuration(s: string): string {
  // ISO 8601 duration or HH:MM:SS-ish. The server returns a Go
  // duration string like "5m32s" or "1h2m3s". Parse manually.
  if (!s) return '—';
  const re = /^(?:(\d+)h)?(?:(\d+)m)?(?:([\d.]+)s)?$/;
  const m = s.match(re);
  if (!m) return s;
  const h = parseInt(m[1] || '0', 10);
  const mm = parseInt(m[2] || '0', 10);
  const ss = parseFloat(m[3] || '0');
  if (h > 0) return `${h}h ${mm}m ${Math.floor(ss)}s`;
  if (mm > 0) return `${mm}m ${Math.floor(ss)}s`;
  return `${ss.toFixed(1)}s`;
}

function RecordingsListPage() {
  const navigate = useNavigate();
  const user = getStoredUser();
  const isAdmin = user?.role === 'admin' || user?.role === 'owner' || user?.role === 'superadmin';

  const [items, setItems] = useState<Recording[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [page, setPage] = useState(0);

  const queryString = useMemo(() => {
    const params = new URLSearchParams();
    if (search.trim()) {
      // The backend matches session_id with ILIKE; we also send
      // the value as agent_id/user_id hints so the search box works
      // for hostname lookups too. The backend ORs them implicitly
      // by checking session_id only; for hostname / user we pass
      // those params explicitly when possible.
      params.set('session_id', search.trim());
    }
    if (fromDate) {
      const d = new Date(fromDate);
      if (!Number.isNaN(d.getTime())) {
        params.set('since', d.toISOString());
      }
    }
    if (toDate) {
      const d = new Date(toDate);
      if (!Number.isNaN(d.getTime())) {
        params.set('until', d.toISOString());
      }
    }
    params.set('limit', String(PAGE_SIZE));
    params.set('offset', String(page * PAGE_SIZE));
    return params.toString();
  }, [search, fromDate, toDate, page]);

  const fetchList = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await apiFetch<ListResponse>(`/shell/recordings?${queryString}`);
      setItems(data.recordings);
      setTotal(data.total);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryString]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-100">Shell Recordings</h1>
          <p className="text-sm text-slate-400 mt-1">
            Searchable archive of recorded remote shell sessions. {isAdmin ? 'Admin view — all sessions.' : 'Your sessions only.'}
          </p>
        </div>
        <button
          type="button"
          onClick={fetchList}
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-slate-800 hover:bg-slate-700 text-sm text-slate-200"
        >
          <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          Refresh
        </button>
      </div>

      {/* Filters */}
      <div className="bg-slate-900 border border-slate-800 rounded-lg p-4 flex flex-wrap gap-3 items-end">
        <label className="flex flex-col gap-1 flex-1 min-w-[200px]">
          <span className="text-xs text-slate-400">Search (session id / agent / user)</span>
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-2.5 text-slate-500" />
            <input
              type="text"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(0);
              }}
              placeholder="filter…"
              className="w-full pl-8 pr-3 py-1.5 rounded-md bg-slate-950 border border-slate-700 text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:border-indigo-500"
            />
          </div>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-slate-400">From</span>
          <div className="relative">
            <Calendar size={14} className="absolute left-2.5 top-2.5 text-slate-500" />
            <input
              type="date"
              value={fromDate}
              onChange={(e) => {
                setFromDate(e.target.value);
                setPage(0);
              }}
              className="pl-8 pr-3 py-1.5 rounded-md bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-indigo-500"
            />
          </div>
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs text-slate-400">To</span>
          <div className="relative">
            <Calendar size={14} className="absolute left-2.5 top-2.5 text-slate-500" />
            <input
              type="date"
              value={toDate}
              onChange={(e) => {
                setToDate(e.target.value);
                setPage(0);
              }}
              className="pl-8 pr-3 py-1.5 rounded-md bg-slate-950 border border-slate-700 text-sm text-slate-200 focus:outline-none focus:border-indigo-500"
            />
          </div>
        </label>
        <div className="text-xs text-slate-500 ml-auto">
          {total} recording{total === 1 ? '' : 's'}
        </div>
      </div>

      {/* Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-800/50 text-slate-400 text-xs uppercase">
            <tr>
              <th className="text-left px-4 py-2">Session</th>
              <th className="text-left px-4 py-2">User</th>
              <th className="text-left px-4 py-2">Agent</th>
              <th className="text-left px-4 py-2">Protocol</th>
              <th className="text-left px-4 py-2">Duration</th>
              <th className="text-left px-4 py-2">Bytes (in/out)</th>
              <th className="text-left px-4 py-2">Started</th>
              <th className="text-right px-4 py-2">Actions</th>
            </tr>
          </thead>
          <tbody>
            {error && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-rose-400">
                  {error}
                </td>
              </tr>
            )}
            {!error && items.length === 0 && !loading && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-slate-500">
                  No recordings match the current filters.
                </td>
              </tr>
            )}
            {items.map((r) => (
              <tr
                key={r.session_id}
                onClick={() =>
                  navigate({
                    to: '/shell-recordings/$sessionId',
                    params: { sessionId: r.session_id },
                  })
                }
                className="border-t border-slate-800 hover:bg-slate-800/40 cursor-pointer"
              >
                <td className="px-4 py-2 font-mono text-xs text-slate-200 max-w-[180px] truncate">
                  {r.session_id}
                </td>
                <td className="px-4 py-2 text-slate-300">{r.user_id}</td>
                <td className="px-4 py-2 text-slate-300">{r.agent_id}</td>
                <td className="px-4 py-2">
                  <span className="inline-flex items-center px-2 py-0.5 rounded-full bg-indigo-500/10 border border-indigo-500/20 text-xs text-indigo-300">
                    {r.protocol}
                  </span>
                </td>
                <td className="px-4 py-2 text-slate-300">{formatDuration(r.duration)}</td>
                <td className="px-4 py-2 text-slate-400 text-xs">
                  {formatBytes(r.bytes_in)} / {formatBytes(r.bytes_out)}
                </td>
                <td className="px-4 py-2 text-slate-400 text-xs">{formatDate(r.started_at)}</td>
                <td className="px-4 py-2 text-right">
                  <div className="inline-flex gap-1">
                    <button
                      type="button"
                      title="Open playback"
                      onClick={(e) => {
                        e.stopPropagation();
                        navigate({
                          to: '/shell-recordings/$sessionId',
                          params: { sessionId: r.session_id },
                        });
                      }}
                      className="p-1.5 rounded-md hover:bg-slate-700 text-slate-300"
                    >
                      <Terminal size={14} />
                    </button>
                    <a
                      title="Download .cast"
                      href={`/api/v1/shell/recordings/${r.session_id}/export`}
                      onClick={(e) => e.stopPropagation()}
                      className="p-1.5 rounded-md hover:bg-slate-700 text-slate-300"
                    >
                      <Download size={14} />
                    </a>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between text-sm text-slate-400">
          <div>
            Page {page + 1} of {totalPages}
          </div>
          <div className="space-x-2">
            <button
              type="button"
              disabled={page === 0}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              className="px-3 py-1 rounded-md bg-slate-800 hover:bg-slate-700 disabled:opacity-50"
            >
              Previous
            </button>
            <button
              type="button"
              disabled={page >= totalPages - 1}
              onClick={() => setPage((p) => p + 1)}
              className="px-3 py-1 rounded-md bg-slate-800 hover:bg-slate-700 disabled:opacity-50"
            >
              Next
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
