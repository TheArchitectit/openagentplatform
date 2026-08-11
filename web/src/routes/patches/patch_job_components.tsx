import { useCallback, useMemo, useState } from 'react';
import {
  Check,
  X,
  CircleCheck,
  CircleX,
  CirclePlay,
  Loader2,
  Clock,
  Power,
  CalendarClock,
  Server,
  Activity,
  AlertCircle,
  ListChecks,
  ChevronRight,
} from 'lucide-react';
import {
  usePatches,
  type PatchTarget,
  type PatchReboot,
  type PatchJobStatus,
  type PatchApproval,
  type RebootStatus,
  type InstallStatus,
  type DeploymentStage,
} from '@/lib/usePatches';
import { SeverityBadge } from '@/components/severity-badge';
import {
  STATUS_META,
  INSTALL_META,
  REBOOT_META,
  ROLLOUT_STAGES,
  formatTime,
  computeProgress,
  findActiveStageIndex,
  defaultScheduleValue,
} from './patch_job_helpers';

// ---------------------------------------------------------------------------
// Target row
// ---------------------------------------------------------------------------

export function TargetRow({
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
          <span className="text-white">{t.hostname || t.agent_id}</span>
          {t.hostname && (
            <span className="text-[10px] text-gray-400 font-mono">{t.agent_id}</span>
          )}
        </div>
      </td>
      <td className="px-4 py-3 text-gray-300 text-xs">
        {t.os || '—'}
        {t.os_version && (
          <span className="block text-gray-400">{t.os_version}</span>
        )}
      </td>
      <td className="px-4 py-3 text-gray-300 text-xs font-mono">
        {t.current_version || '—'}
      </td>
      <td className="px-4 py-3 text-gray-300 text-xs font-mono">
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
          <p className="text-xs text-red-400 mt-1 max-w-[10rem] truncate" title={t.error_message}>
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
          <p className="text-xs text-gray-400 mt-1">{formatTime(t.scheduled_reboot_at)}</p>
        )}
      </td>
      <td className="px-4 py-3 text-right">
        {scheduleOpen ? (
          <div className="inline-flex items-center gap-1.5">
            <input
              type="datetime-local"
              value={scheduleValue}
              onChange={(e) => onScheduleChange(e.target.value)}
              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-xs text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
            />
            <button
              type="button"
              onClick={onScheduleSubmit}
              disabled={busy || !scheduleValue}
              className="h-8 px-2 rounded-md bg-blue-600 hover:bg-blue-600 border border-blue-500 text-xs text-white disabled:opacity-50"
              title="Confirm schedule"
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Check className="h-3.5 w-3.5" />}
            </button>
            <button
              type="button"
              onClick={onScheduleCancel}
              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-xs hover:bg-slate-700"
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
              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-yellow-500/10 border border-yellow-800 text-yellow-400 hover:bg-yellow-500/20 disabled:opacity-50 transition-colors"
              title="Reboot now"
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Power className="h-3.5 w-3.5" />}
              <span>Now</span>
            </button>
            <button
              type="button"
              onClick={onScheduleClick}
              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 transition-colors"
              title="Schedule reboot"
            >
              <CalendarClock className="h-3.5 w-3.5" />
              <span>Schedule</span>
            </button>
          </div>
        ) : (
          <span className="text-xs text-gray-400">—</span>
        )}
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Reboot coordination panel
// ---------------------------------------------------------------------------

export function RebootCoordinationPanel({
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
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-white flex items-center gap-2">
          <Power className="h-4 w-4 text-gray-300" />
          Reboot coordination
        </h2>
        <span className="text-xs text-gray-400">
          {pendingCount} pending reboot{pendingCount === 1 ? '' : 's'}
        </span>
      </div>
      <div className="p-5">
        {grouped.length === 0 ? (
          <div className="text-center text-gray-400 py-6">
            <ListChecks className="inline h-5 w-5 mb-1" />
            <p className="text-sm">No pending reboots.</p>
            <p className="text-xs text-gray-400 mt-1">
              Reboots appear here when the rollout requires a restart.
            </p>
          </div>
        ) : (
          <div className="space-y-4">
            {grouped.map(({ stageIndex, items }) => (
              <div
                key={stageIndex}
                className="rounded-md border border-slate-800 bg-slate-800"
              >
                <div className="px-4 py-2 border-b border-slate-800 flex items-center gap-2">
                  <span
                    className={
                      'inline-flex items-center gap-1.5 text-xs font-medium ' +
                      (stageIndex === -1 ? 'text-gray-300' : 'text-blue-400')
                    }
                  >
                    <Clock className="h-3.5 w-3.5" />
                    {stageIndex === -1 ? 'Unscheduled' : `Stage ${stageIndex + 1}`}
                  </span>
                  {items[0]?.scheduled_at && (
                    <span className="text-xs text-gray-400">
                      · target {formatTime(items[0].scheduled_at)}
                    </span>
                  )}
                  <span className="ml-auto text-xs text-gray-400 tabular-nums">
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
                        <Server className="h-4 w-4 text-gray-400 shrink-0" />
                        <div className="flex-1 min-w-0">
                          <p className="text-white truncate">{r.hostname || r.agent_id}</p>
                          <p className="text-xs text-gray-400">
                            {r.status === 'scheduled' && r.scheduled_at
                              ? `Scheduled for ${formatTime(r.scheduled_at)}`
                              : 'Awaiting reboot'}
                          </p>
                          {r.last_error && (
                            <p className="text-xs text-red-400 mt-0.5 truncate">
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
                              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-xs text-white focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
                            />
                            <button
                              type="button"
                              onClick={() => onScheduleSubmit(r.agent_id)}
                              disabled={actionBusy !== null || !scheduleValue}
                              className="h-8 px-2 rounded-md bg-blue-600 hover:bg-blue-600 border border-blue-500 text-xs text-white disabled:opacity-50"
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
                              className="h-8 px-2 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-xs hover:bg-slate-700"
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
                              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-yellow-500/10 border border-yellow-800 text-yellow-400 hover:bg-yellow-500/20 disabled:opacity-50 transition-colors"
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
                              className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 transition-colors"
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
            <div className="rounded-md border border-slate-800 bg-slate-800 p-4">
              <h3 className="text-xs text-gray-400 uppercase tracking-wider mb-3 flex items-center gap-1.5">
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
                            'absolute -left-[2px] mt-1.5 h-3 w-3 rounded-full border-2 border-surface-primary ' +
                            (isUnscheduled ? 'bg-slate-700' : 'bg-blue-600')
                          }
                        />
                        <div className="pl-5">
                          <p className="text-sm text-white">
                            {isUnscheduled ? 'Unscheduled bucket' : `Stage ${stageIndex + 1}`}
                            <ChevronRight className="inline h-3.5 w-3.5 mx-1 text-gray-400" />
                            <span className="text-gray-300 tabular-nums">
                              {items.length} reboot{items.length === 1 ? '' : 's'}
                            </span>
                          </p>
                          {items[0]?.scheduled_at && (
                            <p className="text-xs text-gray-400">
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
