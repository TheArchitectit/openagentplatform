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
  CircleCheck,
  CircleAlert,
  CircleX,
  CircleDashed,
  Globe,
  Network,
  HardDrive,
  Cpu,
  MemoryStick,
  ServerCog,
  FileCode2,
  ScrollText,
  ShieldCheck,
  Radio,
  Save,
  Power,
  X,
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


import { ResultBarChart, AssignAgentModal, EditCheckModal, formatTime, formatInterval, formatDateTime, deriveStatus } from './check_detail_components'

// ---------------------------------------------------------------------------
// Display helpers (mirrored from the list page; kept local for self-containment)
// ---------------------------------------------------------------------------

const statusIcon: Record<CheckStatus, typeof CircleCheck> = {
  ok: CircleCheck,
  warning: CircleAlert,
  critical: CircleX,
  disabled: CircleDashed,
};

const statusColor: Record<CheckStatus, string> = {
  ok: 'text-green-400',
  warning: 'text-yellow-400',
  critical: 'text-red-400',
  disabled: 'text-gray-400',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-green-500/10 text-green-400 border-green-800',
  warning: 'bg-yellow-500/10 text-yellow-400 border-yellow-800',
  critical: 'bg-red-500/10 text-red-400 border-red-800',
  disabled: 'bg-slate-500/10 text-gray-300 border-slate-700',
};

const typeIcon: Record<CheckType, typeof Globe> = {
  http: Globe,
  tcp: Network,
  ping: Radio,
  disk_usage: HardDrive,
  memory_usage: MemoryStick,
  cpu_usage: Cpu,
  process: ServerCog,
  service: ServerCog,
  tls_cert: ShieldCheck,
  script: FileCode2,
  log_watch: ScrollText,
};

const typeLabel: Record<CheckType, string> = {
  http: 'HTTP',
  tcp: 'TCP',
  ping: 'Ping',
  disk_usage: 'Disk Usage',
  memory_usage: 'Memory Usage',
  cpu_usage: 'CPU Usage',
  process: 'Process',
  service: 'Service',
  tls_cert: 'TLS Certificate',
  script: 'Script',
  log_watch: 'Log Watch',
};


function CheckDetailPage() {
  const { checkId } = Route.useParams();
  const navigate = useNavigate();

  const { fetchCheck, updateCheck, deleteCheck, runCheck, assignAgent, unassignAgent, fetchResults, fetchAssignments } = useChecks();
  const { agents } = useAgents();

  const [check, setCheck] = useState<Check | null>(null);
  const [assignments, setAssignments] = useState<AgentAssignment[]>([]);
  const [results, setResults] = useState<CheckResult[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [showAssign, setShowAssign] = useState(false);
  const [showEdit, setShowEdit] = useState(false);
  const [busy, setBusy] = useState(false);

  // Keep relative times fresh.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const reload = useCallback(async () => {
    try {
      const [c, a, r] = await Promise.all([
        fetchCheck(checkId),
        fetchAssignments(checkId).catch(() => [] as AgentAssignment[]),
        fetchResults(checkId, 20).catch(() => [] as CheckResult[]),
      ]);
      setCheck(c);
      setAssignments(a);
      setResults(r);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIsLoading(false);
    }
  }, [checkId, fetchCheck, fetchAssignments, fetchResults]);

  useEffect(() => {
    setIsLoading(true);
    void reload();
  }, [reload]);

  // -----------------------------------------------------------------------
  // Actions
  // -----------------------------------------------------------------------

  const onToggleEnabled = async () => {
    if (!check) return;
    setBusy(true);
    try {
      const updated = await updateCheck(check.id, { enabled: !check.enabled });
      setCheck(updated);
      toast.success(updated.enabled ? 'Check enabled' : 'Check disabled');
    } catch (e) {
      toast.error(`Update failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const onRunNow = async () => {
    if (!check) return;
    setBusy(true);
    try {
      await runCheck(check.id);
      toast.success(`Triggered "${check.name}"`);
    } catch (e) {
      toast.error(`Run failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const onDelete = async () => {
    if (!check) return;
    if (!confirm(`Delete check "${check.name}"? This cannot be undone.`)) return;
    setBusy(true);
    try {
      await deleteCheck(check.id);
      toast.success(`Deleted "${check.name}"`);
      void navigate({ to: '/checks' });
    } catch (e) {
      toast.error(`Delete failed: ${(e as Error).message}`);
      setBusy(false);
    }
  };

  const onSaveEdit = async (patch: { name?: string; interval_secs?: number; config?: Record<string, unknown> }) => {
    if (!check) return;
    setBusy(true);
    try {
      const updated = await updateCheck(check.id, patch);
      setCheck(updated);
      toast.success('Check updated');
      setShowEdit(false);
    } catch (e) {
      toast.error(`Update failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const onAssign = async (agentId: string) => {
    if (!check) return;
    setBusy(true);
    try {
      await assignAgent(check.id, agentId);
      const a = await fetchAssignments(check.id);
      setAssignments(a);
      toast.success('Agent assigned');
    } catch (e) {
      toast.error(`Assign failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  const onUnassign = async (agentId: string) => {
    if (!check) return;
    setBusy(true);
    try {
      await unassignAgent(check.id, agentId);
      setAssignments((prev) => prev.filter((a) => a.agent_id !== agentId));
      toast.success('Agent removed');
    } catch (e) {
      toast.error(`Unassign failed: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  };

  // -----------------------------------------------------------------------
  // Render
  // -----------------------------------------------------------------------

  const k = deriveStatus(check);
  const Icon = statusIcon[k];
  const TypeIcon = check ? (typeIcon[check.type] ?? Activity) : Activity;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Link
            to="/checks"
            className="p-2 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 transition-colors"
          >
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
                <span className={'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-xs ' + statusBg[k]}>
                  <Icon className="h-3 w-3" />
                  {k}
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
              ) : (
                ' '
              )}
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

          {/* Assigned agents */}
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
                      <td colSpan={4} className="px-4 py-8 text-center text-gray-400">
                        No agents assigned yet.
                      </td>
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
                              <span className="text-white font-medium">
                                {a.hostname ?? a.agent_id}
                              </span>
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
                              onClick={() => {
                                void onUnassign(a.agent_id);
                              }}
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

          {/* Result history bar chart */}
          <div className="rounded-lg border border-slate-800 bg-slate-900 p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-white">Result History</h2>
              <span className="text-xs text-gray-400">Last 20 results</span>
            </div>
            <ResultBarChart results={results} />
          </div>

          {/* Recent results table */}
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
                      <td colSpan={6} className="px-4 py-8 text-center text-gray-400">
                        No results yet.
                      </td>
                    </tr>
                  ) : (
                    results.map((r, idx) => {
                      const sk = (r.status as CheckStatus) ?? 'disabled';
                      const SIcon = statusIcon[sk] ?? CircleDashed;
                      return (
                        <tr key={r.id ?? `${r.timestamp}-${idx}`} className="hover:bg-slate-800/40">
                          <td className="px-4 py-3 text-gray-300">{formatDateTime(r.timestamp)}</td>
                          <td className="px-4 py-3">
                            <Link
                              to="/agents/$agentId"
                              params={{ agentId: r.agent_id }}
                              className="text-white hover:text-blue-400"
                            >
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
        </>
      )}

      {/* Assign Agent modal */}
      {showAssign && check && (
        <AssignAgentModal
          agents={agents}
          assignedIds={new Set(assignments.map((a) => a.agent_id))}
          onClose={() => setShowAssign(false)}
          onAssign={async (agentId) => {
            await onAssign(agentId);
          }}
        />
      )}

      {/* Edit modal */}
      {showEdit && check && (
        <EditCheckModal
          check={check}
          onClose={() => setShowEdit(false)}
          onSubmit={onSaveEdit}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Result bar chart — green / orange / red bars by time bucket
// ---------------------------------------------------------------------------

