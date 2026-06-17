import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft,
  ShieldCheck,
  Play,
  Edit3,
  Eye,
  Trash2,
  Plus,
  X,
  Users,
  CircleCheck,
  CircleAlert,
  CircleX,
  Loader2,
} from 'lucide-react';
import {
  usePolicies,
  type Policy,
  type PolicyAssignment,
  type PolicyViolation,
  type ComplianceSummary,
} from '@/lib/usePolicies';
import { PolicyEditor } from '@/components/policy-editor';
import { SeverityBadge } from '@/components/severity-badge';
import { useAgents, type Agent } from '@/lib/useAgents';
import { ApiError } from '@/lib/api';

export const Route = createFileRoute('/policies/$policyId')({
  component: PolicyDetailPage,
});

function enforcementIcon(mode: string) {
  switch (mode) {
    case 'enforce':
      return ShieldCheck;
    case 'audit':
      return Eye;
    case 'report':
    default:
      return Edit3;
  }
}

function enforcementClasses(mode: string): string {
  switch (mode) {
    case 'enforce':
      return 'bg-danger/10 text-danger border-danger/20';
    case 'audit':
      return 'bg-warning/10 text-warning border-warning/20';
    case 'report':
    default:
      return 'bg-info/10 text-info border-info/20';
  }
}

function categoryClasses(cat: string): string {
  switch (cat) {
    case 'security':
      return 'bg-danger/10 text-danger border-danger/20';
    case 'compliance':
      return 'bg-info/10 text-info border-info/20';
    case 'configuration':
      return 'bg-accent/10 text-accent border-accent/20';
    case 'performance':
      return 'bg-warning/10 text-warning border-warning/20';
    default:
      return 'bg-text-muted/10 text-text-secondary border-text-muted/20';
  }
}

function complianceColor(pct: number | undefined): string {
  if (pct === undefined || pct === null) return 'text-text-muted';
  if (pct >= 80) return 'text-success';
  if (pct >= 60) return 'text-warning';
  return 'text-danger';
}

// Highlight Rego keywords with simple regex-based highlighting — no full
// parser required for the read-only display.
function highlightRego(src: string): string {
  // Escape HTML first.
  const escaped = src
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
  return escaped;
}

function formatTimestamp(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (!t) return '—';
  const ageSec = Math.max(0, Math.floor((now - t) / 1000));
  if (ageSec < 60) return `${ageSec}s ago`;
  if (ageSec < 3600) return `${Math.floor(ageSec / 60)}m ago`;
  if (ageSec < 86400) return `${Math.floor(ageSec / 3600)}h ago`;
  return `${Math.floor(ageSec / 86400)}d ago`;
}

function PolicyDetailPage() {
  const { policyId } = Route.useParams();
  const navigate = useNavigate();

  const {
    fetchPolicy,
    updatePolicy,
    deletePolicy,
    evaluatePolicy,
    validatePolicy,
    fetchAssignments,
    assignAgent,
    unassignAgent,
    fetchViolations,
    dismissViolation,
    fetchComplianceSummary,
  } = usePolicies();

  const { agents } = useAgents();

  const [policy, setPolicy] = useState<Policy | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [editMode, setEditMode] = useState(false);
  const [editorOpen, setEditorOpen] = useState(false);
  const [savingToggle, setSavingToggle] = useState(false);
  const [evaluating, setEvaluating] = useState(false);
  const [assignments, setAssignments] = useState<PolicyAssignment[]>([]);
  const [violations, setViolations] = useState<PolicyViolation[]>([]);
  const [compliance, setCompliance] = useState<ComplianceSummary | null>(null);
  const [showAssignPicker, setShowAssignPicker] = useState(false);
  const [savingEditor, setSavingEditor] = useState(false);
  const [savingEditorError, setSavingEditorError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // Reload policy + assignments + violations whenever the id changes.
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    setLoadError(null);
    setPolicy(null);

    void (async () => {
      try {
        const p = await fetchPolicy(policyId);
        if (cancelled) return;
        setPolicy(p);
        setEnabled(Boolean(p.enabled));
        const [a, v] = await Promise.all([
          fetchAssignments(policyId).catch(() => [] as PolicyAssignment[]),
          fetchViolations(policyId).catch(() => [] as PolicyViolation[]),
        ]);
        if (cancelled) return;
        setAssignments(a);
        setViolations(v);
      } catch (err) {
        if (!cancelled) {
          setLoadError(err instanceof Error ? err.message : 'Failed to load policy');
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [policyId, fetchPolicy, fetchAssignments, fetchViolations]);

  // Tick once every 30s so the "X seconds ago" labels stay current.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(id);
  }, []);

  // Best-effort fetch of the org-wide compliance summary for the right
  // sidebar's "total agents / enabled policies" panel.
  useEffect(() => {
    let cancelled = false;
    void fetchComplianceSummary()
      .then((s) => {
        if (!cancelled) setCompliance(s);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [fetchComplianceSummary]);

  const handleToggleEnabled = async () => {
    if (!policy) return;
    const next = !enabled;
    setSavingToggle(true);
    setEnabled(next);
    try {
      const updated = await updatePolicy(policy.id, { enabled: next });
      setPolicy(updated);
    } catch (err) {
      setEnabled(!next);
      setLoadError(err instanceof Error ? err.message : 'Failed to update policy');
    } finally {
      setSavingToggle(false);
    }
  };

  const handleEvaluate = async () => {
    if (!policy) return;
    setEvaluating(true);
    try {
      await evaluatePolicy(policy.id);
      // Refresh violations + compliance after evaluation
      const v = await fetchViolations(policy.id);
      setViolations(v);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Evaluation failed');
    } finally {
      setEvaluating(false);
    }
  };

  const handleDelete = async () => {
    if (!policy) return;
    if (!confirm(`Delete policy "${policy.name}"? This cannot be undone.`)) return;
    try {
      await deletePolicy(policy.id);
      void navigate({ to: '/policies' });
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  const handleRemoveAssignment = async (agentId: string) => {
    if (!policy) return;
    try {
      await unassignAgent(policy.id, agentId);
      setAssignments((prev) => prev.filter((a) => a.agent_id !== agentId));
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to remove assignment');
    }
  };

  const handleAddAssignment = async (agentId: string) => {
    if (!policy) return;
    try {
      await assignAgent(policy.id, agentId);
      // Refetch to get the full record with timestamps etc.
      const list = await fetchAssignments(policy.id);
      setAssignments(list);
      setShowAssignPicker(false);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to assign');
    }
  };

  const handleDismissViolation = async (id: string) => {
    try {
      await dismissViolation(id);
      setViolations((prev) =>
        prev.map((v) => (v.id === id ? { ...v, status: 'dismissed' as const } : v))
      );
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to dismiss');
    }
  };

  const handleEditorSave = useCallback(
    async (
      input:
        | Parameters<ReturnType<typeof usePolicies>['createPolicy']>[0]
        | { id: string; input: Parameters<ReturnType<typeof usePolicies>['updatePolicy']>[1] }
    ) => {
      if (!('id' in input)) {
        throw new Error('Create not supported in detail view');
      }
      setSavingEditor(true);
      setSavingEditorError(null);
      try {
        const updated = await updatePolicy(input.id, input.input);
        setPolicy(updated);
        setEditorOpen(false);
      } catch (err) {
        setSavingEditorError(
          err instanceof Error ? err.message : (err as ApiError)?.message ?? 'Save failed'
        );
        throw err;
      } finally {
        setSavingEditor(false);
      }
    },
    [updatePolicy]
  );

  // Compute compliance counts from assignments.
  const complianceCounts = useMemo(() => {
    let compliant = 0;
    let nonCompliant = 0;
    for (const a of assignments) {
      if (a.compliant === true) compliant += 1;
      else if (a.compliant === false) nonCompliant += 1;
    }
    return { compliant, nonCompliant, total: assignments.length };
  }, [assignments]);

  const compliancePct = useMemo(() => {
    const denom = complianceCounts.compliant + complianceCounts.nonCompliant;
    if (denom === 0) return undefined;
    return (complianceCounts.compliant / denom) * 100;
  }, [complianceCounts]);

  // Donut chart geometry.
  const donutSize = 140;
  const donutStroke = 14;
  const donutRadius = (donutSize - donutStroke) / 2;
  const donutCircumference = 2 * Math.PI * donutRadius;
  const donutDashArray = donutCircumference;
  const compliantFraction = complianceCounts.total
    ? complianceCounts.compliant / complianceCounts.total
    : 0;
  const donutDashOffset = donutCircumference * (1 - compliantFraction);

  // Build a list of agents not yet assigned for the picker.
  const availableAgents = useMemo(() => {
    const assignedIds = new Set(assignments.map((a) => a.agent_id));
    return agents.filter((a) => !assignedIds.has(a.id));
  }, [agents, assignments]);

  if (isLoading) {
    return (
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted">
        Loading policy…
      </div>
    );
  }

  if (loadError && !policy) {
    return (
      <div className="space-y-4">
        <Link
          to="/policies"
          className="inline-flex items-center gap-2 text-sm text-text-secondary hover:text-text-primary transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to policies
        </Link>
        <div className="rounded-lg border border-danger/30 bg-danger/5 p-12 text-center text-danger">
          Failed to load policy: {loadError}
        </div>
      </div>
    );
  }

  if (!policy) {
    return (
      <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-12 text-center text-text-muted">
        Policy not found.
      </div>
    );
  }

  const EnforceIcon = enforcementIcon(policy.enforcement);

  return (
    <div className="space-y-5">
      {/* Header */}
      <div>
        <Link
          to="/policies"
          className="inline-flex items-center gap-2 text-sm text-text-secondary hover:text-text-primary transition-colors mb-3"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to policies
        </Link>

        <div className="flex items-start justify-between flex-wrap gap-4">
          <div className="flex items-start gap-3 flex-1 min-w-0">
            <div className="h-10 w-10 rounded-md bg-surface-tertiary border border-border-strong flex items-center justify-center shrink-0">
              <ShieldCheck className="h-5 w-5 text-accent" />
            </div>
            <div className="flex-1 min-w-0">
              <h1 className="text-2xl font-bold text-text-primary truncate">{policy.name}</h1>
              {policy.description && (
                <p className="text-text-secondary text-sm mt-1">{policy.description}</p>
              )}
              <div className="flex flex-wrap items-center gap-2 mt-3">
                <span
                  className={
                    'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                    categoryClasses(policy.category)
                  }
                >
                  {policy.category}
                </span>
                <SeverityBadge severity={policy.severity} />
                <span
                  className={
                    'inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                    enforcementClasses(policy.enforcement)
                  }
                >
                  <EnforceIcon className="h-2.5 w-2.5" />
                  {policy.enforcement}
                </span>
              </div>
            </div>
          </div>

          <div className="flex items-center gap-2 flex-wrap">
            <label className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-border-strong bg-surface-tertiary text-sm text-text-primary cursor-pointer select-none">
              <input
                type="checkbox"
                checked={enabled}
                onChange={handleToggleEnabled}
                disabled={savingToggle}
                className="h-4 w-4 rounded border-border-strong bg-surface-tertiary text-accent focus:ring-accent/40"
              />
              <span>{enabled ? 'Enabled' : 'Disabled'}</span>
            </label>
            <button
              type="button"
              onClick={handleEvaluate}
              disabled={evaluating || !enabled}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-success hover:bg-success text-sm text-white disabled:opacity-50 transition-colors"
            >
              {evaluating ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Play className="h-4 w-4" />
              )}
              <span>Evaluate Now</span>
            </button>
            <button
              type="button"
              onClick={() => setEditorOpen(true)}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-surface-tertiary hover:bg-border-strong border border-border-strong text-sm text-text-primary transition-colors"
            >
              <Edit3 className="h-4 w-4" />
              <span>Edit</span>
            </button>
            <button
              type="button"
              onClick={handleDelete}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-danger/20 hover:bg-danger/30 border border-danger/30 text-sm text-danger transition-colors"
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>

      {loadError && (
        <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
          {loadError}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Left column */}
        <div className="lg:col-span-2 space-y-5">
          {/* Rego source */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
            <div className="flex items-center justify-between px-5 py-3 border-b border-border-subtle">
              <h2 className="text-sm font-semibold text-text-primary">Rego source</h2>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setEditMode((m) => !m)}
                  className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-border-strong bg-surface-tertiary hover:bg-border-strong text-text-secondary transition-colors"
                >
                  {editMode ? (
                    <>
                      <Eye className="h-3 w-3" />
                      <span>Read-only</span>
                    </>
                  ) : (
                    <>
                      <Edit3 className="h-3 w-3" />
                      <span>Edit in modal</span>
                    </>
                  )}
                </button>
                {editMode && (
                  <button
                    type="button"
                    onClick={() => setEditorOpen(true)}
                    className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-border-strong bg-surface-tertiary hover:bg-border-strong text-text-secondary transition-colors"
                  >
                    <Edit3 className="h-3 w-3" />
                    <span>Open editor</span>
                  </button>
                )}
              </div>
            </div>
            {editMode ? (
              <textarea
                defaultValue={policy.rego_source}
                className="w-full p-4 bg-surface-primary text-xs font-mono text-text-primary leading-5 focus:outline-none min-h-[280px]"
                spellCheck={false}
                onBlur={async (e) => {
                  if (e.target.value !== policy.rego_source) {
                    try {
                      const updated = await updatePolicy(policy.id, {
                        rego_source: e.target.value,
                      });
                      setPolicy(updated);
                    } catch (err) {
                      setLoadError(
                        err instanceof Error ? err.message : 'Failed to save rego'
                      );
                    }
                  }
                }}
              />
            ) : (
              <pre className="p-4 text-xs font-mono text-text-primary leading-5 overflow-x-auto whitespace-pre">
                {highlightRego(policy.rego_source)}
              </pre>
            )}
          </div>

          {/* Assignments */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
            <div className="flex items-center justify-between px-5 py-3 border-b border-border-subtle">
              <h2 className="text-sm font-semibold text-text-primary">
                Assignments <span className="text-xs text-text-muted ml-1">({assignments.length})</span>
              </h2>
              <button
                type="button"
                onClick={() => setShowAssignPicker(true)}
                className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-border-strong bg-surface-tertiary hover:bg-border-strong text-text-secondary transition-colors"
              >
                <Plus className="h-3 w-3" />
                <span>Add</span>
              </button>
            </div>

            {assignments.length === 0 ? (
              <div className="px-5 py-8 text-center text-sm text-text-muted">
                No agents assigned. Click "Add" to assign this policy.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                      <th className="px-4 py-2.5">Agent</th>
                      <th className="px-4 py-2.5">Status</th>
                      <th className="px-4 py-2.5">Last evaluated</th>
                      <th className="px-4 py-2.5 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {assignments.map((a) => (
                      <tr key={a.id ?? a.agent_id} className="hover:bg-surface-tertiary/40">
                        <td className="px-4 py-2.5">
                          <Link
                            to="/agents/$agentId"
                            params={{ agentId: a.agent_id }}
                            className="text-text-primary hover:text-accent transition-colors"
                          >
                            {a.hostname ?? a.agent_id}
                          </Link>
                        </td>
                        <td className="px-4 py-2.5">
                          {a.compliant === true ? (
                            <span className="inline-flex items-center gap-1 text-xs text-success">
                              <CircleCheck className="h-3.5 w-3.5" />
                              Compliant
                            </span>
                          ) : a.compliant === false ? (
                            <span className="inline-flex items-center gap-1 text-xs text-danger">
                              <CircleX className="h-3.5 w-3.5" />
                              Non-compliant
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-xs text-text-muted">
                              <CircleAlert className="h-3.5 w-3.5" />
                              Unknown
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-2.5 text-text-secondary text-xs">
                          {formatTimestamp(a.last_evaluated, now)}
                        </td>
                        <td className="px-4 py-2.5 text-right">
                          <button
                            type="button"
                            onClick={() => void handleRemoveAssignment(a.agent_id)}
                            className="p-1 rounded text-text-muted hover:text-danger transition-colors"
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

          {/* Violations */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60">
            <div className="flex items-center justify-between px-5 py-3 border-b border-border-subtle">
              <h2 className="text-sm font-semibold text-text-primary">
                Recent violations{' '}
                <span className="text-xs text-text-muted ml-1">({violations.length})</span>
              </h2>
            </div>
            {violations.length === 0 ? (
              <div className="px-5 py-8 text-center text-sm text-text-muted">
                No violations recorded.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="text-left text-xs uppercase tracking-wider text-text-muted border-b border-border-subtle bg-surface-primary/40">
                      <th className="px-4 py-2.5">Status</th>
                      <th className="px-4 py-2.5">Severity</th>
                      <th className="px-4 py-2.5">Agent</th>
                      <th className="px-4 py-2.5">Message</th>
                      <th className="px-4 py-2.5">Detected</th>
                      <th className="px-4 py-2.5 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {violations.map((v) => (
                      <tr key={v.id} className="hover:bg-surface-tertiary/40">
                        <td className="px-4 py-2.5">
                          <span
                            className={
                              'inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border capitalize ' +
                              (v.status === 'open'
                                ? 'bg-danger/10 text-danger border-danger/20'
                                : v.status === 'dismissed'
                                ? 'bg-text-muted/10 text-text-secondary border-text-muted/20'
                                : v.status === 'resolved'
                                ? 'bg-success/10 text-success border-success/20'
                                : 'bg-warning/10 text-warning border-warning/20')
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
                              className="text-text-primary hover:text-accent transition-colors"
                            >
                              {v.hostname ?? v.agent_id}
                            </Link>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td className="px-4 py-2.5 text-text-secondary text-xs max-w-xs truncate">
                          {v.message ?? '—'}
                        </td>
                        <td className="px-4 py-2.5 text-text-secondary text-xs">
                          {formatTimestamp(v.detected_at, now)}
                        </td>
                        <td className="px-4 py-2.5 text-right">
                          {v.status === 'open' && (
                            <button
                              type="button"
                              onClick={() => void handleDismissViolation(v.id)}
                              className="text-xs text-text-secondary hover:text-text-primary transition-colors"
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
        </div>

        {/* Right column: compliance donut */}
        <div className="space-y-5">
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5">
            <h2 className="text-sm font-semibold text-text-primary mb-4">Compliance score</h2>
            <div className="flex flex-col items-center">
              <svg
                width={donutSize}
                height={donutSize}
                viewBox={`0 0 ${donutSize} ${donutSize}`}
                className="-rotate-90"
              >
                <circle
                  cx={donutSize / 2}
                  cy={donutSize / 2}
                  r={donutRadius}
                  fill="none"
                  stroke="rgb(51 65 85)"
                  strokeWidth={donutStroke}
                />
                <circle
                  cx={donutSize / 2}
                  cy={donutSize / 2}
                  r={donutRadius}
                  fill="none"
                  stroke={
                    compliancePct === undefined
                      ? 'rgb(100 116 139)'
                      : compliancePct >= 80
                      ? 'rgb(52 211 153)'
                      : compliancePct >= 60
                      ? 'rgb(251 191 36)'
                      : 'rgb(244 63 94)'
                  }
                  strokeWidth={donutStroke}
                  strokeLinecap="round"
                  strokeDasharray={donutDashArray}
                  strokeDashoffset={donutDashOffset}
                />
              </svg>
              <div className="-mt-20 mb-12 text-center">
                <div
                  className={
                    'text-3xl font-semibold tabular-nums ' +
                    complianceColor(compliancePct)
                  }
                >
                  {compliancePct !== undefined ? `${compliancePct.toFixed(0)}%` : '—'}
                </div>
                <div className="text-xs text-text-muted">compliant</div>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3 text-center mt-2">
              <div>
                <div className="text-2xl font-semibold text-success tabular-nums">
                  {complianceCounts.compliant}
                </div>
                <div className="text-xs text-text-muted">Compliant</div>
              </div>
              <div>
                <div className="text-2xl font-semibold text-danger tabular-nums">
                  {complianceCounts.nonCompliant}
                </div>
                <div className="text-xs text-text-muted">Non-compliant</div>
              </div>
            </div>
            {compliance && (
              <div className="mt-4 pt-4 border-t border-border-subtle text-xs text-text-secondary space-y-1">
                <div className="flex items-center justify-between">
                  <span>Total agents</span>
                  <span className="text-text-primary">{compliance.total_agents}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span>Enabled policies</span>
                  <span className="text-text-primary">
                    {compliance.enabled_policies} / {compliance.total_policies}
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Quick info */}
          <div className="rounded-lg border border-border-subtle bg-surface-secondary/60 p-5">
            <h2 className="text-sm font-semibold text-text-primary mb-3">Info</h2>
            <dl className="text-xs space-y-2">
              <div className="flex justify-between">
                <dt className="text-text-muted">ID</dt>
                <dd className="text-text-secondary font-mono">{policy.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-text-muted">Created</dt>
                <dd className="text-text-secondary">{formatTimestamp(policy.created_at, now)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-text-muted">Updated</dt>
                <dd className="text-text-secondary">{formatTimestamp(policy.updated_at, now)}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>

      {/* Agent picker modal */}
      {showAssignPicker && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-surface-primary/70 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget) setShowAssignPicker(false);
          }}
        >
          <div className="w-full max-w-md rounded-lg border border-border-subtle bg-surface-secondary shadow-2xl">
            <div className="flex items-center justify-between px-5 py-3 border-b border-border-subtle">
              <h2 className="text-sm font-semibold text-text-primary inline-flex items-center gap-2">
                <Users className="h-4 w-4 text-accent" />
                Assign agent
              </h2>
              <button
                type="button"
                onClick={() => setShowAssignPicker(false)}
                className="p-1.5 rounded-md text-text-secondary hover:text-text-primary hover:bg-surface-tertiary transition-colors"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="max-h-80 overflow-y-auto">
              {availableAgents.length === 0 ? (
                <div className="px-5 py-8 text-center text-sm text-text-muted">
                  All agents are already assigned.
                </div>
              ) : (
                <ul className="divide-y divide-border-subtle">
                  {availableAgents.map((a: Agent) => (
                    <li key={a.id}>
                      <button
                        type="button"
                        onClick={() => void handleAddAssignment(a.id)}
                        className="w-full text-left px-5 py-2.5 hover:bg-surface-tertiary/50 transition-colors flex items-center gap-3"
                      >
                        <span className="text-sm text-text-primary flex-1 truncate">
                          {a.hostname || a.id}
                        </span>
                        <span className="text-xs text-text-muted">{a.site_id || '—'}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Editor modal */}
      {editorOpen && (
        <PolicyEditor
          policy={policy}
          onClose={() => {
            if (savingEditor) return;
            setEditorOpen(false);
            setSavingEditorError(null);
          }}
          onSave={handleEditorSave}
          validateRego={validatePolicy}
        />
      )}
      {savingEditorError && editorOpen && (
        <div className="fixed bottom-4 right-4 z-[60] rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger shadow-lg">
          {savingEditorError}
        </div>
      )}
    </div>
  );
}
