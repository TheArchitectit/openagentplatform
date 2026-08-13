// Assignments table for the policy detail page.
import { Link } from '@tanstack/react-router';
import { Plus, X, CircleCheck, CircleAlert, CircleX } from 'lucide-react';
import type { PolicyAssignment } from '@/lib/usePolicies';
import { formatTimestamp } from './policy_detail_helpers';

interface Props {
  assignments: PolicyAssignment[];
  now: number;
  onShowPicker: () => void;
  onRemove: (agentId: string) => void;
}

export function PolicyAssignmentsTable({ assignments, now, onShowPicker, onRemove }: Props) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
        <h2 className="text-sm font-semibold text-white">
          Assignments <span className="text-xs text-gray-400 ml-1">({assignments.length})</span>
        </h2>
        <button
          type="button"
          onClick={onShowPicker}
          className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-slate-700 bg-slate-800 hover:bg-slate-700 text-gray-300 transition-colors"
        >
          <Plus className="h-3 w-3" />
          <span>Add</span>
        </button>
      </div>

      {assignments.length === 0 ? (
        <div className="px-5 py-8 text-center text-sm text-gray-400">
          No agents assigned. Click "Add" to assign this policy.
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
                <th className="px-4 py-2.5">Agent</th>
                <th className="px-4 py-2.5">Status</th>
                <th className="px-4 py-2.5">Last evaluated</th>
                <th className="px-4 py-2.5 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {assignments.map((a) => (
                <tr key={a.id ?? a.agent_id} className="hover:bg-slate-800/40">
                  <td className="px-4 py-2.5">
                    <Link
                      to="/agents/$agentId"
                      params={{ agentId: a.agent_id }}
                      className="text-white hover:text-blue-400 transition-colors"
                    >
                      {a.hostname ?? a.agent_id}
                    </Link>
                  </td>
                  <td className="px-4 py-2.5">
                    {a.compliant === true ? (
                      <span className="inline-flex items-center gap-1 text-xs text-green-400">
                        <CircleCheck className="h-3.5 w-3.5" />
                        Compliant
                      </span>
                    ) : a.compliant === false ? (
                      <span className="inline-flex items-center gap-1 text-xs text-red-400">
                        <CircleX className="h-3.5 w-3.5" />
                        Non-compliant
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-xs text-gray-400">
                        <CircleAlert className="h-3.5 w-3.5" />
                        Unknown
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-gray-300 text-xs">
                    {formatTimestamp(a.last_evaluated, now)}
                  </td>
                  <td className="px-4 py-2.5 text-right">
                    <button
                      type="button"
                      onClick={() => onRemove(a.agent_id)}
                      className="p-1 rounded text-gray-400 hover:text-red-400 transition-colors"
                      title="Remove assignment"
                    >
                      <X className="h-4 w-4" />
                    </button>
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
