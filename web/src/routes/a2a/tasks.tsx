// A2A Task Monitor — filterable, auto-refreshing table of all A2A
// tasks. Subscribes to the task-events SSE stream for live updates.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import {
  ListChecks,
  RefreshCw,
  Radio,
  CircleDot,
  CheckCircle2,
  XCircle,
  AlertCircle,
  StopCircle,
  Loader2,
} from 'lucide-react';
import { useA2ATasks, type A2ATask, type A2ATaskStatus } from '@/lib/useA2A';

export const Route = createFileRoute('/a2a/tasks')({
  component: TaskMonitorPage,
});

type FilterTab = A2ATaskStatus | 'all';

const FILTERS: { value: FilterTab; label: string; icon: React.ReactNode }[] = [
  { value: 'all', label: 'All', icon: <ListChecks className="h-3.5 w-3.5" /> },
  { value: 'pending', label: 'Pending', icon: <CircleDot className="h-3.5 w-3.5" /> },
  { value: 'working', label: 'Working', icon: <Loader2 className="h-3.5 w-3.5" /> },
  { value: 'completed', label: 'Completed', icon: <CheckCircle2 className="h-3.5 w-3.5" /> },
  { value: 'failed', label: 'Failed', icon: <XCircle className="h-3.5 w-3.5" /> },
  { value: 'cancelled', label: 'Cancelled', icon: <StopCircle className="h-3.5 w-3.5" /> },
];

function statusBadgeClasses(status: A2ATaskStatus): string {
  switch (status) {
    case 'completed':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'working':
      return 'bg-blue-500/10 text-blue-400 border-blue-500/20';
    case 'pending':
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
    case 'failed':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'cancelled':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function statusIcon(status: A2ATaskStatus) {
  switch (status) {
    case 'completed':
      return <CheckCircle2 className="h-3 w-3" />;
    case 'working':
      return <Loader2 className="h-3 w-3 animate-spin" />;
    case 'pending':
      return <CircleDot className="h-3 w-3" />;
    case 'failed':
      return <XCircle className="h-3 w-3" />;
    case 'cancelled':
      return <StopCircle className="h-3 w-3" />;
    default:
      return <AlertCircle className="h-3 w-3" />;
  }
}

function shortId(id: string): string {
  if (!id) return '—';
  if (id.length <= 12) return id;
  return id.slice(0, 8);
}

function formatDuration(start: string, end: string | undefined): string {
  if (!start) return '—';
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  if (Number.isNaN(s) || Number.isNaN(e)) return '—';
  const ms = e - s;
  if (ms < 0) return '—';
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ${sec % 60}s`;
  const hr = Math.floor(min / 60);
  return `${hr}h ${min % 60}m`;
}

function formatCost(cost: number | undefined): string {
  if (cost === undefined || cost === null) return '—';
  return `$${cost.toFixed(4)}`;
}

function TaskMonitorPage() {
  const { tasks, isLoading, error, refresh } = useA2ATasks();
  const [filter, setFilter] = useState<FilterTab>('all');

  const filtered = useMemo(() => {
    if (filter === 'all') return tasks;
    return tasks.filter((t) => t.status === filter);
  }, [tasks, filter]);

  const counts = useMemo(() => {
    const c: Record<FilterTab, number> = {
      all: tasks.length,
      pending: 0,
      working: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    };
    for (const t of tasks) c[t.status as FilterTab]++;
    return c;
  }, [tasks]);

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center" aria-hidden="true">
            <Radio className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Task Monitor</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Real-time A2A task stream with auto-refresh
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void refresh()}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* Filter tabs */}
      <nav className="flex items-center gap-1 overflow-x-auto" role="tablist" aria-label="Status filters">
        {FILTERS.map((f) => {
          const isActive = filter === f.value;
          return (
            <button
              key={f.value}
              type="button"
              role="tab"
              aria-selected={isActive}
              onClick={() => setFilter(f.value)}
              className={`inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-xs font-medium border transition-colors ${
                isActive
                  ? 'bg-blue-600 text-white border-blue-600'
                  : 'bg-slate-800 text-gray-300 border-slate-700 hover:bg-slate-700 hover:text-white'
              }`}
            >
              {f.icon}
              {f.label}
              <span className={`ml-1 px-1.5 py-0.5 rounded text-[10px] ${isActive ? 'bg-blue-700 text-white' : 'bg-slate-900 text-gray-300'}`}>
                {counts[f.value]}
              </span>
            </button>
          );
        })}
      </nav>

      {/* Error */}
      {error && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error.message}
        </div>
      )}

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Task</th>
                <th className="px-4 py-2.5 font-medium">Adapter</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Duration</th>
                <th className="px-4 py-2.5 font-medium">Cost</th>
                <th className="px-4 py-2.5 font-medium">Created</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && tasks.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading tasks…
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    No tasks found.
                  </td>
                </tr>
              ) : (
                filtered.map((t) => (
                  <tr key={t.id} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5">
                      <Link
                        to="/a2a/tasks/$taskId"
                        params={{ taskId: t.id }}
                        className="font-mono text-blue-400 hover:text-blue-300 text-xs"
                      >
                        {shortId(t.id)}
                      </Link>
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">
                      {t.adapter ?? '—'}
                    </td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded border ${statusBadgeClasses(t.status)}`}
                      >
                        {statusIcon(t.status)}
                        {t.status}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">
                      {formatDuration(t.created_at, t.completed_at)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">
                      {formatCost(t.cost)}
                    </td>
                    <td className="px-4 py-2.5 text-gray-400 text-xs">
                      {new Date(t.created_at).toLocaleString()}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
