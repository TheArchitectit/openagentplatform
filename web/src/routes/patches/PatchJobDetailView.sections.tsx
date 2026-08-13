// Sub-sections of PatchJobDetailView (approvals list + target agents table).
// Extracted from PatchJobDetailView.tsx to keep that file under the size gate.

import { CircleCheck, CircleX, AlertCircle, Server, ShieldCheck, Check, X, RotateCcw, Loader2 } from 'lucide-react';
import type { ComponentType } from 'react';
import { TargetRow } from './patch_job_components';
import { formatTime } from './patch_job_helpers';
import type { PatchJob, PatchTarget, PatchApproval } from '@/lib/usePatches';

export interface ApprovalsSectionProps {
  approvals: PatchApproval[];
  state: NonNullable<PatchJob['status']>;
}

export function ApprovalsSection({ approvals, state }: ApprovalsSectionProps) {
  return (
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
            const Icon: ComponentType<{ className?: string }> =
              decision === 'approved' ? CircleCheck : decision === 'rejected' ? CircleX : AlertCircle;
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
  );
}

export interface TargetAgentsTableProps {
  targets: PatchTarget[];
  jobTotalAgents: number;
  isTerminal: boolean;
  doRebootNow: (agentId: string) => void;
  makeScheduleClick: (agentId: string) => () => void;
  actionBusy: string | null;
  scheduleOpen: string | null;
  scheduleValue: string;
  setScheduleOpen: (v: string | null) => void;
  setScheduleValue: (v: string) => void;
  doScheduleReboot: (agentId: string) => void;
}

export function TargetAgentsTable({
  targets,
  jobTotalAgents,
  isTerminal,
  doRebootNow,
  makeScheduleClick,
  actionBusy,
  scheduleOpen,
  scheduleValue,
  setScheduleOpen,
  setScheduleValue,
  doScheduleReboot,
}: TargetAgentsTableProps) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-white flex items-center gap-2">
          <Server className="h-4 w-4 text-gray-300" />
          Target agents
        </h2>
        <span className="text-xs text-gray-400">{targets.length} of {jobTotalAgents}</span>
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
                  onScheduleClick={makeScheduleClick(t.agent_id)}
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
  );
}

export interface ActionBarProps {
  state: NonNullable<PatchJob['status']>;
  actionBusy: string | null;
  doAction: (a: 'approve' | 'reject' | 'cancel' | 'rollback' | 'retry') => void;
}

export function ActionBar({ state, actionBusy, doAction }: ActionBarProps) {
  return (
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
  );
}
