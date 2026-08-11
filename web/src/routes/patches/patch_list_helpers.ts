import type { PatchJobStatus } from '@/lib/usePatches';

// ---------------------------------------------------------------------------
// Types & constants shared by PatchesListPage and sub-components
// ---------------------------------------------------------------------------

export type JobFilter =
  | 'all'
  | 'pending_approval'
  | 'approved'
  | 'in_progress'
  | 'completed'
  | 'failed';

export const JOB_TABS: { id: JobFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'pending_approval', label: 'Pending Approval' },
  { id: 'approved', label: 'Approved' },
  { id: 'in_progress', label: 'In Progress' },
  { id: 'completed', label: 'Completed' },
  { id: 'failed', label: 'Failed' },
];

export const STATUS_META: Record<
  PatchJobStatus,
  { label: string; classes: string }
> = {
  pending_approval: {
    label: 'Pending Approval',
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  },
  approved: {
    label: 'Approved',
    classes: 'bg-blue-500/10 text-blue-400 border-blue-800',
  },
  rejected: {
    label: 'Rejected',
    classes: 'bg-slate-800/20 text-gray-300 border-slate-700/30',
  },
  in_progress: {
    label: 'In Progress',
    classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20',
  },
  completed: {
    label: 'Completed',
    classes: 'bg-green-500/10 text-green-400 border-green-800',
  },
  failed: {
    label: 'Failed',
    classes: 'bg-red-500/10 text-red-400 border-red-800',
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-slate-800/20 text-gray-300 border-slate-700/30',
  },
  rolled_back: {
    label: 'Rolled Back',
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  },
};

// ---------------------------------------------------------------------------
// Date helpers
// ---------------------------------------------------------------------------

export function isToday(iso: string | undefined): boolean {
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

export function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

export function statusToTab(s: PatchJobStatus): JobFilter {
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
