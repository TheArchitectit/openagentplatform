import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ArrowLeft,
  ShieldCheck,
  Play,
  Edit3,
  Eye,
  Trash2,
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
import { PolicyRightColumn } from './policy_detail_components'
import { PolicyAssignmentsTable } from './PolicyAssignmentsTable'
import { PolicyViolationsTable } from './PolicyViolationsTable'
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
    setSavingEditorError, validatePolicy,
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
                      const updated = await // @ts-expect-error
updatePolicy(policy.id, {
                        rego_source: e.target.value,
                      });
                      // @ts-expect-error
setPolicy(updated);
                    } catch (err) {
                      // @ts-expect-error
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

          <PolicyAssignmentsTable
            assignments={assignments}
            now={now}
            onShowPicker={() => setShowAssignPicker(true)}
            onRemove={(agentId) => void handleRemoveAssignment(agentId)}
          />

          <PolicyViolationsTable
            violations={violations}
            now={now}
            onDismiss={(id) => void handleDismissViolation(id)}
          />
        </div>

        <PolicyRightColumn
          policy={policy}
          compliance={compliance}
          complianceCounts={complianceCounts}
          compliancePct={compliancePct}
          donutSize={donutSize}
          donutRadius={donutRadius}
          donutStroke={donutStroke}
          donutDashArray={String(donutDashArray)}
          donutDashOffset={String(donutDashOffset)}
          now={now}
          showAssignPicker={showAssignPicker}
          editorOpen={editorOpen}
          savingEditor={savingEditor}
          savingEditorError={savingEditorError}
          availableAgents={availableAgents}
          setShowAssignPicker={setShowAssignPicker}
          setEditorOpen={setEditorOpen}
          setSavingEditorError={setSavingEditorError}
          handleAddAssignment={handleAddAssignment}
          handleEditorSave={handleEditorSave}
          validatePolicy={validatePolicy}
        />
      </div>
    </div>
  );
}
