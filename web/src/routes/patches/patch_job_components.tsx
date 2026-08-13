import { useCallback, useMemo, useState } from 'react';
import { Check, X, Power, Loader2, CalendarClock } from 'lucide-react';
import {
  type PatchTarget,
  type InstallStatus,
} from '@/lib/usePatches';
import { INSTALL_META, REBOOT_META, formatTime } from './patch_job_helpers';

export { RebootCoordinationPanel } from './RebootCoordinationPanel';

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
