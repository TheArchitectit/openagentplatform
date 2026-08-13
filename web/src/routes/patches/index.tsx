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
import { usePatches } from '@/lib/usePatches';
import { type JobFilter } from './patch_list_helpers';
import {
  filterJobs,
  countByTab,
  computeSummary,
  selectableJobIds,
} from './index_helpers';
import { PatchesListView } from './PatchesListView';

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

  const filtered = useMemo(
    () => filterJobs(jobs, filter, query),
    [jobs, filter, query]
  );

  const counts = useMemo(() => countByTab(jobs), [jobs]);

  const summary = useMemo(() => computeSummary(jobs), [jobs]);

  // Selection helpers (only allow selecting rows in pending_approval for
  // batch approve / reject).
  const selectableIds = useMemo(() => selectableJobIds(jobs), [jobs]);
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

  const onOpenJob = useCallback(
    (jobId: string) => {
      void navigate({ to: '/patches/$jobId', params: { jobId } });
    },
    [navigate]
  );

  return (
    <PatchesListView
      jobs={jobs}
      filtered={filtered}
      counts={counts}
      summary={summary}
      filter={filter}
      query={query}
      setFilter={setFilter}
      setQuery={setQuery}
      now={now}
      status={status}
      isLoading={isLoading}
      error={error}
      refresh={refresh}
      selected={selected}
      toggleRow={toggleRow}
      allSelectableSelected={allSelectableSelected}
      selectableIds={selectableIds}
      toggleAllSelectable={toggleAllSelectable}
      clearSelection={clearSelection}
      batchBusy={batchBusy}
      runBatch={runBatch}
      createOpen={createOpen}
      setCreateOpen={setCreateOpen}
      onOpenJob={onOpenJob}
    />
  );
}
