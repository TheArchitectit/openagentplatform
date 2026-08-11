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
  Server,
  ShieldCheck,
  AlertCircle,
  GitBranch,
} from 'lucide-react';
import { SeverityBadge } from '@/components/severity-badge';
import { usePatchJobDetail } from './usePatchJobDetail';
import { TargetRow, RebootCoordinationPanel } from './patch_job_components';
import { STATUS_META, ROLLOUT_STAGES, formatTime } from './patch_job_helpers';

export const Route = createFileRoute('/patches/$jobId')({
  component: PatchJobDetailPage,
});

function PatchJobDetailPage() {
  const { jobId } = Route.useParams();
  const {
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
  } = usePatchJobDetail(jobId);

  if (isLoading && !job) {
    return (
      <div className="text-center text-gray-400 py-24">
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
          className="inline-flex items-center gap-2 text-sm text-gray-300 hover:text-white"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to patches</span>
        </Link>
        <div className="rounded-lg border border-red-800 bg-red-500/5 p-6 text-red-400">
          Failed to load job: {error.message}
        </div>
      </div>
    );
  }

  if (!job) return null;

  const state = (job.status ?? 'pending_approval').toLowerCase() as typeof job.status;
  const statusMeta = STATUS_META[state] ?? STATUS_META.pending_approval;

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

        {/* Action bar */}
        <div className="flex items-center gap-2 flex-wrap">
          {state === 'pending_approval' && (
            <>
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void doAction('approve')}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-green-500/15 border border-green-800 text-green-400 text-sm hover:bg-green-500/25 disabled:opacity-50 transition-colors"
              >
                {actionBusy === 'approve' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" />}
                <span>Approve</span>
              </button>
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => void doAction('reject')}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-red-500/15 border border-red-800 text-red-400 text-sm hover:bg-red-500/25 disabled:opacity-50 transition-colors"
              >
                {actionBusy === 'reject' ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}
                <span>Reject</span>
              </button>
            </>
          )}
          {(state === 'in_progress' || state === 'approved') && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('cancel')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-white text-sm hover:bg-slate-700 disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'cancel' ? <Loader2 className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}
              <span>Cancel</span>
            </button>
          )}
          {state === 'failed' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('retry')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 border border-blue-500 text-white text-sm disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'retry' ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              <span>Retry Failed</span>
            </button>
          )}
          {state === 'completed' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('rollback')}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-yellow-500/15 border border-yellow-800 text-yellow-400 text-sm hover:bg-yellow-500/25 disabled:opacity-50 transition-colors"
            >
              {actionBusy === 'rollback' ? <Loader2 className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              <span>Rollback</span>
            </button>
          )}
        </div>
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
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-gray-300" />
            Approvals
          </h2>
          <span className="text-xs text-gray-400">{approvals.length} decision{approvals.length === 1 ? '' : 's'}</span>
        </div>
        {approvals.length === 0 ? (
          <p className="text-sm text-gray-400 p-5">
            {state === 'pending_approval'
              ? 'This job is waiting for an approver. Use the buttons above to approve or reject.'
              : 'No approval decisions recorded yet.'}
          </p>
        ) : (
          <ul className="divide-y divide-slate-800">
            {approvals.map((a) => {
              const decision = (a.decision ?? '').toLowerCase();
              const Icon = decision === 'approved' ? CircleCheck : decision === 'rejected' ? CircleX : AlertCircle;
              const tone = decision === 'approved' ? 'text-green-400 bg-green-500/10 border-green-800' : decision === 'rejected' ? 'text-red-400 bg-red-500/10 border-red-800' : 'text-yellow-400 bg-yellow-500/10 border-yellow-800';
              return (
                <li key={a.id} className="px-5 py-3 flex items-start gap-3">
                  <div className={'h-7 w-7 rounded-full border flex items-center justify-center shrink-0 ' + tone}>
                    <Icon className="h-3.5 w-3.5" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-baseline gap-2 flex-wrap">
                      <span className="text-sm font-medium text-white capitalize">{a.decision.replace('_', ' ')}</span>
                      <span className="text-xs text-gray-400">{formatTime(a.created_at)}</span>
                    </div>
                    {a.approver && <p className="text-xs text-gray-400">by {a.approver}</p>}
                    {a.note && <p className="text-sm text-gray-300 mt-1 break-words">{a.note}</p>}
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
          const dt = new Date(Date.now() + 5 * 60_000);
          const pad = (n: number) => String(n).padStart(2, '0');
          setScheduleValue(`${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`);
        }}
        actionBusy={actionBusy}
        scheduleOpen={scheduleOpen}
        scheduleValue={scheduleValue}
        onScheduleChange={setScheduleValue}
        onScheduleSubmit={doScheduleReboot}
        onScheduleCancel={() => { setScheduleOpen(null); setScheduleValue(''); }}
      />

      {/* Target agents table */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white flex items-center gap-2">
            <Server className="h-4 w-4 text-gray-300" />
            Target agents
          </h2>
          <span className="text-xs text-gray-400">{targets.length} of {job.total_agents}</span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
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
                  <td colSpan={7} className="px-4 py-12 text-center text-gray-400">
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
                      setScheduleValue(`${dt.getFullYear()}-${pad(dt.getMonth() + 1)}-${pad(dt.getDate())}T${pad(dt.getHours())}:${pad(dt.getMinutes())}`);
                    }}
                    busy={actionBusy === `reboot-${t.agent_id}` || actionBusy === `schedule-${t.agent_id}`}
                    scheduleOpen={scheduleOpen === t.agent_id}
                    scheduleValue={scheduleValue}
                    onScheduleChange={setScheduleValue}
                    onScheduleSubmit={() => void doScheduleReboot(t.agent_id)}
                    onScheduleCancel={() => { setScheduleOpen(null); setScheduleValue(''); }}
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
