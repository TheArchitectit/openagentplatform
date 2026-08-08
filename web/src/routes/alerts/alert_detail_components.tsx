// Alert detail — shared helpers, the snooze menu, and the bottom
// "history" sections (state timeline, notification history, related alerts).
// Extracted from $alertId.tsx to keep that route file under the 500-line limit.

import { createPortal } from 'react-dom';
import {
  X,
  Clock,
  Mail,
  MessageSquare,
  Slack,
  Webhook,
  CircleCheck,
  CircleX,
  CircleDot,
  Check,
} from 'lucide-react';
import { SeverityBadge } from '@/components/severity-badge';
import {
  type Alert,
  type AlertStateTransition,
  type NotificationRecord,
} from '@/lib/useAlerts';

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

export function StateBadge({ state }: { state: string }) {
  const s = (state ?? 'open').toLowerCase();
  const conf = STATE_BADGE[s] ?? STATE_BADGE.open;
  return (
    <span
      className={
        'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
        conf.classes
      }
    >
      {conf.label}
    </span>
  );
}

export function formatTime(iso: string | undefined): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

function channelIcon(channel: string) {
  switch (channel.toLowerCase()) {
    case 'email':
      return Mail;
    case 'slack':
      return Slack;
    case 'webhook':
      return Webhook;
    case 'dashboard':
    case 'in-app':
      return MessageSquare;
    default:
      return MessageSquare;
  }
}

function deliveryTone(status: string): { classes: string; icon: typeof CircleCheck } {
  switch (status.toLowerCase()) {
    case 'delivered':
      return {
        classes: 'bg-green-500/10 text-green-400 border-green-800',
        icon: CircleCheck,
      };
    case 'failed':
    case 'error':
      return {
        classes: 'bg-red-500/10 text-red-400 border-red-800',
        icon: CircleX,
      };
    case 'pending':
    case 'queued':
      return {
        classes: 'bg-slate-500/10 text-gray-300 border-slate-700',
        icon: CircleDot,
      };
    default:
      return {
        classes: 'bg-slate-500/10 text-gray-300 border-slate-700',
        icon: CircleDot,
      };
  }
}

function transitionIcon(toState: string) {
  switch (toState.toLowerCase()) {
    case 'resolved':
      return CircleCheck;
    case 'closed':
      return CircleX;
    case 'open':
      return CircleDot;
    default:
      return CircleDot;
  }
}

function transitionTone(toState: string): string {
  switch (toState.toLowerCase()) {
    case 'resolved':
      return 'bg-green-500/20 border-green-700';
    case 'closed':
      return 'bg-slate-600 border-slate-600';
    case 'acknowledged':
    case 'snoozed':
      return 'bg-yellow-500/20 border-yellow-700';
    case 'open':
      return 'bg-red-500/20 border-red-700';
    default:
      return 'bg-slate-600 border-slate-600';
  }
}

export function SnoozeMenu({
  onPick,
  onClose,
}: {
  onPick: (mins: number) => void;
  onClose: () => void;
}) {
  const presets = [15, 60, 240, 1440, 10080];
  return (
    <>
      {createPortal(
        <div
          className="fixed inset-0 z-40"
          onClick={onClose}
          aria-hidden="true"
        >
          <div className="absolute inset-0 bg-black/40" />
        </div>,
        document.body
      )}
      <div
        role="menu"
        className="absolute right-0 mt-2 z-50 w-44 rounded-md border border-slate-700 bg-slate-900 shadow-lg p-1"
      >
        <div className="px-2 py-1 text-xs text-gray-400 uppercase tracking-wider">
          Snooze for
        </div>
        {presets.map((m) => (
          <button
            key={m}
            type="button"
            role="menuitem"
            onClick={() => onPick(m)}
            className="w-full text-left px-3 py-1.5 rounded text-sm text-white hover:bg-slate-800 flex items-center justify-between"
          >
            <span>{m < 60 ? `${m} min` : m < 1440 ? `${Math.round(m / 60)} hr` : `${m / 1440} day${m === 1440 ? '' : 's'}`}</span>
            <Clock className="h-3.5 w-3.5 text-gray-400" />
          </button>
        ))}
        <button
          type="button"
          role="menuitem"
          onClick={onClose}
          className="w-full text-left px-3 py-1.5 rounded text-sm text-gray-400 hover:bg-slate-800 flex items-center gap-1.5"
        >
          <X className="h-3.5 w-3.5" />
          <span>Cancel</span>
        </button>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// AlertDetailHistory — the "bottom half" of the alert detail view:
// state timeline, notification history, and related alerts.
// ---------------------------------------------------------------------------

export function AlertDetailHistory({
  alert,
  timeline,
  notifications,
  related,
}: {
  alert: Alert;
  timeline: AlertStateTransition[];
  notifications: NotificationRecord[];
  related: Alert[];
}) {
  return (
    <>
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
                <a
                  href={`/alerts/${r.id}`}
                  className="flex-1 min-w-0"
                >
                  <p className="text-sm text-white truncate hover:text-blue-400">
                    {r.title}
                  </p>
                  <p className="text-xs text-gray-400 truncate">
                    {r.hostname ?? r.agent_id ?? ''}
                    {r.check_name ? ` · ${r.check_name}` : ''}
                  </p>
                </a>
                <StateBadge state={r.state} />
                <span className="text-xs text-gray-400 shrink-0">
                  {formatTime(r.created_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}
