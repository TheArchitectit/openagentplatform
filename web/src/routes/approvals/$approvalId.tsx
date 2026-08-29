// HITL Approval Detail (hitl-approval spec R6.2, R6.3) — one approval's
// full payload, requesting agent, and decision history, with approve /
// reject actions and a comment / reason field. Deep-linked from the
// notification emails as {base}/approvals/{id}, so the path shape here is
// fixed by ApprovalNotifier.

import { createFileRoute, Link } from '@tanstack/react-router';
import { useCallback, useEffect, useState } from 'react';
import {
  ArrowLeft,
  Bot,
  CheckCircle2,
  ClipboardCheck,
  Clock,
  Loader2,
  MessageSquare,
  XCircle,
} from 'lucide-react';
import { fetchApprovalDetail } from '@/lib/useApprovals';
import { apiFetch, ApiError } from '@/lib/api';
import { urgencyBadgeClasses } from './index';
import type { ApprovalDetail } from '@/lib/useApprovals_types';
import { useOrg } from '@/lib/org';

export const Route = createFileRoute('/approvals/$approvalId')({
  component: ApprovalDetailPage,
});

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

function ApprovalDetailPage() {
  const { approvalId } = Route.useParams();
  const { role } = useOrg();
  // Mirrors the backend gate on the decision endpoints (admin, technician).
  const canDecide = role === 'admin' || role === 'technician';

  const [detail, setDetail] = useState<ApprovalDetail | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  // R6.3: one shared note field — becomes "comment" on approve, "reason"
  // on reject (the reject endpoint requires it, the approve one does not).
  const [note, setNote] = useState('');

  const load = useCallback(async () => {
    try {
      const d = await fetchApprovalDetail(approvalId);
      setDetail(d);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setError('Approval not found.');
      } else {
        setError(err instanceof Error ? err.message : 'Failed to load approval');
      }
    } finally {
      setIsLoading(false);
    }
  }, [approvalId]);

  useEffect(() => {
    setIsLoading(true);
    void load();
  }, [load]);

  const decide = async (decision: 'approve' | 'reject') => {
    if (decision === 'reject' && !note.trim()) return;
    setIsSubmitting(true);
    try {
      await apiFetch(
        `/a2a/approvals/${encodeURIComponent(approvalId)}/${decision}`,
        decision === 'approve' ? { method: 'POST', json: { comment: note } } : { method: 'POST', json: { reason: note.trim() } }
      );
      setNote('');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : `${decision} failed`);
    } finally {
      setIsSubmitting(false);
    }
  };

  if (isLoading) {
    return (
      <div
        className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400"
        role="status"
        aria-live="polite"
      >
        Loading approval…
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="space-y-4">
        <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error ?? 'Approval could not be loaded.'}
        </div>
        <Link to="/approvals" className="text-sm text-blue-400 hover:text-blue-300 inline-flex items-center gap-1">
          <ArrowLeft className="h-3.5 w-3.5" /> Back to queue
        </Link>
      </div>
    );
  }

  // The engine only persists pending/approved/rejected/expired — an
  // escalation re-arms the same request to pending with a bumped
  // escalation_depth (a2a/hitl/escalation.go), so pending is the sole
  // actionable state.
  const actionable = detail.status === 'pending';

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/approvals"
            className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            aria-label="Back to approval queue"
          >
            <ArrowLeft className="h-4 w-4 text-gray-300" />
          </Link>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-bold text-white flex items-center gap-2">
                <ClipboardCheck className="h-5 w-5 text-gray-300" aria-hidden="true" />
                {detail.action_type}
              </h1>
              <span
                className={`inline-flex items-center text-xs px-2 py-0.5 rounded-full border font-semibold uppercase tracking-wider ${urgencyBadgeClasses(detail.urgency)}`}
              >
                {detail.urgency}
              </span>
            </div>
            <p className="font-mono text-gray-400 text-xs mt-0.5">{detail.id}</p>
          </div>
        </div>
        {error && (
          <div role="alert" className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
            {error}
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Left: request + payload */}
        <div className="lg:col-span-2 space-y-5">
          {/* Summary */}
          <section className="rounded-xl border border-slate-800 bg-slate-900 p-4" aria-label="Request summary">
            <dl className="grid grid-cols-2 sm:grid-cols-3 gap-y-3 gap-x-4 text-sm">
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Status</dt>
                <dd className="mt-1">
                  <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded border bg-slate-800 text-gray-200">
                    {detail.status}
                  </span>
                </dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Requested</dt>
                <dd className="mt-1 text-gray-200">{formatTimestamp(detail.created_at)}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Expires</dt>
                <dd className="mt-1 text-gray-200 flex items-center gap-1">
                  <Clock className="h-3 w-3 text-gray-500" aria-hidden="true" />
                  {formatTimestamp(detail.expires_at)}
                </dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Requested by</dt>
                <dd className="mt-1 text-gray-200 flex items-center gap-1.5">
                  <Bot className="h-3.5 w-3.5 text-gray-500" aria-hidden="true" />
                  <span className="font-mono text-xs">{detail.requester_agent_id}</span>
                </dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Task</dt>
                <dd className="mt-1">
                  {detail.task_id ? (
                    <Link
                      to="/a2a/tasks/$taskId"
                      params={{ taskId: detail.task_id }}
                      className="font-mono text-xs text-blue-400 hover:text-blue-300"
                    >
                      {detail.task_id}
                    </Link>
                  ) : (
                    <span className="text-gray-500">—</span>
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-gray-500">Escalations</dt>
                <dd className="mt-1 text-gray-200">
                  {detail.escalation_depth}
                  {detail.escalated_from && (
                    <span className="text-gray-500">
                      {' '}
                      (from{' '}
                      <Link
                        to="/approvals/$approvalId"
                        params={{ approvalId: detail.escalated_from }}
                        className="font-mono text-xs text-blue-400 hover:text-blue-300"
                      >
                        {detail.escalated_from.slice(0, 8)}
                      </Link>
                      )
                    </span>
                  )}
                </dd>
              </div>
            </dl>
            {detail.decided_by && (
              <div className="mt-3 rounded-md bg-slate-800/60 border border-slate-700 px-3 py-2 text-xs text-gray-300">
                <span className="font-semibold text-gray-200">{detail.decided_by}</span>{' '}
                {detail.status === 'approved' ? 'approved' : detail.status === 'rejected' ? 'rejected' : 'decided'}{' '}
                {detail.decided_at && <>at {formatTimestamp(detail.decided_at)}</>}
                {detail.decision_note && <> — “{detail.decision_note}”</>}
              </div>
            )}
          </section>

          {/* Payload (R6.2: full payload) */}
          <section className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden" aria-label="Request payload">
            <h2 className="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-gray-400 bg-slate-800 border-b border-slate-800">
              Payload
            </h2>
            <pre className="p-4 text-xs text-gray-300 font-mono overflow-x-auto max-h-[28rem] overflow-y-auto">
              {JSON.stringify(detail.payload ?? {}, null, 2)}
            </pre>
          </section>
        </div>

        {/* Right: decision + history */}
        <div className="space-y-5">
          {canDecide && actionable && (
            <section className="rounded-xl border border-slate-800 bg-slate-900 p-4 space-y-3" aria-label="Decision">
              <h2 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
                Record your decision
              </h2>
              <div>
                <label htmlFor="approval-note" className="block text-xs text-gray-400 mb-1">
                  Comment — required for rejection; sent to the requester agent
                </label>
                <textarea
                  id="approval-note"
                  rows={3}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="Why are you approving or rejecting this?"
                  className="w-full rounded-md bg-slate-950 border border-slate-700 px-2.5 py-2 text-sm text-white placeholder:text-gray-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 resize-y"
                />
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={isSubmitting}
                  onClick={() => void decide('approve')}
                  className="flex-1 inline-flex items-center justify-center gap-1.5 h-9 rounded-md bg-green-600 hover:bg-green-500 disabled:opacity-50 text-white text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-green-500 transition-colors"
                >
                  {isSubmitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                  Approve
                </button>
                <button
                  type="button"
                  disabled={isSubmitting || !note.trim()}
                  onClick={() => void decide('reject')}
                  className="flex-1 inline-flex items-center justify-center gap-1.5 h-9 rounded-md bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white text-sm font-medium focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 transition-colors"
                >
                  <XCircle className="h-4 w-4" />
                  Reject
                </button>
              </div>
            </section>
          )}

          {/* History timeline (R6.2) */}
          <section className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden" aria-label="Decision history">
            <h2 className="px-4 py-2.5 text-xs font-semibold uppercase tracking-wider text-gray-400 bg-slate-800 border-b border-slate-800">
              History
            </h2>
            {detail.history.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-gray-500" role="status">
                No recorded events yet.
              </p>
            ) : (
              <ol className="divide-y divide-slate-800">
                {detail.history.map((h, i) => (
                  <li key={`${h.approval_id}-${h.action}-${i}`} className="px-4 py-2.5">
                    <div className="flex items-center gap-2 text-xs">
                      <MessageSquare className="h-3 w-3 text-gray-500 shrink-0" aria-hidden="true" />
                      <span className="font-medium text-gray-200">{h.action}</span>
                      <span className="text-gray-500">by {h.actor}</span>
                      <span className="ml-auto text-gray-500 shrink-0">{formatTimestamp(h.timestamp)}</span>
                    </div>
                    {h.reason && (
                      <p className="mt-1 pl-5 text-xs text-gray-400">{h.reason}</p>
                    )}
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
