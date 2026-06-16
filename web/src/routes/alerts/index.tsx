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
  open: { label: 'Open', classes: 'bg-rose-500/10 text-rose-300 border-rose-500/20' },
  acknowledged: {
    label: 'Acknowledged',
    classes: 'bg-amber-500/10 text-amber-300 border-amber-500/20',
  },
  snoozed: {
    label: 'Snoozed',
    classes: 'bg-slate-500/10 text-slate-300 border-slate-500/30',
  },
  resolved: {
    label: 'Resolved',
    classes: 'bg-emerald-500/10 text-emerald-300 border-emerald-500/20',
  },
  closed: {
    label: 'Closed',
    classes: 'bg-slate-700/30 text-slate-400 border-slate-600/30',
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
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <div className="h-9 w-9 rounded-md bg-rose-500/10 border border-rose-500/20 flex items-center justify-center">
            <BellRing className="h-4 w-4 text-rose-400" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-100">Alerts</h1>
            <p className="text-slate-400 text-sm mt-0.5">
              Active and historical alerts across your fleet.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'inline-flex h-2 w-2 rounded-full ' +
              (status === 'open' ? 'bg-emerald-500' : status === 'connecting' ? 'bg-amber-500' : 'bg-slate-500')
            }
            title={`WebSocket: ${status}`}
          />
          <button
            type="button"
            onClick={toggleSound}
            title={
              soundOn
                ? 'Mute critical-alert notifications'
                : 'Enable critical-alert browser notifications'
            }
            className="inline-flex items-center justify-center h-9 w-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-slate-300 transition-colors"
          >
            {soundOn ? <Volume2 className="h-4 w-4" /> : <VolumeX className="h-4 w-4" />}
          </button>
          <button
            type="button"
            onClick={() => {
              void refresh();
            }}
            disabled={isLoading}
            className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-slate-200 disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={'h-4 w-4 ' + (isLoading ? 'animate-spin' : '')} />
            <span>Refresh</span>
          </button>
        </div>
      </div>

      {/* Tabs + search */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-1 p-1 rounded-md bg-slate-900 border border-slate-800 overflow-x-auto">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                setFilter(t.id);
                setPage(0);
                clearSelection();
              }}
              className={
                'px-3 h-8 rounded text-sm whitespace-nowrap transition-colors ' +
                (filter === t.id
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:text-slate-200')
              }
            >
              {t.label}
              <span className="ml-2 text-xs text-slate-500">{counts[t.id]}</span>
            </button>
          ))}
        </div>

        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <input
            type="search"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(0);
            }}
            placeholder="Search alerts…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-slate-200 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500/40"
          />
        </div>
      </div>

      {/* Batch actions bar */}
      {selected.size > 0 && (
        <div className="flex items-center justify-between gap-3 rounded-md border border-indigo-500/30 bg-indigo-500/5 px-4 py-2">
          <div className="text-sm text-slate-200">
            <span className="font-medium">{selected.size}</span> selected
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('ack')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-amber-500/15 border border-amber-500/30 text-amber-200 text-sm hover:bg-amber-500/25 disabled:opacity-50 transition-colors"
            >
              <Check className="h-3.5 w-3.5" />
              <span>Acknowledge all</span>
            </button>
            <button
              type="button"
              disabled={batchBusy}
              onClick={() => void runBatch('resolve')}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-emerald-500/15 border border-emerald-500/30 text-emerald-200 text-sm hover:bg-emerald-500/25 disabled:opacity-50 transition-colors"
            >
              <CheckCheck className="h-3.5 w-3.5" />
              <span>Resolve all</span>
            </button>
            <button
              type="button"
              onClick={clearSelection}
              className="inline-flex items-center gap-1.5 px-3 h-8 rounded-md bg-slate-800 border border-slate-700 text-slate-300 text-sm hover:bg-slate-700 transition-colors"
            >
              <X className="h-3.5 w-3.5" />
              <span>Clear</span>
            </button>
          </div>
        </div>
      )}

      {/* Table */}
      <div className="rounded-lg border border-slate-800 bg-slate-900/60 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-slate-500 border-b border-slate-800 bg-slate-900/40">
                <th className="px-3 py-3 w-10">
                  <input
                    type="checkbox"
                    aria-label="Select all on this page"
                    checked={allOnPageSelected}
                    onChange={togglePage}
                    className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500/40"
                  />
                </th>
                <th className="px-3 py-3 w-32">Severity</th>
                <th className="px-3 py-3">Title</th>
                <th className="px-3 py-3">Agent</th>
                <th className="px-3 py-3">Check</th>
                <th className="px-3 py-3 w-32">State</th>
                <th className="px-3 py-3 w-36">Created</th>
                <th className="px-3 py-3 text-right w-56">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading && alerts.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-slate-500">
                    Loading alerts…
                  </td>
                </tr>
              ) : error ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-rose-400">
                    Failed to load alerts: {error.message}
                  </td>
                </tr>
              ) : paged.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-slate-500">
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
          <div className="text-slate-500">
            Showing{' '}
            <span className="text-slate-300">
              {filtered.length === 0 ? 0 : currentPage * PAGE_SIZE + 1}
            </span>
            –
            <span className="text-slate-300">
              {Math.min((currentPage + 1) * PAGE_SIZE, filtered.length)}
            </span>{' '}
            of <span className="text-slate-300">{filtered.length}</span>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setPage((p) => Math.max(0, p - 1))}
              disabled={currentPage === 0}
              className="h-8 px-3 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-slate-300 disabled:opacity-40 hover:bg-slate-700 transition-colors"
            >
              Prev
            </button>
            <span className="px-2 text-slate-400 tabular-nums">
              {currentPage + 1} / {totalPages}
            </span>
            <button
              type="button"
              onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
              disabled={currentPage >= totalPages - 1}
              className="h-8 px-3 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-slate-300 disabled:opacity-40 hover:bg-slate-700 transition-colors"
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
      className={
        'cursor-pointer transition-colors ' +
        (isSelected ? 'bg-indigo-500/5' : 'hover:bg-slate-800/40')
      }
    >
      <td className="px-3 py-3" onClick={(e) => e.stopPropagation()}>
        <input
          type="checkbox"
          aria-label={`Select alert ${a.id}`}
          checked={isSelected}
          onChange={onToggleSelect}
          className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-indigo-500 focus:ring-indigo-500/40"
        />
      </td>
      <td className="px-3 py-3">
        <SeverityBadge severity={a.severity} />
      </td>
      <td className="px-3 py-3">
        <div className="flex flex-col">
          <span className="text-slate-100 font-medium truncate max-w-md">{a.title}</span>
          {a.message && (
            <span className="text-xs text-slate-500 truncate max-w-md">{a.message}</span>
          )}
        </div>
      </td>
      <td className="px-3 py-3">
        {a.hostname ? (
          <span className="inline-flex items-center gap-1.5 text-slate-300">
            <Bot className="h-3.5 w-3.5 text-slate-500" />
            <span className="truncate max-w-[10rem]">{a.hostname}</span>
          </span>
        ) : (
          <span className="text-slate-500">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        {a.check_name ? (
          <span className="inline-flex items-center gap-1.5 text-slate-300">
            <Activity className="h-3.5 w-3.5 text-slate-500" />
            <span className="truncate max-w-[10rem]">{a.check_name}</span>
          </span>
        ) : (
          <span className="text-slate-500">—</span>
        )}
      </td>
      <td className="px-3 py-3">
        <StateBadge state={a.state} />
      </td>
      <td className="px-3 py-3 text-slate-400" title={formatTime(a.created_at)}>
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
      <div className="inline-flex items-center gap-1">
        <button
          type="button"
          onClick={() => void acknowledgeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors"
          title="Re-open (acknowledge)"
        >
          <Eye className="h-3.5 w-3.5" />
          <span>Reopen</span>
        </button>
        {state === 'resolved' && (
          <button
            type="button"
            onClick={() => void closeAlert(a.id)}
            className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors"
            title="Close"
          >
            <X className="h-3.5 w-3.5" />
            <span>Close</span>
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="inline-flex items-center gap-1 relative">
      {state === 'open' && (
        <button
          type="button"
          onClick={() => void acknowledgeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-amber-500/10 border border-amber-500/30 text-amber-300 hover:bg-amber-500/20 transition-colors"
          title="Acknowledge"
        >
          <Check className="h-3.5 w-3.5" />
          <span>Ack</span>
        </button>
      )}
      <div className="relative">
        <button
          type="button"
          onClick={() => setSnoozeOpen((v) => !v)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-300 hover:bg-slate-700 transition-colors"
          title="Snooze"
        >
          <Clock className="h-3.5 w-3.5" />
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
        className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 hover:bg-emerald-500/20 transition-colors"
        title="Resolve"
      >
        <CheckCheck className="h-3.5 w-3.5" />
        <span>Resolve</span>
      </button>
      {state !== 'snoozed' && (
        <button
          type="button"
          onClick={() => void closeAlert(a.id)}
          className="inline-flex items-center gap-1 px-2 h-7 rounded-md text-xs bg-slate-800 border border-slate-700 text-slate-400 hover:bg-slate-700 hover:text-slate-200 transition-colors"
          title="Close"
        >
          <CircleDot className="h-3.5 w-3.5" />
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
      className="absolute right-0 top-full mt-1 w-40 rounded-md border border-slate-700 bg-slate-900 shadow-xl py-1 z-20"
    >
      {SNOOZE_PRESETS.map((p) => (
        <button
          key={p.mins}
          type="button"
          onClick={() => void onPick(p.mins)}
          className="w-full text-left px-3 py-1.5 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}
