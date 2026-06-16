// usePatches — manages patch management operations across the platform.
//
// Patch operations exposed:
//   - Catalog:   list of available OS / vendor patches that can be applied
//   - Jobs:      deployment jobs that roll patches out to agents
//   - Scans:     on-demand scan results (missing patches per agent)
//   - Approvals: approval / rejection of pending jobs
//   - Reboots:   pending reboot coordination per agent
//   - WebSocket: real-time merge of patch / job / reboot events
//
// REST endpoints (server-of-record):
//   GET    /patches/catalog?os=&severity=&search=&page=&limit=
//   GET    /patches/jobs?status=&page=&limit=
//   POST   /patches/jobs
//   GET    /patches/jobs/:id
//   POST   /patches/jobs/:id/approve
//   POST   /patches/jobs/:id/reject
//   POST   /patches/jobs/:id/cancel
//   POST   /patches/jobs/:id/rollback
//   POST   /patches/jobs/:id/retry
//   GET    /patches/jobs/:id/targets
//   GET    /patches/jobs/:id/approvals
//   GET    /patches/jobs/:id/reboots
//   POST   /patches/jobs/:id/reboots/:agentId/reboot-now
//   POST   /patches/jobs/:id/reboots/:agentId/schedule
//   GET    /patches/scans?agent_id=&job_id=
//   POST   /patches/scans
//
// WebSocket event vocabulary (server -> client):
//   { channel: "patches", event: "patch.job.created",   data: PatchJob }
//   { channel: "patches", event: "patch.job.updated",   data: PatchJob }
//   { channel: "patches", event: "patch.job.state",     data: { id, status, stage?, timestamp? } }
//   { channel: "patches", event: "patch.target.updated", data: PatchTarget }
//   { channel: "patches", event: "patch.reboot",        data: PatchReboot }
//   { channel: "patches", event: "patch.scan.completed", data: PatchScan }

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import { getWsClient, type WsEnvelope, type Status } from './websocket';
import type { Severity } from '@/components/severity-badge';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type PatchJobStatus =
  | 'pending_approval'
  | 'approved'
  | 'rejected'
  | 'in_progress'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'rolled_back';

export type PatchSeverity = 'critical' | 'important' | 'moderate' | 'low' | Severity;
export type PatchCategory = 'security' | 'os' | 'application' | 'driver' | 'firmware' | 'other';

export type RebootStatus =
  | 'not_required'
  | 'pending'
  | 'scheduled'
  | 'in_progress'
  | 'completed'
  | 'failed';

export type InstallStatus =
  | 'pending'
  | 'downloading'
  | 'installing'
  | 'completed'
  | 'failed'
  | 'skipped'
  | 'rolled_back';

export type DeploymentStage = 'queued' | 'canary' | 'early' | 'majority' | 'complete';

export interface PatchCatalogItem {
  id: string;
  kb_number?: string;
  cve_ids?: string[];
  title: string;
  description?: string;
  os: string;
  category: PatchCategory;
  severity: PatchSeverity;
  release_date?: string;
  size_mb?: number;
  vendor?: string;
  product?: string;
  requires_reboot?: boolean;
  affected_agent_count?: number;
  cvss_score?: number;
}

export interface PatchTarget {
  id: string;
  job_id: string;
  agent_id: string;
  hostname: string;
  os: string;
  os_version?: string;
  current_version?: string;
  target_version?: string;
  install_status: InstallStatus;
  reboot_status: RebootStatus;
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  scheduled_reboot_at?: string;
  rebooted_at?: string;
}

export interface PatchApproval {
  id: string;
  job_id: string;
  approver?: string;
  decision: 'approved' | 'rejected' | 'requested_changes';
  note?: string;
  created_at: string;
}

export interface PatchReboot {
  id: string;
  job_id: string;
  agent_id: string;
  hostname: string;
  status: RebootStatus;
  scheduled_at?: string;
  rebooted_at?: string;
  // Minutes from job creation to perform the reboot. Null = manual.
  delay_minutes?: number | null;
  // Index into the staggered timeline (0, 1, 2, …).
  stage_index?: number;
  last_error?: string;
}

export interface PatchJob {
  id: string;
  name: string;
  description?: string;
  status: PatchJobStatus;
  severity: PatchSeverity;
  patch_ids: string[];
  patch_count: number;
  // Target agent selection
  target_agent_ids: string[];
  target_tags?: string[];
  target_label?: string;
  // Counts (denormalized for fast UI rendering)
  total_agents: number;
  completed_agents: number;
  failed_agents: number;
  in_progress_agents: number;
  // Deployment strategy
  strategy?: 'immediate' | 'staged' | 'maintenance_window';
  stages?: DeploymentStage[];
  // Maintenance / reboot config
  maintenance_window_start?: string;
  maintenance_window_end?: string;
  reboot_policy?: 'never' | 'if_required' | 'always' | 'scheduled';
  // Audit
  created_by?: string;
  created_at: string;
  updated_at?: string;
  approved_at?: string;
  approved_by?: string;
  started_at?: string;
  completed_at?: string;
  progress_pct?: number;
  // Aggregated / derived
  targets?: PatchTarget[];
  reboots?: PatchReboot[];
  approvals?: PatchApproval[];
}

export interface PatchScanResult {
  id: string;
  agent_id: string;
  hostname: string;
  os: string;
  patch_id: string;
  kb_number?: string;
  cve_ids?: string[];
  severity: PatchSeverity;
  detected_at: string;
  job_id?: string;
  // Per-agent scan metadata
  missing?: boolean;
  installed?: boolean;
  current_version?: string;
  available_version?: string;
  release_date?: string;
  cvss_score?: number;
}

export interface CreatePatchJobInput {
  name: string;
  description?: string;
  patch_ids: string[];
  target_agent_ids: string[];
  target_tags?: string[];
  strategy?: 'immediate' | 'staged' | 'maintenance_window';
  maintenance_window_start?: string;
  maintenance_window_end?: string;
  reboot_policy?: 'never' | 'if_required' | 'always' | 'scheduled';
  // How many agents per batch (for staged rollouts). Default 10.
  batch_size?: number;
  // How many minutes between batch advances. Default 15.
  batch_interval_minutes?: number;
  // When the job should be queued for execution. ISO 8601.
  scheduled_at?: string;
}

export interface UsePatchesResult {
  // Catalog
  catalog: PatchCatalogItem[];
  catalogTotal: number;
  catalogLoading: boolean;
  catalogError: Error | null;
  fetchCatalog: (filters?: PatchCatalogFilters) => Promise<void>;
  scanMissing: (agentIds?: string[]) => Promise<PatchScanResult[]>;

  // Jobs
  jobs: PatchJob[];
  jobsTotal: number;
  isLoading: boolean;
  error: Error | null;
  status: Status;
  refresh: () => Promise<void>;
  fetchJob: (id: string) => Promise<PatchJob>;
  createJob: (input: CreatePatchJobInput) => Promise<PatchJob>;
  cancelJob: (id: string) => Promise<PatchJob>;
  rollbackJob: (id: string) => Promise<PatchJob>;
  retryJob: (id: string) => Promise<PatchJob>;
  approveJob: (id: string, note?: string) => Promise<PatchJob>;
  rejectJob: (id: string, note?: string) => Promise<PatchJob>;
  batchApprove: (ids: string[], note?: string) => Promise<{ succeeded: string[]; failed: string[] }>;
  batchReject: (ids: string[], note?: string) => Promise<{ succeeded: string[]; failed: string[] }>;

  // Job details
  fetchJobTargets: (jobId: string) => Promise<PatchTarget[]>;
  fetchJobApprovals: (jobId: string) => Promise<PatchApproval[]>;
  fetchJobReboots: (jobId: string) => Promise<PatchReboot[]>;
  rebootAgentNow: (jobId: string, agentId: string) => Promise<PatchReboot>;
  scheduleReboot: (
    jobId: string,
    agentId: string,
    scheduledAt: string
  ) => Promise<PatchReboot>;

  // Scans
  scans: PatchScanResult[];
  scansLoading: boolean;
  fetchScans: (filters?: { agent_id?: string; job_id?: string }) => Promise<PatchScanResult[]>;
}

export interface PatchCatalogFilters {
  os?: string;
  severity?: PatchSeverity;
  category?: PatchCategory;
  search?: string;
  limit?: number;
  offset?: number;
}

// ---------------------------------------------------------------------------
// WebSocket helpers
// ---------------------------------------------------------------------------

type WsPatchEvent =
  | { event: 'patch.job.created'; data: PatchJob }
  | { event: 'patch.job.updated'; data: PatchJob }
  | { event: 'patch.job.state'; data: { id: string; status: PatchJobStatus; stage?: DeploymentStage; timestamp?: string; previous_status?: string } }
  | { event: 'patch.target.updated'; data: PatchTarget }
  | { event: 'patch.reboot'; data: PatchReboot }
  | { event: 'patch.scan.completed'; data: PatchScanResult };

function isPatchEvent(env: WsEnvelope): env is WsEnvelope & WsPatchEvent {
  if (env.type !== 'event' || env.channel !== 'patches') return false;
  const ev = env.event;
  if (
    ev !== 'patch.job.created' &&
    ev !== 'patch.job.updated' &&
    ev !== 'patch.job.state' &&
    ev !== 'patch.target.updated' &&
    ev !== 'patch.reboot' &&
    ev !== 'patch.scan.completed'
  ) {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function usePatches(): UsePatchesResult {
  const [jobs, setJobs] = useState<PatchJob[]>([]);
  const [jobsTotal, setJobsTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [status, setStatus] = useState<Status>('closed');

  const [catalog, setCatalog] = useState<PatchCatalogItem[]>([]);
  const [catalogTotal, setCatalogTotal] = useState(0);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [catalogError, setCatalogError] = useState<Error | null>(null);

  const [scans, setScans] = useState<PatchScanResult[]>([]);
  const [scansLoading, setScansLoading] = useState(false);

  const mountedRef = useRef(true);

  // --- Jobs list --------------------------------------------------------

  const fetchJobs = useCallback(async (): Promise<void> => {
    try {
      const res = await apiFetch<{ jobs?: PatchJob[]; total?: number } | PatchJob[]>(
        '/patches/jobs?limit=500'
      );
      if (!mountedRef.current) return;
      if (Array.isArray(res)) {
        setJobs(res);
        setJobsTotal(res.length);
      } else {
        setJobs(res.jobs ?? []);
        setJobsTotal(res.total ?? (res.jobs?.length ?? 0));
      }
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    void fetchJobs();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchJobs]);

  // --- WebSocket subscription ------------------------------------------

  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());
    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const handler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isPatchEvent(env)) return;
      const payload = env.data as unknown;

      if (env.event === 'patch.job.created') {
        const j = payload as PatchJob;
        setJobs((prev) => {
          if (prev.some((x) => x.id === j.id)) return prev;
          return [j, ...prev];
        });
        return;
      }

      if (env.event === 'patch.job.updated') {
        const j = payload as PatchJob;
        setJobs((prev) => {
          const idx = prev.findIndex((x) => x.id === j.id);
          if (idx === -1) return [j, ...prev];
          const next = prev.slice();
          next[idx] = { ...next[idx], ...j };
          return next;
        });
        return;
      }

      if (env.event === 'patch.job.state') {
        const s = payload as {
          id: string;
          status: PatchJobStatus;
          stage?: DeploymentStage;
          timestamp?: string;
          previous_status?: string;
        };
        setJobs((prev) =>
          prev.map((j) =>
            j.id === s.id
              ? {
                  ...j,
                  status: s.status,
                  updated_at: s.timestamp ?? j.updated_at,
                  ...(s.stage ? { stages: dedupStages([...(j.stages ?? []), s.stage]) } : {}),
                }
              : j
          )
        );
        return;
      }

      if (env.event === 'patch.target.updated') {
        const t = payload as PatchTarget;
        setJobs((prev) =>
          prev.map((j) => {
            if (j.id !== t.job_id) return j;
            const existing = j.targets ?? [];
            const idx = existing.findIndex((x) => x.id === t.id);
            const nextTargets =
              idx === -1 ? [...existing, t] : existing.map((x) => (x.id === t.id ? { ...x, ...t } : x));
            return { ...j, targets: nextTargets };
          })
        );
        return;
      }

      if (env.event === 'patch.reboot') {
        const r = payload as PatchReboot;
        setJobs((prev) =>
          prev.map((j) => {
            if (j.id !== r.job_id) return j;
            const existing = j.reboots ?? [];
            const idx = existing.findIndex((x) => x.id === r.id);
            const nextReboots =
              idx === -1
                ? [...existing, r]
                : existing.map((x) => (x.id === r.id ? { ...x, ...r } : x));
            return { ...j, reboots: nextReboots };
          })
        );
        return;
      }

      if (env.event === 'patch.scan.completed') {
        const s = payload as PatchScanResult;
        setScans((prev) => {
          if (prev.some((x) => x.id === s.id)) return prev;
          return [s, ...prev].slice(0, 500);
        });
        return;
      }
    };

    const unsub = ws.subscribe('patches', handler);
    return () => {
      clearInterval(statusInterval);
      unsub();
    };
  }, []);

  // --- Catalog ---------------------------------------------------------

  const fetchCatalog = useCallback(
    async (filters?: PatchCatalogFilters): Promise<void> => {
      setCatalogLoading(true);
      try {
        const params = new URLSearchParams();
        if (filters?.os) params.set('os', filters.os);
        if (filters?.severity) params.set('severity', filters.severity);
        if (filters?.category) params.set('category', filters.category);
        if (filters?.search) params.set('search', filters.search);
        params.set('limit', String(filters?.limit ?? 200));
        if (filters?.offset !== undefined) params.set('offset', String(filters.offset));

        const qs = params.toString();
        const res = await apiFetch<
          { items?: PatchCatalogItem[]; total?: number } | PatchCatalogItem[]
        >(`/patches/catalog${qs ? `?${qs}` : ''}`);

        if (!mountedRef.current) return;
        if (Array.isArray(res)) {
          setCatalog(res);
          setCatalogTotal(res.length);
        } else {
          setCatalog(res.items ?? []);
          setCatalogTotal(res.total ?? (res.items?.length ?? 0));
        }
        setCatalogError(null);
      } catch (err) {
        if (!mountedRef.current) return;
        setCatalogError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
      } finally {
        if (mountedRef.current) setCatalogLoading(false);
      }
    },
    []
  );

  // --- Scans -----------------------------------------------------------

  const fetchScans = useCallback(
    async (filters?: { agent_id?: string; job_id?: string }): Promise<PatchScanResult[]> => {
      setScansLoading(true);
      try {
        const params = new URLSearchParams();
        if (filters?.agent_id) params.set('agent_id', filters.agent_id);
        if (filters?.job_id) params.set('job_id', filters.job_id);
        params.set('limit', '500');
        const res = await apiFetch<
          { scans?: PatchScanResult[] } | PatchScanResult[]
        >(`/patches/scans?${params.toString()}`);

        const list = Array.isArray(res) ? res : res.scans ?? [];
        if (!mountedRef.current) return list;
        setScans(list);
        return list;
      } catch (err) {
        if (!mountedRef.current) return [];
        throw err instanceof Error ? err : new ApiError(0, 'Unknown', String(err));
      } finally {
        if (mountedRef.current) setScansLoading(false);
      }
    },
    []
  );

  const scanMissing = useCallback(
    async (agentIds?: string[]): Promise<PatchScanResult[]> => {
      const res = await apiFetch<
        { scans?: PatchScanResult[] } | PatchScanResult[]
      >('/patches/scans', {
        method: 'POST',
        json: agentIds && agentIds.length > 0 ? { agent_ids: agentIds } : undefined,
      });
      const list = Array.isArray(res) ? res : res.scans ?? [];
      if (!mountedRef.current) return list;
      setScans((prev) => {
        const map = new Map(prev.map((s) => [s.id, s]));
        for (const s of list) map.set(s.id, s);
        return Array.from(map.values()).slice(0, 500);
      });
      return list;
    },
    []
  );

  // --- Single job ------------------------------------------------------

  const applyJobMutation = useCallback((updated: PatchJob): PatchJob => {
    setJobs((prev) => {
      const idx = prev.findIndex((x) => x.id === updated.id);
      if (idx === -1) return [updated, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...updated };
      return next;
    });
    return updated;
  }, []);

  const fetchJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}`);
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const createJob = useCallback(async (input: CreatePatchJobInput): Promise<PatchJob> => {
    const j = await apiFetch<PatchJob>('/patches/jobs', {
      method: 'POST',
      json: input,
    });
    setJobs((prev) => {
      if (prev.some((x) => x.id === j.id)) return prev;
      return [j, ...prev];
    });
    return j;
  }, []);

  const cancelJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}/cancel`, {
        method: 'POST',
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rollbackJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}/rollback`, {
        method: 'POST',
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const retryJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}/retry`, {
        method: 'POST',
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const approveJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}/approve`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rejectJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/jobs/${encodeURIComponent(id)}/reject`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  // --- Batch -----------------------------------------------------------

  const runBatchDecision = useCallback(
    async (
      ids: string[],
      action: 'approve' | 'reject',
      note?: string
    ): Promise<{ succeeded: string[]; failed: string[] }> => {
      const succeeded: string[] = [];
      const failed: string[] = [];
      await Promise.all(
        ids.map(async (id) => {
          try {
            if (action === 'approve') {
              await approveJob(id, note);
            } else {
              await rejectJob(id, note);
            }
            succeeded.push(id);
          } catch {
            failed.push(id);
          }
        })
      );
      return { succeeded, failed };
    },
    [approveJob, rejectJob]
  );

  const batchApprove = useCallback(
    (ids: string[], note?: string) => runBatchDecision(ids, 'approve', note),
    [runBatchDecision]
  );

  const batchReject = useCallback(
    (ids: string[], note?: string) => runBatchDecision(ids, 'reject', note),
    [runBatchDecision]
  );

  // --- Job details endpoints -------------------------------------------

  const fetchJobTargets = useCallback(async (jobId: string): Promise<PatchTarget[]> => {
    const res = await apiFetch<{ targets: PatchTarget[] } | PatchTarget[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/targets`
    );
    const list = Array.isArray(res) ? res : res.targets ?? [];

    // Merge into the cached job record.
    setJobs((prev) =>
      prev.map((j) =>
        j.id === jobId
          ? { ...j, targets: list }
          : j
      )
    );
    return list;
  }, []);

  const fetchJobApprovals = useCallback(async (jobId: string): Promise<PatchApproval[]> => {
    const res = await apiFetch<{ approvals: PatchApproval[] } | PatchApproval[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/approvals`
    );
    const list = Array.isArray(res) ? res : res.approvals ?? [];
    setJobs((prev) =>
      prev.map((j) => (j.id === jobId ? { ...j, approvals: list } : j))
    );
    return list;
  }, []);

  const fetchJobReboots = useCallback(async (jobId: string): Promise<PatchReboot[]> => {
    const res = await apiFetch<{ reboots: PatchReboot[] } | PatchReboot[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/reboots`
    );
    const list = Array.isArray(res) ? res : res.reboots ?? [];
    setJobs((prev) =>
      prev.map((j) => (j.id === jobId ? { ...j, reboots: list } : j))
    );
    return list;
  }, []);

  const rebootAgentNow = useCallback(
    async (jobId: string, agentId: string): Promise<PatchReboot> => {
      const r = await apiFetch<PatchReboot>(
        `/patches/jobs/${encodeURIComponent(jobId)}/reboots/${encodeURIComponent(agentId)}/reboot-now`,
        { method: 'POST' }
      );
      setJobs((prev) =>
        prev.map((j) => {
          if (j.id !== jobId) return j;
          const existing = j.reboots ?? [];
          const idx = existing.findIndex((x) => x.id === r.id || x.agent_id === agentId);
          const nextReboots =
            idx === -1
              ? [...existing, r]
              : existing.map((x) => (x.id === r.id || x.agent_id === agentId ? { ...x, ...r } : x));
          return { ...j, reboots: nextReboots };
        })
      );
      return r;
    },
    []
  );

  const scheduleReboot = useCallback(
    async (jobId: string, agentId: string, scheduledAt: string): Promise<PatchReboot> => {
      const r = await apiFetch<PatchReboot>(
        `/patches/jobs/${encodeURIComponent(jobId)}/reboots/${encodeURIComponent(agentId)}/schedule`,
        { method: 'POST', json: { scheduled_at: scheduledAt } }
      );
      setJobs((prev) =>
        prev.map((j) => {
          if (j.id !== jobId) return j;
          const existing = j.reboots ?? [];
          const idx = existing.findIndex((x) => x.id === r.id || x.agent_id === agentId);
          const nextReboots =
            idx === -1
              ? [...existing, r]
              : existing.map((x) => (x.id === r.id || x.agent_id === agentId ? { ...x, ...r } : x));
          return { ...j, reboots: nextReboots };
        })
      );
      return r;
    },
    []
  );

  return {
    // catalog
    catalog,
    catalogTotal,
    catalogLoading,
    catalogError,
    fetchCatalog,
    scanMissing,
    // jobs
    jobs,
    jobsTotal,
    isLoading,
    error,
    status,
    refresh: fetchJobs,
    fetchJob,
    createJob,
    cancelJob,
    rollbackJob,
    retryJob,
    approveJob,
    rejectJob,
    batchApprove,
    batchReject,
    // job details
    fetchJobTargets,
    fetchJobApprovals,
    fetchJobReboots,
    rebootAgentNow,
    scheduleReboot,
    // scans
    scans,
    scansLoading,
    fetchScans,
  };
}

export default usePatches;

// --- Helpers ---------------------------------------------------------------

function dedupStages(stages: DeploymentStage[]): DeploymentStage[] {
  const seen = new Set<DeploymentStage>();
  const out: DeploymentStage[] = [];
  for (const s of stages) {
    if (!seen.has(s)) {
      seen.add(s);
      out.push(s);
    }
  }
  return out;
}
