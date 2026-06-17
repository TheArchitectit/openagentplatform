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
  { value: 'input_required', label: 'Input Required', icon: <AlertCircle className="h-3.5 w-3.5" /> },
  { value: 'completed', label: 'Completed', icon: <CheckCircle2 className="h-3.5 w-3.5" /> },
  { value: 'failed', label: 'Failed', icon: <XCircle className="h-3.5 w-3.5" /> },
  { value: 'cancelled', label: 'Cancelled', icon: <StopCircle className="h-3.5 w-3.5" /> },
];

function shortId(id: string): string {
  if (!id) return '—';
  return id.length > 12 ? `${id.slice(0, 8)}…` : id;
}

function formatDuration(ms?: number): string {
  if (ms === undefined || ms === null) return '—';
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${Math.floor(s % 60)}s`;
}

function formatCost(cost?: number): string {
  if (cost === undefined || cost === null) return '—';
  return `$${cost.toFixed(4)}`;
}

function formatTokens(n?: number): string {
  if (n === undefined || n === null) return '—';
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

function formatTime(iso: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  const now = Date.now();
  const diff = (now - d.getTime()) / 1000;
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  return d.toLocaleString();
}

function statusBadge(status: A2ATaskStatus): { classes: string; icon: React.ReactNode; label: string } {
  switch (status) {
    case 'pending':
      return { classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20', icon: <CircleDot className="h-3 w-3" />, label: 'Pending' };
    case 'working':
      return { classes: 'bg-info/10 text-info border-info/20', icon: <Loader2 className="h-3 w-3 animate-spin" />, label: 'Working' };
    case 'input_required':
      return { classes: 'bg-warning/10 text-warning border-warning/20', icon: <AlertCircle className="h-3 w-3" />, label: 'Input Required' };
    case 'completed':
      return { classes: 'bg-success/10 text-success border-success/20', icon: <CheckCircle2 className="h-3 w-3" />, label: 'Completed' };
    case 'failed':
      return { classes: 'bg-danger/10 text-danger border-danger/20', icon: <XCircle className="h-3 w-3" />, label: 'Failed' };
    case 'cancelled':
      return { classes: 'bg-text-muted/10 text-text-secondary border-text-muted/20', icon: <StopCircle className="h-3 w-3" />, label: 'Cancelled' };
  }
}

function TaskMonitorPage() {
  const [activeFilter, setActiveFilter] = useState<FilterTab>('all');
  const { tasks, isLoading, error, refresh, sseConnected } = useA2ATasks({
    status: activeFilter === 'all' ? undefined : (activeFilter as A2ATaskStatus),
  });

  const counts = useMemo(() => {
    const c: Record<FilterTab, number> = {
      all: 0,
      pending: 0,
      working: 0,
      input_required: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
    };
    for (const t of tasks) {
      c.all += 1;
      if (t.status in c) c[t.status] += 1;
    }
    return c;
  }, [tasks]);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-text-primary flex items-center gap-2">
            <ListChecks className="h-6 w-6 text-accent" />
            Task Monitor
          </h1>
          <p className="text-sm text-text-secondary mt-1 flex items-center gap-2">
            Live A2A task telemetry
            <span className="inline-flex items-center gap-1 text-xs">
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  sseConnected ? 'bg-success' : 'bg-text-muted'
                }`}
              />
              {sseConnected ? 'Live' : 'Disconnected'}
            </span>
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md border border-border-strong bg-surface-tertiary hover:bg-border-strong text-text-primary"
        >
          <RefreshCw className="h-4 w-4" />
          Refresh
        </button>
      </div>

      {/* Filter tabs */}
      <div className="flex flex-wrap gap-1 border-b border-border-subtle pb-0">
        {FILTERS.map((f) => {
          const active = activeFilter === f.value;
          return (
            <button
              key={f.value}
              type="button"
              onClick={() => setActiveFilter(f.value)}
              className={`flex items-center gap-1.5 px-3 py-2 text-xs font-medium border-b-2 transition-colors ${
                active
                  ? 'border-accent text-accent'
                  : 'border-transparent text-text-secondary hover:text-text-primary'
              }`}
            >
              {f.icon}
              {f.label}
              <span
                className={`ml-1 px-1.5 py-0.5 text-[10px] rounded ${
                  active ? 'bg-accent/20 text-accent' : 'bg-surface-tertiary text-text-secondary'
                }`}
              >
                {counts[f.value]}
              </span>
            </button>
          );
        })}
      </div>

      {/* Errors */}
      {error && (
        <div className="p-3 rounded-md border border-danger/30 bg-danger/10 text-danger text-sm">
          Failed to load tasks: {error.message}
        </div>
      )}

      {/* Table */}
      {isLoading ? (
        <div className="text-center py-12 text-text-secondary text-sm">Loading tasks...</div>
      ) : tasks.length === 0 ? (
        <div className="text-center py-12 text-text-muted text-sm">No tasks for this filter.</div>
      ) : (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase text-text-muted border-b border-border-subtle bg-surface-secondary/50">
                  <th className="py-2.5 px-3">Task ID</th>
                  <th className="py-2.5 px-3">Adapter</th>
                  <th className="py-2.5 px-3">Status</th>
                  <th className="py-2.5 px-3 text-right">Duration</th>
                  <th className="py-2.5 px-3 text-right">Tokens</th>
                  <th className="py-2.5 px-3 text-right">Cost</th>
                  <th className="py-2.5 px-3">Created</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((t) => {
                  const badge = statusBadge(t.status);
                  return (
                    <tr
                      key={t.id}
                      className="border-b border-border-subtle/50 hover:bg-surface-tertiary/30 cursor-pointer"
                    >
                      <td className="py-2.5 px-3">
                        <Link
                          to="/a2a/tasks/$taskId"
                          params={{ taskId: t.id }}
                          className="font-mono text-accent hover:text-accent"
                        >
                          {shortId(t.id)}
                        </Link>
                      </td>
                      <td className="py-2.5 px-3 text-text-secondary">{t.adapter}</td>
                      <td className="py-2.5 px-3">
                        <span
                          className={`inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded border ${badge.classes}`}
                        >
                          {badge.icon}
                          {badge.label}
                        </span>
                      </td>
                      <td className="py-2.5 px-3 text-right text-text-secondary">
                        {formatDuration(t.duration_ms)}
                      </td>
                      <td className="py-2.5 px-3 text-right text-text-secondary">
                        {formatTokens(t.total_tokens)}
                      </td>
                      <td className="py-2.5 px-3 text-right text-text-secondary">
                        {formatCost(t.cost)}
                      </td>
                      <td className="py-2.5 px-3 text-text-secondary">{formatTime(t.created_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
