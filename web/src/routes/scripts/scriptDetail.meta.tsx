// Script detail page — static metadata maps + run-history table. Extracted
// from ScriptDetailPage.tsx to keep that file under the size gate.

import {
  Terminal,
  Code2,
  Braces,
  CircleDashed,
  CirclePlay,
  CircleCheck,
  CircleX,
  CircleAlert,
} from 'lucide-react';
import {
  formatTime,
  formatDuration,
} from './script_helpers';
import type { ScriptRuntime, ScriptRun, ScriptRunStatus } from '@/lib/useScripts';

export const RUNTIME_META: Record<
  ScriptRuntime,
  { label: string; icon: typeof Terminal; classes: string }
> = {
  bash: {
    label: 'Bash',
    icon: Terminal,
    classes: 'bg-green-500/10 text-green-400 border-green-800',
  },
  powershell: {
    label: 'PowerShell',
    icon: Terminal,
    classes: 'bg-blue-500/10 text-blue-400 border-blue-800',
  },
  python: {
    label: 'Python',
    icon: Code2,
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  },
  node: {
    label: 'Node',
    icon: Braces,
    classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20',
  },
};

export const STATUS_META: Record<
  ScriptRunStatus,
  { label: string; classes: string; icon: typeof CircleCheck }
> = {
  pending: {
    label: 'Pending',
    classes: 'bg-slate-500/10 text-gray-300 border-slate-700',
    icon: CircleDashed,
  },
  in_progress: {
    label: 'Running',
    classes: 'bg-blue-600/10 text-blue-400 border-blue-500/20',
    icon: CirclePlay,
  },
  completed: {
    label: 'Success',
    classes: 'bg-green-500/10 text-green-400 border-green-800',
    icon: CircleCheck,
  },
  failed: {
    label: 'Failed',
    classes: 'bg-red-500/10 text-red-400 border-red-800',
    icon: CircleX,
  },
  cancelled: {
    label: 'Cancelled',
    classes: 'bg-slate-500/10 text-gray-300 border-slate-700',
    icon: CircleDashed,
  },
  timeout: {
    label: 'Timeout',
    classes: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
    icon: CircleAlert,
  },
};

export function RunHistoryTable({ runs }: { runs: ScriptRun[] }) {
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900/50 overflow-hidden">
      <div className="px-4 h-11 flex items-center border-b border-slate-800 text-sm font-medium text-slate-300">
        Run History ({runs.length})
      </div>
      {runs.length === 0 ? (
        <div className="p-6 text-sm text-slate-500 text-center">No runs yet.</div>
      ) : (
        <table className="w-full text-sm">
          <thead className="text-xs text-slate-500 border-b border-slate-800">
            <tr>
              <th className="text-left font-medium px-4 py-2">Agent</th>
              <th className="text-left font-medium px-4 py-2">Status</th>
              <th className="text-left font-medium px-4 py-2">Started</th>
              <th className="text-left font-medium px-4 py-2">Duration</th>
              <th className="text-left font-medium px-4 py-2">Exit</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => {
              const sm = STATUS_META[r.status];
              return (
                <tr key={r.id} className="border-b border-slate-800/50 last:border-0">
                  <td className="px-4 py-2 text-slate-300">{r.hostname ?? r.agent_id}</td>
                  <td className="px-4 py-2">
                    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs border ${sm?.classes ?? ''}`}>
                      {sm && <sm.icon className="h-3 w-3" />} {r.status}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-slate-400">{formatTime(r.started_at)}</td>
                  <td className="px-4 py-2 text-slate-400">{formatDuration(r.duration_ms)}</td>
                  <td className="px-4 py-2 text-slate-400">{r.exit_code ?? '—'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </div>
  );
}
