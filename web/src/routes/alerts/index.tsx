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
  Eye,
  CircleDot,
  Clock,
  CheckCheck,
  Volume2,
  VolumeX,
  Bot,
  Activity,
} from 'lucide-react';
import { useAlerts, type Alert, type AlertFilter, type AlertState } from '@/lib/useAlerts';
import { SeverityBadge } from '@/components/severity-badge';

export const Route = createFileRoute('/alerts/')({
  component: AlertsInboxPage,
});

const STATE_BADGES: Record<AlertState | string, { label: string; classes: string }> = {
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
    classes: 'bg-slate-800/30 text-gray-300 border-slate-700/30',
  },
};

const TABS: { id: AlertFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'critical', label: 'Critical' },
  { id: 'warning', label: 'Warning' },
  { id: 'info', label: 'Info' },
  { id: 'acknowledged', label: 'Acknowledged' },
  { id: 'snoozed', label: 'Snoozed' },
  { id: 'resolved', label: 'Resolved' },
];

const PAGE_SIZE = 50;

function StateBadge({ state }: { state: string }) {
  const key = (state ?? 'open').toLowerCase();
  const meta = STATE_BADGES[key] ?? STATE_BADGES.open;
  return (
    <span
      role="status"
      aria-label={`State: ${meta.label}`}
      className={
        'inline-flex items-center px-2 py-0.5 rounded-full border text-xs font-medium ' +
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

function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const age = Math.max(0, Math.floor((now - t) / 1000));
  if (age < 60) return `${age}s ago`;
  if (age < 3600) return `${Math.floor(age / 60)}m ago`;
  if (age < 86400) return `${Math.floor(age / 3600)}h ago`;
  return `${Math.floor(age / 86400)}d ago`;
}

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
    const c: Record<AlertFilter, number> = {
      all: alerts.length,
      critical: 0,
      warning: 0,
      info: 0,
      acknowledged: 0,
      snoozed: 0,
      resolved: 0,
    };
    for (const a of alerts) {
      const sev = (a.severity ?? '').toLowerCase();
      const st = (a.state ?? '').toLowerCase();
      if (sev === 'critical' || sev === 'emergency') c.critical += 1;
      if (sev === 'warning') c.warning += 1;
      if (sev === 'info') c.info += 1;
      if (st === 'acknowledged') c.acknowledged += 1;
      if (st === 'snoozed') c.snoozed += 1;
      if (st === 'resolved' || st === 'closed') c.resolved += 1;
    }
    return c;
  }, [alerts]);

  // Critical-alert browser notifications (optional).
  useEffect(() => {
    if (typeof window === 'undefined' || !('Notification' in window)) return;
    if (Notification.permission === 'default') {
      // Don't auto-prompt — wait for explicit user opt-in via the sound toggle.
      return;
    }
    if (Notification.permission !== 'granted') return;
    if (alerts.length === 0) return;

    // Find the newest critical/emergency alert that is still "open".
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
      /* ignore — some browsers block from non-foreground tabs */
    }
  }, [alerts]);

  const toggleSound = useCallback(() => {
    if (!soundOn) {
      // Enabling also requests notification permission in one user gesture.
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
            aria-label={
              soundOn
                ? 'Mute critical-alert notifications'
                : 'Enable critical-alert browser notifications'
            }
            aria-pressed={soundOn}
            className="inline-flex items-center justify-center h-9 w-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            {soundOn ? <Volume2 className="h-4 w-4" aria-hidden="true" /> : <VolumeX className="h-4 w-4" aria-hidden="true" />}
          </button>
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
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
        <div
          role="tablist"
          aria-label="Alert filters"
          className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto"
        >
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={filter === t.id}
              onClick={() => {
                setFilter(t.id);
                setPage(0);
                clearSelection();
              }}
              className={
                'px-3 h-8 rounded text-sm whitespace-nowrap transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 ' +
                (filter === t.id
                  ? 'bg-slate-800 text-white'
                  : 'text-gray-300 hover:text-white')
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
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(0);
            }}
            placeholder="Search alerts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
      </div>

      {/* Batch actions bar */}
      {selected.size > 0 && (
        <div
          role="region"
          aria-label="Batch actions"
          className="flex items-center justify-between gap-3 rounded-md border border-blue-500/30 bg-blue-600/5 px-4 py-2"
        >
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

      {/* Table */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
        <div className="overflow-x-auto">
          <table role="table" aria-label="Alerts inbox" className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                <th className="px-3 py-3 w-10" scope="col">
                  <input
                    type="checkbox"
                    aria-label="Select all alerts on this page"
                    checked={allOnPageSelected}
                    onChange={togglePage}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40"
                  />
                </th>
                <th className="px-3 py-3 w-32" scope="col">Severity</th>
                <th className="px-3 py-3" scope="col">Title</th>
                <th className="px-3 py-3" scope="col">Agent</th>
                <th className="px-3 py-3" scope="col">Check</th>
                <th className="px-3 py-3 w-32" scope="col">State</th>
                <th className="px-3 py-3 w-36" scope="col">Created</th>
                <th className="px-3 py-3 text-right w-56" scope="col">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && alerts.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status" aria-live="polite">
                    Loading alerts…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-red-400" role="alert">
                    Failed to load alerts: {error.message}
                  </td>
                </tr>
              ) : paged.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-gray-400" role="status">
                    No alerts match the current filter.
                  </td>
                </tr>
              ) : (
                paged.map((a) => (
                  <RowItem
                    key={a.id}
                    alert={a}
                    isSelected={selected.has(a.id)}
                    onToggleSelect={() => toggleRow(a.id)}
                    onOpen={() =>
                      void navigate({ to: '/alerts/$alertId', params: { alertId: a.id } })
                    }
                    now={now}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        <div className="px-4 py-3 border-t border-slate-800 flex items-center justify-between text-sm">
          <div className="text-gray-400" aria-live="polite">
            Showing{' '}
            <span className="text-gray-300">
              {filtered.length === 0 ? 0 : currentPage * PAGE_SIZE + 1}
            </span>
            –
            <span className="text-gray-300">
              {Math.min((currentPage + 1) * PAGE_SIZE, filtered.length)}
            </span>{' '}
            of <span className="text-gray-300">{filtered.length}</span>
          </div>
          <div className="flex items-center gap-1" role="navigation" aria-label="Pagination">
            <button
              type="button"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={currentPage === 0}
              aria-label="Previous page"
              className="h-8 px-3 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-gray-300 disabled:opacity-40 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              Prev
            </button>
            <span className="px-2 text-gray-300 tabular-nums" aria-label={`Page ${currentPage + 1} of ${totalPages}`}>
              {currentPage + 1} / {totalPages}
            </span>
            <button
              type="button"
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={currentPage >= totalPages - 1}
              aria-label="Next page"
              className="h-8 px-3 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-gray-300 disabled:opacity-40 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            >
              Next
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

interface RowItemProps {
  alert: Alert;
  isSelected: boolean;
  onToggleSelect: () => void;
  onOpen: () => void;
  now: number;
}

function RowItem({ alert: a, isSelected, onToggleSelect, onOpen, now }: RowItemProps) {
  return (
    <tr
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          onOpen();
        }
      }}
      tabIndex={0}
      aria-selected={isSelected}
      className={
        'cursor-pointer transition-colors focus:outline-none focus-visible:bg-slate-800/60 ' +
        (isSelected ? 'bg-blue-600/5' : 'hover:bg-slate-800/40')
      }
    >
      <td className="px-3 py-3" onClick={(e) => e.stopPropagation()}>
        <input
          type="checkbox"
          aria-label={`Select alert ${a.title ?? a.id}`}
          checked={isSelected}
          onChange={onToggleSelect}
          className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40"
        />
      </td>
      <td className="px-3 py-3">
        <SeverityBadge severity={a.severity} />
      </td>
      <td className="px-3 py-3">
        <div className="flex flex-col">
          <span className="text-white font-medium truncate max-w-md">{a.title}</span>
          {a.message && (
            <span className="text-xs text-gray-400 truncate max-w-md">{a.message}</span>
          )}
        </div>
      </td>
      <td className="px-3 py-3">
        {a.hostname ? (
          <span className="inline-flex items-center gap-1.5 text-gray-300">
            <Bot className="h-3.5 w-3.5 text-gray-400" aria-hidden="true" />
            <span className="truncate max-w-[10rem]">{a.hostname}</span>
          </span>
        ) : (
          <span className="text-gray-400" aria-hidden="true">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        {a.check_name ? (
          <span className="inline-flex items-center gap-1.5 text-gray-300">
            <Activity className="h-3.5 w-3.5 text-gray-400" aria-hidden="true" />
            <span className="truncate max-w-[10rem]">{a.check_name}</span>
          </span>
        ) : (
          <span className="text-gray-400" aria-hidden="true">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        <StateBadge state={a.state} />
      </td>
      <td className="px-3 py-3 text-gray-300" title={formatTime(a.created_at)}>
        {formatRelative(a.created_at, now)}
      </td>
      <td className="px-3 py-3 text-right" onClick={(e) => e.stopPropagation()}>
        <InlineActions alert={a} />
      </td>
    </tr>
  );
}

function InlineActions({ alert: a }: { alert: Alert }) {
  const { acknowledgeAlert, snoozeAlert, resolveAlert, closeAlert } = useAlerts('all');
  const [snoozeOpen, setSnoozeOpen] = useState(false);

  const state = (a.state ?? 'open').toLowerCase();

  if (state === 'resolved' || state === 'closed') {
    return (
      <div className="inline-flex items-center gap-1" role="group" aria-label={`Actions for alert ${a.title ?? a.id}`}>
        <button
          type="button"
          onClick={() => void acknowledgeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          aria-label={`Re-open alert ${a.title ?? a.id}`}
        >
          <Eye className="h-3.5 w-3.5" aria-hidden="true" />
          <span>Reopen</span>
        </button>
        {state === 'resolved' && (
          <button
            type="button"
            onClick={() => void closeAlert(a.id)}
            className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
            aria-label={`Close alert ${a.title ?? a.id}`}
          >
            <X className="h-3.5 w-3.5" aria-hidden="true" />
            <span>Close</span>
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="inline-flex items-center gap-1 relative" role="group" aria-label={`Actions for alert ${a.title ?? a.id}`}>
      {state === 'open' && (
        <button
          type="button"
          onClick={() => void acknowledgeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-yellow-500/10 border border-yellow-800 text-yellow-400 hover:bg-yellow-500/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          aria-label={`Acknowledge alert ${a.title ?? a.id}`}
        >
          <Check className="h-3.5 w-3.5" aria-hidden="true" />
          <span>Ack</span>
        </button>
      )}
      <div className="relative">
        <button
          type="button"
          onClick={() => setSnoozeOpen((v) => !v)}
          aria-expanded={snoozeOpen}
          aria-haspopup="menu"
          aria-label={`Snooze alert ${a.title ?? a.id}`}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-300 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Clock className="h-3.5 w-3.5" aria-hidden="true" />
          <span>Snooze</span>
        </button>
        {snoozeOpen && (
          <SnoozeMenu
            onPick={async (mins) => {
              setSnoozeOpen(false);
              await snoozeAlert(a.id, mins);
            }}
            onClose={() => setSnoozeOpen(false)}
          />
        )}
      </div>
      <button
        type="button"
        onClick={() => void resolveAlert(a.id)}
        className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-green-500/10 border border-green-800 text-green-400 hover:bg-green-500/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        aria-label={`Resolve alert ${a.title ?? a.id}`}
      >
        <CheckCheck className="h-3.5 w-3.5" aria-hidden="true" />
        <span>Resolve</span>
      </button>
      {state !== 'snoozed' && (
        <button
          type="button"
          onClick={() => void closeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-gray-400 hover:bg-slate-700 hover:text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          aria-label={`Close alert ${a.title ?? a.id}`}
        >
          <CircleDot className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

const SNOOZE_PRESETS: { label: string; mins: number }[] = [
  { label: '15 min', mins: 15 },
  { label: '1 hour', mins: 60 },
  { label: '4 hours', mins: 240 },
  { label: '24 hours', mins: 1440 },
  { label: '3 days', mins: 4320 },
];

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
      role="menu"
      aria-label="Snooze duration"
      className="absolute right-0 top-full mt-1 w-40 rounded-md border border-slate-700 bg-slate-900 shadow-xl py-1 z-20"
    >
      {SNOOZE_PRESETS.map((p) => (
        <button
          key={p.mins}
          type="button"
          role="menuitem"
          onClick={() => void onPick(p.mins)}
          className="w-full text-left px-3 py-1.5 text-sm text-gray-300 hover:bg-slate-800 hover:text-white focus:outline-none focus-visible:bg-slate-800 focus-visible:text-white transition-colors"
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}
