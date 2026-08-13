import { type PatchJob } from '@/lib/usePatches';
import { isToday, statusToTab, type JobFilter } from './patch_list_helpers';

export function filterJobs(
  jobs: PatchJob[],
  filter: JobFilter,
  query: string
): PatchJob[] {
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
}

export function countByTab(jobs: PatchJob[]): Record<JobFilter, number> {
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
}

export interface PatchesSummary {
  total: number;
  critical: number;
  security: number;
  approved: number;
  inProgress: number;
  completedToday: number;
}

export function computeSummary(jobs: PatchJob[]): PatchesSummary {
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
}

export function selectableJobIds(jobs: PatchJob[]): string[] {
  return jobs.filter((j) => j.status === 'pending_approval').map((j) => j.id);
}
