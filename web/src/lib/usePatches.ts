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
//   POST   /patches/jobs
//   GET    /patches/:id
//   POST   /patches/:id/approve
//   POST   /patches/:id/reject
//   POST   /patches/:id/cancel
//   POST   /patches/:id/rollback
//   POST   /patches/catalog/scan
//   POST   /patches/catalog/scan/site/:siteId
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
import { usePatchWebSocket } from './usePatches_ws'
import type { Status } from './websocket';

import type {
  PatchJob, PatchCatalogItem, PatchCatalogFilters, PatchTarget, PatchApproval,
  PatchReboot, PatchScanResult, CreatePatchJobInput, UsePatchesResult,
} from './usePatches_types'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------


export type {
  PatchJobStatus, PatchSeverity, PatchCategory, RebootStatus, InstallStatus,
  DeploymentStage, PatchCatalogItem, PatchTarget, PatchApproval, PatchReboot,
  PatchJob, PatchScanResult, CreatePatchJobInput, UsePatchesResult, PatchCatalogFilters,
} from './usePatches_types'

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

  // WebSocket updates handled by usePatchWebSocket hook
  usePatchWebSocket(mountedRef, setJobs, setStatus, setScans);
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
        >(`/patches/catalog/scan?${params.toString()}`);

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
      >('/patches/catalog/scan', {
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
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}`);
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
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/cancel`, {
        method: 'POST',
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rollbackJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/rollback`, {
        method: 'POST',
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const approveJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/approve`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rejectJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/reject`, {
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

