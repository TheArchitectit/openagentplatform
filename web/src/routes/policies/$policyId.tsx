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

import { enforcementIcon, enforcementClasses, categoryClasses, complianceColor, highlightRego, formatTimestamp } from './policy_detail_helpers'
import { usePolicyDetail } from './usePolicyDetail'
export const Route = createFileRoute('/policies/$policyId')({
  component: PolicyDetailPage,
});


function PolicyDetailPage() {
  const { policyId } = Route.useParams();
  const {
    policy, enabled, isLoading, loadError, editMode, editorOpen,
    savingToggle, evaluating, assignments, violations, compliance,
    showAssignPicker, savingEditor, savingEditorError, now,
    complianceCounts, compliancePct,
    donutDashArray, donutDashOffset, donutRadius, donutStroke, donutSize,
    availableAgents, navigate,
    setEnabled, setEditMode, setEditorOpen, setShowAssignPicker,
    handleToggleEnabled, handleEvaluate, handleDelete,
    handleRemoveAssignment, handleAddAssignment, handleDismissViolation,
    handleEditorSave,
  } = usePolicyDetail(policyId);
  if (isLoading) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400">
        Loading policy…
      </div>
    );
  }

  if (loadError && !policy) {
    return (
      <div className="space-y-4">
        <Link
          to="/policies"
          className="inline-flex items-center gap-2 text-sm text-gray-300 hover:text-white transition-colors"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to policies
        </Link>
        <div className="rounded-lg border border-red-800 bg-red-500/5 p-12 text-center text-red-400">
          Failed to load policy: {loadError}
        </div>
      </div>
    );
  }

  if (!policy) {
    return (
      <div className="rounded-lg border border-slate-800 bg-slate-900 p-12 text-center text-gray-400">
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
          className="inline-flex items-center gap-2 text-sm text-gray-300 hover:text-white transition-colors mb-3"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to policies
        </Link>

        <div className="flex items-start justify-between flex-wrap gap-4">
          <div className="flex items-start gap-3 flex-1 min-w-0">
            <div className="h-10 w-10 rounded-md bg-slate-800 border border-slate-700 flex items-center justify-center shrink-0">
              <ShieldCheck className="h-5 w-5 text-blue-400" />
            </div>
            <div className="flex-1 min-w-0">
              <h1 className="text-2xl font-bold text-white truncate">{policy.name}</h1>
              {policy.description && (
                <p className="text-gray-300 text-sm mt-1">{policy.description}</p>
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
            <label className="inline-flex items-center gap-2 px-3 h-9 rounded-md border border-slate-700 bg-slate-800 text-sm text-white cursor-pointer select-none">
              <input
                type="checkbox"
                checked={enabled}
                onChange={handleToggleEnabled}
                disabled={savingToggle}
                className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40"
              />
              <span>{enabled ? 'Enabled' : 'Disabled'}</span>
            </label>
            <button
              type="button"
              onClick={handleEvaluate}
              disabled={evaluating || !enabled}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-green-500 hover:bg-green-500 text-sm text-white disabled:opacity-50 transition-colors"
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
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white transition-colors"
            >
              <Edit3 className="h-4 w-4" />
              <span>Edit</span>
            </button>
            <button
              type="button"
              onClick={handleDelete}
              className="inline-flex items-center gap-2 px-3 h-9 rounded-md bg-red-500/20 hover:bg-red-500/30 border border-red-800 text-sm text-red-400 transition-colors"
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>

      {loadError && (
        <div className="rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {loadError}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* Left column */}
        <div className="lg:col-span-2 space-y-5">
          {/* Rego source */}
          <div className="rounded-lg border border-slate-800 bg-slate-900">
            <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
              <h2 className="text-sm font-semibold text-white">Rego source</h2>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setEditMode((m) => !m)}
                  className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-slate-700 bg-slate-800 hover:bg-slate-700 text-gray-300 transition-colors"
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
                    className="inline-flex items-center gap-1.5 px-2 h-7 rounded text-xs border border-slate-700 bg-slate-800 hover:bg-slate-700 text-gray-300 transition-colors"
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
                className="w-full p-4 bg-slate-950 text-xs font-mono text-white leading-5 focus:outline-none min-h-[280px]"
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
              <pre className="p-4 text-xs font-mono text-white leading-5 overflow-x-auto whitespace-pre">
                {highlightRego(policy.rego_source)}
              </pre>
            )}
          </div>

          {/* Assignments */}
          <div className="rounded-lg border border-slate-800 bg-slate-900">
            <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
              <h2 className="text-sm font-semibold text-white">
                Assignments <span className="text-xs text-gray-400 ml-1">({assignments.length})</span>
              </h2>
              <button
                type="button"
                onClick={() => setShowAssignPicker(true)}
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
                            onClick={() => void handleRemoveAssignment(a.agent_id)}
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

          {/* Violations */}
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
                              onClick={() => void handleDismissViolation(v.id)}
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
        </div>

        {/* Right column: compliance donut */}
        <div className="space-y-5">
          <div className="rounded-lg border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-sm font-semibold text-white mb-4">Compliance score</h2>
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
                <div className="text-xs text-gray-400">compliant</div>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3 text-center mt-2">
              <div>
                <div className="text-2xl font-semibold text-green-400 tabular-nums">
                  {complianceCounts.compliant}
                </div>
                <div className="text-xs text-gray-400">Compliant</div>
              </div>
              <div>
                <div className="text-2xl font-semibold text-red-400 tabular-nums">
                  {complianceCounts.nonCompliant}
                </div>
                <div className="text-xs text-gray-400">Non-compliant</div>
              </div>
            </div>
            {compliance && (
              <div className="mt-4 pt-4 border-t border-slate-800 text-xs text-gray-300 space-y-1">
                <div className="flex items-center justify-between">
                  <span>Total agents</span>
                  <span className="text-white">{compliance.total_agents}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span>Enabled policies</span>
                  <span className="text-white">
                    {compliance.enabled_policies} / {compliance.total_policies}
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Quick info */}
          <div className="rounded-lg border border-slate-800 bg-slate-900 p-5">
            <h2 className="text-sm font-semibold text-white mb-3">Info</h2>
            <dl className="text-xs space-y-2">
              <div className="flex justify-between">
                <dt className="text-gray-400">ID</dt>
                <dd className="text-gray-300 font-mono">{policy.id}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-gray-400">Created</dt>
                <dd className="text-gray-300">{formatTimestamp(policy.created_at, now)}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-gray-400">Updated</dt>
                <dd className="text-gray-300">{formatTimestamp(policy.updated_at, now)}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>

      {/* Agent picker modal */}
      {showAssignPicker && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/70 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => {
            if (e.target === e.currentTarget) setShowAssignPicker(false);
          }}
        >
          <div className="w-full max-w-md rounded-lg border border-slate-800 bg-slate-900 shadow-2xl">
            <div className="flex items-center justify-between px-5 py-3 border-b border-slate-800">
              <h2 className="text-sm font-semibold text-white inline-flex items-center gap-2">
                <Users className="h-4 w-4 text-blue-400" />
                Assign agent
              </h2>
              <button
                type="button"
                onClick={() => setShowAssignPicker(false)}
                className="p-1.5 rounded-md text-gray-300 hover:text-white hover:bg-slate-800 transition-colors"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="max-h-80 overflow-y-auto">
              {availableAgents.length === 0 ? (
                <div className="px-5 py-8 text-center text-sm text-gray-400">
                  All agents are already assigned.
                </div>
              ) : (
                <ul className="divide-y divide-slate-800">
                  {availableAgents.map((a: Agent) => (
                    <li key={a.id}>
                      <button
                        type="button"
                        onClick={() => void handleAddAssignment(a.id)}
                        className="w-full text-left px-5 py-2.5 hover:bg-slate-800/50 transition-colors flex items-center gap-3"
                      >
                        <span className="text-sm text-white flex-1 truncate">
                          {a.hostname || a.id}
                        </span>
                        <span className="text-xs text-gray-400">{a.site_id || '—'}</span>
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
        <div className="fixed bottom-4 right-4 z-[60] rounded-md border border-red-800 bg-red-500/10 px-3 py-2 text-xs text-red-400 shadow-lg">
          {savingEditorError}
        </div>
      )}
    </div>
  );
}
