import { StateBadge, formatTime } from './alert_detail_components';
import { formatRelative } from './alertPage.helpers';
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


interface RowItemProps {
  alert: Alert;
  isSelected: boolean;
  onToggleSelect: () => void;
  onOpen: () => void;
  now: number;
}

export function RowItem({ alert: a, isSelected, onToggleSelect, onOpen, now }: RowItemProps) {
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

export function InlineActions({ alert: a }: { alert: Alert }) {
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

export function SnoozeMenu({
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
