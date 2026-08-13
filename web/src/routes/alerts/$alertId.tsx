// Alert detail page — deep view of a single alert.
//
// Layout:
//   • Header: title, severity badge, state badge, key timestamps.
//   • Action bar: Acknowledge / Snooze (duration picker) / Resolve / Close.
//   • Details card: check name, agent hostname, monospace output, metrics.
//   • State timeline, notification history, and related alerts live in
//     <AlertDetailHistory> (see alert_detail_components.tsx).
//   • State/effects live in useAlertDetail (see useAlertDetail.ts).

import { createFileRoute, Link } from '@tanstack/react-router';
import {
  ArrowLeft,
  Check,
  CheckCheck,
  Clock,
  X,
  Loader2,
} from 'lucide-react';
import { SeverityBadge } from '@/components/severity-badge';
import {
  StateBadge,
  SnoozeMenu,
  AlertDetailHistory,
} from './alert_detail_components';
import { useAlertDetail } from './useAlertDetail';
import { AlertDetailView } from './AlertDetailView';

export const Route = createFileRoute('/alerts/$alertId')({
  component: AlertDetailPage,
});

function AlertDetailPage() {
  const { alertId } = Route.useParams();
  const {
    alert,
    timeline,
    notifications,
    related,
    isLoading,
    error,
    actionBusy,
    snoozeOpen,
    copyOk,
    setSnoozeOpen,
    doAction,
    handleCopyId,
  } = useAlertDetail(alertId);

  if (isLoading && !alert) {
    return (
      <div className="text-center text-gray-400 py-24">
        <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
        Loading alert…
      </div>
    );
  }

  if (error && !alert) {
    return (
      <div className="space-y-4">
        <Link
          to="/alerts"
          className="inline-flex items-center gap-2 text-sm text-gray-300 hover:text-white"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to alerts</span>
        </Link>
        <div className="rounded-lg border border-red-800 bg-red-500/5 p-6 text-red-400">
          Failed to load alert: {error.message}
        </div>
      </div>
    );
  }

  if (!alert) return null;

  const state = (alert.state ?? 'open').toLowerCase();
  const isTerminal = state === 'resolved' || state === 'closed';

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between flex-wrap gap-3">
        <div className="flex items-start gap-3 min-w-0">
          <Link
            to="/alerts"
            className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center hover:bg-slate-700 transition-colors shrink-0"
            title="Back to alerts"
          >
            <ArrowLeft className="h-4 w-4 text-gray-300" />
          </Link>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <SeverityBadge severity={alert.severity} size="md" />
              <StateBadge state={alert.state} />
            </div>
            <h1 className="text-2xl font-bold text-white mt-2 break-words">
              {alert.title}
            </h1>
            {alert.message && (
              <p className="text-gray-300 mt-1 break-words">{alert.message}</p>
            )}
            <button
              type="button"
              onClick={() => void handleCopyId()}
              aria-label={`Copy alert ID ${alert.id} to clipboard`}
              className="mt-2 inline-flex items-center gap-1.5 text-xs text-gray-400 hover:text-gray-300 font-mono focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              <span>{alert.id}</span>
              {copyOk ? (
                <Check className="h-3 w-3 text-green-400" aria-hidden="true" />
              ) : null}
            </button>
          </div>
        </div>

        {/* Action bar */}
        <div className="flex items-center gap-2 flex-wrap" role="group" aria-label="Alert actions">
          {state === 'open' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('ack')}
              aria-label="Acknowledge this alert"
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-yellow-500/15 border border-yellow-800 text-yellow-400 text-sm hover:bg-yellow-500/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              {actionBusy === 'ack' ? (
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
              ) : (
                <Check className="h-4 w-4" aria-hidden="true" />
              )}
              <span>Acknowledge</span>
            </button>
          )}
          {!isTerminal && (
            <div className="relative">
              <button
                type="button"
                disabled={actionBusy !== null}
                onClick={() => setSnoozeOpen(!snoozeOpen)}
                aria-expanded={snoozeOpen}
                aria-haspopup="menu"
                aria-label="Snooze this alert"
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-white text-sm hover:bg-slate-700 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
              >
                <Clock className="h-4 w-4" aria-hidden="true" />
                <span>Snooze</span>
              </button>
              {snoozeOpen && (
                <SnoozeMenu
                  onPick={async (mins) => {
                    setSnoozeOpen(false);
                    await doAction({ snooze: mins });
                  }}
                  onClose={() => setSnoozeOpen(false)}
                />
              )}
            </div>
          )}
          {state !== 'resolved' && state !== 'closed' && (
            <button
              type="button"
              disabled={actionBusy !== null}
              onClick={() => void doAction('resolve')}
              aria-label="Resolve this alert"
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-green-500/15 border border-green-800 text-green-400 text-sm hover:bg-green-500/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              {actionBusy === 'resolve' ? (
                <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
              ) : (
                <CheckCheck className="h-4 w-4" aria-hidden="true" />
              )}
              <span>Resolve</span>
            </button>
          )}
          <button
            type="button"
            disabled={actionBusy !== null}
            onClick={() => void doAction('close')}
            aria-label="Close this alert"
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-sm hover:bg-slate-700 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            {actionBusy === 'close' ? (
              <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <X className="h-4 w-4" aria-hidden="true" />
            )}
            <span>Close</span>
          </button>
        </div>
      </div>

      {/* Timestamps + details card (extracted) */}
      <AlertDetailView alert={alert} />

      {/* State timeline, notification history, related alerts */}
      <AlertDetailHistory
        alert={alert}
        timeline={timeline}
        notifications={notifications}
        related={related}
      />
    </div>
  );
}
