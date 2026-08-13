// Patch job mutation operations — extracted from usePatches.ts to keep that
// file under the line limit. These callbacks operate on the cached jobs array
// via setJobs and rely on apiFetch for REST calls.
import { useCallback } from 'react';
import { apiFetch, ApiError } from './api';
import type { MutableRefObject } from 'react';
import type {
  PatchJob,
  PatchTarget,
  PatchApproval,
  PatchReboot,
  CreatePatchJobInput,
} from './usePatches_types';
import {
  applyJobToJobs,
  mergeJobDetail,
  upsertReboot,
} from './usePatches.helpers';

interface PatchOpsArgs {
  mountedRef: MutableRefObject<boolean>;
  setJobs: React.Dispatch<React.SetStateAction<PatchJob[]>>;
  applyJobMutation: (updated: PatchJob) => PatchJob;
  approveJob: (id: string, note?: string) => Promise<PatchJob>;
  rejectJob: (id: string, note?: string) => Promise<PatchJob>;
}

export function usePatchJobOps({
  mountedRef,
  setJobs,
  applyJobMutation,
  approveJob,
  rejectJob,
}: PatchOpsArgs) {
  const fetchJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}`);
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const createJob = useCallback(async (input: CreatePatchJobInput): Promise<PatchJob> => {
    const j = await apiFetch<PatchJob>('/patches/jobs', { method: 'POST', json: input });
    setJobs((prev) => {
      if (prev.some((x) => x.id === j.id)) return prev;
      return [j, ...prev];
    });
    return j;
  }, [setJobs]);

  const cancelJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/cancel`, { method: 'POST' });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const rollbackJob = useCallback(
    async (id: string): Promise<PatchJob> => {
      const j = await apiFetch<PatchJob>(`/patches/${encodeURIComponent(id)}/rollback`, { method: 'POST' });
      return applyJobMutation(j);
    },
    [applyJobMutation]
  );

  const fetchJobTargets = useCallback(async (jobId: string): Promise<PatchTarget[]> => {
    const res = await apiFetch<{ targets: PatchTarget[] } | PatchTarget[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/targets`
    );
    const list = Array.isArray(res) ? res : res.targets ?? [];
    setJobs((prev) => mergeJobDetail(prev, jobId, list, 'targets'));
    return list;
  }, [setJobs]);

  const fetchJobApprovals = useCallback(async (jobId: string): Promise<PatchApproval[]> => {
    const res = await apiFetch<{ approvals: PatchApproval[] } | PatchApproval[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/approvals`
    );
    const list = Array.isArray(res) ? res : res.approvals ?? [];
    setJobs((prev) => mergeJobDetail(prev, jobId, list, 'approvals'));
    return list;
  }, [setJobs]);

  const fetchJobReboots = useCallback(async (jobId: string): Promise<PatchReboot[]> => {
    const res = await apiFetch<{ reboots: PatchReboot[] } | PatchReboot[]>(
      `/patches/jobs/${encodeURIComponent(jobId)}/reboots`
    );
    const list = Array.isArray(res) ? res : res.reboots ?? [];
    setJobs((prev) => mergeJobDetail(prev, jobId, list, 'reboots'));
    return list;
  }, [setJobs]);

  const rebootAgentNow = useCallback(
    async (jobId: string, agentId: string): Promise<PatchReboot> => {
      const r = await apiFetch<PatchReboot>(
        `/patches/jobs/${encodeURIComponent(jobId)}/reboots/${encodeURIComponent(agentId)}/reboot-now`,
        { method: 'POST' }
      );
      setJobs((prev) => upsertReboot(prev, jobId, r, agentId));
      return r;
    },
    [setJobs]
  );

  const scheduleReboot = useCallback(
    async (jobId: string, agentId: string, scheduledAt: string): Promise<PatchReboot> => {
      const r = await apiFetch<PatchReboot>(
        `/patches/jobs/${encodeURIComponent(jobId)}/reboots/${encodeURIComponent(agentId)}/schedule`,
        { method: 'POST', json: { scheduled_at: scheduledAt } }
      );
      setJobs((prev) => upsertReboot(prev, jobId, r, agentId));
      return r;
    },
    [setJobs]
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
            if (action === 'approve') await approveJob(id, note);
            else await rejectJob(id, note);
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

  return {
    fetchJob,
    createJob,
    cancelJob,
    rollbackJob,
    fetchJobTargets,
    fetchJobApprovals,
    fetchJobReboots,
    rebootAgentNow,
    scheduleReboot,
    batchApprove,
    batchReject,
  };
}
