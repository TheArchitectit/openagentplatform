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

function shortId(id: string): string {
  if (!id) return '—';
  if (id.length <= 12) return id;
  return id.slice(0, 8);
}

function RecordingsListPage() {
  const navigate = useNavigate();
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [page, setPage] = useState(0);

  const fetchRecordings = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({
        limit: String(PAGE_SIZE),
        offset: String(page * PAGE_SIZE),
      });
      if (dateFrom) params.set('started_after', new Date(dateFrom).toISOString());
      if (dateTo) params.set('started_before', new Date(dateTo).toISOString());
      const res = await apiFetch(`/api/v1/shell/recordings?${params}`);
      if (!res.ok) throw new Error(`Failed to load recordings (${res.status})`);
      const data: ListResponse = await res.json();
      setRecordings(data.recordings ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void fetchRecordings();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim();
    if (!q) return recordings;
    return recordings.filter(
      (r) =>
        r.agent_id.toLowerCase().includes(q) ||
        r.user_id.toLowerCase().includes(q) ||
        r.session_id.toLowerCase().includes(q)
    );
  }, [recordings, search]);

  const handleExport = () => {
    const blob = new Blob([JSON.stringify(filtered, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `shell-recordings-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
            <Terminal className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Shell Recordings</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Recorded remote shell sessions — click a row to play back
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void fetchRecordings()}
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            type="button"
            onClick={handleExport}
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <Download className="h-4 w-4" />
            Export
          </button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-2 flex-wrap">
        <div className="relative w-full sm:w-64" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search recordings"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by agent, user, or session…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <div className="flex items-center gap-1.5">
          <Calendar className="h-3.5 w-3.5 text-gray-400" />
          <input
            type="date"
            value={dateFrom}
            onChange={(e) => setDateFrom(e.target.value)}
            aria-label="From date"
            className="h-9 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <span className="text-xs text-gray-400">to</span>
          <input
            type="date"
            value={dateTo}
            onChange={(e) => setDateTo(e.target.value)}
            aria-label="To date"
            className="h-9 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <button
            type="button"
            onClick={() => void fetchRecordings()}
            className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            Apply
          </button>
        </div>
      </div>

      {error && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error}
        </div>
      )}

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Session</th>
                <th className="px-4 py-2.5 font-medium">Agent</th>
                <th className="px-4 py-2.5 font-medium">User</th>
                <th className="px-4 py-2.5 font-medium">Protocol</th>
                <th className="px-4 py-2.5 font-medium">Started</th>
                <th className="px-4 py-2.5 font-medium">Duration</th>
                <th className="px-4 py-2.5 font-medium text-right">Events</th>
                <th className="px-4 py-2.5 font-medium text-right">In / Out</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && recordings.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading recordings…
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
                    No recordings found.
                  </td>
                </tr>
              ) : (
                filtered.map((r) => (
                  <tr
                    key={r.session_id}
                    onClick={() => void navigate({ to: '/shell-recordings/$sessionId', params: { sessionId: r.session_id } })}
                    className="hover:bg-slate-800/40 transition-colors cursor-pointer"
                  >
                    <td className="px-4 py-2.5 font-mono text-blue-400 text-xs">
                      {shortId(r.session_id)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">{r.agent_id}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">{r.user_id}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs uppercase">{r.protocol}</td>
                    <td className="px-4 py-2.5 text-gray-400 text-xs">
                      {new Date(r.started_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">{r.duration}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {r.event_count.toLocaleString()}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {formatBytes(r.bytes_in)} / {formatBytes(r.bytes_out)}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Pagination */}
      <div className="flex items-center justify-between text-xs text-gray-400">
        <span>Page {page + 1}</span>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setPage((p) => Math.max(0, p - 1))}
            disabled={page === 0}
            className="inline-flex items-center gap-1 px-2 h-7 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Prev
          </button>
          <button
            type="button"
            onClick={() => setPage((p) => p + 1)}
            disabled={recordings.length < PAGE_SIZE}
            className="inline-flex items-center gap-1 px-2 h-7 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
