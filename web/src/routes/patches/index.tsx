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
  AlertTriangle,
  Activity,
  CalendarCheck,
  Check,
  X,
  Package,
} from 'lucide-react';
import { usePatches } from '@/lib/usePatches';
import { SummaryTile, JobRow } from './patch_list_components';
import { CreateJobModal } from './create_job_modal';
import { JOB_TABS, isToday, statusToTab, type JobFilter } from './patch_list_helpers';

export const Route = createFileRoute('/patches/')({
  component: PatchesListPage,
});

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
      total += 1;

      const sev = (j.severity ?? '').toLowerCase();
      if (sev === 'critical' || sev === 'emergency') critical += 1;
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
          <div className="h-9 w-9 rounded-md bg-blue-600/10 border border-blue-500/20 flex items-center justify-center">
            <Wrench className="h-4 w-4 text-blue-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Patches</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Manage OS and application patch rollouts across the fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-green-500' : status === 'connecting' ? 'bg-yellow-500' : 'bg-slate-500')
            }
            title={`WebSocket: ${status}`}
          />
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} />
            <span>Refresh</span>
          </button>
          <button
            type="button"
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 border border-blue-500 text-sm text-white transition-colors"
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
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto">
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
                  ? 'bg-slate-800 text-white'
                  : 'text-gray-300 hover:text-white')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-gray-400">{counts[t.id]}</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search patch jobs"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search jobs…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
      </div>

      {/* Batch actions bar */}
      {selected.size > 0 && (
        <div className="flex items-center justify-between gap-3 rounded-md border border-blue-500/30 bg-blue-600/5 px-4 py-2">
          <div className="text-sm text-white">
            <span className="font-medium">{selected.size}</span> job
            {selected.size === 1 ? '' : 's'} selected (pending approval)
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('approve')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-green-500/15 border border-green-800 text-green-400 text-sm hover:bg-green-500/25 disabled:opacity-50 transition-colors"
            >
              <Check className="h-3.5 w-3.5" />
              <span>Approve all</span>
            </button>
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('reject')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-red-500/15 border border-red-800 text-red-400 text-sm hover:bg-red-500/25 disabled:opacity-50 transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              <span>Reject all</span>
            </button>
            <button
              type="button"
              onClick={clearSelection}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-sm hover:bg-slate-700 transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              <span>Clear</span>
            </button>
          </div>
        </div>
      )}

      {/* Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Patch jobs" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                <th className="px-3 py-3 w-10" scope="col">
                  <input
                    type="checkbox"
                    aria-label="Select all pending-approval jobs"
                    checked={allSelectableSelected}
                    onChange={toggleAllSelectable}
                    disabled={selectableIds.length === 0}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40 disabled:opacity-40"
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
            <tbody className="divide-y divide-slate-800">
              {isLoading && jobs.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status" aria-live="polite">
                    Loading patches…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-red-400" role="alert">
                    Failed to load jobs: {error.message}
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
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
