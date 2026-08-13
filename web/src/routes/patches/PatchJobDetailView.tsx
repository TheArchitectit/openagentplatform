import {
  Wrench,
  Check,
  X,
  RotateCcw,
  CircleCheck,
  CircleX,
  CirclePlay,
  Loader2,
  Server,
  ShieldCheck,
  AlertCircle,
  GitBranch,
  ArrowLeft,
} from 'lucide-react';
import { Link } from '@tanstack/react-router';
import type { ComponentType } from 'react';
import { SeverityBadge } from '@/components/severity-badge';
import { type PatchJob } from '@/lib/usePatches';
import { TargetRow, RebootCoordinationPanel } from './patch_job_components';
import { STATUS_META, ROLLOUT_STAGES, formatTime } from './patch_job_helpers';
import { ApprovalsSection, TargetAgentsTable, ActionBar } from './PatchJobDetailView.sections';

type JobStatus = NonNullable<PatchJob['status']>;

export function PatchJobDetailView({
  job,
  state,
  progress,
  activeStageIdx,
  targetsByStage,
  targets,
  approvals,
  reboots,
  actionBusy,
  scheduleOpen,
  scheduleValue,
  doAction,
  doRebootNow,
  setScheduleOpen,
  setScheduleValue,
  doScheduleReboot,
  isTerminal,
}: {
  job: PatchJob;
  state: JobStatus;
  progress: number;
  activeStageIdx: number;
  targetsByStage: Record<string, unknown[]>;
  targets: import('@/lib/usePatches').PatchTarget[];
  approvals: import('@/lib/usePatches').PatchApproval[];
  reboots: import('@/lib/usePatches').PatchReboot[];
  actionBusy: string | null;
  scheduleOpen: string | null;
  scheduleValue: string;
  doAction: (a: 'approve' | 'reject' | 'cancel' | 'rollback' | 'retry') => void;
  doRebootNow: (agentId: string) => void;
  setScheduleOpen: (v: string | null) => void;
  setScheduleValue: (v: string) => void;
  doScheduleReboot: (agentId: string) => void;
  isTerminal: boolean;
}) {
  const statusMeta = STATUS_META[state] ?? STATUS_META.pending_approval;

  const makeScheduleClick = (agentId: string) => () => {
    setScheduleOpen(agentId);
    const dt = new Date(Date.now() + 5 * 60_000);
    const pad = (n: number) => String(n).padStart(2, '0');
    setScheduleValue(
      `${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`
    );
  };

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
            <ArrowLeft className="h-4 w-4 text-gray-300" />
          </Link>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <div className="h-9 w-9 rounded-md bg-blue-600/10 border border-blue-500/20 flex items-center justify-center">
                <Wrench className="h-4 w-4 text-blue-400" />
              </div>
              <h1 className="text-2xl font-bold text-white break-words">{job.name || job.id}</h1>
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
              <p className="text-gray-300 mt-1 break-words">{job.description}</p>
            )}
            <p className="text-xs text-gray-400 mt-1 font-mono">{job.id}</p>
          </div>
        </div>

        <ActionBar state={state} actionBusy={actionBusy} doAction={doAction} />
      </div>

      {/* Job metadata strip */}
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Created</dt>
            <dd className="text-white mt-1">
              {formatTime(job.created_at)}
              {job.created_by && <span className="block text-xs text-gray-400">by {job.created_by}</span>}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Approved</dt>
            <dd className="text-white mt-1">
              {formatTime(job.approved_at)}
              {job.approved_by && <span className="block text-xs text-gray-400">by {job.approved_by}</span>}
              {!job.approved_at && <span className="text-gray-400">—</span>}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Started</dt>
            <dd className="text-white mt-1">{formatTime(job.started_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Completed</dt>
            <dd className="text-white mt-1">{formatTime(job.completed_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Patches</dt>
            <dd className="text-white mt-1 tabular-nums">{job.patch_count}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Target agents</dt>
            <dd className="text-white mt-1 tabular-nums">{job.total_agents}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Strategy</dt>
            <dd className="text-white mt-1 capitalize">{(job.strategy ?? 'staged').replace('_', ' ')}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Reboot policy</dt>
            <dd className="text-white mt-1 capitalize">{(job.reboot_policy ?? 'if_required').replace('_', ' ')}</dd>
          </div>
        </dl>
      </div>

      {/* Deployment progress (staged rollout) */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white flex items-center gap-2">
            <GitBranch className="h-4 w-4 text-gray-300" />
            Deployment progress
          </h2>
          <span className="text-xs text-gray-400 tabular-nums">
            {job.completed_agents} / {job.total_agents} completed
            {job.failed_agents > 0 && <span className="text-red-400 ml-2">({job.failed_agents} failed)</span>}
          </span>
        </div>
        <div className="p-5 space-y-4">
          <div className="h-2 w-full rounded-full bg-slate-800 overflow-hidden">
            <div
              className={
                'h-full transition-all ' +
                (state === 'completed' ? 'bg-green-500' : state === 'failed' ? 'bg-red-500' : state === 'cancelled' ? 'bg-slate-500' : 'bg-blue-600')
              }
              style={{ width: `${Math.max(0, Math.min(100, progress))}%` }}
            />
          </div>
          <div className="flex items-center justify-between text-xs">
            <span className="text-gray-400">0%</span>
            <span className="text-gray-300 tabular-nums font-medium">{Math.round(progress)}%</span>
            <span className="text-gray-400">100%</span>
          </div>
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
                      (reached ? 'border-blue-500/30 bg-blue-600/5' : 'border-slate-800 bg-slate-800')
                    }
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5">
                        {reached ? (
                          <CircleCheck className="h-3.5 w-3.5 text-green-400" />
                        ) : (
                          <div className="h-3.5 w-3.5 rounded-full border border-slate-700" />
                        )}
                        <span className={'text-xs font-medium ' + (reached ? 'text-white' : 'text-gray-300')}>
                          {s.label}
                        </span>
                      </div>
                      {active && (
                        <span className="inline-flex items-center gap-1 text-[10px] text-blue-400">
                          <CirclePlay className="h-3 w-3 animate-pulse" />
                          <span>active</span>
                        </span>
                      )}
                    </div>
                    <p className="text-[10px] text-gray-400 mt-1 capitalize">
                      {s.stage}{stageTargets.length > 0 && ` · ${stageTargets.length} agents`}
                    </p>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* Approvals section */}
      <ApprovalsSection approvals={approvals} state={state} />

      {/* Reboot coordination panel */}
      <RebootCoordinationPanel
        reboots={reboots}
        jobStatus={state}
        onRebootNow={doRebootNow}
        onScheduleClick={makeScheduleClick}
        actionBusy={actionBusy}
        scheduleOpen={scheduleOpen}
        scheduleValue={scheduleValue}
        onScheduleChange={setScheduleValue}
        onScheduleSubmit={doScheduleReboot}
        onScheduleCancel={() => { setScheduleOpen(null); setScheduleValue(''); }}
      />

      {/* Target agents table */}
      <TargetAgentsTable
        targets={targets}
        jobTotalAgents={job.total_agents}
        isTerminal={isTerminal}
        doRebootNow={doRebootNow}
        makeScheduleClick={makeScheduleClick}
        actionBusy={actionBusy}
        scheduleOpen={scheduleOpen}
        scheduleValue={scheduleValue}
        setScheduleOpen={setScheduleOpen}
        setScheduleValue={setScheduleValue}
        doScheduleReboot={doScheduleReboot}
      />
    </div>
  );
}
