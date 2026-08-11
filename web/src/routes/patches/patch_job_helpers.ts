import type {
  PatchJobStatus,
  InstallStatus,
  RebootStatus,
  DeploymentStage,
} from '@/lib/usePatches';

// ---------------------------------------------------------------------------
// Status/install/reboot display metadata
// ---------------------------------------------------------------------------

export const STATUS_META: Record<PatchJobStatus, { label: string; classes: string }> = {
  pending_approval: {
    label: 'Pending Approval',
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  },
  approved: { label: 'Approved', classes: 'bg-blue-500/10 text-blue-400 border-blue-800' },
  rejected: { label: 'Rejected', classes: 'bg-slate-700/20 text-gray-300 border-slate-700' },
  in_progress: { label: 'In Progress', classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20' },
  completed: { label: 'Completed', classes: 'bg-green-500/10 text-green-400 border-green-800' },
  failed: { label: 'Failed', classes: 'bg-red-500/10 text-red-400 border-red-800' },
  cancelled: { label: 'Cancelled', classes: 'bg-slate-700/20 text-gray-300 border-slate-700' },
  rolled_back: { label: 'Rolled Back', classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800' },
};

export const INSTALL_META: Record<InstallStatus, { label: string; classes: string }> = {
  pending: { label: 'Pending', classes: 'bg-slate-700/20 text-gray-300 border-slate-700' },
  downloading: { label: 'Downloading', classes: 'bg-blue-500/10 text-blue-400 border-blue-800' },
  installing: { label: 'Installing', classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20' },
  completed: { label: 'Completed', classes: 'bg-green-500/10 text-green-400 border-green-800' },
  failed: { label: 'Failed', classes: 'bg-red-500/10 text-red-400 border-red-800' },
  skipped: { label: 'Skipped', classes: 'bg-slate-700/20 text-gray-300 border-slate-700' },
  rolled_back: { label: 'Rolled Back', classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800' },
};

export const REBOOT_META: Record<RebootStatus, { label: string; classes: string }> = {
  not_required: { label: 'Not Required', classes: 'bg-slate-700/30 text-gray-300 border-slate-700/30' },
  pending: { label: 'Pending', classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800' },
  scheduled: { label: 'Scheduled', classes: 'bg-blue-500/10 text-blue-400 border-blue-800' },
  in_progress: { label: 'In Progress', classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20' },
  completed: { label: 'Completed', classes: 'bg-green-500/10 text-green-400 border-green-800' },
  failed: { label: 'Failed', classes: 'bg-red-500/10 text-red-400 border-red-800' },
};

export const ROLLOUT_STAGES: { stage: DeploymentStage; pct: number; label: string }[] = [
  { stage: 'canary', pct: 10, label: '10%' },
  { stage: 'early', pct: 25, label: '25%' },
  { stage: 'majority', pct: 50, label: '50%' },
  { stage: 'complete', pct: 100, label: '100%' },
];

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '—';
  return t.toLocaleString();
}

export function computeProgress(j: { progress_pct?: number; total_agents: number; completed_agents: number; status: string }): number {
  if (typeof j.progress_pct === 'number') return j.progress_pct;
  if (j.total_agents <= 0) {
    if (j.status === 'completed') return 100;
    if (j.status === 'failed' || j.status === 'rolled_back') return 100;
    if (j.status === 'cancelled' || j.status === 'rejected') return 0;
    return 0;
  }
  return Math.min(100, (j.completed_agents / j.total_agents) * 100);
}

export function findActiveStageIndex(progress: number): number {
  for (let i = ROLLOUT_STAGES.length - 1; i >= 0; i -= 1) {
    if (progress >= ROLLOUT_STAGES[i].pct - 0.5) return i;
  }
  return -1;
}

export function defaultScheduleValue(): string {
  const dt = new Date(Date.now() + 5 * 60_000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`;
}
