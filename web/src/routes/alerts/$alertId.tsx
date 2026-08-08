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
  Activity,
  Bot,
  Loader2,
  AlertCircle,
} from 'lucide-react';
import { SeverityBadge } from '@/components/severity-badge';
import {
  StateBadge,
  formatTime,
  SnoozeMenu,
  AlertDetailHistory,
} from './alert_detail_components';
import { useAlertDetail } from './useAlertDetail';

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
                onClick={() => setSnoozeOpen((v) => !v)}
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

      {/* Timestamps row */}
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">Created</dt>
            <dd className="text-white mt-1">{formatTime(alert.created_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-gray-400 uppercase tracking-wider">
              Last state change
            </dt>
            <dd className="text-white mt-1">
              {formatTime(alert.updated_at ?? alert.created_at)}
            </dd>
          </div>
          {alert.acknowledged_at && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Acknowledged
              </dt>
              <dd className="text-white mt-1">
                {formatTime(alert.acknowledged_at)}
                {alert.acknowledged_by && (
                  <span className="text-gray-400"> · {alert.acknowledged_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.resolved_at && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Resolved
              </dt>
              <dd className="text-white mt-1">
                {formatTime(alert.resolved_at)}
                {alert.resolved_by && (
                  <span className="text-gray-400"> · {alert.resolved_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.snoozed_until && state === 'snoozed' && (
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">
                Snoozed until
              </dt>
              <dd className="text-white mt-1">{formatTime(alert.snoozed_until)}</dd>
            </div>
          )}
        </dl>
      </div>

      {/* Details card */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800">
          <h2 className="text-sm font-semibold text-white">Details</h2>
        </div>
        <div className="p-5 space-y-5">
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">Check</dt>
              <dd className="text-white mt-1 flex items-center gap-2">
                <Activity className="h-3.5 w-3.5 text-gray-400" />
                {alert.check_name ? (
                  alert.check_id ? (
                    <Link
                      to="/checks/$checkId"
                      params={{ checkId: alert.check_id }}
                      className="text-white hover:text-blue-400 underline-offset-2 hover:underline"
                    >
                      {alert.check_name}
                    </Link>
                  ) : (
                    <span>{alert.check_name}</span>
                  )
                ) : alert.check_id ? (
                  <span className="font-mono text-xs">{alert.check_id}</span>
                ) : (
                  <span className="text-gray-400">—</span>
                )}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-gray-400 uppercase tracking-wider">Agent</dt>
              <dd className="text-white mt-1 flex items-center gap-2">
                <Bot className="h-3.5 w-3.5 text-gray-400" />
                {alert.hostname ? (
                  alert.agent_id ? (
                    <Link
                      to="/agents/$agentId"
                      params={{ agentId: alert.agent_id }}
                      className="text-white hover:text-blue-400 underline-offset-2 hover:underline"
                    >
                      {alert.hostname}
                    </Link>
                  ) : (
                    <span>{alert.hostname}</span>
                  )
                ) : alert.agent_id ? (
                  <span className="font-mono text-xs">{alert.agent_id}</span>
                ) : (
                  <span className="text-gray-400">—</span>
                )}
              </dd>
            </div>
            {alert.source && (
              <div>
                <dt className="text-xs text-gray-400 uppercase tracking-wider">
                  Source
                </dt>
                <dd className="text-white mt-1 font-mono text-xs">
                  {alert.source}
                </dd>
              </div>
            )}
            {alert.tags && alert.tags.length > 0 && (
              <div>
                <dt className="text-xs text-gray-400 uppercase tracking-wider">Tags</dt>
                <dd className="mt-1 flex flex-wrap gap-1.5">
                  {alert.tags.map((t) => (
                    <span
                      key={t}
                      className="inline-flex px-2 py-0.5 rounded-full bg-slate-800 border border-slate-700 text-xs text-gray-300"
                    >
                      {t}
                    </span>
                  ))}
                </dd>
              </div>
            )}
          </dl>

          {alert.output && (
            <div>
              <h3 className="text-xs text-gray-400 uppercase tracking-wider mb-1.5">
                Check output
              </h3>
              <pre className="rounded-md bg-slate-950/80 border border-slate-800 p-3 text-xs text-white font-mono whitespace-pre-wrap break-words max-h-72 overflow-auto">
                {alert.output}
              </pre>
            </div>
          )}

          {alert.metrics && Object.keys(alert.metrics).length > 0 && (
            <div>
              <h3 className="text-xs text-gray-400 uppercase tracking-wider mb-1.5">
                Metrics
              </h3>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
                {Object.entries(alert.metrics).map(([k, v]) => (
                  <div
                    key={k}
                    className="rounded-md border border-slate-800 bg-slate-800 px-3 py-2"
                  >
                    <div className="text-xs text-gray-400">{k}</div>
                    <div className="text-sm text-white font-medium tabular-nums">
                      {String(v)}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

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
