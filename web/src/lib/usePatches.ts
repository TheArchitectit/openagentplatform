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
import {
  buildCatalogParams,
  buildScanParams,
  applyJobToJobs,
  mergeScans,
  unwrapList,
} from './usePatches.helpers';
import { usePatchJobOps } from './usePatches.operations';

import type {
  PatchJob, PatchCatalogItem, PatchCatalogFilters, PatchTarget, PatchApproval,
  PatchReboot, PatchScanResult, CreatePatchJobInput, UsePatchesResult,
} from './usePatches_types'

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
        setJobs(res); setJobsTotal(res.length);
      } else {
        setJobs(res.jobs ?? []); setJobsTotal(res.total ?? (res.jobs?.length ?? 0));
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
    return () => { mountedRef.current = false; };
  }, [fetchJobs]);

  usePatchWebSocket(mountedRef, setJobs, setStatus, setScans);

  // --- Catalog ---------------------------------------------------------

  const fetchCatalog = useCallback(
    async (filters?: PatchCatalogFilters): Promise<void> => {
      setCatalogLoading(true);
      try {
        const qs = buildCatalogParams(filters);
        const res = await apiFetch<
          { items?: PatchCatalogItem[]; total?: number } | PatchCatalogItem[]
        >(`/patches/catalog${qs ? `?${qs}` : ''}`);
        if (!mountedRef.current) return;
        const { list, total } = unwrapList<PatchCatalogItem>(res, 'items');
        setCatalog(list); setCatalogTotal(total); setCatalogError(null);
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
        const qs = buildScanParams(filters);
        const res = await apiFetch<{ scans?: PatchScanResult[] } | PatchScanResult[]>(
          `/patches/catalog/scan?${qs}`
        );
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
      const res = await apiFetch<{ scans?: PatchScanResult[] } | PatchScanResult[]>(
        '/patches/catalog/scan',
        { method: 'POST', json: agentIds && agentIds.length > 0 ? { agent_ids: agentIds } : undefined }
      );
      const list = Array.isArray(res) ? res : res.scans ?? [];
      if (!mountedRef.current) return list;
      setScans((prev) => mergeScans(prev, list));
      return list;
    },
    []
  );

  // --- Single job mutation (used by usePatchJobOps) --------------------

  const applyJobMutation = useCallback((updated: PatchJob): PatchJob => {
    setJobs((prev) => applyJobToJobs(prev, updated));
    return updated;
  }, []);

  const approveJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/approve`, {
        method: 'POST', json: note ? { note } : undefined,
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rejectJob = useCallback(
    async (id: string, note?: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/reject`, {
        method: 'POST', json: note ? { note } : undefined,
      });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const ops = usePatchJobOps({ mountedRef, setJobs, applyJobMutation, approveJob, rejectJob });

  return {
    catalog, catalogTotal, catalogLoading, catalogError, fetchCatalog, scanMissing,
    jobs, jobsTotal, isLoading, error, status, refresh: fetchJobs,
    fetchJob: ops.fetchJob, createJob: ops.createJob, cancelJob: ops.cancelJob,
    rollbackJob: ops.rollbackJob, approveJob, rejectJob,
    batchApprove: ops.batchApprove, batchReject: ops.batchReject,
    fetchJobTargets: ops.fetchJobTargets, fetchJobApprovals: ops.fetchJobApprovals,
    fetchJobReboots: ops.fetchJobReboots, rebootAgentNow: ops.rebootAgentNow,
    scheduleReboot: ops.scheduleReboot,
    scans, scansLoading, fetchScans,
  };
}

export default usePatches;
