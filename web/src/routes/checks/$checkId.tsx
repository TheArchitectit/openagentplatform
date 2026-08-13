import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft,
  Activity,
  Play,
  Trash2,
  Plus,
  Bot,
  Loader2,
  Save,
  Power,
  X,
  CircleDashed,
} from 'lucide-react';
import { toast } from 'sonner';
import { useAgents, type Agent } from '@/lib/useAgents';
import {
  useChecks,
  type Check,
  type CheckResult,
  type CheckStatus,
  type CheckType,
  type AgentAssignment,
} from '@/lib/useChecks';
import { MonacoEditor } from '@/components/monaco-editor';

export const Route = createFileRoute('/checks/$checkId')({
  component: CheckDetailPage,
});

import {
  TypeIcon,
  statusIcon,
  statusBg,
  typeIcon,
  typeLabel,
} from './check_detail_helpers';
import { ResultBarChart, AssignAgentModal, EditCheckModal, formatTime, formatInterval, deriveStatus } from './check_detail_components'
import { useCheckDetail } from './useCheckDetail'
import { CheckAssignedAgentsTable } from './CheckAssignedAgentsTable'
import { CheckResultsTable } from './CheckResultsTable'

function CheckDetailPage() {
  const { checkId } = Route.useParams();
  const {
    check, assignments, results, isLoading, error, now,
    showAssign, showEdit, busy, agents,
    setShowAssign, setShowEdit,
    onToggleEnabled, onRunNow, onDelete, onSaveEdit, onAssign, onUnassign, reload,
  } = useCheckDetail(checkId);
  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link to="/checks" className="p-2 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 transition-colors">
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center">
            <TypeIcon className="h-4 w-4 text-gray-300" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold text-white">
                {isLoading && !check ? 'Loading…' : check?.name ?? 'Unknown check'}
              </h1>
              {check && (
                <span className={'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-xs ' + statusBg[deriveStatus(check)]}>
                  <TypeIcon className="h-3 w-3" />
                  {deriveStatus(check)}
                </span>
              )}
            </div>
            <p className="text-gray-300 text-sm mt-0.5">
              {check ? (
                <>
                  <span className="text-gray-300">{typeLabel[check.type]}</span>
                  <span className="mx-2 text-gray-400">•</span>
                  Runs every {formatInterval(check.interval_secs)}
                  <span className="mx-2 text-gray-400">•</span>
                  Last run {formatTime(check.last_run, now)}
                </>
              ) : (' ')}
            </p>
          </div>
        </div>

        {check && (
          <div className="flex items-center gap-2 flex-wrap">
            <button
              type="button"
              onClick={onToggleEnabled}
              disabled={busy}
              className={
                'inline-flex items-center gap-2 px-3 h-9 rounded-md border text-sm transition-colors disabled:opacity-50 ' +
                (check.enabled
                  ? 'border-green-800 bg-green-500/10 text-green-400 hover:bg-green-500/20'
                  : 'border-slate-700 bg-slate-800 text-gray-300 hover:bg-slate-700')
              }
            >
              <Power className="h-4 w-4" />
              <span>{check.enabled ? 'Enabled' : 'Disabled'}</span>
            </button>
            <button
              type="button"
              onClick={onRunNow}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 transition-colors"
            >
              <Play className="h-4 w-4" />
              <span>Run Now</span>
            </button>
            <button
              type="button"
              onClick={() => setShowEdit(true)}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white disabled:opacity-50 transition-colors"
            >
              <Save className="h-4 w-4" />
              <span>Edit</span>
            </button>
            <button
              type="button"
              onClick={() => setShowAssign(true)}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 text-sm text-white disabled:opacity-50 transition-colors"
            >
              <Plus className="h-4 w-4" />
              <span>Assign Agent</span>
            </button>
            <button
              type="button"
              onClick={onDelete}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-red-800 bg-red-500/10 text-red-400 hover:bg-red-500/20 text-sm disabled:opacity-50 transition-colors"
            >
              <Trash2 className="h-4 w-4" />
              <span>Delete</span>
            </button>
          </div>
        )}
      </div>

      {error && (
        <div className="rounded-md border border-red-800 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {isLoading && !check ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400">
          <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
          Loading check…
        </div>
      ) : !check ? (
        <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400">
          Check not found.
        </div>
      ) : (
        <>
          {/* Info card */}
          <div className="rounded-lg border border-slate-800 bg-slate-900 p-5">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div>
                <p className="text-xs uppercase tracking-wider text-gray-400">Name</p>
                <p className="text-sm text-white mt-1">{check.name}</p>
              </div>
              <div>
                <p className="text-xs uppercase tracking-wider text-gray-400">Type</p>
                <p className="text-sm text-white mt-1">{typeLabel[check.type]}</p>
              </div>
              <div>
                <p className="text-xs uppercase tracking-wider text-gray-400">Interval</p>
                <p className="text-sm text-white mt-1">{formatInterval(check.interval_secs)}</p>
              </div>
            </div>
            <div className="mt-4 pt-4 border-t border-slate-800">
              <p className="text-xs uppercase tracking-wider text-gray-400 mb-2">Configuration</p>
              <pre className="rounded-md bg-slate-950/60 border border-slate-800 p-3 text-xs text-gray-300 overflow-x-auto">
{JSON.stringify(check.config ?? {}, null, 2)}
              </pre>
            </div>
          </div>

          <CheckAssignedAgentsTable
            assignments={assignments}
            now={now}
            onUnassign={(agentId) => void onUnassign(agentId)}
          />

          {/* Result history bar chart */}
          <div className="rounded-lg border border-slate-800 bg-slate-900 p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-white">Result History</h2>
              <span className="text-xs text-gray-400">Last 20 results</span>
            </div>
            <ResultBarChart results={results} />
          </div>

          <CheckResultsTable results={results} />
        </>
      )}

      {/* Assign Agent modal */}
      {showAssign && check && (
        <AssignAgentModal
          agents={agents}
          assignedIds={new Set(assignments.map((a) => a.agent_id))}
          onClose={() => setShowAssign(false)}
          onAssign={async (agentId) => { await onAssign(agentId); }}
        />
      )}

      {/* Edit modal */}
      {showEdit && check && (
        <EditCheckModal check={check} onClose={() => setShowEdit(false)} onSubmit={onSaveEdit} />
      )}
    </div>
  );
}
