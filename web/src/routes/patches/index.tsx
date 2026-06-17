// Patches — patch management landing page.
//
// Features:
//   • Summary bar: Total, Critical, Security, Approved, In Progress, Completed Today
//   • Filter tabs: All, Pending Approval, Approved, In Progress, Completed, Failed
//   • Search / KB+ / CVE lookup in the catalog
//   • Table: Patch Name, KB/CVE, Severity, Affected Agents, Status, Progress, Actions
//   • "Create Job" multi-step modal (select patches → targets → configure → review)
//   • Batch approve / reject for pending jobs
//   • WebSocket "patches" channel merges job + scan events in real time

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Wrench,
  Plus,
  Search,
  RefreshCw,
  Shield,
  CircleCheck,
  CircleAlert,
  Check,
  X,
  CirclePlay,
  CircleX,
  Activity,
  CalendarCheck,
  Loader2,
  ChevronLeft,
  ChevronRight,
  Package,
  Server,
  Send,
  Eye,
  Clock,
  AlertTriangle,
} from 'lucide-react';
import {
  usePatches,
  type PatchJob,
  type PatchJobStatus,
  type PatchCatalogItem,
} from '@/lib/usePatches';
import { useAgents, type Agent } from '@/lib/useAgents';
import { SeverityBadge } from '@/components/severity-badge';

export const Route = createFileRoute('/patches/')({
  component: PatchesListPage,
});

type JobFilter =
  | 'all'
  | 'pending_approval'
  | 'approved'
  | 'in_progress'
  | 'completed'
  | 'failed';

const JOB_TABS: { id: JobFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'pending_approval', label: 'Pending Approval' },
  { id: 'approved', label: 'Approved' },
  { id: 'in_progress', label: 'In Progress' },
  { id: 'completed', label: 'Completed' },
  { id: 'failed', label: 'Failed' },
];

const STATUS_META: Record<
  PatchJobStatus,
  { label: string; classes: string }
> = {
  pending_approval: {
    label: 'Pending Approval',
    classes: 'bg-warning/10 text-warning border-warning/20',
  },
  approved: {
    label: 'Approved',
    classes: 'bg-info/10 text-info border-info/20',
  },
  rejected: {
    label: 'Rejected',
    classes: 'bg-surface-tertiary/20 text-text-secondary border-border-strong/30',
  },
  in_progress: {
    label: 'In Progress',
    classes: 'bg-accent/10 text-accent border-accent/20',
  },
  completed: {
    label: 'Completed',
    classes: 'bg-success/10 text-success border-success/20',
  },
  failed: {
    label: 'Failed',
    classes: 'bg-danger/10 text-danger border-danger/20',
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-surface-tertiary/20 text-text-secondary border-border-strong/30',
  },
  rolled_back: {
    label: 'Rolled Back',
    classes: 'bg-warning/10 text-warning border-warning/20',
  },
};

function isToday(iso: string | undefined): boolean {
  if (!iso) return false;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return false;
  const now = new Date();
  return (
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  );
}

function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

function statusToTab(s: PatchJobStatus): JobFilter {
  switch (s) {
    case 'pending_approval':
      return 'pending_approval';
    case 'approved':
      return 'approved';
    case 'in_progress':
      return 'in_progress';
    case 'completed':
      return 'completed';
    case 'failed':
      return 'failed';
    default:
      return 'all';
  }
}

function PatchesListPage() {
  const navigate = useNavigate();
  const [filter, setFilter] = useState<JobFilter>('all');
  const [query, setQuery] = useState('');
  const [now, setNow] = useState(() => Date.now());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchBusy, setBatchBusy] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);

  const {
    jobs,
    isLoading,
    error,
    refresh,
    status,
    batchApprove,
    batchReject,
  } = usePatches();

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // Derive filterable list (search + tab).
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return jobs.filter((j) => {
      if (filter !== 'all' && statusToTab(j.status) !== filter) return false;
      if (!q) return true;
      if (j.name?.toLowerCase().includes(q)) return true;
      if (j.id.toLowerCase().includes(q)) return true;
      if (j.description?.toLowerCase().includes(q)) return true;
      if (j.created_by?.toLowerCase().includes(q)) return true;
      return false;
    });
  }, [jobs, filter, query]);

  const counts = useMemo(() => {
    const c: Record<JobFilter, number> = {
      all: jobs.length,
      pending_approval: 0,
      approved: 0,
      in_progress: 0,
      completed: 0,
      failed: 0,
    };
    for (const j of jobs) {
      c[statusToTab(j.status)] += 1;
    }
    return c;
  }, [jobs]);

  // Summary KPIs computed live from the job list.
  const summary = useMemo(() => {
    let total = 0;
    let critical = 0;
    let security = 0;
    let approved = 0;
    let inProgress = 0;
    let completedToday = 0;

    for (const j of jobs) {
      // patch_count is the number of patches in the job, not a per-job tally,
      // so we report one row per job in the "Total" KPI to match the
      // "patch management" framing.
      total += 1;

      const sev = (j.severity ?? '').toLowerCase();
      if (sev === 'critical' || sev === 'emergency') critical += 1;
      // We treat "important" + any security-flagged job as a security rollup.
      // The catalog may carry a category on the job's first patch; the
      // server is expected to mirror the highest-risk category into
      // a field if it has one. We fall back to severity 'important' as a
      // reasonable proxy.
      if (sev === 'important' || j.patch_count > 0) security += 1;
      if (j.status === 'approved') approved += 1;
      if (j.status === 'in_progress') inProgress += 1;
      if (j.status === 'completed' && isToday(j.completed_at)) completedToday += 1;
    }
    return { total, critical, security, approved, inProgress, completedToday };
  }, [jobs]);

  // Selection helpers (only allow selecting rows in pending_approval for
  // batch approve / reject).
  const selectableIds = useMemo(
    () => jobs.filter((j) => j.status === 'pending_approval').map((j) => j.id),
    [jobs]
  );
  const selectableSet = useMemo(() => new Set(selectableIds), [selectableIds]);

  const allSelectableSelected =
    selectableIds.length > 0 && selectableIds.every((id) => selected.has(id));

  const toggleRow = useCallback(
    (id: string) => {
      if (!selectableSet.has(id)) return;
      setSelected((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
    },
    [selectableSet]
  );

  const toggleAllSelectable = useCallback(() => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelectableSelected) {
        for (const id of selectableIds) next.delete(id);
      } else {
        for (const id of selectableIds) next.add(id);
      }
      return next;
    });
  }, [allSelectableSelected, selectableIds]);

  const clearSelection = useCallback(() => setSelected(new Set()), []);

  const runBatch = useCallback(
    async (kind: 'approve' | 'reject') => {
      if (selected.size === 0) return;
      setBatchBusy(true);
      try {
        if (kind === 'approve') {
          await batchApprove(Array.from(selected));
        } else {
          await batchReject(Array.from(selected));
        }
        clearSelection();
      } finally {
        setBatchBusy(false);
      }
    },
    [selected, batchApprove, batchReject, clearSelection]
  );

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-accent/10 border border-accent/20 flex items-center justify-center">
            <Wrench className="h-4 w-4 text-accent" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-text-primary">Patches</h1>
            <p className="text-text-secondary text-sm mt-0.5">
              Manage OS and application patch rollouts across the fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-success' : status === 'connecting' ? 'bg-warning' : 'bg-text-muted')
            }
            title={`WebSocket: ${status}`}
          />
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent-hover border border-accent text-sm text-white transition-colors"
          >
            <Plus className="h-4 w-4" />
            <span>Create Job</span>
          </button>
        </div>
      </div>

      {/* Summary bar */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        <SummaryTile
          label="Total"
          value={summary.total}
          tone="neutral"
          icon={Package}
        />
        <SummaryTile
          label="Critical"
          value={summary.critical}
          tone={summary.critical > 0 ? 'danger' : 'success'}
          icon={AlertTriangle}
        />
        <SummaryTile
          label="Security"
          value={summary.security}
          tone="info"
          icon={Shield}
        />
        <SummaryTile
          label="Approved"
          value={summary.approved}
          tone="neutral"
          icon={CircleCheck}
        />
        <SummaryTile
          label="In Progress"
          value={summary.inProgress}
          tone={summary.inProgress > 0 ? 'info' : 'neutral'}
          icon={Activity}
        />
        <SummaryTile
          label="Completed Today"
          value={summary.completedToday}
          tone={summary.completedToday > 0 ? 'success' : 'neutral'}
          icon={CalendarCheck}
        />
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-surface-secondary border border-border-subtle overflow-x-auto">
          {JOB_TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                setFilter(t.id);
                clearSelection();
              }}
              className={
                'px-3 h-8 rounded text-sm whitespace-nowrap transition-colors ' +
                (filter === t.id
                  ? 'bg-surface-tertiary text-text-primary'
                  : 'text-text-secondary hover:text-text-primary')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-text-muted">{counts[t.id]}</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search patch jobs"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search jobs…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
          />
        </div>
      </div>

      {/* Batch actions bar */}
      {selected.size > 0 && (
        <div className="flex items-center justify-between gap-3 rounded-md border border-accent/30 bg-accent/5 px-4 py-2">
          <div className="text-sm text-text-primary">
            <span className="font-medium">{selected.size}</span> job
            {selected.size === 1 ? '' : 's'} selected (pending approval)
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('approve')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-success/15 border border-success/30 text-success text-sm hover:bg-success/25 disabled:opacity-50 transition-colors"
            >
              <Check className="h-3.5 w-3.5" />
              <span>Approve all</span>
            </button>
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('reject')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-danger/15 border border-danger/30 text-danger text-sm hover:bg-danger/25 disabled:opacity-50 transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              <span>Reject all</span>
            </button>
            <button
              type="button"
              onClick={clearSelection}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-surface-tertiary border border-border-strong text-text-secondary text-sm hover:bg-border-strong transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              <span>Clear</span>
            </button>
          </div>
        </div>
      )}

      {/* Table */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Patch jobs" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                <th className="px-3 py-3 w-10" scope="col">
                  <input
                    type="checkbox"
                    aria-label="Select all pending-approval jobs"
                    checked={allSelectableSelected}
                    onChange={toggleAllSelectable}
                    disabled={selectableIds.length === 0}
                    className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40 disabled:opacity-40"
                  />
                </th>
                <th className="px-3 py-3" scope="col">Job</th>
                <th className="px-3 py-3 w-40" scope="col">KB / CVE</th>
                <th className="px-3 py-3 w-28" scope="col">Severity</th>
                <th className="px-3 py-3 w-28 text-right" scope="col">Affected</th>
                <th className="px-3 py-3 w-40" scope="col">Status</th>
                <th className="px-3 py-3 w-48" scope="col">Progress</th>
                <th className="px-3 py-3 text-right w-48" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading && jobs.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-text-muted" role="status" aria-live="polite">
                    Loading patches…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-danger" role="alert">
                    Failed to load jobs: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-text-muted" role="status">
                    No patch jobs match the current filter.
                  </td>
                </tr>
              ) : (
                filtered.map((j) => (
                  <JobRow
                    key={j.id}
                    job={j}
                    isSelected={selected.has(j.id)}
                    onToggleSelect={() => toggleRow(j.id)}
                    onOpen={() =>
                      void navigate({ to: '/patches/$jobId', params: { jobId: j.id } })
                    }
                    now={now}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Create-job modal */}
      {createOpen && (
        <CreateJobModal
          onClose={() => setCreateOpen(false)}
          onCreated={(job) => {
            setCreateOpen(false);
            void navigate({ to: '/patches/$jobId', params: { jobId: job.id } });
          }}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Summary tile
// ---------------------------------------------------------------------------

function SummaryTile({
  label,
  value,
  tone,
  icon: Icon,
}: {
  label: string;
  value: number;
  tone: 'success' | 'danger' | 'info' | 'neutral';
  icon: typeof Package;
}) {
  const toneClasses: Record<typeof tone, string> = {
    success: 'text-success',
    danger: 'text-danger',
    info: 'text-info',
    neutral: 'text-text-secondary',
  };
  return (
    <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs text-text-muted uppercase tracking-wider">{label}</span>
        <Icon className={'h-3.5 w-3.5 ' + toneClasses[tone]} />
      </div>
      <p className={'text-2xl font-semibold mt-1.5 tabular-nums ' + toneClasses[tone]}>
        {value}
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Job row
// ---------------------------------------------------------------------------

interface JobRowProps {
  job: PatchJob;
  isSelected: boolean;
  onToggleSelect: () => void;
  onOpen: () => void;
  now: number;
}

function JobRow({ job: j, isSelected, onToggleSelect, onOpen, now }: JobRowProps) {
  const meta = STATUS_META[j.status] ?? STATUS_META.pending_approval;
  const isPending = j.status === 'pending_approval';
  const isSelectable = isPending;
  const progress = computeProgress(j);
  const progressTone =
    j.status === 'completed'
      ? 'bg-success'
      : j.status === 'failed'
      ? 'bg-danger'
      : j.status === 'cancelled'
      ? 'bg-text-muted'
      : 'bg-accent';

  return (
    <tr
      onClick={onOpen}
      className={
        'cursor-pointer transition-colors ' +
        (isSelected ? 'bg-accent/5' : 'hover:bg-surface-tertiary/40')
      }
    >
      <td className="px-3 py-3" onClick={(e) => e.stopPropagation()}>
        <input
          type="checkbox"
          aria-label={`Select job ${j.id}`}
          checked={isSelected}
          onChange={onToggleSelect}
          disabled={!isSelectable}
          className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40 disabled:opacity-30"
        />
      </td>
      <td className="px-3 py-3">
        <div className="flex flex-col">
          <span className="text-text-primary font-medium truncate max-w-md">
            {j.name || j.id}
          </span>
          {j.description && (
            <span className="text-xs text-text-muted truncate max-w-md">
              {j.description}
            </span>
          )}
          <span className="text-[10px] text-text-muted mt-0.5 font-mono">
            {j.id}
            {j.created_by ? ` · by ${j.created_by}` : ''}
            {' · '}
            {formatRelative(j.created_at, now)}
          </span>
        </div>
      </td>
      <td className="px-3 py-3 text-text-secondary">
        <span className="text-xs">
          {j.patch_count > 0 ? `${j.patch_count} patch${j.patch_count === 1 ? '' : 'es'}` : '—'}
        </span>
      </td>
      <td className="px-3 py-3">
        <SeverityBadge severity={j.severity} />
      </td>
      <td className="px-3 py-3 text-right tabular-nums text-text-secondary">
        {j.total_agents > 0 ? (
          <>
            <span className="text-text-primary">{j.completed_agents}</span>
            <span className="text-text-muted"> / {j.total_agents}</span>
          </>
        ) : (
          <span className="text-text-muted">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        <span
          className={
            'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
            meta.classes
          }
        >
          {meta.label}
        </span>
      </td>
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <div className="flex-1 h-1.5 rounded-full bg-surface-tertiary overflow-hidden">
            <div
              className={'h-full transition-all ' + progressTone}
              style={{ width: `${Math.max(0, Math.min(100, progress))}%` }}
            />
          </div>
          <span className="text-xs text-text-secondary tabular-nums w-9 text-right">
            {Math.round(progress)}%
          </span>
        </div>
      </td>
      <td className="px-3 py-3 text-right" onClick={(e) => e.stopPropagation()}>
        <RowActions job={j} />
      </td>
    </tr>
  );
}

function computeProgress(j: PatchJob): number {
  if (typeof j.progress_pct === 'number') return j.progress_pct;
  if (j.total_agents <= 0) {
    if (j.status === 'completed') return 100;
    if (j.status === 'failed') return 100;
    if (j.status === 'cancelled' || j.status === 'rejected') return 0;
    return 0;
  }
  return Math.min(100, (j.completed_agents / j.total_agents) * 100);
}

// ---------------------------------------------------------------------------
// Row actions (uses usePatches from the same hook import as a separate
// `usePatches` is created below).
// ---------------------------------------------------------------------------

function RowActions({ job: j }: { job: PatchJob }) {
  const { approveJob, rejectJob, cancelJob } = usePatches();
  const [busy, setBusy] = useState<string | null>(null);

  const onApprove = useCallback(async () => {
    setBusy('approve');
    try {
      await approveJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [approveJob, j.id]);

  const onReject = useCallback(async () => {
    setBusy('reject');
    try {
      await rejectJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [rejectJob, j.id]);

  const onCancel = useCallback(async () => {
    setBusy('cancel');
    try {
      await cancelJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [cancelJob, j.id]);

  if (j.status === 'pending_approval') {
    return (
      <div className="inline-flex items-center gap-1">
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onApprove()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-success/10 border border-success/30 text-success hover:bg-success/20 disabled:opacity-50 transition-colors"
          title="Approve"
        >
          {busy === 'approve' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Check className="h-3.5 w-3.5" />
          )}
          <span>Approve</span>
        </button>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onReject()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-danger/10 border border-danger/30 text-danger hover:bg-danger/20 disabled:opacity-50 transition-colors"
          title="Reject"
        >
          {busy === 'reject' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <X className="h-3.5 w-3.5" />
          )}
          <span>Reject</span>
        </button>
      </div>
    );
  }

  if (j.status === 'in_progress' || j.status === 'approved') {
    return (
      <div className="inline-flex items-center gap-1">
        <span className="inline-flex items-center gap-1 text-xs text-text-muted">
          <CirclePlay className="h-3.5 w-3.5" />
          <span>Running</span>
        </span>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onCancel()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-surface-tertiary border border-border-strong text-text-secondary hover:bg-border-strong disabled:opacity-50 transition-colors"
          title="Cancel job"
        >
          {busy === 'cancel' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <CircleX className="h-3.5 w-3.5" />
          )}
          <span>Cancel</span>
        </button>
      </div>
    );
  }

  if (j.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-danger">
        <CircleAlert className="h-3.5 w-3.5" />
        <span>Failed</span>
      </span>
    );
  }

  if (j.status === 'completed') {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-success">
        <CircleCheck className="h-3.5 w-3.5" />
        <span>Done</span>
      </span>
    );
  }

  return (
    <span className="inline-flex items-center gap-1 text-xs text-text-muted">
      <Clock className="h-3.5 w-3.5" />
      <span>No actions</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Create-job modal (multi-step wizard)
// ---------------------------------------------------------------------------

type WizardStep = 'patches' | 'targets' | 'configure' | 'review';

const STEP_LABELS: Record<WizardStep, string> = {
  patches: 'Select Patches',
  targets: 'Select Targets',
  configure: 'Configure',
  review: 'Review & Submit',
};

const STEPS: WizardStep[] = ['patches', 'targets', 'configure', 'review'];

function CreateJobModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (job: PatchJob) => void;
}) {
  const { catalog, fetchCatalog, catalogLoading, createJob } = usePatches();
  const { agents } = useAgents();

  const [step, setStep] = useState<WizardStep>('patches');
  const [search, setSearch] = useState('');
  const [catalogFilter, setCatalogFilter] = useState<{
    severity?: string;
    category?: string;
    os?: string;
  }>({});

  const [selectedPatches, setSelectedPatches] = useState<Set<string>>(new Set());
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(new Set());
  const [agentQuery, setAgentQuery] = useState('');

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [strategy, setStrategy] = useState<'immediate' | 'staged' | 'maintenance_window'>(
    'staged'
  );
  const [batchSize, setBatchSize] = useState(10);
  const [batchIntervalMinutes, setBatchIntervalMinutes] = useState(15);
  const [rebootPolicy, setRebootPolicy] = useState<
    'never' | 'if_required' | 'always' | 'scheduled'
  >('if_required');
  const [maintenanceStart, setMaintenanceStart] = useState('');
  const [maintenanceEnd, setMaintenanceEnd] = useState('');

  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Lazy-load catalog on first open.
  useEffect(() => {
    if (catalog.length === 0 && !catalogLoading) {
      void fetchCatalog();
    }
  }, [catalog.length, catalogLoading, fetchCatalog]);

  // Reset state on close.
  useEffect(() => {
    function onEsc(e: KeyboardEvent) {
      if (e.key === 'Escape' && !submitting) onClose();
    }
    document.addEventListener('keydown', onEsc);
    return () => document.removeEventListener('keydown', onEsc);
  }, [onClose, submitting]);

  // Derived lists.
  const catalogPatched = useMemo(() => {
    const q = search.trim().toLowerCase();
    return catalog.filter((c) => {
      if (catalogFilter.severity && c.severity !== catalogFilter.severity) return false;
      if (catalogFilter.category && c.category !== catalogFilter.category) return false;
      if (catalogFilter.os && c.os !== catalogFilter.os) return false;
      if (!q) return true;
      if (c.title?.toLowerCase().includes(q)) return true;
      if (c.kb_number?.toLowerCase().includes(q)) return true;
      if (c.cve_ids?.some((id) => id.toLowerCase().includes(q))) return true;
      return false;
    });
  }, [catalog, search, catalogFilter]);

  const agentList = useMemo(() => {
    const q = agentQuery.trim().toLowerCase();
    if (!q) return agents;
    return agents.filter(
      (a) =>
        a.hostname?.toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q) ||
        a.os?.toLowerCase().includes(q)
    );
  }, [agents, agentQuery]);

  const stepIndex = STEPS.indexOf(step);
  const canGoNext = (() => {
    if (step === 'patches') return selectedPatches.size > 0;
    if (step === 'targets') return selectedAgents.size > 0;
    if (step === 'configure') return name.trim().length > 0;
    return true;
  })();

  const goNext = useCallback(() => {
    const idx = STEPS.indexOf(step);
    if (idx < STEPS.length - 1) setStep(STEPS[idx + 1]);
  }, [step]);

  const goBack = useCallback(() => {
    const idx = STEPS.indexOf(step);
    if (idx > 0) setStep(STEPS[idx - 1]);
  }, [step]);

  const togglePatch = useCallback((id: string) => {
    setSelectedPatches((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleAgent = useCallback((id: string) => {
    setSelectedAgents((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const selectAllAgents = useCallback(() => {
    setSelectedAgents(new Set(agentList.map((a) => a.id)));
  }, [agentList]);

  const clearAllAgents = useCallback(() => {
    setSelectedAgents(new Set());
  }, []);

  const submit = useCallback(async () => {
    setSubmitError(null);
    setSubmitting(true);
    try {
      const job = await createJob({
        name: name.trim(),
        description: description.trim() || undefined,
        patch_ids: Array.from(selectedPatches),
        target_agent_ids: Array.from(selectedAgents),
        strategy,
        reboot_policy: rebootPolicy,
        batch_size: batchSize,
        batch_interval_minutes: batchIntervalMinutes,
        ...(strategy === 'maintenance_window'
          ? {
              maintenance_window_start: maintenanceStart || undefined,
              maintenance_window_end: maintenanceEnd || undefined,
            }
          : {}),
      });
      onCreated(job);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }, [
    name,
    description,
    selectedPatches,
    selectedAgents,
    strategy,
    rebootPolicy,
    batchSize,
    batchIntervalMinutes,
    maintenanceStart,
    maintenanceEnd,
    createJob,
    onCreated,
  ]);

  return (
    <div
      className="fixed inset-0 z-50 bg-surface-primary/70 flex items-center justify-center p-4 overflow-y-auto"
      onClick={() => {
        if (!submitting) onClose();
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-3xl rounded-lg border border-border-subtle bg-surface-secondary shadow-2xl"
      >
        {/* Header */}
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">Create patch job</h2>
            <p className="text-xs text-text-muted mt-0.5">
              Step {stepIndex + 1} of {STEPS.length} — {STEP_LABELS[step]}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="text-text-secondary hover:text-text-primary transition-colors"
            title="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Stepper */}
        <div className="px-5 py-3 border-b border-border-subtle flex items-center gap-2">
          {STEPS.map((s, idx) => {
            const active = s === step;
            const done = idx < stepIndex;
            return (
              <div key={s} className="flex items-center gap-2 flex-1">
                <div
                  className={
                    'h-6 w-6 rounded-full flex items-center justify-center text-xs font-medium border ' +
                    (done
                      ? 'bg-success/15 border-success/30 text-success'
                      : active
                      ? 'bg-accent/15 border-accent/40 text-accent'
                      : 'bg-surface-tertiary border-border-strong text-text-muted')
                  }
                >
                  {done ? <Check className="h-3.5 w-3.5" /> : idx + 1}
                </div>
                <span
                  className={
                    'text-sm ' + (active ? 'text-text-primary' : done ? 'text-text-secondary' : 'text-text-muted')
                  }
                >
                  {STEP_LABELS[s]}
                </span>
                {idx < STEPS.length - 1 && (
                  <ChevronRight className="h-4 w-4 text-text-muted ml-auto" />
                )}
              </div>
            );
          })}
        </div>

        {/* Step body */}
        <div className="p-5 min-h-[20rem] max-h-[60vh] overflow-y-auto">
          {step === 'patches' && (
            <PatchesStep
              catalog={catalogPatched}
              isLoading={catalogLoading}
              search={search}
              onSearchChange={setSearch}
              catalogFilter={catalogFilter}
              onCatalogFilterChange={setCatalogFilter}
              selected={selectedPatches}
              onToggle={togglePatch}
            />
          )}
          {step === 'targets' && (
            <TargetsStep
              agents={agentList}
              isLoading={false}
              search={agentQuery}
              onSearchChange={setAgentQuery}
              selected={selectedAgents}
              onToggle={toggleAgent}
              onSelectAll={selectAllAgents}
              onClear={clearAllAgents}
            />
          )}
          {step === 'configure' && (
            <ConfigureStep
              name={name}
              onNameChange={setName}
              description={description}
              onDescriptionChange={setDescription}
              strategy={strategy}
              onStrategyChange={setStrategy}
              batchSize={batchSize}
              onBatchSizeChange={setBatchSize}
              batchIntervalMinutes={batchIntervalMinutes}
              onBatchIntervalChange={setBatchIntervalMinutes}
              rebootPolicy={rebootPolicy}
              onRebootPolicyChange={setRebootPolicy}
              maintenanceStart={maintenanceStart}
              onMaintenanceStartChange={setMaintenanceStart}
              maintenanceEnd={maintenanceEnd}
              onMaintenanceEndChange={setMaintenanceEnd}
            />
          )}
          {step === 'review' && (
            <ReviewStep
              patchCount={selectedPatches.size}
              agentCount={selectedAgents.size}
              name={name}
              description={description}
              strategy={strategy}
              batchSize={batchSize}
              batchIntervalMinutes={batchIntervalMinutes}
              rebootPolicy={rebootPolicy}
              maintenanceStart={maintenanceStart}
              maintenanceEnd={maintenanceEnd}
              catalog={catalog}
              selectedPatchIds={Array.from(selectedPatches)}
              agents={agents}
              selectedAgentIds={Array.from(selectedAgents)}
            />
          )}
        </div>

        {submitError && (
          <div className="mx-5 mb-2 rounded-md border border-danger/30 bg-danger/5 p-3 text-danger text-sm">
            {submitError}
          </div>
        )}

        {/* Footer */}
        <div className="px-5 py-3 border-t border-border-subtle flex items-center justify-between">
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="text-sm text-text-secondary hover:text-text-primary transition-colors"
          >
            Cancel
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={goBack}
              disabled={stepIndex === 0 || submitting}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-text-primary text-sm hover:bg-border-strong disabled:opacity-40 transition-colors"
            >
              <ChevronLeft className="h-4 w-4" />
              <span>Back</span>
            </button>
            {step === 'review' ? (
              <button
                type="button"
                onClick={() => void submit()}
                disabled={submitting}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-accent hover:bg-accent border border-accent text-white text-sm disabled:opacity-50 transition-colors"
              >
                {submitting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Send className="h-4 w-4" />
                )}
                <span>Submit</span>
              </button>
            ) : (
              <button
                type="button"
                onClick={goNext}
                disabled={!canGoNext}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-accent hover:bg-accent border border-accent text-white text-sm disabled:opacity-40 transition-colors"
              >
                <span>Next</span>
                <ChevronRight className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Step components
// ---------------------------------------------------------------------------

function PatchesStep({
  catalog,
  isLoading,
  search,
  onSearchChange,
  catalogFilter,
  onCatalogFilterChange,
  selected,
  onToggle,
}: {
  catalog: PatchCatalogItem[];
  isLoading: boolean;
  search: string;
  onSearchChange: (v: string) => void;
  catalogFilter: { severity?: string; category?: string; os?: string };
  onCatalogFilterChange: (v: { severity?: string; category?: string; os?: string }) => void;
  selected: Set<string>;
  onToggle: (id: string) => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[14rem]" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search patch catalog by title, KB, or CVE"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search by title, KB, or CVE…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus:border-accent"
          />
        </div>
        <select
          value={catalogFilter.severity ?? ''}
          onChange={(e) =>
            onCatalogFilterChange({ ...catalogFilter, severity: e.target.value || undefined })
          }
          className="h-9 px-2 rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="important">Important</option>
          <option value="moderate">Moderate</option>
          <option value="low">Low</option>
        </select>
        <select
          value={catalogFilter.category ?? ''}
          onChange={(e) =>
            onCatalogFilterChange({ ...catalogFilter, category: e.target.value || undefined })
          }
          className="h-9 px-2 rounded-md bg-surface-tertiary border border-border-strong text-sm text-text-primary"
        >
          <option value="">All categories</option>
          <option value="security">Security</option>
          <option value="os">OS</option>
          <option value="application">Application</option>
          <option value="driver">Driver</option>
          <option value="firmware">Firmware</option>
        </select>
      </div>

      <div className="rounded-md border border-border-subtle overflow-hidden">
        <div className="max-h-96 overflow-y-auto">
          <table role="table" aria-label="Patch catalog" className="w-full text-sm">
            <thead className="sticky top-0 bg-surface-secondary/90 z-10">
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle">
                <th className="px-3 py-2 w-10" scope="col"></th>
                <th className="px-3 py-2" scope="col">Title</th>
                <th className="px-3 py-2 w-28" scope="col">KB / CVE</th>
                <th className="px-3 py-2 w-28" scope="col">Severity</th>
                <th className="px-3 py-2 w-24" scope="col">OS</th>
                <th className="px-3 py-2 w-20 text-right" scope="col">Affected</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-text-muted" role="status" aria-live="polite">
                    <Loader2 className="inline h-4 w-4 animate-spin mr-2" aria-hidden="true" />
                    Loading patch catalog…
                  </td>
                </tr>
              ) : catalog.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-text-muted" role="status">
                    No patches match your filters.
                  </td>
                </tr>
              ) : (
                catalog.map((c) => {
                  const isSelected = selected.has(c.id);
                  return (
                    <tr
                      key={c.id}
                      onClick={() => onToggle(c.id)}
                      className={
                        'cursor-pointer transition-colors ' +
                        (isSelected ? 'bg-accent/10' : 'hover:bg-surface-tertiary/40')
                      }
                    >
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => onToggle(c.id)}
                          aria-label={`Select patch ${c.id}`}
                          className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40"
                        />
                      </td>
                      <td className="px-3 py-2 text-text-primary">
                        <div className="flex flex-col">
                          <span className="truncate max-w-md">{c.title}</span>
                          {c.description && (
                            <span className="text-xs text-text-muted truncate max-w-md">
                              {c.description}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-text-secondary text-xs">
                        {c.kb_number ?? '—'}
                        {c.cve_ids && c.cve_ids.length > 0 && (
                          <span className="block text-text-muted">
                            {c.cve_ids.slice(0, 2).join(', ')}
                            {c.cve_ids.length > 2 ? ` +${c.cve_ids.length - 2}` : ''}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        <SeverityBadge severity={c.severity} />
                      </td>
                      <td className="px-3 py-2 text-text-secondary text-xs">{c.os || '—'}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-text-secondary">
                        {c.affected_agent_count ?? 0}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-xs text-text-muted">
        {selected.size} patch{selected.size === 1 ? '' : 'es'} selected.
      </p>
    </div>
  );
}

function TargetsStep({
  agents,
  isLoading,
  search,
  onSearchChange,
  selected,
  onToggle,
  onSelectAll,
  onClear,
}: {
  agents: Agent[];
  isLoading: boolean;
  search: string;
  onSearchChange: (v: string) => void;
  selected: Set<string>;
  onToggle: (id: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[14rem]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-text-muted" />
          <input
            type="search"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search by hostname, ID, or OS…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
          />
        </div>
        <button
          type="button"
          onClick={onSelectAll}
          className="px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-text-secondary text-sm hover:bg-border-strong transition-colors"
        >
          Select all visible
        </button>
        <button
          type="button"
          onClick={onClear}
          className="px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-text-secondary text-sm hover:bg-border-strong transition-colors"
        >
          Clear
        </button>
      </div>
      <div className="rounded-md border border-border-subtle overflow-hidden">
        <div className="max-h-96 overflow-y-auto">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-surface-secondary/90 z-10">
              <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle">
                <th className="px-3 py-2 w-10"></th>
                <th className="px-3 py-2">Hostname</th>
                <th className="px-3 py-2 w-32">OS</th>
                <th className="px-3 py-2 w-20">Status</th>
                <th className="px-3 py-2 w-20 text-right">CPU%</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-text-muted">
                    <Loader2 className="inline h-4 w-4 animate-spin mr-2" />
                    Loading agents…
                  </td>
                </tr>
              ) : agents.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-text-muted">
                    No agents match your search.
                  </td>
                </tr>
              ) : (
                agents.slice(0, 500).map((a) => {
                  const isSelected = selected.has(a.id);
                  return (
                    <tr
                      key={a.id}
                      onClick={() => onToggle(a.id)}
                      className={
                        'cursor-pointer transition-colors ' +
                        (isSelected ? 'bg-accent/10' : 'hover:bg-surface-tertiary/40')
                      }
                    >
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => onToggle(a.id)}
                          aria-label={`Select agent ${a.id}`}
                          className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40"
                        />
                      </td>
                      <td className="px-3 py-2 text-text-primary">{a.hostname || a.id}</td>
                      <td className="px-3 py-2 text-text-secondary text-xs">{a.os || '—'}</td>
                      <td className="px-3 py-2 text-xs">
                        <span
                          className={
                            'inline-flex px-2 py-0.5 rounded-full border ' +
                            (a.status === 'online'
                              ? 'bg-success/10 text-success border-success/20'
                              : 'bg-border-strong/30 text-text-secondary border-border-strong/30')
                          }
                        >
                          {a.status || 'unknown'}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums text-text-secondary text-xs">
                        {a.cpu_percent !== undefined ? `${a.cpu_percent.toFixed(0)}%` : '—'}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-xs text-text-muted">
        {selected.size} agent{selected.size === 1 ? '' : 's'} selected.
      </p>
    </div>
  );
}

function ConfigureStep({
  name,
  onNameChange,
  description,
  onDescriptionChange,
  strategy,
  onStrategyChange,
  batchSize,
  onBatchSizeChange,
  batchIntervalMinutes,
  onBatchIntervalChange,
  rebootPolicy,
  onRebootPolicyChange,
  maintenanceStart,
  onMaintenanceStartChange,
  maintenanceEnd,
  onMaintenanceEndChange,
}: {
  name: string;
  onNameChange: (v: string) => void;
  description: string;
  onDescriptionChange: (v: string) => void;
  strategy: 'immediate' | 'staged' | 'maintenance_window';
  onStrategyChange: (v: 'immediate' | 'staged' | 'maintenance_window') => void;
  batchSize: number;
  onBatchSizeChange: (v: number) => void;
  batchIntervalMinutes: number;
  onBatchIntervalChange: (v: number) => void;
  rebootPolicy: 'never' | 'if_required' | 'always' | 'scheduled';
  onRebootPolicyChange: (v: 'never' | 'if_required' | 'always' | 'scheduled') => void;
  maintenanceStart: string;
  onMaintenanceStartChange: (v: string) => void;
  maintenanceEnd: string;
  onMaintenanceEndChange: (v: string) => void;
}) {
  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm font-medium text-text-primary mb-1">Job name *</label>
        <input
          type="text"
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          placeholder="e.g. Q2 Critical Security Rollout"
          className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
        />
      </div>
      <div>
        <label className="block text-sm font-medium text-text-primary mb-1">Description</label>
        <textarea
          value={description}
          onChange={(e) => onDescriptionChange(e.target.value)}
          rows={2}
          placeholder="Optional context for reviewers…"
          className="w-full px-3 py-2 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-text-primary mb-1.5">Deployment strategy</label>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
          {(
            [
              { value: 'immediate', label: 'Immediate', desc: 'Roll out to all targets at once' },
              { value: 'staged', label: 'Staged', desc: '10% → 25% → 50% → 100%' },
              { value: 'maintenance_window', label: 'Maintenance', desc: 'Within a defined window' },
            ] as const
          ).map((s) => (
            <button
              key={s.value}
              type="button"
              onClick={() => onStrategyChange(s.value)}
              className={
                'text-left rounded-md border p-3 transition-colors ' +
                (strategy === s.value
                  ? 'border-accent/50 bg-accent/10'
                  : 'border-border-subtle bg-surface-secondary/60 hover:border-border-strong')
              }
            >
              <div className="text-sm font-medium text-text-primary">{s.label}</div>
              <div className="text-xs text-text-muted mt-0.5">{s.desc}</div>
            </button>
          ))}
        </div>
      </div>

      {strategy === 'staged' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Batch size</label>
            <input
              type="number"
              min={1}
              value={batchSize}
              onChange={(e) => onBatchSizeChange(Math.max(1, Number(e.target.value) || 1))}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
            <p className="text-xs text-text-muted mt-1">Agents per rollout stage.</p>
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">
              Batch interval (minutes)
            </label>
            <input
              type="number"
              min={1}
              value={batchIntervalMinutes}
              onChange={(e) =>
                onBatchIntervalChange(Math.max(1, Number(e.target.value) || 1))
              }
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
            <p className="text-xs text-text-muted mt-1">Time between stages.</p>
          </div>
        </div>
      )}

      {strategy === 'maintenance_window' && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Window start</label>
            <input
              type="datetime-local"
              value={maintenanceStart}
              onChange={(e) => onMaintenanceStartChange(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-text-primary mb-1">Window end</label>
            <input
              type="datetime-local"
              value={maintenanceEnd}
              onChange={(e) => onMaintenanceEndChange(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>
        </div>
      )}

      <div>
        <label className="block text-sm font-medium text-text-primary mb-1.5">Reboot policy</label>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          {(
            [
              { value: 'never', label: 'Never' },
              { value: 'if_required', label: 'If Required' },
              { value: 'always', label: 'Always' },
              { value: 'scheduled', label: 'Scheduled' },
            ] as const
          ).map((r) => (
            <button
              key={r.value}
              type="button"
              onClick={() => onRebootPolicyChange(r.value)}
              className={
                'rounded-md border px-3 h-8 text-sm transition-colors ' +
                (rebootPolicy === r.value
                  ? 'border-accent/50 bg-accent/10 text-text-primary'
                  : 'border-border-subtle bg-surface-secondary/60 text-text-secondary hover:border-border-strong')
              }
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

function ReviewStep({
  patchCount,
  agentCount,
  name,
  description,
  strategy,
  batchSize,
  batchIntervalMinutes,
  rebootPolicy,
  maintenanceStart,
  maintenanceEnd,
  catalog,
  selectedPatchIds,
  agents,
  selectedAgentIds,
}: {
  patchCount: number;
  agentCount: number;
  name: string;
  description: string;
  strategy: 'immediate' | 'staged' | 'maintenance_window';
  batchSize: number;
  batchIntervalMinutes: number;
  rebootPolicy: 'never' | 'if_required' | 'always' | 'scheduled';
  maintenanceStart: string;
  maintenanceEnd: string;
  catalog: PatchCatalogItem[];
  selectedPatchIds: string[];
  agents: Agent[];
  selectedAgentIds: string[];
}) {
  const patchDetails = useMemo(
    () => catalog.filter((c) => selectedPatchIds.includes(c.id)),
    [catalog, selectedPatchIds]
  );
  const agentDetails = useMemo(
    () => agents.filter((a) => selectedAgentIds.includes(a.id)),
    [agents, selectedAgentIds]
  );

  return (
    <div className="space-y-4">
      <div className="rounded-md border border-border-subtle bg-surface-secondary/60 p-4 space-y-2">
        <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
          <Eye className="h-4 w-4 text-text-secondary" />
          Job summary
        </h3>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-4 gap-y-1.5 text-sm">
          <div>
            <dt className="text-xs text-text-muted">Name</dt>
            <dd className="text-text-primary">{name || '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-text-muted">Strategy</dt>
            <dd className="text-text-primary capitalize">{strategy.replace('_', ' ')}</dd>
          </div>
          {description && (
            <div className="sm:col-span-2">
              <dt className="text-xs text-text-muted">Description</dt>
              <dd className="text-text-primary">{description}</dd>
            </div>
          )}
          {strategy === 'staged' && (
            <>
              <div>
                <dt className="text-xs text-text-muted">Batch size</dt>
                <dd className="text-text-primary">{batchSize}</dd>
              </div>
              <div>
                <dt className="text-xs text-text-muted">Batch interval</dt>
                <dd className="text-text-primary">{batchIntervalMinutes} min</dd>
              </div>
            </>
          )}
          {strategy === 'maintenance_window' && (
            <>
              <div>
                <dt className="text-xs text-text-muted">Window start</dt>
                <dd className="text-text-primary">{maintenanceStart || '—'}</dd>
              </div>
              <div>
                <dt className="text-xs text-text-muted">Window end</dt>
                <dd className="text-text-primary">{maintenanceEnd || '—'}</dd>
              </div>
            </>
          )}
          <div>
            <dt className="text-xs text-text-muted">Reboot policy</dt>
            <dd className="text-text-primary capitalize">{rebootPolicy.replace('_', ' ')}</dd>
          </div>
          <div>
            <dt className="text-xs text-text-muted">Patches / Agents</dt>
            <dd className="text-text-primary tabular-nums">
              {patchCount} / {agentCount}
            </dd>
          </div>
        </dl>
      </div>

      <div className="rounded-md border border-border-subtle bg-surface-secondary/60">
        <div className="px-4 py-3 border-b border-border-subtle flex items-center justify-between">
          <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
            <Package className="h-4 w-4 text-text-secondary" />
            Patches
          </h3>
          <span className="text-xs text-text-muted">{patchCount} selected</span>
        </div>
        <ul className="divide-y divide-border-subtle max-h-48 overflow-y-auto">
          {patchDetails.length === 0 ? (
            <li className="px-4 py-3 text-sm text-text-muted">No patches selected.</li>
          ) : (
            patchDetails.map((p) => (
              <li key={p.id} className="px-4 py-2 flex items-center gap-3 text-sm">
                <SeverityBadge severity={p.severity} showLabel={false} />
                <div className="flex-1 min-w-0">
                  <p className="text-text-primary truncate">{p.title}</p>
                  <p className="text-xs text-text-muted">
                    {p.kb_number ?? '—'}
                    {p.os ? ` · ${p.os}` : ''}
                  </p>
                </div>
              </li>
            ))
          )}
        </ul>
      </div>

      <div className="rounded-md border border-border-subtle bg-surface-secondary/60">
        <div className="px-4 py-3 border-b border-border-subtle flex items-center justify-between">
          <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
            <Server className="h-4 w-4 text-text-secondary" />
            Target agents
          </h3>
          <span className="text-xs text-text-muted">{agentCount} selected</span>
        </div>
        <ul className="divide-y divide-border-subtle max-h-48 overflow-y-auto">
          {agentDetails.length === 0 ? (
            <li className="px-4 py-3 text-sm text-text-muted">No agents selected.</li>
          ) : (
            agentDetails.map((a) => (
              <li key={a.id} className="px-4 py-2 flex items-center gap-3 text-sm">
                <span className="text-text-primary truncate flex-1">{a.hostname || a.id}</span>
                <span className="text-xs text-text-muted">{a.os || '—'}</span>
                <span
                  className={
                    'inline-flex px-2 py-0.5 rounded-full border text-xs ' +
                    (a.status === 'online'
                      ? 'bg-success/10 text-success border-success/20'
                      : 'bg-border-strong/30 text-text-secondary border-border-strong/30')
                  }
                >
                  {a.status || 'unknown'}
                </span>
              </li>
            ))
          )}
        </ul>
      </div>
    </div>
  );
}
