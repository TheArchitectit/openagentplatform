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
  ok: 'text-success',
  warning: 'text-warning',
  critical: 'text-danger',
  disabled: 'text-text-muted',
};

const statusBg: Record<CheckStatus, string> = {
  ok: 'bg-success/10 text-success border-success/20',
  warning: 'bg-warning/10 text-warning border-warning/20',
  critical: 'bg-danger/10 text-danger border-danger/20',
  disabled: 'bg-text-muted/10 text-text-secondary border-text-muted/20',
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

function formatTime(iso?: string, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

function deriveStatus(c: Check | null | undefined): CheckStatus {
  if (!c) return 'disabled';
  if (!c.enabled) return 'disabled';
  return (c.last_status ?? 'disabled') as CheckStatus;
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

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
            className="p-2 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          >
            <ArrowLeft className="h-4 w-4" />
          </Link>
          <div className="h-9 w-9 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center">
            <TypeIcon className="h-4 w-4 text-text-secondary" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-2xl font-bold text-text-primary">
                {isLoading && !check ? 'Loading…' : check?.name ?? 'Unknown check'}
              </h1>
              {check && (
                <span className={'inline-flex items-center gap-1 px-2 py-0.5 rounded-md border text-xs ' + statusBg[k]}>
                  <Icon className="h-3 w-3" />
                  {k}
                </span>
              )}
            </div>
            <p className="text-text-secondary text-sm mt-0.5">
              {check ? (
                <>
                  <span className="text-text-secondary">{typeLabel[check.type]}</span>
                  <span className="mx-2 text-text-muted">•</span>
                  Runs every {formatInterval(check.interval_secs)}
                  <span className="mx-2 text-text-muted">•</span>
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
                  ? 'border-success/30 bg-success/10 text-success hover:bg-success/20'
                  : 'border-border-strong bg-surface-tertiary text-text-secondary hover:bg-border-strong')
              }
            >
              <Power className="h-4 w-4" />
              <span>{check.enabled ? 'Enabled' : 'Disabled'}</span>
            </button>
            <button
              type="button"
              onClick={onRunNow}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 transition-colors"
            >
              <Play className="h-4 w-4" />
              <span>Run Now</span>
            </button>
            <button
              type="button"
              onClick={() => setShowEdit(true)}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary disabled:opacity-50 transition-colors"
            >
              <Save className="h-4 w-4" />
              <span>Edit</span>
            </button>
            <button
              type="button"
              onClick={() => setShowAssign(true)}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent text-sm text-white disabled:opacity-50 transition-colors"
            >
              <Plus className="h-4 w-4" />
              <span>Assign Agent</span>
            </button>
            <button
              type="button"
              onClick={onDelete}
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-danger/30 bg-danger/10 text-danger hover:bg-danger/20 text-sm disabled:opacity-50 transition-colors"
            >
              <Trash2 className="h-4 w-4" />
              <span>Delete</span>
            </button>
          </div>
        )}
      </div>

      {error && (
        <div className="rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {error}
        </div>
      )}

      {isLoading && !check ? (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted">
          <Loader2 className="inline h-5 w-5 animate-spin mr-2" />
          Loading check…
        </div>
      ) : !check ? (
        <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted">
          Check not found.
        </div>
      ) : (
        <>
          {/* Info card */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div>
                <p className="text-xs uppercase tracking-wider text-text-muted">Name</p>
                <p className="text-sm text-text-primary mt-1">{check.name}</p>
              </div>
              <div>
                <p className="text-xs uppercase tracking-wider text-text-muted">Type</p>
                <p className="text-sm text-text-primary mt-1">{typeLabel[check.type]}</p>
              </div>
              <div>
                <p className="text-xs uppercase tracking-wider text-text-muted">Interval</p>
                <p className="text-sm text-text-primary mt-1">{formatInterval(check.interval_secs)}</p>
              </div>
            </div>
            <div className="mt-4 pt-4 border-t border-border-subtle">
              <p className="text-xs uppercase tracking-wider text-text-muted mb-2">Configuration</p>
              <pre className="rounded-md bg-surface-primary/60 border border-border-subtle p-3 text-xs text-text-secondary overflow-x-auto">
{JSON.stringify(check.config ?? {}, null, 2)}
              </pre>
            </div>
          </div>

          {/* Assigned agents */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
            <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
              <h2 className="text-sm font-semibold text-text-primary">Assigned Agents</h2>
              <span className="text-xs text-text-muted">{assignments.length} agent{assignments.length === 1 ? '' : 's'}</span>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                    <th className="px-4 py-3">Agent</th>
                    <th className="px-4 py-3">Last Result</th>
                    <th className="px-4 py-3">Last Run</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {assignments.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="px-4 py-8 text-center text-text-muted">
                        No agents assigned yet.
                      </td>
                    </tr>
                  ) : (
                    assignments.map((a) => {
                      const sk = (a.last_status as CheckStatus) ?? 'disabled';
                      const SIcon = statusIcon[sk] ?? CircleDashed;
                      return (
                        <tr key={a.id ?? a.agent_id} className="hover:bg-surface-tertiary/40">
                          <td className="px-4 py-3">
                            <div className="flex items-center gap-2">
                              <Bot className="h-4 w-4 text-text-muted" />
                              <span className="text-text-primary font-medium">
                                {a.hostname ?? a.agent_id}
                              </span>
                            </div>
                          </td>
                          <td className="px-4 py-3">
                            <span className={'inline-flex items-center gap-1.5 text-xs ' + (statusColor[sk] ?? 'text-text-secondary')}>
                              <SIcon className="h-3.5 w-3.5" />
                              <span className="capitalize">{sk}</span>
                            </span>
                          </td>
                          <td className="px-4 py-3 text-text-secondary">{formatTime(a.last_run, now)}</td>
                          <td className="px-4 py-3 text-right">
                            <button
                              type="button"
                              onClick={() => {
                                void onUnassign(a.agent_id);
                              }}
                              className="px-2 h-7 rounded text-xs text-danger hover:bg-danger/10 border border-danger/30"
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
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-text-primary">Result History</h2>
              <span className="text-xs text-text-muted">Last 20 results</span>
            </div>
            <ResultBarChart results={results} />
          </div>

          {/* Recent results table */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
            <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
              <h2 className="text-sm font-semibold text-text-primary">Recent Results</h2>
              <span className="text-xs text-text-muted">Last 20</span>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                    <th className="px-4 py-3">Time</th>
                    <th className="px-4 py-3">Agent</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3 text-right">Value</th>
                    <th className="px-4 py-3 text-right">Duration</th>
                    <th className="px-4 py-3">Message</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-subtle">
                  {results.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="px-4 py-8 text-center text-text-muted">
                        No results yet.
                      </td>
                    </tr>
                  ) : (
                    results.map((r, idx) => {
                      const sk = (r.status as CheckStatus) ?? 'disabled';
                      const SIcon = statusIcon[sk] ?? CircleDashed;
                      return (
                        <tr key={r.id ?? `${r.timestamp}-${idx}`} className="hover:bg-surface-tertiary/40">
                          <td className="px-4 py-3 text-text-secondary">{formatDateTime(r.timestamp)}</td>
                          <td className="px-4 py-3">
                            <Link
                              to="/agents/$agentId"
                              params={{ agentId: r.agent_id }}
                              className="text-text-primary hover:text-accent"
                            >
                              {r.agent_id}
                            </Link>
                          </td>
                          <td className="px-4 py-3">
                            <span className={'inline-flex items-center gap-1.5 text-xs ' + (statusColor[sk] ?? 'text-text-secondary')}>
                              <SIcon className="h-3.5 w-3.5" />
                              <span className="capitalize">{sk}</span>
                            </span>
                          </td>
                          <td className="px-4 py-3 text-right tabular-nums text-text-primary">
                            {r.value !== undefined && r.value !== null ? String(r.value) : '—'}
                          </td>
                          <td className="px-4 py-3 text-right tabular-nums text-text-secondary">
                            {r.duration_ms !== undefined ? `${r.duration_ms}ms` : '—'}
                          </td>
                          <td className="px-4 py-3 text-text-secondary truncate max-w-md">{r.message ?? '—'}</td>
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

function ResultBarChart({ results }: { results: CheckResult[] }) {
  // Bucket results by time (oldest -> newest). Up to 20 bars.
  const bars = useMemo(() => {
    if (results.length === 0) return [] as { status: CheckStatus; label: string }[];
    const sorted = [...results]
      .filter((r) => !!r.timestamp)
      .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime());
    return sorted.map((r) => {
      const ts = new Date(r.timestamp);
      const hh = ts.getHours().toString().padStart(2, '0');
      const mm = ts.getMinutes().toString().padStart(2, '0');
      return {
        status: (r.status as CheckStatus) ?? 'disabled',
        label: `${hh}:${mm}`,
      };
    });
  }, [results]);

  if (bars.length === 0) {
    return (
      <div className="text-center text-text-muted text-sm py-8">No results to chart yet.</div>
    );
  }

  return (
    <div className="flex items-end gap-1 h-32">
      {bars.map((b, i) => {
        const color =
          b.status === 'ok'
            ? 'bg-success'
            : b.status === 'warning'
            ? 'bg-warning'
            : b.status === 'critical'
            ? 'bg-danger'
            : 'bg-border-strong';
        // Show timestamp labels only on first, middle, and last to avoid clutter.
        const showLabel = bars.length <= 6 || i === 0 || i === Math.floor(bars.length / 2) || i === bars.length - 1;
        return (
          <div key={i} className="flex-1 flex flex-col items-center justify-end gap-1 min-w-0">
            <div
              className={'w-full rounded-t ' + color}
              style={{ height: b.status === 'disabled' ? '4px' : '100%' }}
              title={b.status}
            />
            {showLabel && (
              <span className="text-[10px] text-text-muted truncate w-full text-center">{b.label}</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Assign Agent modal
// ---------------------------------------------------------------------------

interface AssignAgentModalProps {
  agents: Agent[];
  assignedIds: Set<string>;
  onClose: () => void;
  onAssign: (agentId: string) => Promise<void>;
}

function AssignAgentModal({ agents, assignedIds, onClose, onAssign }: AssignAgentModalProps) {
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState(false);

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return agents
      .filter((a) => !assignedIds.has(a.id))
      .filter((a) => !q || a.hostname.toLowerCase().includes(q))
      .slice(0, 50);
  }, [agents, assignedIds, query]);

  const handleAssign = async (id: string) => {
    setBusy(true);
    try {
      await onAssign(id);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md rounded-lg border border-border-subtle bg-surface-secondary shadow-xl">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Assign Agent</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-4">
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search agents…"
            className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40 mb-3"
          />
          <ul className="max-h-80 overflow-y-auto divide-y divide-border-subtle rounded-md border border-border-subtle">
            {candidates.length === 0 ? (
              <li className="px-3 py-6 text-center text-text-muted text-sm">No agents available.</li>
            ) : (
              candidates.map((a) => (
                <li key={a.id} className="px-3 py-2 flex items-center justify-between hover:bg-surface-tertiary/40">
                  <div className="flex items-center gap-2 min-w-0">
                    <Bot className="h-4 w-4 text-text-muted shrink-0" />
                    <span className="text-sm text-text-primary truncate">{a.hostname || a.id}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      void handleAssign(a.id);
                    }}
                    disabled={busy}
                    className="px-2 h-7 rounded text-xs text-accent hover:bg-accent/10 border border-accent/30 disabled:opacity-50"
                  >
                    Assign
                  </button>
                </li>
              ))
            )}
          </ul>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Edit modal
// ---------------------------------------------------------------------------

interface EditCheckModalProps {
  check: Check;
  onClose: () => void;
  onSubmit: (patch: { name?: string; interval_secs?: number; config?: Record<string, unknown> }) => Promise<void>;
}

function EditCheckModal({ check, onClose, onSubmit }: EditCheckModalProps) {
  const [name, setName] = useState(check.name);
  const [interval, setInterval] = useState(check.interval_secs);
  const [configJson, setConfigJson] = useState(JSON.stringify(check.config ?? {}, null, 2));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    let config: Record<string, unknown>;
    try {
      const parsed = JSON.parse(configJson);
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        throw new Error('Config must be a JSON object');
      }
      config = parsed as Record<string, unknown>;
    } catch (e) {
      setError(`Invalid config JSON: ${(e as Error).message}`);
      return;
    }
    if (!name.trim()) {
      setError('Name is required');
      return;
    }
    if (interval < 10) {
      setError('Interval must be at least 10 seconds');
      return;
    }
    setBusy(true);
    try {
      await onSubmit({ name: name.trim(), interval_secs: interval, config });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60 flex items-center justify-center p-4"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-lg rounded-lg border border-border-subtle bg-surface-secondary shadow-xl">
        <div className="px-5 py-4 border-b border-border-subtle flex items-center justify-between">
          <h2 className="text-sm font-semibold text-text-primary">Edit Check</h2>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="p-5 space-y-4">
          <div>
            <label className="block text-xs text-text-secondary mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>
          <div>
            <label className="block text-xs text-text-secondary mb-1">Interval (seconds, min 10)</label>
            <input
              type="number"
              value={interval}
              min={10}
              onChange={(e) => setInterval(Number(e.target.value) || 60)}
              className="w-full h-9 px-3 rounded-md bg-surface-tertiary/60 border border-border-strong text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-accent/40 focus:border-accent/40"
            />
          </div>
          <div>
            <label className="block text-xs text-text-secondary mb-1">Config (JSON)</label>
            <MonacoEditor
              value={configJson}
              onChange={(v) => setConfigJson(v)}
              language="json"
              height={220}
              theme="vs-dark"
              options={{
                fontSize: 12,
                minimap: { enabled: false },
                lineNumbers: 'on',
                tabSize: 2,
                formatOnPaste: true,
              }}
            />
          </div>
          {error && (
            <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
              {error}
            </div>
          )}
          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="px-3 h-9 rounded-md border border-border-strong bg-surface-tertiary text-sm text-text-primary hover:bg-border-strong transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-accent hover:bg-accent text-sm text-white disabled:opacity-50 transition-colors"
            >
              {busy && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{busy ? 'Saving…' : 'Save'}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
