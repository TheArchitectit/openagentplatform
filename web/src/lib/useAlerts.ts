// useAlerts — manages the alert inbox and per-alert detail state.
//
// Responsibilities:
//   • Fetch the alert list from REST with optional server-side filters.
//   • Subscribe to the "alerts" WebSocket channel and merge real-time
//     events (created, updated, state-changed) into the in-memory list.
//   • Expose mutations for the standard alert lifecycle actions:
//       acknowledge, snooze, resolve, close.
//   • Provide helpers for related-alert queries (same check or agent).
//
// WebSocket event vocabulary (server -> client):
//   { channel: "alerts", event: "alert.created",   data: Alert }
//   { channel: "alerts", event: "alert.updated",   data: Alert }
//   { channel: "alerts", event: "alert.state",     data: { id, state, timestamp, previous_state } }
//   { channel: "alerts", event: "alert.deleted",   data: { id } }

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import { getWsClient, type WsEnvelope, type Status } from './websocket';
import type { Severity } from '@/components/severity-badge';

export type AlertState =
  | 'open'
  | 'acknowledged'
  | 'snoozed'
  | 'resolved'
  | 'closed';

export interface Alert {
  id: string;
  title: string;
  message?: string;
  severity: Severity | string;
  state: AlertState | string;
  check_id?: string;
  check_name?: string;
  agent_id?: string;
  hostname?: string;
  output?: string;
  metrics?: Record<string, number | string>;
  created_at: string;
  updated_at?: string;
  acknowledged_at?: string;
  acknowledged_by?: string;
  resolved_at?: string;
  resolved_by?: string;
  snoozed_until?: string;
  source?: string;
  tags?: string[];
}

export interface AlertStateTransition {
  id: string;
  alert_id: string;
  from_state?: string;
  to_state: string;
  actor?: string;
  note?: string;
  timestamp: string;
}

export interface NotificationRecord {
  id: string;
  alert_id: string;
  channel: string;
  target: string;
  status: 'pending' | 'sent' | 'delivered' | 'failed';
  error?: string;
  sent_at?: string;
  delivered_at?: string;
}

export type AlertFilter =
  | 'all'
  | 'critical'
  | 'warning'
  | 'info'
  | 'acknowledged'
  | 'snoozed'
  | 'resolved';

export interface AlertListResponse {
  alerts: Alert[];
  total: number;
  limit: number;
  offset: number;
}

export interface UseAlertsResult {
  alerts: Alert[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  status: Status;
  refresh: () => Promise<void>;
  fetchAlert: (id: string) => Promise<Alert>;
  fetchTimeline: (id: string) => Promise<AlertStateTransition[]>;
  fetchNotifications: (id: string) => Promise<NotificationRecord[]>;
  fetchRelated: (alert: Alert) => Promise<Alert[]>;
  acknowledgeAlert: (id: string, note?: string) => Promise<Alert>;
  snoozeAlert: (id: string, durationMins: number, note?: string) => Promise<Alert>;
  resolveAlert: (id: string, note?: string) => Promise<Alert>;
  closeAlert: (id: string, note?: string) => Promise<Alert>;
  batchAcknowledge: (ids: string[]) => Promise<{ succeeded: string[]; failed: string[] }>;
  batchResolve: (ids: string[]) => Promise<{ succeeded: string[]; failed: string[] }>;
}

type WsAlertEvent =
  | { event: 'alert.created'; data: Alert }
  | { event: 'alert.updated'; data: Alert }
  | { event: 'alert.state'; data: { id: string; state: string; previous_state?: string; timestamp?: string; actor?: string } }
  | { event: 'alert.deleted'; data: { id: string } };

function isAlertEvent(env: WsEnvelope): env is WsEnvelope & WsAlertEvent {
  if (env.type !== 'event' || env.channel !== 'alerts') return false;
  const ev = env.event;
  if (
    ev !== 'alert.created' &&
    ev !== 'alert.updated' &&
    ev !== 'alert.state' &&
    ev !== 'alert.deleted'
  ) {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}

function filterToQuery(filter: AlertFilter): string {
  switch (filter) {
    case 'all':
      return '';
    case 'critical':
      return 'severity=critical,emergency&state=open';
    case 'warning':
      return 'severity=warning&state=open';
    case 'info':
      return 'severity=info&state=open';
    case 'acknowledged':
      return 'state=acknowledged';
    case 'snoozed':
      return 'state=snoozed';
    case 'resolved':
      return 'state=resolved,closed';
    default:
      return '';
  }
}

export function useAlerts(filter: AlertFilter = 'all'): UseAlertsResult {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [status, setStatus] = useState<Status>('closed');
  const mountedRef = useRef(true);

  const fetchAlerts = useCallback(async () => {
    try {
      const qs = filterToQuery(filter);
      const path = qs ? `/alerts?${qs}&limit=500` : '/alerts?limit=500';
      const res = await apiFetch<AlertListResponse>(path);
      if (!mountedRef.current) return;
      setAlerts(res.alerts ?? []);
      setTotal(res.total ?? (res.alerts?.length ?? 0));
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    void fetchAlerts();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchAlerts]);

  // Live updates from the alerts channel.
  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());

    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const handler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isAlertEvent(env)) return;

      const payload = env.data as unknown;

      if (env.event === 'alert.created') {
        const a = payload as Alert;
        setAlerts((prev) => {
          if (prev.some((p) => p.id === a.id)) return prev;
          return [a, ...prev];
        });
        return;
      }

      if (env.event === 'alert.updated') {
        const a = payload as Alert;
        setAlerts((prev) => {
          const idx = prev.findIndex((p) => p.id === a.id);
          if (idx === -1) return [a, ...prev];
          const next = prev.slice();
          next[idx] = { ...next[idx], ...a };
          return next;
        });
        return;
      }

      if (env.event === 'alert.state') {
        const s = payload as { id: string; state: string; timestamp?: string; previous_state?: string; actor?: string };
        setAlerts((prev) =>
          prev.map((a) =>
            a.id === s.id
              ? {
                  ...a,
                  state: s.state,
                  updated_at: s.timestamp ?? a.updated_at,
                  ...(s.state === 'acknowledged'
                    ? { acknowledged_at: s.timestamp ?? a.acknowledged_at, acknowledged_by: s.actor ?? a.acknowledged_by }
                    : {}),
                  ...(s.state === 'resolved'
                    ? { resolved_at: s.timestamp ?? a.resolved_at, resolved_by: s.actor ?? a.resolved_by }
                    : {}),
                }
              : a
          )
        );
        return;
      }

      if (env.event === 'alert.deleted') {
        const { id } = payload as { id: string };
        setAlerts((prev) => prev.filter((p) => p.id !== id));
        return;
      }
    };

    const unsub = ws.subscribe('alerts', handler);
    return () => {
      clearInterval(statusInterval);
      unsub();
    };
  }, []);

  // --- single alert ----------------------------------------------------

  const fetchAlert = useCallback(async (id: string): Promise<Alert> => {
    const a = await apiFetch<Alert>(`/alerts/${encodeURIComponent(id)}`);
    setAlerts((prev) => {
      const idx = prev.findIndex((p) => p.id === a.id);
      if (idx === -1) return [a, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...a };
      return next;
    });
    return a;
  }, []);

  const fetchTimeline = useCallback(
    async (id: string): Promise<AlertStateTransition[]> => {
      const res = await apiFetch<
        { transitions: AlertStateTransition[] } | AlertStateTransition[]
      >(`/alerts/${encodeURIComponent(id)}/timeline`);
      return Array.isArray(res) ? res : (res.transitions ?? []);
    },
    []
  );

  const fetchNotifications = useCallback(
    async (id: string): Promise<NotificationRecord[]> => {
      const res = await apiFetch<
        { notifications: NotificationRecord[] } | NotificationRecord[]
      >(`/alerts/${encodeURIComponent(id)}/notifications`);
      return Array.isArray(res) ? res : (res.notifications ?? []);
    },
    []
  );

  const fetchRelated = useCallback(async (alert: Alert): Promise<Alert[]> => {
    const params = new URLSearchParams();
    if (alert.check_id) {
      params.set('check_id', alert.check_id);
    } else if (alert.agent_id) {
      params.set('agent_id', alert.agent_id);
    } else {
      return [];
    }
    params.set('exclude_id', alert.id);
    params.set('limit', '20');
    const res = await apiFetch<AlertListResponse>(`/alerts?${params.toString()}`);
    return res.alerts ?? [];
  }, []);

  // --- mutations -------------------------------------------------------

  const applyMutation = useCallback((updated: Alert) => {
    setAlerts((prev) => {
      const idx = prev.findIndex((p) => p.id === updated.id);
      if (idx === -1) return [updated, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...updated };
      return next;
    });
    return updated;
  }, []);

  const acknowledgeAlert = useCallback(
    async (id: string, note?: string): Promise<Alert> => {
      const updated = await apiFetch<Alert>(`/alerts/${encodeURIComponent(id)}/acknowledge`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyMutation(updated);
    },
    [applyMutation]
  );

  const snoozeAlert = useCallback(
    async (id: string, durationMins: number, note?: string): Promise<Alert> => {
      const updated = await apiFetch<Alert>(`/alerts/${encodeURIComponent(id)}/snooze`, {
        method: 'POST',
        json: { duration_mins: durationMins, ...(note ? { note } : {}) },
      });
      return applyMutation(updated);
    },
    [applyMutation]
  );

  const resolveAlert = useCallback(
    async (id: string, note?: string): Promise<Alert> => {
      const updated = await apiFetch<Alert>(`/alerts/${encodeURIComponent(id)}/resolve`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyMutation(updated);
    },
    [applyMutation]
  );

  const closeAlert = useCallback(
    async (id: string, note?: string): Promise<Alert> => {
      const updated = await apiFetch<Alert>(`/alerts/${encodeURIComponent(id)}/close`, {
        method: 'POST',
        json: note ? { note } : undefined,
      });
      return applyMutation(updated);
    },
    [applyMutation]
  );

  // --- batch -----------------------------------------------------------

  const batchAction = useCallback(
    async (
      ids: string[],
      action: 'acknowledge' | 'resolve'
    ): Promise<{ succeeded: string[]; failed: string[] }> => {
      const succeeded: string[] = [];
      const failed: string[] = [];
      await Promise.all(
        ids.map(async (id) => {
          try {
            if (action === 'acknowledge') {
              await acknowledgeAlert(id);
            } else {
              await resolveAlert(id);
            }
            succeeded.push(id);
          } catch {
            failed.push(id);
          }
        })
      );
      return { succeeded, failed };
    },
    [acknowledgeAlert, resolveAlert]
  );

  const batchAcknowledge = useCallback(
    (ids: string[]) => batchAction(ids, 'acknowledge'),
    [batchAction]
  );

  const batchResolve = useCallback(
    (ids: string[]) => batchAction(ids, 'resolve'),
    [batchAction]
  );

  return {
    alerts,
    total,
    isLoading,
    error,
    status,
    refresh: fetchAlerts,
    fetchAlert,
    fetchTimeline,
    fetchNotifications,
    fetchRelated,
    acknowledgeAlert,
    snoozeAlert,
    resolveAlert,
    closeAlert,
    batchAcknowledge,
    batchResolve,
  };
}

export default useAlerts;
