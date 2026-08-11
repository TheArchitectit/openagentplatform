import { useCallback, useState } from 'react';
import {
  Check,
  X,
  Loader2,
  CirclePlay,
  CircleX,
  CircleAlert,
  CircleCheck,
  Clock,
  Package,
} from 'lucide-react';
import { usePatches, type PatchJob } from '@/lib/usePatches';
import { SeverityBadge } from '@/components/severity-badge';
import { STATUS_META, formatRelative } from './patch_list_helpers';

// ---------------------------------------------------------------------------
// Summary tile
// ---------------------------------------------------------------------------

export function SummaryTile({
  label,
  value,
  tone,
  icon: Icon,
}: {
  label: string;
  value: number;
  tone: 'success' | 'danger' | 'info' | 'neutral';
  icon: typeof Package;
}) {
  const toneClasses: Record<typeof tone, string> = {
    success: 'text-green-400',
    danger: 'text-red-400',
    info: 'text-blue-400',
    neutral: 'text-gray-300',
  };
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-400 uppercase tracking-wider">{label}</span>
        <Icon className={'h-3.5 w-3.5 ' + toneClasses[tone]} />
      </div>
      <p className={'text-2xl font-semibold mt-1.5 tabular-nums ' + toneClasses[tone]}>
        {value}
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Job row
// ---------------------------------------------------------------------------

export interface JobRowProps {
  job: PatchJob;
  isSelected: boolean;
  onToggleSelect: () => void;
  onOpen: () => void;
  now: number;
}

export function JobRow({ job: j, isSelected, onToggleSelect, onOpen, now }: JobRowProps) {
  const meta = STATUS_META[j.status] ?? STATUS_META.pending_approval;
  const isPending = j.status === 'pending_approval';
  const isSelectable = isPending;
  const progress = computeProgress(j);
  const progressTone =
    j.status === 'completed'
      ? 'bg-green-500'
      : j.status === 'failed'
      ? 'bg-red-500'
      : j.status === 'cancelled'
      ? 'bg-slate-500'
      : 'bg-blue-600';

  return (
    <tr
      onClick={onOpen}
      className={
        'cursor-pointer transition-colors ' +
        (isSelected ? 'bg-blue-600/5' : 'hover:bg-slate-800/40')
      }
    >
      <td className="px-3 py-3" onClick={(e) => e.stopPropagation()}>
        <input
          type="checkbox"
          aria-label={`Select job ${j.id}`}
          checked={isSelected}
          onChange={onToggleSelect}
          disabled={!isSelectable}
          className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40 disabled:opacity-30"
        />
      </td>
      <td className="px-3 py-3">
        <div className="flex flex-col">
          <span className="text-white font-medium truncate max-w-md">
            {j.name || j.id}
          </span>
          {j.description && (
            <span className="text-xs text-gray-400 truncate max-w-md">
              {j.description}
            </span>
          )}
          <span className="text-[10px] text-gray-400 mt-0.5 font-mono">
            {j.id}
            {j.created_by ? ` · by ${j.created_by}` : ''}
            {' · '}
            {formatRelative(j.created_at, now)}
          </span>
        </div>
      </td>
      <td className="px-3 py-3 text-gray-300">
        <span className="text-xs">
          {j.patch_count > 0 ? `${j.patch_count} patch${j.patch_count === 1 ? '' : 'es'}` : '—'}
        </span>
      </td>
      <td className="px-3 py-3">
        <SeverityBadge severity={j.severity} />
      </td>
      <td className="px-3 py-3 text-right tabular-nums text-gray-300">
        {j.total_agents > 0 ? (
          <>
            <span className="text-white">{j.completed_agents}</span>
            <span className="text-gray-400"> / {j.total_agents}</span>
          </>
        ) : (
          <span className="text-gray-400">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        <span
          className={
            'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
            meta.classes
          }
        >
          {meta.label}
        </span>
      </td>
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <div className="flex-1 h-1.5 rounded-full bg-slate-800 overflow-hidden">
            <div
              className={'h-full transition-all ' + progressTone}
              style={{ width: `${Math.max(0, Math.min(100, progress))}%` }}
            />
          </div>
          <span className="text-xs text-gray-300 tabular-nums w-9 text-right">
            {Math.round(progress)}%
          </span>
        </div>
      </td>
      <td className="px-3 py-3 text-right" onClick={(e) => e.stopPropagation()}>
        <RowActions job={j} />
      </td>
    </tr>
  );
}

// ---------------------------------------------------------------------------
// Progress computation
// ---------------------------------------------------------------------------

export function computeProgress(j: PatchJob): number {
  if (typeof j.progress_pct === 'number') return j.progress_pct;
  if (j.total_agents <= 0) {
    if (j.status === 'completed') return 100;
    if (j.status === 'failed') return 100;
    if (j.status === 'cancelled' || j.status === 'rejected') return 0;
    return 0;
  }
  return Math.min(100, (j.completed_agents / j.total_agents) * 100);
}

// ---------------------------------------------------------------------------
// Row actions
// ---------------------------------------------------------------------------

export function RowActions({ job: j }: { job: PatchJob }) {
  const { approveJob, rejectJob, cancelJob } = usePatches();
  const [busy, setBusy] = useState<string | null>(null);

  const onApprove = useCallback(async () => {
    setBusy('approve');
    try {
      await approveJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [approveJob, j.id]);

  const onReject = useCallback(async () => {
    setBusy('reject');
    try {
      await rejectJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [rejectJob, j.id]);

  const onCancel = useCallback(async () => {
    setBusy('cancel');
    try {
      await cancelJob(j.id);
    } finally {
      setBusy(null);
    }
  }, [cancelJob, j.id]);

  if (j.status === 'pending_approval') {
    return (
      <div className="inline-flex items-center gap-1">
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onApprove()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-green-500/10 border border-green-800 text-green-400 hover:bg-green-500/20 disabled:opacity-50 transition-colors"
          title="Approve"
        >
          {busy === 'approve' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Check className="h-3.5 w-3.5" />
          )}
          <span>Approve</span>
        </button>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onReject()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-red-500/10 border border-red-800 text-red-400 hover:bg-red-500/20 disabled:opacity-50 transition-colors"
          title="Reject"
        >
          {busy === 'reject' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <X className="h-3.5 w-3.5" />
          )}
          <span>Reject</span>
        </button>
      </div>
    );
  }

  if (j.status === 'in_progress' || j.status === 'approved') {
    return (
      <div className="inline-flex items-center gap-1">
        <span className="inline-flex items-center gap-1 text-xs text-gray-400">
          <CirclePlay className="h-3.5 w-3.5" />
          <span>Running</span>
        </span>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => void onCancel()}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 disabled:opacity-50 transition-colors"
          title="Cancel job"
        >
          {busy === 'cancel' ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <CircleX className="h-3.5 w-3.5" />
          )}
          <span>Cancel</span>
        </button>
      </div>
    );
  }

  if (j.status === 'failed') {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-red-400">
        <CircleAlert className="h-3.5 w-3.5" />
        <span>Failed</span>
      </span>
    );
  }

  if (j.status === 'completed') {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-green-400">
        <CircleCheck className="h-3.5 w-3.5" />
        <span>Done</span>
      </span>
    );
  }

  return (
    <span className="inline-flex items-center gap-1 text-xs text-gray-400">
      <Clock className="h-3.5 w-3.5" />
      <span>No actions</span>
    </span>
  );
}
