// Assigned-agents table for the check detail page.
import { Bot } from 'lucide-react';
import type { AgentAssignment, CheckStatus } from '@/lib/useChecks';
import { statusIcon, statusColor } from './check_detail_helpers';
import { formatTime } from './check_detail_components';
import { CircleDashed } from 'lucide-react';

interface Props {
  assignments: AgentAssignment[];
  now: number;
  onUnassign: (agentId: string) => void;
}

export function CheckAssignedAgentsTable({ assignments, now, onUnassign }: Props) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900">
      <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-white">Assigned Agents</h2>
        <span className="text-xs text-gray-400">{assignments.length} agent{assignments.length === 1 ? '' : 's'}</span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800 bg-slate-800">
              <th className="px-4 py-3">Agent</th>
              <th className="px-4 py-3">Last Result</th>
              <th className="px-4 py-3">Last Run</th>
              <th className="px-4 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800">
            {assignments.length === 0 ? (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-gray-400">No agents assigned yet.</td>
              </tr>
            ) : (
              assignments.map((a) => {
                const sk = (a.last_status as CheckStatus) ?? 'disabled';
                const SIcon = statusIcon[sk] ?? CircleDashed;
                return (
                  <tr key={a.id ?? a.agent_id} className="hover:bg-slate-800/40">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Bot className="h-4 w-4 text-gray-400" />
                        <span className="text-white font-medium">{a.hostname ?? a.agent_id}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <span className={'inline-flex items-center gap-1.5 text-xs ' + (statusColor[sk] ?? 'text-gray-300')}>
                        <SIcon className="h-3.5 w-3.5" />
                        <span className="capitalize">{sk}</span>
                      </span>
                    </td>
                    <td className="px-4 py-3 text-gray-300">{formatTime(a.last_run, now)}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        type="button"
                        onClick={() => void onUnassign(a.agent_id)}
                        className="px-2 h-7 rounded text-xs text-red-400 hover:bg-red-500/10 border border-red-800"
                      >
                        Remove
                      </button>
                    </td>
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
