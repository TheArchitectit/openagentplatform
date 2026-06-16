// Patch job detail page.
//
// Layout:
//   • Header: job name, severity, status badge, creator, key timestamps.
//   • Action bar: Approve / Reject / Cancel / Rollback / Retry Failed.
//   • Approval section: approval history with decision, approver, note, time.
//   • Deployment progress: staged rollout visualization (10% → 25% → 50% → 100%).
//   • Target agents table: hostname, current/target versions, install + reboot
//     status, schedule reboot / reboot now inline actions.
//   • Reboot coordination panel: pending reboots with staggered timeline view.
//   • Real-time WebSocket merge of job updates, target updates, and reboots.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft,
  Wrench,
  Check,
  X,
  CircleCheck,
  CircleX,
  CirclePlay,
  RotateCcw,
  Loader2,
  Clock,
  Power,
  CalendarClock,
  Server,
  Activity,
  ShieldCheck,
  AlertCircle,
  ChevronRight,
  ListChecks,
  GitBranch,
} from 'lucide-react';
import {
  usePatches,
  type PatchJob,
  type PatchJobStatus,
  type PatchTarget,
  type PatchReboot,
  type PatchApproval,
  type RebootStatus,
  type InstallStatus,
  type DeploymentStage,
} from '@/lib/usePatches';
import { SeverityBadge } from '@/components/severity-badge';
import { getWsClient } from '@/lib/websocket';

export const Route = createFileRoute('/patches/$jobId')({
  component: PatchJobDetailPage,
});

const STATUS_META: Record<PatchJobStatus, { label: string; classes: string }> = {
  pending_approval: {
    label: 'Pending Approval',
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
  },
  approved: { label: 'Approved', classes: 'bg-sky-500/10 text-sky-300 border-sky-500/20' },
  rejected: { label: 'Rejected', classes: 'bg-slate-600/20 text-slate-300 border-slate-500/30' },
  in_progress: { label: 'In Progress', classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20' },
  completed: { label: 'Completed', classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20' },
  failed: { label: 'Failed', classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20' },
  cancelled: { label: 'Cancelled', classes: 'bg-slate-600/20 text-slate-300 border-slate-500/30' },
  rolled_back: { label: 'Rolled Back', classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20' },
};

const INSTALL_META: Record<InstallStatus, { label: string; classes: string }> = {
  pending: { label: 'Pending', classes: 'bg-slate-600/20 text-slate-300 border-slate-500/30' },
  downloading: { label: 'Downloading', classes: 'bg-sky-500/10 text-sky-300 border-sky-500/20' },
  installing: { label: 'Installing', classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20' },
  completed: { label: 'Completed', classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20' },
  failed: { label: 'Failed', classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20' },
  skipped: { label: 'Skipped', classes: 'bg-slate-600/20 text-slate-300 border-slate-500/30' },
  rolled_back: { label: 'Rolled Back', classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20' },
};

const REBOOT_META: Record<RebootStatus, { label: string; classes: string }> = {
  not_required: { label: 'Not Required', classes: 'bg-slate-700/30 text-slate-400 border-slate-600/30' },
  pending: { label: 'Pending', classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20' },
  scheduled: { label: 'Scheduled', classes: 'bg-sky-500/10 text-sky-300 border-sky-500/20' },
  in_progress: { label: 'In Progress', classes: 'bg-indigo-500/10 text-indigo-300 border-indigo-500/20' },
  completed: { label: 'Completed', classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20' },
  failed: { label: 'Failed', classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20' },
};

const ROLLOUT_STAGES: { stage: DeploymentStage; pct: number; label: string }[] = [
  { stage: 'canary', pct: 10, label: '10%' },
  { stage: 'early', pct: 25, label: '25%' },
  { stage: 'majority', pct: 50, label: '50%' },
  { stage: 'complete', pct: 100, label: '100%' },
];

function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '—';
  return t.toLocaleString();
}

function computeProgress(j: PatchJob): number {
  if (typeof j.progress_pct === 'number') return j.progress_pct;
  if (j.total_agents <= 0) {
    if (j.status === 'completed') return 100;
    if (j.status === 'failed' || j.status === 'rolled_back') return 100;
    if (j.status === 'cancelled' || j.status === 'rejected') return 0;
    return 0;
  }
  return Math.min(100, (j.completed_agents / j.total_agents) * 100);
}

function findActiveStageIndex(progress: number): number {
  for (let i = ROLLOUT_STAGES.length - 1; i >= 0; i -= 1) {
    if (progress >= ROLLOUT_STAGES[i].pct - 0.5) return i;
  }
  return -1;
}

function PatchJobDetailPage() {
  const { jobId } = Route.useParams();

  const {
    fetchJob,
    fetchJobTargets,
    fetchJobApprovals,
    fetchJobReboots,
    approveJob,
    rejectJob,
    cancelJob,
    rollbackJob,
    retryJob,
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
      // Fire and merge the rest; failures here shouldn't block the page.
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

  // Merge live job updates from the WebSocket so progress / status changes
  // surface without a manual refresh.
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

  // --- Action handlers --------------------------------------------------

  const doAction = useCallback(
    async (kind: 'approve' | 'reject' | 'cancel' | 'rollback' | 'retry') => {
      if (!job) return;
      setActionBusy(kind);
      try {
        if (kind === 'approve') await approveJob(job.id);
        else if (kind === 'reject') await rejectJob(job.id);
        else if (kind === 'cancel') await cancelJob(job.id);
        else if (kind === 'rollback') await rollbackJob(job.id);
        else await retryJob(job.id);
        await reloadAll();
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setActionBusy(null);
      }
    },
    [job, approveJob, rejectJob, cancelJob, rollbackJob, retryJob, reloadAll]
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
        // Convert datetime-local string to ISO 8601
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

  // --- Derived view data ------------------------------------------------

  const progress = job ? computeProgress(job) : 0;
  const activeStageIdx = findActiveStageIndex(progress);

  const pendingReboots = useMemo(
    () => reboots.filter((r) => r.status === 'pending' || r.status === 'scheduled'),
    [reboots]
  );

  const targetsByStage = useMemo(() => {
    const map: Record<DeploymentStage, PatchTarget[]> = {
      queued: [],
      canary: [],
      early: [],
      majority: [],
      complete: [],
    };
    if (targets.length === 0 || !job || job.total_agents === 0) return map;
    // Bucket by cumulative percentage of total_agents based on
    // simple quartile splits.
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

  if (isLoading && !job) {
    return (
      <div className="text-center text-slate-500 py-24">
        <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
        Loading patch job…
      </div>
    );
  }

  if (error && !job) {
    return (
      <div className="space-y-4">
        <Link
          to="/patches"
          className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-200"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to patches</span>
        </Link>
        <div className="rounded-lg border border-rose-500/30 bg-rose-500/5 p-6 text-rose-300">
          Failed to load job: {error.message}
        </div>
      </div>
    );
  }

  if (!job) return null;

  const state = (job.status ?? 'pending_approval').toLowerCase() as PatchJobStatus;
  const statusMeta = STATUS_META[state] ?? STATUS_META.pending_approval;
  const isTerminal =
    state === 'completed' ||
    state === 'cancelled' ||
    state === 'rejected' ||
    state === 'rolled_back';

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div className="flex items-start gap-3 min-w-0">
          <Link
            to="/patches"
            className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center hover:bg-slate-700 transition-colors shrink-0"
            title="Back to patches"
          >
            <ArrowLeft className="h-4 w-4 text-slate-300" />
          </Link>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <div className="h-9 w-9 rounded-md bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center">
                <Wrench className="h-4 w-4 text-indigo-400" />
              </div>
              <h1 className="text-2xl font-bold text-slate-100 break-words">{job.name || job.id}</h1>
              <SeverityBadge severity={job.severity} />
              <span
                className={
                  'inline-flex items-center px-2.5 py-1 rounded-full border text-sm font-medium ' +
                  statusMeta.classes
                }
              >
                {statusMeta.label}
              </span>
            </div>
            {job.description && (
              <p className="text-slate-400 mt-1 break-words">{job.description}</p>
            )}
            <p className="text-xs text-slate-500 mt-1 font-mono">{job.id}</p>
          </div>
        </div>

        {/* Action bar */}
        <div className="flex items-center gap-2 flex-wrap">
          {state === 'pending_approval' && (
            <>
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void doAction('approve')}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-emerald-500/15 border border-emerald-500/30 text-emerald-200 text-sm hover:bg-emerald-500/25 disabled:opacity-50 transition-colors"
              >
                {actionBusy === 'approve' ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Check className="h-4 w-4" />
                )}
                <span>Approve</span>
              </button>
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void doAction('reject')}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-rose-500/15 border border-rose-500/30 text-rose-200 text-sm hover:bg-rose-500/25 disabled:opacity-50 transition-colors"
              >
                {actionBusy === 'reject' ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <X className="h-4 w-4" />
                )}
                <span>Reject</span>
              </button>
            </>
          )}
          {(state === 'in_progress' || state === 'approved') && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('cancel')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-slate-200 text-sm hover:bg-slate-700 disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'cancel' ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <X className="h-4 w-4" />
              )}
              <span>Cancel</span>
            </button>
          )}
          {state === 'failed' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('retry')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-indigo-600 hover:bg-indigo-500 border border-indigo-500 text-white text-sm disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'retry' ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RotateCcw className="h-4 w-4" />
              )}
              <span>Retry Failed</span>
            </button>
          )}
          {state === 'completed' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('rollback')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-amber-500/15 border border-amber-500/30 text-amber-200 text-sm hover:bg-amber-500/25 disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'rollback' ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RotateCcw className="h-4 w-4" />
              )}
              <span>Rollback</span>
            </button>
          )}
        </div>
      </div>

      {/* Job metadata strip */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 p-4">
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Created</dt>
            <dd className="text-slate-200 mt-1">
              {formatTime(job.created_at)}
              {job.created_by && (
                <span className="block text-xs text-slate-500">by {job.created_by}</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Approved</dt>
            <dd className="text-slate-200 mt-1">
              {formatTime(job.approved_at)}
              {job.approved_by && (
                <span className="block text-xs text-slate-500">by {job.approved_by}</span>
              )}
              {!job.approved_at && <span className="text-slate-500">—</span>}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Started</dt>
            <dd className="text-slate-200 mt-1">{formatTime(job.started_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Completed</dt>
            <dd className="text-slate-200 mt-1">{formatTime(job.completed_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Patches</dt>
            <dd className="text-slate-200 mt-1 tabular-nums">{job.patch_count}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Target agents</dt>
            <dd className="text-slate-200 mt-1 tabular-nums">{job.total_agents}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Strategy</dt>
            <dd className="text-slate-200 mt-1 capitalize">
              {(job.strategy ?? 'staged').replace('_', ' ')}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500 uppercase tracking-wider">Reboot policy</dt>
            <dd className="text-slate-200 mt-1 capitalize">
              {(job.reboot_policy ?? 'if_required').replace('_', ' ')}
            </dd>
          </div>
        </dl>
      </div>

      {/* Deployment progress (staged rollout) */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100 flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-slate-400" />
            Deployment progress
          </h2>
          <span className="text-xs text-slate-500 tabular-nums">
            {job.completed_agents} / {job.total_agents} completed
            {job.failed_agents > 0 && (
              <span className="text-rose-400 ml-2">
                ({job.failed_agents} failed)
              </span>
            )}
          </span>
        </div>
        <div className="p-5 space-y-4">
          <div className="h-2 w-full rounded-full bg-slate-800 overflow-hidden">
            <div
              className={
                'h-full transition-all ' +
                (state === 'completed'
                  ? 'bg-emerald-500'
                  : state === 'failed'
                  ? 'bg-rose-500'
                  : state === 'cancelled'
                  ? 'bg-slate-500'
                  : 'bg-indigo-500')
              }
              style={{ width: `${Math.max(0, Math.min(100, progress))}%` }}
            />
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-slate-500">0%</span>
            <span className="text-slate-300 tabular-nums font-medium">
              {Math.round(progress)}%
            </span>
            <span className="text-slate-500">100%</span>
          </div>

          {/* Staged rollout steps */}
          <div className="mt-2">
            <div className="grid grid-cols-4 gap-2">
              {ROLLOUT_STAGES.map((s, idx) => {
                const reached = progress >= s.pct - 0.5;
                const active = idx === activeStageIdx;
                const stageTargets = targetsByStage[s.stage] ?? [];
                return (
                  <div
                    key={s.stage}
                    className={
                      'rounded-md border p-3 ' +
                      (reached
                        ? 'border-indigo-500/30 bg-indigo-500/5'
                        : 'border-slate-800 bg-slate-900/40')
                    }
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5">
                        {reached ? (
                          <CircleCheck className="h-3.5 w-3.5 text-emerald-400" />
                        ) : (
                          <div className="h-3.5 w-3.5 rounded-full border border-slate-700" />
                        )}
                        <span
                          className={
                            'text-xs font-medium ' +
                            (reached ? 'text-slate-100' : 'text-slate-400')
                          }
                        >
                          {s.label}
                        </span>
                      </div>
                      {active && (
                        <span className="inline-flex items-center gap-1 text-[10px] text-indigo-300">
                          <CirclePlay className="h-3 w-3 animate-pulse" />
                          <span>active</span>
                        </span>
                      )}
                    </div>
                    <p className="text-[10px] text-slate-500 mt-1 capitalize">
                      {s.stage}
                      {stageTargets.length > 0 && ` · ${stageTargets.length} agents`}
                    </p>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* Approvals section */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100 flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-slate-400" />
            Approvals
          </h2>
          <span className="text-xs text-slate-500">
            {approvals.length} decision{approvals.length === 1 ? '' : 's'}
          </span>
        </div>
        {approvals.length === 0 ? (
          <p className="text-sm text-slate-500 p-5">
            {state === 'pending_approval'
              ? 'This job is waiting for an approver. Use the buttons above to approve or reject.'
              : 'No approval decisions recorded yet.'}
          </p>
        ) : (
          <ul className="divide-y divide-slate-800">
            {approvals.map((a) => {
              const decision = (a.decision ?? '').toLowerCase();
              const icon =
                decision === 'approved'
                  ? CircleCheck
                  : decision === 'rejected'
                  ? CircleX
                  : AlertCircle;
              const tone =
                decision === 'approved'
                  ? 'text-emerald-300 bg-emerald-500/10 border-emerald-500/20'
                  : decision === 'rejected'
                  ? 'text-rose-300 bg-rose-500/10 border-rose-500/20'
                  : 'text-amber-300 bg-amber-500/10 border-amber-500/20';
              const Icon = icon;
              return (
                <li key={a.id} className="px-5 py-3 flex items-start gap-3">
                  <div
                    className={
                      'h-7 w-7 rounded-full border flex items-center justify-center shrink-0 ' +
                      tone
                    }
                  >
                    <Icon className="h-3.5 w-3.5" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2 flex-wrap">
                      <span className="text-sm font-medium text-slate-100 capitalize">
                        {a.decision.replace('_', ' ')}
                      </span>
                      <span className="text-xs text-slate-500">
                        {formatTime(a.created_at)}
                      </span>
                    </div>
                    {a.approver && (
                      <p className="text-xs text-slate-500">by {a.approver}</p>
                    )}
                    {a.note && (
                      <p className="text-sm text-slate-300 mt-1 break-words">{a.note}</p>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {/* Reboot coordination panel */}
      <RebootCoordinationPanel
        reboots={reboots}
        jobStatus={state}
        onRebootNow={doRebootNow}
        onScheduleClick={(agentId) => {
          setScheduleOpen(agentId);
          // Default to +5 minutes from now.
          const dt = new Date(Date.now() + 5 * 60_000);
          // datetime-local needs YYYY-MM-DDTHH:mm
          const pad = (n: number) => String(n).padStart(2, '0');
          setScheduleValue(
            `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(
              dt.getHours()
            )}:${pad(dt.getMinutes())}`
          );
        }}
        actionBusy={actionBusy}
        scheduleOpen={scheduleOpen}
        scheduleValue={scheduleValue}
        onScheduleChange={setScheduleValue}
        onScheduleSubmit={doScheduleReboot}
        onScheduleCancel={() => {
          setScheduleOpen(null);
          setScheduleValue('');
        }}
      />

      {/* Target agents table */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-slate-100 flex items-center gap-2">
            <Server className="h-4 w-4 text-slate-400" />
            Target agents
          </h2>
          <span className="text-xs text-slate-500">
            {targets.length} of {job.total_agents}
          </span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-4 py-3">Hostname</th>
                <th className="px-4 py-3 w-32">OS / Version</th>
                <th className="px-4 py-3 w-32">Current</th>
                <th className="px-4 py-3 w-32">Target</th>
                <th className="px-4 py-3 w-36">Install</th>
                <th className="px-4 py-3 w-36">Reboot</th>
                <th className="px-4 py-3 text-right w-48">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {targets.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-slate-500">
                    No target agents have been reported yet.
                  </td>
                </tr>
              ) : (
                targets.map((t) => (
                  <TargetRow
                    key={t.id}
                    target={t}
                    isJobTerminal={isTerminal}
                    onRebootNow={() => void doRebootNow(t.agent_id)}
                    onScheduleClick={() => {
                      setScheduleOpen(t.agent_id);
                      const dt = new Date(Date.now() + 5 * 60_000);
                      const pad = (n: number) => String(n).padStart(2, '0');
                      setScheduleValue(
                        `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(
                          dt.getDate()
                        )}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`
                      );
                    }}
                    busy={actionBusy === `reboot-${t.agent_id}` || actionBusy === `schedule-${t.agent_id}`}
                    scheduleOpen={scheduleOpen === t.agent_id}
                    scheduleValue={scheduleValue}
                    onScheduleChange={setScheduleValue}
                    onScheduleSubmit={() => void doScheduleReboot(t.agent_id)}
                    onScheduleCancel={() => {
                      setScheduleOpen(null);
                      setScheduleValue('');
                    }}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Target row
// ---------------------------------------------------------------------------

function TargetRow({
  target: t,
  isJobTerminal,
  onRebootNow,
  onScheduleClick,
  busy,
  scheduleOpen,
  scheduleValue,
  onScheduleChange,
  onScheduleSubmit,
  onScheduleCancel,
}: {
  target: PatchTarget;
  isJobTerminal: boolean;
  onRebootNow: () => void;
  onScheduleClick: () => void;
  busy: boolean;
  scheduleOpen: boolean;
  scheduleValue: string;
  onScheduleChange: (v: string) => void;
  onScheduleSubmit: () => void;
  onScheduleCancel: () => void;
}) {
  const installMeta = INSTALL_META[t.install_status] ?? INSTALL_META.pending;
  const rebootMeta = REBOOT_META[t.reboot_status] ?? REBOOT_META.not_required;
  const needsReboot =
    t.reboot_status === 'pending' || t.reboot_status === 'scheduled';
  const canScheduleReboot =
    needsReboot && !isJobTerminal;

  return (
    <tr className="hover:bg-slate-800/40 transition-colors">
      <td className="px-4 py-3">
        <div className="flex flex-col">
          <span className="text-slate-100">{t.hostname || t.agent_id}</span>
          {t.hostname && (
            <span className="text-[10px] text-slate-500 font-mono">{t.agent_id}</span>
          )}
        </div>
      </td>
      <td className="px-4 py-3 text-slate-300 text-xs">
        {t.os || '—'}
        {t.os_version && (
          <span className="block text-slate-500">{t.os_version}</span>
        )}
      </td>
      <td className="px-4 py-3 text-slate-300 text-xs font-mono">
        {t.current_version || '—'}
      </td>
      <td className="px-4 py-3 text-slate-300 text-xs font-mono">
        {t.target_version || '—'}
      </td>
      <td className="px-4 py-3">
        <span
          className={
            'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
            installMeta.classes
          }
        >
          {installMeta.label}
        </span>
        {t.error_message && (
          <p className="text-xs text-rose-300 mt-1 max-w-[10rem] truncate" title={t.error_message}>
            {t.error_message}
          </p>
        )}
      </td>
      <td className="px-4 py-3">
        <span
          className={
            'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
            rebootMeta.classes
          }
        >
          {rebootMeta.label}
        </span>
        {t.scheduled_reboot_at && t.reboot_status === 'scheduled' && (
          <p className="text-xs text-slate-500 mt-1">{formatTime(t.scheduled_reboot_at)}</p>
        )}
      </td>
      <td className="px-4 py-3 text-right">
        {scheduleOpen ? (
          <div className="inline-flex items-center gap-1.5">
            <input
              type="datetime-local"
              value={scheduleValue}
              onChange={(e) => onScheduleChange(e.target.value)}
              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
            />
            <button
              type="button"
              onClick={onScheduleSubmit}
              disabled={busy || !scheduleValue}
              className="h-8 px-2 rounded-md bg-indigo-600 hover:bg-indigo-500 border border-indigo-500 text-xs text-white disabled:opacity-50"
              title="Confirm schedule"
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
            </button>
            <button
              type="button"
              onClick={onScheduleCancel}
              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-slate-300 text-xs hover:bg-slate-700"
              title="Cancel"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : canScheduleReboot ? (
          <div className="inline-flex items-center gap-1.5">
            <button
              type="button"
              onClick={onRebootNow}
              disabled={busy}
              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-amber-500/10 border border-amber-500/30 text-amber-300 hover:bg-amber-500/20 disabled:opacity-50 transition-colors"
              title="Reboot now"
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Power className="h-3.5 w-3.5" />}
              <span>Now</span>
            </button>
            <button
              type="button"
              onClick={onScheduleClick}
              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors"
              title="Schedule reboot"
            >
              <CalendarClock className="h-3.5 w-3.5" />
              <span>Schedule</span>
            </button>
          </div>
        ) : (
          <span className="text-xs text-slate-500">—</span>
        )}
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Reboot coordination panel
// ---------------------------------------------------------------------------

function RebootCoordinationPanel({
  reboots,
  jobStatus,
  onRebootNow,
  onScheduleClick,
  actionBusy,
  scheduleOpen,
  scheduleValue,
  onScheduleChange,
  onScheduleSubmit,
  onScheduleCancel,
}: {
  reboots: PatchReboot[];
  jobStatus: PatchJobStatus;
  onRebootNow: (agentId: string) => void;
  onScheduleClick: (agentId: string) => void;
  actionBusy: string | null;
  scheduleOpen: string | null;
  scheduleValue: string;
  onScheduleChange: (v: string) => void;
  onScheduleSubmit: (agentId: string) => void;
  onScheduleCancel: () => void;
}) {
  // Group pending/scheduled reboots by their stage_index (0-based staggered
  // timeline). If stage_index is missing, fall back to scheduled_at.
  const grouped = useMemo(() => {
    const buckets = new Map<number, PatchReboot[]>();
    for (const r of reboots) {
      if (r.status !== 'pending' && r.status !== 'scheduled') continue;
      const key = r.stage_index ?? -1;
      if (!buckets.has(key)) buckets.set(key, []);
      buckets.get(key)!.push(r);
    }
    return Array.from(buckets.entries())
      .sort(([a], [b]) => {
        // Sentinels (-1) go last.
        if (a === -1) return 1;
        if (b === -1) return -1;
        return a - b;
      })
      .map(([k, list]) => {
        list.sort((a, b) => {
          const ta = a.scheduled_at ? new Date(a.scheduled_at).getTime() : 0;
          const tb = b.scheduled_at ? new Date(b.scheduled_at).getTime() : 0;
          return ta - tb;
        });
        return { stageIndex: k, items: list };
      });
  }, [reboots]);

  const pendingCount = reboots.filter(
    (r) => r.status === 'pending' || r.status === 'scheduled'
  ).length;

  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/60">
      <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-slate-100 flex items-center gap-2">
          <Power className="h-4 w-4 text-slate-400" />
          Reboot coordination
        </h2>
        <span className="text-xs text-slate-500">
          {pendingCount} pending reboot{pendingCount === 1 ? '' : 's'}
        </span>
      </div>
      <div className="p-5">
        {grouped.length === 0 ? (
          <div className="text-center text-slate-500 py-6">
            <ListChecks className="inline h-5 w-5 mb-1" />
            <p className="text-sm">No pending reboots.</p>
            <p className="text-xs text-slate-600 mt-1">
              Reboots appear here when the rollout requires a restart.
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            {grouped.map(({ stageIndex, items }) => (
              <div
                key={stageIndex}
                className="rounded-md border border-slate-800 bg-slate-950/40"
              >
                <div className="px-4 py-2 border-b border-slate-800 flex items-center gap-2">
                  <span
                    className={
                      'inline-flex items-center gap-1.5 text-xs font-medium ' +
                      (stageIndex === -1 ? 'text-slate-300' : 'text-indigo-300')
                    }
                  >
                    <Clock className="h-3.5 w-3.5" />
                    {stageIndex === -1 ? 'Unscheduled' : `Stage ${stageIndex + 1}`}
                  </span>
                  {items[0]?.scheduled_at && (
                    <span className="text-xs text-slate-500">
                      · target {formatTime(items[0].scheduled_at)}
                    </span>
                  )}
                  <span className="ml-auto text-xs text-slate-500 tabular-nums">
                    {items.length} agent{items.length === 1 ? '' : 's'}
                  </span>
                </div>
                <ul className="divide-y divide-slate-800">
                  {items.map((r) => {
                    const isScheduling = scheduleOpen === r.agent_id;
                    return (
                      <li
                        key={r.id}
                        className="px-4 py-2 flex items-center gap-3 text-sm"
                      >
                        <Server className="h-4 w-4 text-slate-500 shrink-0" />
                        <div className="flex-1 min-w-0">
                          <p className="text-slate-200 truncate">{r.hostname || r.agent_id}</p>
                          <p className="text-xs text-slate-500">
                            {r.status === 'scheduled' && r.scheduled_at
                              ? `Scheduled for ${formatTime(r.scheduled_at)}`
                              : 'Awaiting reboot'}
                          </p>
                          {r.last_error && (
                            <p className="text-xs text-rose-300 mt-0.5 truncate">
                              {r.last_error}
                            </p>
                          )}
                        </div>
                        {isScheduling ? (
                          <div className="inline-flex items-center gap-1.5">
                            <input
                              type="datetime-local"
                              value={scheduleValue}
                              onChange={(e) => onScheduleChange(e.target.value)}
                              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-xs text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
                            />
                            <button
                              type="button"
                              onClick={() => onScheduleSubmit(r.agent_id)}
                              disabled={actionBusy !== null || !scheduleValue}
                              className="h-8 px-2 rounded-md bg-indigo-600 hover:bg-indigo-500 border border-indigo-500 text-xs text-white disabled:opacity-50"
                            >
                              {actionBusy === `schedule-${r.agent_id}` ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              ) : (
                                <Check className="h-3.5 w-3.5" />
                              )}
                            </button>
                            <button
                              type="button"
                              onClick={onScheduleCancel}
                              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-slate-300 text-xs hover:bg-slate-700"
                            >
                              <X className="h-3.5 w-3.5" />
                            </button>
                          </div>
                        ) : (
                          <div className="inline-flex items-center gap-1.5">
                            <button
                              type="button"
                              onClick={() => onRebootNow(r.agent_id)}
                              disabled={actionBusy !== null || jobStatus === 'cancelled' || jobStatus === 'rejected'}
                              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-amber-500/10 border border-amber-500/30 text-amber-300 hover:bg-amber-500/20 disabled:opacity-50 transition-colors"
                            >
                              {actionBusy === `reboot-${r.agent_id}` ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              ) : (
                                <Power className="h-3.5 w-3.5" />
                              )}
                              <span>Now</span>
                            </button>
                            <button
                              type="button"
                              onClick={() => onScheduleClick(r.agent_id)}
                              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors"
                            >
                              <CalendarClock className="h-3.5 w-3.5" />
                              <span>Schedule</span>
                            </button>
                          </div>
                        )}
                      </li>
                    );
                  })}
                </ul>
              </div>
            ))}

            {/* Staggered timeline view */}
            <div className="rounded-md border border-slate-800 bg-slate-950/40 p-4">
              <h3 className="text-xs text-slate-500 uppercase tracking-wider mb-3 flex items-center gap-1.5">
                <Activity className="h-3.5 w-3.5" />
                Staggered timeline
              </h3>
              <div className="relative pl-4">
                <div className="absolute left-1.5 top-1 bottom-1 w-px bg-slate-800" />
                <ol className="space-y-3">
                  {grouped.map(({ stageIndex, items }) => {
                    const isUnscheduled = stageIndex === -1;
                    return (
                      <li key={stageIndex} className="relative flex items-start gap-3">
                        <span
                          className={
                            'absolute -left-[2px] mt-1.5 h-3 w-3 rounded-full border-2 border-slate-950 ' +
                            (isUnscheduled ? 'bg-slate-600' : 'bg-indigo-500')
                          }
                        />
                        <div className="pl-5">
                          <p className="text-sm text-slate-200">
                            {isUnscheduled ? 'Unscheduled bucket' : `Stage ${stageIndex + 1}`}
                            <ChevronRight className="inline h-3.5 w-3.5 mx-1 text-slate-500" />
                            <span className="text-slate-400 tabular-nums">
                              {items.length} reboot{items.length === 1 ? '' : 's'}
                            </span>
                          </p>
                          {items[0]?.scheduled_at && (
                            <p className="text-xs text-slate-500">
                              {formatTime(items[0].scheduled_at)}
                            </p>
                          )}
                        </div>
                      </li>
                    );
                  })}
                </ol>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
