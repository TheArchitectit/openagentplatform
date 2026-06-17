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
  open: { label: 'Open', classes: 'bg-danger/10 text-danger border-danger/20' },
  acknowledged: {
    label: 'Acknowledged',
    classes: 'bg-warning/10 text-warning border-warning/20',
  },
  snoozed: {
    label: 'Snoozed',
    classes: 'bg-text-muted/10 text-text-secondary border-text-muted/30',
  },
  resolved: {
    label: 'Resolved',
    classes: 'bg-success/10 text-success border-success/20',
  },
  closed: {
    label: 'Closed',
    classes: 'bg-border-strong/30 text-text-secondary border-border-strong/30',
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
        classes: 'text-success bg-success/10 border-success/20',
        icon: CircleCheck,
      };
    case 'sent':
      return {
        classes: 'text-info bg-info/10 border-info/20',
        icon: Send,
      };
    case 'failed':
      return {
        classes: 'text-danger bg-danger/10 border-danger/20',
        icon: CircleX,
      };
    case 'pending':
    default:
      return {
        classes: 'text-text-secondary bg-border-strong/30 border-border-strong/30',
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
  if (s === 'resolved' || s === 'closed') return 'bg-success/15 border-success/30 text-success';
  if (s === 'acknowledged') return 'bg-warning/15 border-warning/30 text-warning';
  if (s === 'snoozed') return 'bg-border-strong/20 border-text-muted/30 text-text-secondary';
  if (s === 'open') return 'bg-danger/15 border-danger/30 text-danger';
  return 'bg-border-strong/30 border-border-strong/30 text-text-secondary';
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
      <div className="text-center text-text-muted py-24">
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
          className="inline-flex items-center gap-2 text-sm text-text-secondary hover:text-text-primary"
        >
          <ArrowLeft className="h-4 w-4" />
          <span>Back to alerts</span>
        </Link>
        <div className="rounded-lg border border-danger/30 bg-danger/5 p-6 text-danger">
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
            className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center hover:bg-border-strong transition-colors shrink-0"
            title="Back to alerts"
          >
            <ArrowLeft className="h-4 w-4 text-text-secondary" />
          </Link>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <SeverityBadge severity={alert.severity} size="md" />
              <StateBadge state={alert.state} />
            </div>
            <h1 className="text-2xl font-bold text-text-primary mt-2 break-words">
              {alert.title}
            </h1>
            {alert.message && (
              <p className="text-text-secondary mt-1 break-words">{alert.message}</p>
            )}
            <button
              type="button"
              onClick={() => void handleCopyId()}
              aria-label={`Copy alert ID ${alert.id} to clipboard`}
              className="mt-2 inline-flex items-center gap-1.5 text-xs text-text-muted hover:text-text-secondary font-mono focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
            >
              <span>{alert.id}</span>
              {copyOk ? (
                <Check className="h-3 w-3 text-success" aria-hidden="true" />
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
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-warning/15 border border-warning/30 text-warning text-sm hover:bg-warning/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-text-primary text-sm hover:bg-border-strong disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-success/15 border border-success/30 text-success text-sm hover:bg-success/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
            className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-surface-tertiary border border-border-strong text-text-secondary text-sm hover:bg-border-strong disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent transition-colors"
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
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-4">
        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <div>
            <dt className="text-xs text-text-muted uppercase tracking-wider">Created</dt>
            <dd className="text-text-primary mt-1">{formatTime(alert.created_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-text-muted uppercase tracking-wider">
              Last state change
            </dt>
            <dd className="text-text-primary mt-1">
              {formatTime(alert.updated_at ?? alert.created_at)}
            </dd>
          </div>
          {alert.acknowledged_at && (
            <div>
              <dt className="text-xs text-text-muted uppercase tracking-wider">
                Acknowledged
              </dt>
              <dd className="text-text-primary mt-1">
                {formatTime(alert.acknowledged_at)}
                {alert.acknowledged_by && (
                  <span className="text-text-muted"> · {alert.acknowledged_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.resolved_at && (
            <div>
              <dt className="text-xs text-text-muted uppercase tracking-wider">
                Resolved
              </dt>
              <dd className="text-text-primary mt-1">
                {formatTime(alert.resolved_at)}
                {alert.resolved_by && (
                  <span className="text-text-muted"> · {alert.resolved_by}</span>
                )}
              </dd>
            </div>
          )}
          {alert.snoozed_until && state === 'snoozed' && (
            <div>
              <dt className="text-xs text-text-muted uppercase tracking-wider">
                Snoozed until
              </dt>
              <dd className="text-text-primary mt-1">{formatTime(alert.snoozed_until)}</dd>
            </div>
          )}
        </dl>
      </div>

      {/* Details card */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
        <div className="px-5 py-4 border-b border-border-subtle">
          <h2 className="text-sm font-semibold text-text-primary">Details</h2>
        </div>
        <div className="p-5 space-y-5">
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
            <div>
              <dt className="text-xs text-text-muted uppercase tracking-wider">Check</dt>
              <dd className="text-text-primary mt-1 flex items-center gap-2">
                <Activity className="h-3.5 w-3.5 text-text-muted" />
                {alert.check_name ? (
                  alert.check_id ? (
                    <Link
                      to="/checks/$checkId"
                      params={{ checkId: alert.check_id }}
                      className="text-text-primary hover:text-accent underline-offset-2 hover:underline"
                    >
                      {alert.check_name}
                    </Link>
                  ) : (
                    <span>{alert.check_name}</span>
                  )
                ) : alert.check_id ? (
                  <span className="font-mono text-xs">{alert.check_id}</span>
                ) : (
                  <span className="text-text-muted">—</span>
                )}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-text-muted uppercase tracking-wider">Agent</dt>
              <dd className="text-text-primary mt-1 flex items-center gap-2">
                <Bot className="h-3.5 w-3.5 text-text-muted" />
                {alert.hostname ? (
                  alert.agent_id ? (
                    <Link
                      to="/agents/$agentId"
                      params={{ agentId: alert.agent_id }}
                      className="text-text-primary hover:text-accent underline-offset-2 hover:underline"
                    >
                      {alert.hostname}
                    </Link>
                  ) : (
                    <span>{alert.hostname}</span>
                  )
                ) : alert.agent_id ? (
                  <span className="font-mono text-xs">{alert.agent_id}</span>
                ) : (
                  <span className="text-text-muted">—</span>
                )}
              </dd>
            </div>
            {alert.source && (
              <div>
                <dt className="text-xs text-text-muted uppercase tracking-wider">
                  Source
                </dt>
                <dd className="text-text-primary mt-1 font-mono text-xs">
                  {alert.source}
                </dd>
              </div>
            )}
            {alert.tags && alert.tags.length > 0 && (
              <div>
                <dt className="text-xs text-text-muted uppercase tracking-wider">Tags</dt>
                <dd className="mt-1 flex flex-wrap gap-1.5">
                  {alert.tags.map((t) => (
                    <span
                      key={t}
                      className="inline-flex px-2 py-0.5 rounded-full bg-surface-tertiary border border-border-strong text-xs text-text-secondary"
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
              <h3 className="text-xs text-text-muted uppercase tracking-wider mb-1.5">
                Check output
              </h3>
              <pre className="rounded-md bg-surface-primary/80 border border-border-subtle p-3 text-xs text-text-primary font-mono whitespace-pre-wrap break-words max-h-72 overflow-auto">
                {alert.output}
              </pre>
            </div>
          )}

          {alert.metrics && Object.keys(alert.metrics).length > 0 && (
            <div>
              <h3 className="text-xs text-text-muted uppercase tracking-wider mb-1.5">
                Metrics
              </h3>
              <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-3">
                {Object.entries(alert.metrics).map(([k, v]) => (
                  <div
                    key={k}
                    className="rounded-md border border-border-subtle bg-surface-primary/40 px-3 py-2"
                  >
                    <div className="text-xs text-text-muted">{k}</div>
                    <div className="text-sm text-text-primary font-medium tabular-nums">
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
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
        <div className="px-5 py-4 border-b border-border-subtle">
          <h2 className="text-sm font-semibold text-text-primary">State timeline</h2>
        </div>
        <div className="p-5">
          {timeline.length === 0 ? (
            <p className="text-sm text-text-muted">No state changes recorded yet.</p>
          ) : (
            <ol className="relative border-l border-border-subtle ml-2 space-y-4">
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
                      <span className="text-sm text-text-primary font-medium">
                        {t.from_state ? `${t.from_state} → ` : ''}
                        {t.to_state}
                      </span>
                      <span className="text-xs text-text-muted">
                        {formatTime(t.timestamp)}
                      </span>
                    </div>
                    {t.actor && (
                      <p className="text-xs text-text-muted mt-0.5">by {t.actor}</p>
                    )}
                    {t.note && (
                      <p className="text-xs text-text-secondary mt-1">{t.note}</p>
                    )}
                  </li>
                );
              })}
            </ol>
          )}
        </div>
      </div>

      {/* Notification history */}
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Notification history</h2>
          <span className="text-xs text-text-muted">
            {notifications.length} attempt{notifications.length === 1 ? '' : 's'}
          </span>
        </div>
        <div className="overflow-x-auto">
          {notifications.length === 0 ? (
            <p className="text-sm text-text-muted p-5">
              No notifications have been dispatched for this alert.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                  <th className="px-4 py-3">Channel</th>
                  <th className="px-4 py-3">Target</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Sent</th>
                  <th className="px-4 py-3">Delivered</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-subtle">
                {notifications.map((n) => {
                  const Icon = channelIcon(n.channel);
                  const tone = deliveryTone(n.status);
                  const StatusIcon = tone.icon;
                  return (
                    <tr key={n.id}>
                      <td className="px-4 py-3">
                        <span className="inline-flex items-center gap-2 text-text-primary">
                          <Icon className="h-4 w-4 text-text-secondary" />
                          <span>{n.channel}</span>
                        </span>
                      </td>
                      <td className="px-4 py-3 text-text-secondary break-all">
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
                          <p className="text-xs text-danger mt-1 break-all">
                            {n.error}
                          </p>
                        )}
                      </td>
                      <td className="px-4 py-3 text-text-secondary">
                        {formatTime(n.sent_at)}
                      </td>
                      <td className="px-4 py-3 text-text-secondary">
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
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Related alerts</h2>
          <span className="text-xs text-text-muted">
            {alert.check_id
              ? 'Same check'
              : alert.agent_id
              ? 'Same agent'
              : 'No relation available'}
          </span>
        </div>
        {related.length === 0 ? (
          <p className="text-sm text-text-muted p-5">
            No other alerts share this{' '}
            {alert.check_id ? 'check' : 'agent'}.
          </p>
        ) : (
          <ul className="divide-y divide-border-subtle">
            {related.map((r) => (
              <li
                key={r.id}
                className="px-5 py-3 flex items-center gap-4 hover:bg-surface-secondary transition-colors"
              >
                <SeverityBadge severity={r.severity} showLabel={false} />
                <Link
                  to="/alerts/$alertId"
                  params={{ alertId: r.id }}
                  className="flex-1 min-w-0"
                >
                  <p className="text-sm text-text-primary truncate hover:text-accent">
                    {r.title}
                  </p>
                  <p className="text-xs text-text-muted truncate">
                    {r.hostname ?? r.agent_id ?? ''}
                    {r.check_name ? ` · ${r.check_name}` : ''}
                  </p>
                </Link>
                <StateBadge state={r.state} />
                <span className="text-xs text-text-muted shrink-0">
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
      className="absolute right-0 top-full mt-1 w-44 rounded-md border border-border-strong bg-surface-secondary shadow-xl py-1 z-30"
    >
      <div className="px-3 py-1.5 text-xs text-text-muted uppercase tracking-wider border-b border-border-subtle">
        Snooze for…
      </div>
      {SNOOZE_PRESETS.map((p) => (
        <button
          key={p.mins}
          type="button"
          onClick={() => void onPick(p.mins)}
          className="w-full text-left px-3 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary hover:text-text-primary transition-colors"
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}
