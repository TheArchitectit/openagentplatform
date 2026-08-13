// Pure helper functions for usePatches — no React, no side effects.
import type {
  PatchCatalogFilters,
  PatchJob,
  PatchJobStatus,
  PatchReboot,
  PatchScanResult,
  PatchTarget,
  PatchApproval,
} from './usePatches_types';

// Normalize a list-or-wrapped API response into a plain array + total.
export function unwrapList<T>(
  res: { items?: T[]; jobs?: T[]; total?: number } | T[],
  key: 'items' | 'jobs'
): { list: T[]; total: number } {
  if (Array.isArray(res)) return { list: res, total: res.length };
  const list = res[key] ?? [];
  const total = res.total ?? list.length;
  return { list, total };
}

// Build query string for catalog endpoint.
export function buildCatalogParams(filters?: PatchCatalogFilters): string {
  const params = new URLSearchParams();
  if (filters?.os) params.set('os', filters.os);
  if (filters?.severity) params.set('severity', filters.severity);
  if (filters?.category) params.set('category', filters.category);
  if (filters?.search) params.set('search', filters.search);
  params.set('limit', String(filters?.limit ?? 200));
  if (filters?.offset !== undefined) params.set('offset', String(filters.offset));
  return params.toString();
}

// Build query string for scans endpoint.
export function buildScanParams(filters?: { agent_id?: string; job_id?: string }): string {
  const params = new URLSearchParams();
  if (filters?.agent_id) params.set('agent_id', filters.agent_id);
  if (filters?.job_id) params.set('job_id', filters.job_id);
  params.set('limit', '500');
  return params.toString();
}

// Merge a freshly fetched job into the cached job array (immutably).
export function applyJobToJobs(prev: PatchJob[], updated: PatchJob): PatchJob[] {
  const idx = prev.findIndex((x) => x.id === updated.id);
  if (idx === -1) return [updated, ...prev];
  const next = prev.slice();
  next[idx] = { ...next[idx], ...updated };
  return next;
}

// Merge a list of scans into the cached scan array (dedup by id, cap 500).
export function mergeScans(prev: PatchScanResult[], list: PatchScanResult[]): PatchScanResult[] {
  const map = new Map(prev.map((s) => [s.id, s]));
  for (const s of list) map.set(s.id, s);
  return Array.from(map.values()).slice(0, 500);
}

// Merge job details (targets / approvals / reboots) into the cached job array.
export function mergeJobDetail<T>(
  prev: PatchJob[],
  jobId: string,
  list: T[],
  key: 'targets' | 'approvals' | 'reboots'
): PatchJob[] {
  return prev.map((j) => (j.id === jobId ? { ...j, [key]: list } : j));
}

// Upsert a single reboot record into the cached job array.
export function upsertReboot(
  prev: PatchJob[],
  jobId: string,
  r: PatchReboot,
  agentId: string
): PatchJob[] {
  return prev.map((j) => {
    if (j.id !== jobId) return j;
    const existing = j.reboots ?? [];
    const idx = existing.findIndex((x) => x.id === r.id || x.agent_id === agentId);
    const nextReboots =
      idx === -1
        ? [...existing, r]
        : existing.map((x) => (x.id === r.id || x.agent_id === agentId ? { ...x, ...r } : x));
    return { ...j, reboots: nextReboots };
  });
}
