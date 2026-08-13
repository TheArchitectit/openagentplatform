// Alert inbox — the operational landing page for alerts.
//
// Features:
//   • Server-driven filter tabs (All, Critical, Warning, Info,
//     Acknowledged, Snoozed, Resolved).
//   • Tabular inbox with severity icon, title, agent/check, state badge,
//     created time, and inline per-row actions.
//   • Click row -> /alerts/$alertId.
//   • Multi-select + batch Acknowledge / Resolve.
//   • WebSocket "alerts" channel merges new / updated alerts in real time.
//   • Optional browser Notification + audible cue on incoming critical.

import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  BellRing,
  RefreshCw,
  Search,
  Check,
  X,
  CheckCheck,
  Volume2,
  VolumeX,
} from 'lucide-react';
import { useAlerts, type Alert, type AlertFilter, type AlertState } from '@/lib/useAlerts';

import { RowItem, InlineActions, SnoozeMenu } from './alert_components'
import { PAGE_SIZE, TABS, computeAlertCounts } from './alertPage.helpers'
import { AlertsTable } from './AlertsTable'

function AlertsInboxPage() {
  const navigate = useNavigate();
  const [filter, setFilter] = useState<AlertFilter>('all');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchBusy, setBatchBusy] = useState(false);
  const [soundOn, setSoundOn] = useState(false);
  const lastSeenCriticalRef = useRef<string | null>(null);

  const { alerts, isLoading, error, refresh, status, batchAcknowledge, batchResolve } =
    useAlerts(filter);

  // Keep relative times current.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // Filter / search / paginate.
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return alerts;
    return alerts.filter((a) => {
      if (a.title?.toLowerCase().includes(q)) return true;
      if (a.message?.toLowerCase().includes(q)) return true;
      if (a.hostname?.toLowerCase().includes(q)) return true;
      if (a.check_name?.toLowerCase().includes(q)) return true;
      if (a.id.toLowerCase().includes(q)) return true;
      return false;
    });
  }, [alerts, query]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const currentPage = Math.min(page, totalPages - 1);
  const paged = filtered.slice(currentPage * PAGE_SIZE, (currentPage + 1) * PAGE_SIZE);

  const counts = useMemo(() => {
    const base = computeAlertCounts(alerts, ['critical', 'warning', 'info', 'acknowledged', 'snoozed', 'resolved']);
    return { all: alerts.length, ...base } as Record<AlertFilter, number>;
  }, [alerts]);

  // Critical-alert browser notifications (optional).
  useEffect(() => {
    if (typeof window === 'undefined' || !('Notification' in window)) return;
    if (Notification.permission === 'default') return;
    if (Notification.permission !== 'granted') return;
    if (alerts.length === 0) return;

    const openCritical = alerts
      .filter(
        (a) =>
          (a.severity === 'critical' || a.severity === 'emergency') &&
          (a.state === 'open' || a.state === undefined)
      )
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0];

    if (!openCritical) return;
    if (lastSeenCriticalRef.current === openCritical.id) return;
    lastSeenCriticalRef.current = openCritical.id;

    try {
      new Notification(`Critical alert: ${openCritical.title}`, {
        body:
          openCritical.message ??
          `${openCritical.check_name ?? 'check'} on ${openCritical.hostname ?? 'agent'}`,
        tag: openCritical.id,
      });
    } catch {
      /* ignore */
    }
  }, [alerts]);

  const toggleSound = useCallback(() => {
    if (!soundOn) {
      if (typeof window !== 'undefined' && 'Notification' in window) {
        if (Notification.permission === 'default') {
          void Notification.requestPermission();
        }
      }
    }
    setSoundOn((v) => !v);
  }, [soundOn]);

  // Selection helpers.
  const allOnPageSelected =
    paged.length > 0 && paged.every((a) => selected.has(a.id));
  const toggleRow = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);
  const togglePage = useCallback(() => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allOnPageSelected) {
        for (const a of paged) next.delete(a.id);
      } else {
        for (const a of paged) next.add(a.id);
      }
      return next;
    });
  }, [allOnPageSelected, paged]);
  const clearSelection = useCallback(() => setSelected(new Set()), []);

  const runBatch = useCallback(
    async (kind: 'ack' | 'resolve') => {
      if (selected.size === 0) return;
      setBatchBusy(true);
      try {
        if (kind === 'ack') {
          await batchAcknowledge(Array.from(selected));
        } else {
          await batchResolve(Array.from(selected));
        }
        clearSelection();
      } finally {
        setBatchBusy(false);
      }
    },
    [selected, batchAcknowledge, batchResolve, clearSelection]
  );

  return (
    <div className="space-y-5" aria-busy={isLoading}>
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-red-500/10 border border-red-800 flex items-center justify-center" aria-hidden="true">
            <BellRing className="h-4 w-4 text-red-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">Alerts</h1>
            <p className="text-gray-300 text-sm mt-0.5">
              Active and historical alerts across your fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-green-500' : status === 'connecting' ? 'bg-yellow-500' : 'bg-slate-500')
            }
            role="status"
            aria-label={`WebSocket connection: ${status}`}
          />
          <button
            type="button"
            onClick={toggleSound}
            aria-label={soundOn ? 'Mute critical-alert notifications' : 'Enable critical-alert browser notifications'}
            aria-pressed={soundOn}
            className="inline-flex items-center justify-center h-9 w-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            {soundOn ? <Volume2 className="h-4 w-4" aria-hidden="true" /> : <VolumeX className="h-4 w-4" aria-hidden="true" />}
          </button>
          <button
            type="button"
            onClick={() => void refresh()}
            disabled={isLoading}
            aria-label="Refresh alerts"
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} aria-hidden="true" />
            <span>Refresh</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div role="tablist" aria-label="Alert filters" className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={filter === t.id}
              onClick={() => { setFilter(t.id); setPage(0); clearSelection(); }}
              className={
                'px-3 h-8 rounded text-sm whitespace-nowrap transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ' +
                (filter === t.id ? 'bg-slate-800 text-white' : 'text-gray-300 hover:text-white')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-gray-400" aria-hidden="true">{counts[t.id]}</span>
              <span className="sr-only">({counts[t.id]} alerts)</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search alerts"
            value={query}
            onChange={(e) => { setQuery(e.target.value); setPage(0); }}
            placeholder="Search alerts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
      </div>

      {/* Batch actions bar */}
      {selected.size > 0 && (
        <div role="region" aria-label="Batch actions" className="flex items-center justify-between gap-3 rounded-md border border-blue-500/30 bg-blue-600/5 px-4 py-2">
          <div className="text-sm text-white" aria-live="polite">
            <span className="font-medium">{selected.size}</span> selected
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('ack')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-yellow-500/15 border border-yellow-800 text-yellow-400 text-sm hover:bg-yellow-500/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              <Check className="h-3.5 w-3.5" aria-hidden="true" />
              <span>Acknowledge all</span>
            </button>
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('resolve')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-green-500/15 border border-green-800 text-green-400 text-sm hover:bg-green-500/25 disabled:opacity-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              <CheckCheck className="h-3.5 w-3.5" aria-hidden="true" />
              <span>Resolve all</span>
            </button>
            <button
              type="button"
              onClick={clearSelection}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-sm hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              <X className="h-3.5 w-3.5" aria-hidden="true" />
              <span>Clear</span>
            </button>
          </div>
        </div>
      )}

      <AlertsTable
        isLoading={isLoading}
        error={error}
        filtered={filtered}
        paged={paged}
        selected={selected}
        currentPage={currentPage}
        totalPages={totalPages}
        now={now}
        onToggleRow={toggleRow}
        onNavigate={(id) => void navigate({ to: '/alerts/$alertId', params: { alertId: id } })}
        onTogglePage={togglePage}
        onSetPage={setPage}
      />
    </div>
  );
}

export const Route = createFileRoute('/alerts/')({
  component: AlertsInboxPage,
});
