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
import './settings.css';

export const Route = createFileRoute('/settings/audit-log')({
  component: AuditLogPage,
});

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
    <>
      <div className="settings-page-header">
        <div>
          <h1>Audit Log</h1>
          <p>View and filter all activity across the platform.</p>
        </div>
        <button
          type="button"
          className="settings-input"
          style={{
            width: 'auto',
            height: '2.25rem',
            padding: '0 0.75rem',
            cursor: 'pointer',
            display: 'inline-flex',
            alignItems: 'center',
            gap: '0.375rem',
            background: 'rgb(99 102 241)',
            color: 'white',
            border: 'none',
            fontWeight: 500,
          }}
          onClick={handleExport}
          disabled={auditEvents.length === 0}
        >
          <Download className="h-4 w-4" />
          Export CSV
        </button>
      </div>

      <div className="settings-table-wrap">
        <div className="settings-filter-bar">
          <Filter className="h-4 w-4 text-text-muted" />
          <input
            type="text"
            className="settings-input"
            placeholder="Actor"
            value={actorFilter}
            onChange={(e) => setActorFilter(e.target.value)}
          />
          <input
            type="text"
            className="settings-input"
            placeholder="Action"
            value={actionFilter}
            onChange={(e) => setActionFilter(e.target.value)}
          />
          <input
            type="text"
            className="settings-input"
            placeholder="Resource type"
            value={resourceFilter}
            onChange={(e) => setResourceFilter(e.target.value)}
          />
          <select
            className="settings-select"
            value={outcomeFilter}
            onChange={(e) => setOutcomeFilter(e.target.value as AuditOutcome | '')}
          >
            <option value="">All outcomes</option>
            <option value="success">Success</option>
            <option value="failure">Failure</option>
            <option value="denied">Denied</option>
          </select>
          <input
            type="date"
            className="settings-input"
            value={fromDate}
            onChange={(e) => setFromDate(e.target.value)}
            title="From date"
          />
          <input
            type="date"
            className="settings-input"
            value={toDate}
            onChange={(e) => setToDate(e.target.value)}
            title="To date"
          />
          {hasFilters && (
            <button
              type="button"
              className="settings-input"
              style={{
                width: 'auto',
                height: '2rem',
                padding: '0 0.5rem',
                cursor: 'pointer',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
                fontSize: '0.75rem',
              }}
              onClick={handleClearFilters}
            >
              <X className="h-3 w-3" /> Clear
            </button>
          )}
          <div style={{ marginLeft: 'auto', fontSize: '0.8125rem', color: 'rgb(100 116 139)' }}>
            {auditEvents.length} event{auditEvents.length === 1 ? '' : 's'}
          </div>
        </div>

        <table className="settings-table">
          <thead>
            <tr>
              <th style={{ width: '1.5rem' }} />
              <th>Timestamp</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Resource</th>
              <th>Outcome</th>
              <th>IP</th>
            </tr>
          </thead>
          <tbody>
            {isLoadingAudit ? (
              <tr className="empty-row">
                <td colSpan={7}>Loading audit events...</td>
              </tr>
            ) : auditEvents.length === 0 ? (
              <tr className="empty-row">
                <td colSpan={7}>
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
    </>
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
      <tr onClick={onToggle} style={{ cursor: 'pointer' }}>
        <td>
          {expanded ? (
            <ChevronDown className="h-3 w-3 text-text-muted" />
          ) : (
            <ChevronRight className="h-3 w-3 text-text-muted" />
          )}
        </td>
        <td style={{ fontSize: '0.75rem', whiteSpace: 'nowrap' }}>
          {new Date(event.timestamp).toLocaleString()}
        </td>
        <td style={{ color: 'rgb(241 245 249)' }}>{event.actor}</td>
        <td style={{ fontFamily: 'monospace', fontSize: '0.75rem' }}>{event.action}</td>
        <td>
          <span style={{ color: 'rgb(148 163 184)' }}>{event.resource_type}</span>
          {event.resource_id && (
            <span style={{ fontFamily: 'monospace', fontSize: '0.6875rem', marginLeft: '0.375rem', color: 'rgb(100 116 139)' }}>
              {event.resource_id}
            </span>
          )}
        </td>
        <td>
          <span className={`settings-badge settings-badge--${event.outcome}`}>
            {event.outcome}
          </span>
        </td>
        <td style={{ fontSize: '0.75rem', color: 'rgb(148 163 184)' }}>
          {event.ip_address ?? '-'}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={7} style={{ padding: 0 }}>
            <div className="settings-details-panel" style={{ margin: '0 0.75rem 0.5rem' }}>
              <div style={{ marginBottom: '0.5rem' }}>
                <strong>Event ID:</strong> {event.id}
              </div>
              {event.user_agent && (
                <div style={{ marginBottom: '0.5rem' }}>
                  <strong>User Agent:</strong> {event.user_agent}
                </div>
              )}
              {event.details && Object.keys(event.details).length > 0 && (
                <div>
                  <strong>Details:</strong>
                  <pre>{JSON.stringify(event.details, null, 2)}</pre>
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
