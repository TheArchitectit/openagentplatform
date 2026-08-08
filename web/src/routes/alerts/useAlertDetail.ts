// Alert detail page state and effects — extracted for file-size compliance.

import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { apiFetch, ApiError } from '@/lib/api';
import { getWsClient, type WsEnvelope } from '@/lib/websocket';
import { useAlerts, type Alert, type AlertStateTransition, type NotificationRecord } from '@/lib/useAlerts';

export interface AlertDetailState {
  alert: Alert | null;
  timeline: AlertStateTransition[];
  notifications: NotificationRecord[];
  related: Alert[];
  isLoading: boolean;
  error: Error | null;
  actionBusy: string | null;
  snoozeOpen: boolean;
  copyOk: boolean;
  setSnoozeOpen: (v: boolean) => void;
  doAction: (kind: 'ack' | 'resolve' | 'close' | { snooze: number }) => Promise<void>;
  handleCopyId: () => Promise<void>;
}

export function useAlertDetail(alertId: string): AlertDetailState {
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

  const { acknowledgeAlert, snoozeAlert, resolveAlert, closeAlert } = useAlerts('all');

  const load = useCallback(async () => {
    setIsLoading(true);
    try {
      const a = await apiFetch<Alert>(`/alerts/${encodeURIComponent(alertId)}`);
      setAlert(a);
      setError(null);
      const [tlRes, nRes, relRes] = await Promise.allSettled([
        apiFetch<{ transitions: AlertStateTransition[] } | AlertStateTransition[]>(`/alerts/${encodeURIComponent(alertId)}/timeline`),
        apiFetch<{ notifications: NotificationRecord[] } | NotificationRecord[]>(`/alerts/${encodeURIComponent(alertId)}/notifications`),
        (async (): Promise<Alert[]> => {
          const params = new URLSearchParams();
          if (a.check_id) params.set('check_id', a.check_id);
          else if (a.agent_id) params.set('agent_id', a.agent_id);
          else return [];
          params.set('exclude_id', a.id);
          params.set('limit', '20');
          const res = await apiFetch<{ alerts?: Alert[] }>(`/alerts?${params.toString()}`);
          return res.alerts ?? [];
        })(),
      ]);
      if (tlRes.status === 'fulfilled') {
        const v = tlRes.value;
        setTimeline(Array.isArray(v) ? v : v.transitions ?? []);
      } else setTimeline([]);
      if (nRes.status === 'fulfilled') {
        const v = nRes.value;
        setNotifications(Array.isArray(v) ? v : v.notifications ?? []);
      } else setNotifications([]);
      if (relRes.status === 'fulfilled') setRelated(relRes.value);
      else setRelated([]);
    } catch (err) {
      setError(err instanceof ApiError ? err : new Error(String(err)));
    } finally {
      setIsLoading(false);
    }
  }, [alertId]);

  useEffect(() => { void load(); }, [load]);

  useEffect(() => {
    const ws = getWsClient();
    const unsub = ws.subscribe('alerts', (env: WsEnvelope) => {
      if (env.type !== 'event' || !env.data) return;
      if (env.event === 'alert.updated') {
        const a = env.data as Alert;
        if (a.id === alertId) setAlert((prev) => (prev ? { ...prev, ...a } : a));
      } else if (env.event === 'alert.state') {
        const s = env.data as { id: string; state: string; previous_state?: string; timestamp?: string; actor?: string };
        if (s.id !== alertId) return;
        setAlert((prev) => prev ? {
          ...prev,
          state: s.state,
          updated_at: s.timestamp ?? prev.updated_at,
          ...(s.state === 'acknowledged' ? { acknowledged_at: s.timestamp ?? prev.acknowledged_at, acknowledged_by: s.actor ?? prev.acknowledged_by } : {}),
          ...(s.state === 'resolved' ? { resolved_at: s.timestamp ?? prev.resolved_at, resolved_by: s.actor ?? prev.resolved_by } : {}),
        } : prev);
        setTimeline((prev) => [...prev, {
          id: `live-${Date.now()}`,
          alert_id: alertId,
          from_state: s.previous_state,
          to_state: s.state,
          actor: s.actor,
          timestamp: s.timestamp ?? new Date().toISOString(),
        }]);
      } else if (env.event === 'alert.deleted') {
        const d = env.data as { id: string };
        if (d.id === alertId) void navigate({ to: '/alerts' });
      }
    });
    return unsub;
  }, [alertId, navigate]);

  const doAction = useCallback(async (kind: 'ack' | 'resolve' | 'close' | { snooze: number }): Promise<void> => {
    if (!alert) return;
    setActionBusy(typeof kind === 'string' ? kind : 'snooze');
    try {
      if (kind === 'ack') await acknowledgeAlert(alert.id);
      else if (kind === 'resolve') await resolveAlert(alert.id);
      else if (kind === 'close') await closeAlert(alert.id);
      else await snoozeAlert(alert.id, kind.snooze);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setActionBusy(null);
      setSnoozeOpen(false);
    }
  }, [alert, acknowledgeAlert, snoozeAlert, resolveAlert, closeAlert, load]);

  const handleCopyId = useCallback(async () => {
    if (!alert) return;
    try {
      await navigator.clipboard.writeText(alert.id);
      setCopyOk(true);
      setTimeout(() => setCopyOk(false), 1200);
    } catch { /* ignore */ }
  }, [alert]);

  return { alert, timeline, notifications, related, isLoading, error, actionBusy, snoozeOpen, copyOk, setSnoozeOpen, doAction, handleCopyId };
}
