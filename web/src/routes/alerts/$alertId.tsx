// Alert detail page — deep view of a single alert.
//
// Layout:
//   • Header: title, severity badge, state badge, key timestamps.
//   • Action bar: Acknowledge / Snooze (duration picker) / Resolve / Close.
//   • Details card: check name, agent hostname, monospace output, metrics.
//   • State timeline: vertical timeline of state transitions.
//   • Notification history: which channels were notified, delivery status.
//   • Related alerts: other alerts for the same check or agent.

import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ArrowLeft,
  BellRing,
  Check,
  CheckCheck,
  Clock,
  X,
  Mail,
  MessageSquare,
  Slack,
  Webhook,
  Send,
  AlertCircle,
  Bot,
  Activity,
  CircleCheck,
  CircleX,
  CircleDot,
  Loader2,
} from 'lucide-react';
import { apiFetch, ApiError } from '@/lib/api';
import { getWsClient, type WsEnvelope } from '@/lib/websocket';
import {
  useAlerts,
  type Alert,
  type AlertStateTransition,
  type NotificationRecord,
} from '@/lib/useAlerts';
import { SeverityBadge } from '@/components/severity-badge';

export const Route = createFileRoute('/alerts/$alertId')({
  component: AlertDetailPage,
});

const STATE_BADGE: Record<string, { label: string; classes: string }> = {
  open: { label: 'Open', classes: 'bg-red-500/10 text-red-400 border-red-800' },
  acknowledged: {
    label: 'Acknowledged',
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  },
  snoozed: {
    label: 'Snoozed',
    classes: 'bg-slate-500/10 text-gray-300 border-slate-700',
  },
  resolved: {
    label: 'Resolved',
    classes: 'bg-green-500/10 text-green-400 border-green-800',
  },
  closed: {
    label: 'Closed',
    classes: 'bg-slate-700/30 text-gray-300 border-slate-700/30',
  },
};

function StateBadge({ state }: { state: string }) {
  const key = (state ?? 'open').toLowerCase();
  const meta = STATE_BADGE[key] ?? STATE_BADGE.open;
  return (
    <span
      className={
        'inline-flex items-center px-2.5 py-1 rounded-full border text-sm font-medium ' +
        meta.classes
      }
    >
      {meta.label}
    </span>
  );
}

function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '—';
  return t.toLocaleString();
}

function channelIcon(channel: string) {
  const c = channel.toLowerCase();
  if (c.includes('slack')) return Slack;
  if (c.includes('mail') || c.includes('email') || c.includes('smtp')) return Mail;
  if (c.includes('sms') || c.includes('phone') || c.includes('twilio')) return MessageSquare;
  if (c.includes('webhook')) return Webhook;
  if (c.includes('pagerduty') || c.includes('pd')) return BellRing;
  return Send;
}

function deliveryTone(status: string): { classes: string; icon: typeof CircleCheck } {
  switch (status.toLowerCase()) {
    case 'delivered':
      return {
        classes: 'text-green-400 bg-green-500/10 border-green-800',
        icon: CircleCheck,
      };
    case 'sent':
      return {
        classes: 'text-blue-400 bg-blue-500/10 border-blue-800',
        icon: Send,
      };
    case 'failed':
      return {
        classes: 'text-red-400 bg-red-500/10 border-red-800',
        icon: CircleX,
      };
    case 'pending':
    default:
      return {
        classes: 'text-gray-300 bg-slate-700/30 border-slate-700/30',
        icon: CircleDot,
      };
  }
}

function transitionIcon(toState: string) {
  const s = (toState ?? '').toLowerCase();
  if (s === 'acknowledged') return Check;
  if (s === 'resolved') return CheckCheck;
  if (s === 'closed') return X;
  if (s === 'snoozed') return Clock;
  if (s === 'open') return AlertCircle;
  return CircleDot;
}

function transitionTone(toState: string): string {
  const s = (toState ?? '').toLowerCase();
  if (s === 'resolved' || s === 'closed') return 'bg-green-500/15 border-green-800 text-green-400';
  if (s === 'acknowledged') return 'bg-yellow-500/15 border-yellow-800 text-yellow-400';
  if (s === 'snoozed') return 'bg-slate-700/20 border-slate-700 text-gray-300';
  if (s === 'open') return 'bg-red-500/15 border-red-800 text-red-400';
  return 'bg-slate-700/30 border-slate-700/30 text-gray-300';
}

const SNOOZE_PRESETS: { label: string; mins: number }[] = [
  { label: '15 min', mins: 15 },
  { label: '1 hour', mins: 60 },
  { label: '4 hours', mins: 240 },
  { label: '24 hours', mins: 1440 },
  { label: '3 days', mins: 4320 },
];

function AlertDetailPage() {
  const { alertId } = Route.useParams();
  const navigate = useNavigate();
  const [alert, setAlert] = useState<Alert | null>(null);
  const [timeline, setTimeline] = useState<AlertStateTransition[]>([]);
  const [notifications, setNotifications] = useState<NotificationRecord[]>([]);
  const [related, setRelated] = useState<Alert[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [actionBusy, setActionBusy] = useState<string | null>(null);
  const [snoozeOpen, setSnoozeOpen] = useState(false);
  const [copyOk, setCopyOk] = useState(false);

  const {
    acknowledgeAlert,
    snoozeAlert,
    resolveAlert,
    closeAlert,
  } = useAlerts('all');

  const load = useCallback(async () => {
    setIsLoading(true);
    try {
      const a = await apiFetch<Alert>(`/alerts/${encodeURIComponent(alertId)}`);
      setAlert(a);
      setError(null);
      // Fetch timeline, notifications, and related in parallel.
      const [tlRes, nRes, relRes] = await Promise.allSettled([
        apiFetch<
          { transitions: AlertStateTransition[] } | AlertStateTransition[]
        >(`/alerts/${encodeURIComponent(alertId)}/timeline`),
        apiFetch<
          { notifications: NotificationRecord[] } | NotificationRecord[]
        >(`/alerts/${encodeURIComponent(alertId)}/notifications`),
        (async (): Promise<Alert[]> => {
          const params = new URLSearchParams();
          if (a.check_id) params.set('check_id', a.check_id);
          else if (a.agent_id) params.set('agent_id', a.agent_id);
          else return [];
          params.set('exclude_id', a.id);
          params.set('limit', '20');
          const res = await apiFetch<{ alerts?: Alert[] }>(
            `/alerts?${params.toString()}`
          );
          return res.alerts ?? [];
        })(),
      ]);
      if (tlRes.status === 'fulfilled') {
        const v = tlRes.value;
        setTimeline(Array.isArray(v) ? v : v.transitions ?? []);
      } else {
        setTimeline([]);
      }
      if (nRes.status === 'fulfilled') {
        const v = nRes.value;
        setNotifications(Array.isArray(v) ? v : v.notifications ?? []);
      } else {
        setNotifications([]);
      }
      if (relRes.status === 'fulfilled') {
        setRelated(relRes.value);
      } else {
        setRelated([]);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [alertId]);

  useEffect(() => {
    void load();
  }, [load]);

  // Real-time updates: keep alert, timeline and related list in sync.
  useEffect(() => {
    const ws = getWsClient();
    const unsub = ws.subscribe('alerts', (env: WsEnvelope) => {
      if (env.type !== 'event' || !env.data) return;
      if (env.event === 'alert.updated') {
        const a = env.data as Alert;
        if (a.id === alertId) setAlert((prev) => (prev ? { ...prev, ...a } : a));
      } else if (env.event === 'alert.state') {
        const s = env.data as {
          id: string;
          state: string;
          previous_state?: string;
          timestamp?: string;
          actor?: string;
        };
        if (s.id !== alertId) return;
        setAlert((prev) =>
          prev
            ? {
                ...prev,
                state: s.state,
                updated_at: s.timestamp ?? prev.updated_at,
                ...(s.state === 'acknowledged'
                  ? {
                      acknowledged_at: s.timestamp ?? prev.acknowledged_at,
                      acknowledged_by: s.actor ?? prev.acknowledged_by,
                    }
                  : {}),
                ...(s.state === 'resolved'
                  ? {
                      resolved_at: s.timestamp ?? prev.resolved_at,
                      resolved_by: s.actor ?? prev.resolved_by,
                    }
                  : {}),
              }
            : prev
        );
        // Append a synthetic transition for live-feel.
        setTimeline((prev) => [
          ...prev,
          {
            id: `live-${Date.now()}`,
            alert_id: alertId,
            from_state: s.previous_state,
            to_state: s.state,
            actor: s.actor,
            timestamp: s.timestamp ?? new Date().toISOString(),
          },
        ]);
      } else if (env.event === 'alert.deleted') {
        const d = env.data as { id: string };
        if (d.id === alertId) {
          // Bounce to inbox on delete.
          void navigate({ to: '/alerts' });
        }
      }
    });
    return unsub;
  }, [alertId, navigate]);

  const doAction = useCallback(
    async (
      kind: 'ack' | 'resolve' | 'close' | { snooze: number }
    ): Promise<void> => {
      if (!alert) return;
      setActionBusy(typeof kind === 'string' ? kind : 'snooze');
      try {
        if (kind === 'ack') {
          await acknowledgeAlert(alert.id);
        } else if (kind === 'resolve') {
          await resolveAlert(alert.id);
        } else if (kind === 'close') {
          await closeAlert(alert.id);
        } else {
          await snoozeAlert(alert.id, kind.snooze);
        }
        // Refresh server-of-record data.
        await load();
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setActionBusy(null);
        setSnoozeOpen(false);
      }
    },
    [alert, acknowledgeAlert, snoozeAlert, resolveAlert, closeAlert, load]
  );

  const handleCopyId = useCallback(async () => {
    if (!alert) return;
    try {
      await navigator.clipboard.writeText(alert.id);
      setCopyOk(true);
      setTimeout(() => setCopyOk(false), 1200);
    } catch {
      /* ignore */
    }
  }, [alert]);

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

      {/* State timeline */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800">
          <h2 className="text-sm font-semibold text-white">State timeline</h2>
        </div>
        <div className="p-5">
          {timeline.length === 0 ? (
            <p className="text-sm text-gray-400">No state changes recorded yet.</p>
          ) : (
            <ol className="relative border-l border-slate-800 ml-2 space-y-4">
              {timeline.map((t) => {
                const Icon = transitionIcon(t.to_state);
                return (
                  <li key={t.id} className="ml-4">
                    <span
                      className={
                        'absolute -left-[9px] flex h-4 w-4 items-center justify-center rounded-full border ' +
                        transitionTone(t.to_state)
                      }
                    >
                      <Icon className="h-2.5 w-2.5" />
                    </span>
                    <div className="flex flex-wrap items-baseline gap-2">
                      <span className="text-sm text-white font-medium">
                        {t.from_state ? `${t.from_state} → ` : ''}
                        {t.to_state}
                      </span>
                      <span className="text-xs text-gray-400">
                        {formatTime(t.timestamp)}
                      </span>
                    </div>
                    {t.actor && (
                      <p className="text-xs text-gray-400 mt-0.5">by {t.actor}</p>
                    )}
                    {t.note && (
                      <p className="text-xs text-gray-300 mt-1">{t.note}</p>
                    )}
                  </li>
                );
              })}
            </ol>
          )}
        </div>
      </div>

      {/* Notification history */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white">Notification history</h2>
          <span className="text-xs text-gray-400">
            {notifications.length} attempt{notifications.length === 1 ? '' : 's'}
          </span>
        </div>
        <div className="overflow-x-auto">
          {notifications.length === 0 ? (
            <p className="text-sm text-gray-400 p-5">
              No notifications have been dispatched for this alert.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                  <th className="px-4 py-3">Channel</th>
                  <th className="px-4 py-3">Target</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Sent</th>
                  <th className="px-4 py-3">Delivered</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800">
                {notifications.map((n) => {
                  const Icon = channelIcon(n.channel);
                  const tone = deliveryTone(n.status);
                  const StatusIcon = tone.icon;
                  return (
                    <tr key={n.id}>
                      <td className="px-4 py-3">
                        <span className="inline-flex items-center gap-2 text-white">
                          <Icon className="h-4 w-4 text-gray-300" />
                          <span>{n.channel}</span>
                        </span>
                      </td>
                      <td className="px-4 py-3 text-gray-300 break-all">
                        {n.target || '—'}
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={
                            'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-xs font-medium ' +
                            tone.classes
                          }
                        >
                          <StatusIcon className="h-3 w-3" />
                          <span className="capitalize">{n.status}</span>
                        </span>
                        {n.error && (
                          <p className="text-xs text-red-400 mt-1 break-all">
                            {n.error}
                          </p>
                        )}
                      </td>
                      <td className="px-4 py-3 text-gray-300">
                        {formatTime(n.sent_at)}
                      </td>
                      <td className="px-4 py-3 text-gray-300">
                        {formatTime(n.delivered_at)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Related alerts */}
      <div className="rounded-lg border border-slate-800 bg-slate-900">
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-white">Related alerts</h2>
          <span className="text-xs text-gray-400">
            {alert.check_id
              ? 'Same check'
              : alert.agent_id
              ? 'Same agent'
              : 'No relation available'}
          </span>
        </div>
        {related.length === 0 ? (
          <p className="text-sm text-gray-400 p-5">
            No other alerts share this{' '}
            {alert.check_id ? 'check' : 'agent'}.
          </p>
        ) : (
          <ul className="divide-y divide-slate-800">
            {related.map((r) => (
              <li
                key={r.id}
                className="px-5 py-3 flex items-center gap-4 hover:bg-slate-900 transition-colors"
              >
                <SeverityBadge severity={r.severity} showLabel={false} />
                <Link
                  to="/alerts/$alertId"
                  params={{ alertId: r.id }}
                  className="flex-1 min-w-0"
                >
                  <p className="text-sm text-white truncate hover:text-blue-400">
                    {r.title}
                  </p>
                  <p className="text-xs text-gray-400 truncate">
                    {r.hostname ?? r.agent_id ?? ''}
                    {r.check_name ? ` · ${r.check_name}` : ''}
                  </p>
                </Link>
                <StateBadge state={r.state} />
                <span className="text-xs text-gray-400 shrink-0">
                  {formatTime(r.created_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function SnoozeMenu({
  onPick,
  onClose,
}: {
  onPick: (mins: number) => Promise<void> | void;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    function onClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    }
    document.addEventListener('mousedown', onClick);
    return () => document.removeEventListener('mousedown', onClick);
  }, [onClose]);
  return (
    <div
      ref={ref}
      className="absolute right-0 top-full mt-1 w-44 rounded-md border border-slate-700 bg-slate-900 shadow-xl py-1 z-30"
    >
      <div className="px-3 py-1.5 text-xs text-gray-400 uppercase tracking-wider border-b border-slate-800">
        Snooze for…
      </div>
      {SNOOZE_PRESETS.map((p) => (
        <button
          key={p.mins}
          type="button"
          onClick={() => void onPick(p.mins)}
          className="w-full text-left px-3 py-1.5 text-sm text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}
