// Settings — Audit Log.
//
// Read-only viewer for the platform audit log. Supports filtering by actor,
// action, resource type, outcome, and date range. Click a row to expand full
// details JSON. Export to CSV.

import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Download, Filter, X, ChevronDown, ChevronRight } from 'lucide-react';
import {
  useSettings,
  type AuditEvent,
  type AuditFilter,
  type AuditOutcome,
} from '@/lib/useSettings';

export const Route = createFileRoute('/settings/audit-log')({
  component: AuditLogPage,
});

function outcomeBadgeClasses(outcome: AuditOutcome): string {
  switch (outcome) {
    case 'success':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'failure':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'denied':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function AuditLogPage() {
  const { auditEvents, isLoadingAudit, fetchAuditEvents } = useSettings();

  const [actorFilter, setActorFilter] = useState('');
  const [actionFilter, setActionFilter] = useState('');
  const [resourceFilter, setResourceFilter] = useState('');
  const [outcomeFilter, setOutcomeFilter] = useState<AuditOutcome | ''>('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');

  const [expandedId, setExpandedId] = useState<string | null>(null);

  const buildFilter = useCallback((): AuditFilter => {
    const f: AuditFilter = {};
    if (actorFilter.trim()) f.actor = actorFilter.trim();
    if (actionFilter.trim()) f.action = actionFilter.trim();
    if (resourceFilter.trim()) f.resource_type = resourceFilter.trim();
    if (outcomeFilter) f.outcome = outcomeFilter;
    if (fromDate) f.from = fromDate;
    if (toDate) f.to = toDate;
    return f;
  }, [actorFilter, actionFilter, resourceFilter, outcomeFilter, fromDate, toDate]);

  useEffect(() => {
    fetchAuditEvents(buildFilter());
  }, [buildFilter, fetchAuditEvents]);

  const handleClearFilters = useCallback(() => {
    setActorFilter('');
    setActionFilter('');
    setResourceFilter('');
    setOutcomeFilter('');
    setFromDate('');
    setToDate('');
  }, []);

  const hasFilters = useMemo(
    () =>
      actorFilter ||
      actionFilter ||
      resourceFilter ||
      outcomeFilter ||
      fromDate ||
      toDate,
    [actorFilter, actionFilter, resourceFilter, outcomeFilter, fromDate, toDate]
  );

  const handleExport = useCallback(() => {
    const headers = [
      'timestamp',
      'actor',
      'action',
      'resource_type',
      'resource_id',
      'outcome',
      'ip_address',
    ];
    const rows = auditEvents.map((e) =>
      [
        e.timestamp,
        e.actor,
        e.action,
        e.resource_type,
        e.resource_id ?? '',
        e.outcome,
        e.ip_address ?? '',
      ]
        .map((v) => `"${String(v).replace(/"/g, '""')}"`)
        .join(',')
    );
    const csv = [headers.join(','), ...rows].join('\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }, [auditEvents]);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">Audit Log</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            View and filter all activity across the platform.
          </p>
        </div>
        <button
          type="button"
          onClick={handleExport}
          disabled={auditEvents.length === 0}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 disabled:cursor-not-allowed focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Download className="h-4 w-4" />
          Export CSV
        </button>
      </div>

      {/* Table card */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        {/* Filter bar */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-slate-800 flex-wrap">
          <Filter className="h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="text"
            placeholder="Actor"
            aria-label="Filter by actor"
            value={actorFilter}
            onChange={(e) => setActorFilter(e.target.value)}
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <input
            type="text"
            placeholder="Action"
            aria-label="Filter by action"
            value={actionFilter}
            onChange={(e) => setActionFilter(e.target.value)}
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <input
            type="text"
            placeholder="Resource type"
            aria-label="Filter by resource type"
            value={resourceFilter}
            onChange={(e) => setResourceFilter(e.target.value)}
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <select
            value={outcomeFilter}
            onChange={(e) => setOutcomeFilter(e.target.value as AuditOutcome | '')}
            aria-label="Filter by outcome"
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          >
            <option value="">All outcomes</option>
            <option value="success">Success</option>
            <option value="failure">Failure</option>
            <option value="denied">Denied</option>
          </select>
          <input
            type="date"
            value={fromDate}
            onChange={(e) => setFromDate(e.target.value)}
            title="From date"
            aria-label="From date"
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <input
            type="date"
            value={toDate}
            onChange={(e) => setToDate(e.target.value)}
            title="To date"
            aria-label="To date"
            className="h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-xs text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          {hasFilters && (
            <button
              type="button"
              onClick={handleClearFilters}
              className="inline-flex items-center gap-1 h-8 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors"
            >
              <X className="h-3 w-3" /> Clear
            </button>
          )}
          <span className="ml-auto text-xs text-gray-400">
            {auditEvents.length} event{auditEvents.length === 1 ? '' : 's'}
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-2 py-2.5 w-6" />
                <th className="px-4 py-2.5 font-medium">Timestamp</th>
                <th className="px-4 py-2.5 font-medium">Actor</th>
                <th className="px-4 py-2.5 font-medium">Action</th>
                <th className="px-4 py-2.5 font-medium">Resource</th>
                <th className="px-4 py-2.5 font-medium">Outcome</th>
                <th className="px-4 py-2.5 font-medium">IP</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoadingAudit ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading audit events...
                  </td>
                </tr>
              ) : auditEvents.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-gray-400" role="status">
                    {hasFilters ? 'No events match your filters.' : 'No audit events recorded yet.'}
                  </td>
                </tr>
              ) : (
                auditEvents.map((e) => (
                  <AuditRow
                    key={e.id}
                    event={e}
                    expanded={expandedId === e.id}
                    onToggle={() => setExpandedId(expandedId === e.id ? null : e.id)}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Audit row with expandable details
// ---------------------------------------------------------------------------

function AuditRow({
  event,
  expanded,
  onToggle,
}: {
  event: AuditEvent;
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <>
      <tr onClick={onToggle} className="cursor-pointer hover:bg-slate-800/40 transition-colors">
        <td className="px-2 py-2.5">
          {expanded ? (
            <ChevronDown className="h-3 w-3 text-gray-400" />
          ) : (
            <ChevronRight className="h-3 w-3 text-gray-400" />
          )}
        </td>
        <td className="px-4 py-2.5 text-xs text-white whitespace-nowrap">
          {new Date(event.timestamp).toLocaleString()}
        </td>
        <td className="px-4 py-2.5 text-white">{event.actor}</td>
        <td className="px-4 py-2.5 font-mono text-xs text-gray-300">{event.action}</td>
        <td className="px-4 py-2.5">
          <span className="text-gray-300">{event.resource_type}</span>
          {event.resource_id && (
            <span className="ml-1.5 font-mono text-[11px] text-gray-400">
              {event.resource_id}
            </span>
          )}
        </td>
        <td className="px-4 py-2.5">
          <span
            className={`inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border ${outcomeBadgeClasses(event.outcome)}`}
          >
            {event.outcome}
          </span>
        </td>
        <td className="px-4 py-2.5 text-xs text-gray-300">
          {event.ip_address ?? '-'}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={7} className="px-4 pb-3">
            <div className="rounded-md border border-slate-800 bg-slate-800/40 p-3 text-xs text-gray-300">
              <div className="mb-2">
                <strong className="text-white">Event ID:</strong> <span className="font-mono">{event.id}</span>
              </div>
              {event.user_agent && (
                <div className="mb-2">
                  <strong className="text-white">User Agent:</strong> {event.user_agent}
                </div>
              )}
              {event.details && Object.keys(event.details).length > 0 && (
                <div>
                  <strong className="text-white">Details:</strong>
                  <pre className="mt-1 font-mono whitespace-pre-wrap text-white text-[11px]">
                    {JSON.stringify(event.details, null, 2)}
                  </pre>
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
