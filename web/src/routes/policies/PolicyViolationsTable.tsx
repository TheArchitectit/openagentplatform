// Violations table for the policy detail page.
import { Link } from '@tanstack/react-router';
import type { PolicyViolation } from '@/lib/usePolicies';
import { SeverityBadge } from '@/components/severity-badge';
import { formatTimestamp } from './policy_detail_helpers';

interface Props {
  violations: PolicyViolation[];
  now: number;
  onDismiss: (id: string) => void;
}

export function PolicyViolationsTable({ violations, now, onDismiss }: Props) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
        <h2 className="text-sm font-semibold text-white">
          Recent violations{' '}
          <span className="text-xs text-gray-400 ml-1">({violations.length})</span>
        </h2>
      </div>
      {violations.length === 0 ? (
        <div className="px-5 py-8 text-center text-sm text-gray-400">
          No violations recorded.
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                <th className="px-4 py-2.5">Status</th>
                <th className="px-4 py-2.5">Severity</th>
                <th className="px-4 py-2.5">Agent</th>
                <th className="px-4 py-2.5">Message</th>
                <th className="px-4 py-2.5">Detected</th>
                <th className="px-4 py-2.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {violations.map((v) => (
                <tr key={v.id} className="hover:bg-slate-800/40">
                  <td className="px-4 py-2.5">
                    <span
                      className={
                        'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                        (v.status === 'open'
                          ? 'bg-red-500/10 text-red-400 border-red-800'
                          : v.status === 'dismissed'
                          ? 'bg-slate-500/10 text-gray-300 border-slate-700'
                          : v.status === 'resolved'
                          ? 'bg-green-500/10 text-green-400 border-green-800'
                          : 'bg-yellow-500/10 text-yellow-400 border-yellow-800')
                      }
                    >
                      {v.status}
                    </span>
                  </td>
                  <td className="px-4 py-2.5">
                    <SeverityBadge severity={v.severity} />
                  </td>
                  <td className="px-4 py-2.5">
                    {v.agent_id ? (
                      <Link
                        to="/agents/$agentId"
                        params={{ agentId: v.agent_id }}
                        className="text-white hover:text-blue-400 transition-colors"
                      >
                        {v.hostname ?? v.agent_id}
                      </Link>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-gray-300 text-xs max-w-xs truncate">
                    {v.message ?? '—'}
                  </td>
                  <td className="px-4 py-2.5 text-gray-300 text-xs">
                    {formatTimestamp(v.detected_at, now)}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    {v.status === 'open' && (
                      <button
                        type="button"
                        onClick={() => void onDismiss(v.id)}
                        className="text-xs text-gray-300 hover:text-white transition-colors"
                      >
                        Dismiss
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
