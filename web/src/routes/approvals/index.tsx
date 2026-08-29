// HITL Approval Queue (hitl-approval spec R6.1, R6.4, R6.5) — the list of
// approval requests a human has to act on, with urgency badges, a status
// filter, batch approve/reject, and live updates over SSE.
//
// Permission posture mirrors the backend exactly: reads need only an
// authenticated org member (so no page-level gate), while the decision
// endpoints require the admin or technician role (internal/api/
// hitl_approvals.go RequireRole). The decide buttons are shown only to
// those roles; everyone else can still browse the queue.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useMemo, useState } from 'react';
import {
  ClipboardCheck,
  RefreshCw,
  Radio,
  CircleDot,
  CheckCircle2,
  XCircle,
  Clock,
  TrendingUp,
  Loader2,
} from 'lucide-react';
import { useApprovals } from '@/lib/useApprovals';
import type { ApprovalStatus } from '@/lib/useApprovals_types';
import { useOrg } from '@/lib/org';

export const Route = createFileRoute('/approvals/')({
  component: ApprovalQueuePage,
});

// The engine re-arms an escalated request to `pending` with a bumped
// escalation_depth (a2a/hitl/escalation.go), so there is no separate
// "escalated" bucket to filter — escalated items surface in Pending with
// an escalation badge.
const FILTERS: { value: ApprovalStatus; label: string; icon: React.ReactNode }[] = [
  { value: 'pending', label: 'Pending', icon: <CircleDot className="h-3.5 w-3.5" /> },
  { value: 'approved', label: 'Approved', icon: <CheckCircle2 className="h-3.5 w-3.5" /> },
  { value: 'rejected', label: 'Rejected', icon: <XCircle className="h-3.5 w-3.5" /> },
  { value: 'expired', label: 'Expired', icon: <Clock className="h-3.5 w-3.5" /> },
];

// Urgency tones mirror the alert severity convention (critical red, high
// orange, medium yellow, low blue) so operators read both the same way.
export function urgencyBadgeClasses(urgency: string): string {
  switch (urgency) {
    case 'critical':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'high':
      return 'bg-orange-500/10 text-orange-400 border-orange-500/20';
    case 'medium':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    case 'low':
      return 'bg-blue-500/10 text-blue-400 border-blue-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function statusBadgeClasses(status: ApprovalStatus): string {
  switch (status) {
    case 'pending':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    case 'escalated':
      return 'bg-purple-500/10 text-purple-400 border-purple-500/20';
    case 'approved':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'rejected':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'expired':
      return 'bg-slate-500/10 text-gray-400 border-slate-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function shortId(id: string): string {
  if (!id) return '—';
  if (id.length <= 12) return id;
  return id.slice(0, 8);
}

/** Human-readable time until expiry (pending rows) — e.g. "in 2h 14m". */
function formatCountdown(expiresAt: string): string {
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms)) return '—';
  if (ms <= 0) return 'overdue';
  const min = Math.floor(ms / 60000);
  if (min < 60) return `in ${min}m`;
  const hr = Math.floor(min / 60);
  return `in ${hr}h ${min % 60}m`;
}

function ApprovalQueuePage() {
  const {
    approvals,
    status,
    setStatus,
    isLoading,
    isMutating,
    error,
    sseConnected,
    approve,
    reject,
    approveBatch,
    rejectBatch,
    refresh,
  } = useApprovals('pending');
  const { role } = useOrg();
  // Mirrors the backend gate on /approve + /reject (admin, technician).
  const canDecide = role === 'admin' || role === 'technician';

  const [selected, setSelected] = useState<Set<string>>(new Set());
  // R6.4 batch reject needs a reason per request; the batch path asks for
  // one shared reason in an inline panel before submitting.
  const [batchMode, setBatchMode] = useState<'none' | 'reject'>('none');
  const [batchReason, setBatchReason] = useState('');

  const pendingIds = useMemo(
    () => approvals.filter((a) => a.status === 'pending').map((a) => a.id),
    [approvals]
  );
  const allSelected = pendingIds.length > 0 && pendingIds.every((id) => selected.has(id));

  const toggle = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(pendingIds));
  };

  const clearBatch = () => {
    setSelected(new Set());
    setBatchMode('none');
    setBatchReason('');
  };

  const selectedIds = [...selected];

  // Columns: base 7 + selection + decide when the row controls are shown.
  const colCount = canDecide && status === 'pending' ? 9 : 7;

  const handleBatchApprove = async () => {
    await approveBatch(selectedIds);
    clearBatch();
  };

  const handleBatchReject = async () => {
    if (!batchReason.trim()) return;
    await rejectBatch(selectedIds, batchReason.trim());
    clearBatch();
  };

  const handleApprove = async (id: string) => {
    await approve(id);
    setSelected((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  };

  // R6.3 inline reject: clicking Reject swaps the row's buttons for a
  // reason input + confirm, so the operator never leaves the queue.
  const [rejectingId, setRejectingId] = useState<string | null>(null);
  const [rejectReason, setRejectReason] = useState('');

  const handleRejectConfirm = async (id: string) => {
    if (!rejectReason.trim()) return;
    await reject(id, rejectReason.trim());
    setRejectingId(null);
    setRejectReason('');
    setSelected((prev) => {
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  };

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div
            className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center"
            aria-hidden="true"
          >
            <ClipboardCheck className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Approvals</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Human decisions agents are waiting on
            </p>
            {!sseConnected && (
              <p className="text-yellow-400 text-xs mt-1" role="status">
                Live updates paused — reconnecting…
              </p>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void refresh()}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* Status filter */}
      <nav className="flex items-center gap-1 overflow-x-auto" role="tablist" aria-label="Status filters">
        {FILTERS.map((f) => {
          const isActive = status === f.value;
          return (
            <button
              key={f.value}
              type="button"
              role="tab"
              aria-selected={isActive}
              onClick={() => {
                setStatus(f.value);
                clearBatch();
              }}
              className={`inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-xs font-medium border transition-colors ${
                isActive
                  ? 'bg-blue-600 text-white border-blue-600'
                  : 'bg-slate-800 text-gray-300 border-slate-700 hover:bg-slate-700 hover:text-white'
              }`}
            >
              {f.icon}
              {f.label}
            </button>
          );
        })}
      </nav>

      {/* Error */}
      {error && (
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error.message}
        </div>
      )}

      {/* Batch actions */}
      {canDecide && selected.size > 0 && (
        <div className="rounded-lg border border-slate-700 bg-slate-800/60 px-3 py-2.5 flex items-center gap-3 flex-wrap">
          <span className="text-sm text-gray-200">
            {selected.size} selected
          </span>
          {batchMode === 'none' && (
            <>
              <button
                type="button"
                disabled={isMutating}
                onClick={() => void handleBatchApprove()}
                className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-green-600 hover:bg-green-500 disabled:opacity-50 text-white text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-green-500 transition-colors"
              >
                {isMutating ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CheckCircle2 className="h-3.5 w-3.5" />}
                Approve all
              </button>
              <button
                type="button"
                disabled={isMutating}
                onClick={() => setBatchMode('reject')}
                className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
              >
                <XCircle className="h-3.5 w-3.5" />
                Reject all
              </button>
            </>
          )}
          {batchMode === 'reject' && (
            <form
              className="flex items-center gap-2 flex-1 min-w-[16rem]"
              onSubmit={(e) => {
                e.preventDefault();
                void handleBatchReject();
              }}
            >
              <label htmlFor="batch-reason" className="text-xs text-gray-400">
                Reason
              </label>
              <input
                id="batch-reason"
                type="text"
                required
                value={batchReason}
                onChange={(e) => setBatchReason(e.target.value)}
                placeholder="Why are these being rejected?"
                className="flex-1 min-w-0 h-8 rounded-md bg-slate-900 border border-slate-700 px-2.5 text-sm text-white placeholder:text-gray-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500"
              />
              <button
                type="submit"
                disabled={isMutating || !batchReason.trim()}
                className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
              >
                {isMutating ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
                Reject {selected.size}
              </button>
              <button
                type="button"
                onClick={() => setBatchMode('none')}
                className="px-3 h-8 rounded-md bg-slate-700 hover:bg-slate-600 text-sm text-gray-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
              >
                Cancel
              </button>
            </form>
          )}
          <button
            type="button"
            onClick={clearBatch}
            className="ml-auto text-xs text-gray-400 hover:text-gray-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 rounded"
          >
            Clear selection
          </button>
        </div>
      )}

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                {canDecide && status === 'pending' && (
                  <th className="px-4 py-2.5 w-10">
                    <input
                      type="checkbox"
                      aria-label="Select all pending approvals"
                      checked={allSelected}
                      onChange={toggleAll}
                      className="h-4 w-4 accent-blue-600"
                    />
                  </th>
                )}
                <th className="px-4 py-2.5 font-medium">Approval</th>
                <th className="px-4 py-2.5 font-medium">Action</th>
                <th className="px-4 py-2.5 font-medium">Requester</th>
                <th className="px-4 py-2.5 font-medium">Urgency</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Expires</th>
                {canDecide && status === 'pending' && (
                  <th className="px-4 py-2.5 font-medium text-right">Decide</th>
                )}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && approvals.length === 0 ? (
                <tr>
                  <td colSpan={colCount} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading approvals…
                  </td>
                </tr>
              ) : approvals.length === 0 ? (
                <tr>
                  <td colSpan={colCount} className="px-4 py-12 text-center text-gray-400" role="status">
                    No {status} approvals.
                  </td>
                </tr>
              ) : (
                approvals.map((a) => (
                  <tr key={a.id} className="hover:bg-slate-800/40 transition-colors">
                    {canDecide && status === 'pending' && (
                      <td className="px-4 py-2.5">
                        <input
                          type="checkbox"
                          aria-label={`Select approval ${a.id}`}
                          checked={selected.has(a.id)}
                          disabled={a.status !== 'pending'}
                          onChange={() => toggle(a.id)}
                          className="h-4 w-4 accent-blue-600"
                        />
                      </td>
                    )}
                    <td className="px-4 py-2.5">
                      <Link
                        to="/approvals/$approvalId"
                        params={{ approvalId: a.id }}
                        className="font-mono text-blue-400 hover:text-blue-300 text-xs"
                      >
                        {shortId(a.id)}
                      </Link>
                    </td>
                    <td className="px-4 py-2.5 text-gray-200 text-xs font-medium">
                      {a.action_type}
                      {a.escalation_depth > 0 && (
                        <span
                          className="ml-1.5 inline-flex items-center gap-0.5 text-[10px] px-1.5 py-0.5 rounded-full border bg-purple-500/10 text-purple-400 border-purple-500/20 font-semibold uppercase tracking-wider"
                          title={`Escalated ${a.escalation_depth} time${a.escalation_depth === 1 ? '' : 's'} — now with the next approval group`}
                        >
                          <TrendingUp className="h-2.5 w-2.5" aria-hidden="true" />
                          escalated ×{a.escalation_depth}
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs font-mono">
                      {a.requester_agent_id}
                    </td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`inline-flex items-center text-xs px-2 py-0.5 rounded-full border font-semibold uppercase tracking-wider ${urgencyBadgeClasses(a.urgency)}`}
                      >
                        {a.urgency}
                      </span>
                    </td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded border ${statusBadgeClasses(a.status)}`}
                      >
                        {a.status}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">
                      {a.status === 'pending'
                        ? formatCountdown(a.expires_at)
                        : new Date(a.expires_at).toLocaleString()}
                    </td>
                    {canDecide && status === 'pending' && (
                      <td className="px-4 py-2.5 text-right">
                        {rejectingId === a.id ? (
                          <form
                            className="flex items-center gap-1.5 justify-end flex-wrap"
                            onSubmit={(e) => {
                              e.preventDefault();
                              void handleRejectConfirm(a.id);
                            }}
                          >
                            <label htmlFor={`reject-reason-${a.id}`} className="sr-only">
                              Reason for rejecting approval {a.id}
                            </label>
                            <input
                              id={`reject-reason-${a.id}`}
                              type="text"
                              autoFocus
                              required
                              value={rejectReason}
                              onChange={(e) => setRejectReason(e.target.value)}
                              placeholder="Reason (required)"
                              className="h-7 w-40 rounded-md bg-slate-950 border border-slate-700 px-2 text-xs text-white placeholder:text-gray-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500"
                            />
                            <button
                              type="submit"
                              disabled={isMutating || !rejectReason.trim()}
                              className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
                            >
                              {isMutating ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <XCircle className="h-3.5 w-3.5" />}
                              Confirm
                            </button>
                            <button
                              type="button"
                              onClick={() => {
                                setRejectingId(null);
                                setRejectReason('');
                              }}
                              className="h-7 px-2 rounded-md bg-slate-700 hover:bg-slate-600 text-gray-200 text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
                            >
                              Cancel
                            </button>
                          </form>
                        ) : (
                          <div className="inline-flex items-center gap-1.5">
                            <button
                              type="button"
                              disabled={isMutating}
                              onClick={() => void handleApprove(a.id)}
                              className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-green-600 hover:bg-green-500 disabled:opacity-50 text-white text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-green-500 transition-colors"
                              aria-label={`Approve ${a.id}`}
                            >
                              <CheckCircle2 className="h-3.5 w-3.5" />
                              Approve
                            </button>
                            <button
                              type="button"
                              disabled={isMutating}
                              onClick={() => {
                                setRejectingId(a.id);
                                setRejectReason('');
                              }}
                              className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-xs font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
                              aria-label={`Reject ${a.id}`}
                            >
                              <XCircle className="h-3.5 w-3.5" />
                              Reject
                            </button>
                          </div>
                        )}
                      </td>
                    )}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <p className="text-xs text-gray-500 flex items-center gap-1.5">
        <Radio className="h-3 w-3" aria-hidden="true" />
        Live updates: every create/decision/expiry refreshes this list automatically.
      </p>
    </div>
  );
}
