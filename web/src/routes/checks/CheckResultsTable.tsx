// Recent-results table for the check detail page.
import { Link } from '@tanstack/react-router';
import type { CheckResult, CheckStatus } from '@/lib/useChecks';
import { statusIcon, statusColor } from './check_detail_helpers';
import { formatDateTime } from './check_detail_components';
import { CircleDashed } from 'lucide-react';

interface Props {
  results: CheckResult[];
}

export function CheckResultsTable({ results }: Props) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-white">Recent Results</h2>
        <span className="text-xs text-gray-400">Last 20</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
              <th className="px-4 py-3">Time</th>
              <th className="px-4 py-3">Agent</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3 text-right">Value</th>
              <th className="px-4 py-3 text-right">Duration</th>
              <th className="px-4 py-3">Message</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {results.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-gray-400">No results yet.</td>
              </tr>
            ) : (
              results.map((r, idx) => {
                const sk = (r.status as CheckStatus) ?? 'disabled';
                const SIcon = statusIcon[sk] ?? CircleDashed;
                return (
                  <tr key={r.id ?? `${r.timestamp}-${idx}`} className="hover:bg-slate-800/40">
                    <td className="px-4 py-3 text-gray-300">{formatDateTime(r.timestamp)}</td>
                    <td className="px-4 py-3">
                      <Link to="/agents/$agentId" params={{ agentId: r.agent_id }} className="text-white hover:text-blue-400">
                        {r.agent_id}
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <span className={'inline-flex items-center gap-1.5 text-xs ' + (statusColor[sk] ?? 'text-gray-300')}>
                        <SIcon className="h-3.5 w-3.5" />
                        <span className="capitalize">{sk}</span>
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-white">
                      {r.value !== undefined && r.value !== null ? String(r.value) : '—'}
                    </td>
                    <td className="px-4 py-3 text-right tabular-nums text-gray-300">
                      {r.duration_ms !== undefined ? `${r.duration_ms}ms` : '—'}
                    </td>
                    <td className="px-4 py-3 text-gray-300 truncate max-w-md">{r.message ?? '—'}</td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
