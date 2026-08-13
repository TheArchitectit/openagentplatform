// Alerts inbox table + pagination — extracted from alerts/index.tsx.
import { RowItem } from './alert_components';
import { PAGE_SIZE } from './alertPage.helpers';
import type { Alert } from '@/lib/useAlerts';

interface Props {
  isLoading: boolean;
  error: Error | null;
  filtered: Alert[];
  paged: Alert[];
  selected: Set<string>;
  currentPage: number;
  totalPages: number;
  now: number;
  onToggleRow: (id: string) => void;
  onNavigate: (alertId: string) => void;
  onTogglePage: () => void;
  onSetPage: (fn: (p: number) => number) => void;
}

export function AlertsTable({
  isLoading, error, filtered, paged, selected, currentPage, totalPages, now,
  onToggleRow, onNavigate, onTogglePage, onSetPage,
}: Props) {
  return (
    <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
      <div className="overflow-x-auto">
        <table role="table" aria-label="Alerts inbox" className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
              <th className="px-3 py-3 w-10" scope="col">
                <input
                  type="checkbox"
                  aria-label="Select all alerts on this page"
                  checked={paged.length > 0 && paged.every((a) => selected.has(a.id))}
                  onChange={onTogglePage}
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
            {isLoading && paged.length === 0 ? (
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
              paged.map((a: Alert) => (
                <RowItem
                  key={a.id}
                  alert={a}
                  isSelected={selected.has(a.id)}
                  onToggleSelect={() => onToggleRow(a.id)}
                  onOpen={() => onNavigate(a.id)}
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
            onClick={() => onSetPage((p) => Math.max(0, p - 1))}
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
            onClick={() => onSetPage((p) => Math.min(totalPages - 1, p + 1))}
            disabled={currentPage >= totalPages - 1}
            aria-label="Next page"
            className="h-8 px-3 inline-flex items-center justify-center rounded-md border border-slate-700 bg-slate-800 text-gray-300 disabled:opacity-40 hover:bg-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
          >
            Next
          </button>
        </div>
      </div>
    </div>
  );
}
