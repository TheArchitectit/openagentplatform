import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  usePatches,
  type PatchJob,
  type PatchJobStatus,
  type PatchTarget,
  type PatchReboot,
  type PatchApproval,
  type DeploymentStage,
} from '@/lib/usePatches';
import { getWsClient } from '@/lib/websocket';
import { computeProgress, findActiveStageIndex } from './patch_job_helpers';

// ---------------------------------------------------------------------------
// Custom hook: all state, effects, and action handlers for PatchJobDetailPage
// ---------------------------------------------------------------------------

export function usePatchJobDetail(jobId: string) {
  const {
    fetchJob,
    fetchJobTargets,
    fetchJobApprovals,
    fetchJobReboots,
    approveJob,
    rejectJob,
    cancelJob,
    rollbackJob,
    rebootAgentNow,
    scheduleReboot,
  } = usePatches();

  const [job, setJob] = useState<PatchJob | null>(null);
  const [targets, setTargets] = useState<PatchTarget[]>([]);
  const [approvals, setApprovals] = useState<PatchApproval[]>([]);
  const [reboots, setReboots] = useState<PatchReboot[]>([]);
  const [error, setError] = useState<Error | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [actionBusy, setActionBusy] = useState<string | null>(null);
  const [scheduleOpen, setScheduleOpen] = useState<string | null>(null);
  const [scheduleValue, setScheduleValue] = useState('');

  const reloadAll = useCallback(async () => {
    setIsLoading(true);
    try {
      const j = await fetchJob(jobId);
      setJob(j);
      setError(null);
      const [t, a, r] = await Promise.allSettled([
        fetchJobTargets(jobId),
        fetchJobApprovals(jobId),
        fetchJobReboots(jobId),
      ]);
      if (t.status === 'fulfilled') setTargets(t.value);
      else setTargets([]);
      if (a.status === 'fulfilled') setApprovals(a.value);
      else setApprovals([]);
      if (r.status === 'fulfilled') setReboots(r.value);
      else setReboots([]);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [jobId, fetchJob, fetchJobTargets, fetchJobApprovals, fetchJobReboots]);

  useEffect(() => {
    void reloadAll();
  }, [reloadAll]);

  // Merge live job updates from the WebSocket
  useEffect(() => {
    const ws = getWsClient();
    const unsub = ws.subscribe('patches', (env) => {
      if (env.type !== 'event' || !env.data) return;
      if (env.event === 'patch.job.updated') {
        const j = env.data as PatchJob;
        if (j.id === jobId) setJob((prev) => (prev ? { ...prev, ...j } : j));
      } else if (env.event === 'patch.job.state') {
        const s = env.data as { id: string; status: PatchJobStatus; timestamp?: string };
        if (s.id !== jobId) return;
        setJob((prev) =>
          prev
            ? { ...prev, status: s.status, updated_at: s.timestamp ?? prev.updated_at }
            : prev
        );
      } else if (env.event === 'patch.target.updated') {
        const t = env.data as PatchTarget;
        if (t.job_id !== jobId) return;
        setTargets((prev) => {
          const idx = prev.findIndex((x) => x.id === t.id);
          if (idx === -1) return [...prev, t];
          const next = prev.slice();
          next[idx] = { ...next[idx], ...t };
          return next;
        });
      } else if (env.event === 'patch.reboot') {
        const r = env.data as PatchReboot;
        if (r.job_id !== jobId) return;
        setReboots((prev) => {
          const idx = prev.findIndex((x) => x.id === r.id);
          if (idx === -1) return [...prev, r];
          const next = prev.slice();
          next[idx] = { ...next[idx], ...r };
          return next;
        });
      }
    });
    return unsub;
  }, [jobId]);

  // --- Action handlers ---

  const doAction = useCallback(
    async (
      kind: 'approve' | 'reject' | 'cancel' | 'rollback' | 'retry'
    ) => {
      if (!job) return;
      setActionBusy(kind);
      try {
        if (kind === 'approve') await approveJob(job.id);
        else if (kind === 'reject') await rejectJob(job.id);
        else if (kind === 'cancel') await cancelJob(job.id);
        else if (kind !== 'retry') await rollbackJob(job.id);
        await reloadAll();
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setActionBusy(null);
      }
    },
    [job, approveJob, rejectJob, cancelJob, rollbackJob, reloadAll]
  );

  const doRebootNow = useCallback(
    async (agentId: string) => {
      if (!job) return;
      setActionBusy(`reboot-${agentId}`);
      try {
        await rebootAgentNow(job.id, agentId);
        const list = await fetchJobReboots(job.id);
        setReboots(list);
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setActionBusy(null);
      }
    },
    [job, rebootAgentNow, fetchJobReboots]
  );

  const doScheduleReboot = useCallback(
    async (agentId: string) => {
      if (!job || !scheduleValue) return;
      setActionBusy(`schedule-${agentId}`);
      try {
        const iso = new Date(scheduleValue).toISOString();
        await scheduleReboot(job.id, agentId, iso);
        const list = await fetchJobReboots(job.id);
        setReboots(list);
        setScheduleOpen(null);
        setScheduleValue('');
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setActionBusy(null);
      }
    },
    [job, scheduleValue, scheduleReboot, fetchJobReboots]
  );

  // --- Derived data ---

  const progress = job ? computeProgress(job) : 0;
  const activeStageIdx = findActiveStageIndex(progress);

  const targetsByStage = useMemo(() => {
    const map: Record<DeploymentStage, PatchTarget[]> = {
      queued: [],
      canary: [],
      early: [],
      majority: [],
      complete: [],
    };
    if (targets.length === 0 || !job || job.total_agents === 0) return map;
    const total = job.total_agents;
    for (let i = 0; i < targets.length; i += 1) {
      const t = targets[i];
      const slice = (i / total) * 100;
      if (slice < 10) map.canary.push(t);
      else if (slice < 25) map.early.push(t);
      else if (slice < 50) map.majority.push(t);
      else map.complete.push(t);
    }
    return map;
  }, [targets, job]);

  const isTerminal = job
    ? ['completed', 'cancelled', 'rejected', 'rolled_back'].includes(job.status)
    : false;

  return {
    job,
    targets,
    approvals,
    reboots,
    error,
    isLoading,
    actionBusy,
    scheduleOpen,
    scheduleValue,
    progress,
    activeStageIdx,
    targetsByStage,
    isTerminal,
    doAction,
    doRebootNow,
    doScheduleReboot,
    setScheduleOpen,
    setScheduleValue,
  };
}
