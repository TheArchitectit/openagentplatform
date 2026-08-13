import { X, Users } from 'lucide-react';
import type {
  Policy,
  PolicyAssignment,
  ComplianceSummary,
  CreatePolicyInput,
  UpdatePolicyInput,
  PolicyValidationResult,
} from '@/lib/usePolicies';
import type { Agent } from '@/lib/useAgents';
import { PolicyEditor } from '@/components/policy-editor';
import { complianceColor, formatTimestamp } from './policy_detail_helpers';

interface PolicyRightColumnProps {
  policy: Policy;
  compliance: ComplianceSummary | null;
  complianceCounts: { compliant: number; nonCompliant: number };
  compliancePct: number | undefined;
  donutSize: number;
  donutRadius: number;
  donutStroke: number;
  donutDashArray: string;
  donutDashOffset: string;
  now: number;
  showAssignPicker: boolean;
  editorOpen: boolean;
  savingEditor: boolean;
  savingEditorError: string | null;
  availableAgents: Agent[];
  setShowAssignPicker: (v: boolean) => void;
  setEditorOpen: (v: boolean) => void;
  setSavingEditorError: (v: string | null) => void;
  handleAddAssignment: (agentId: string) => Promise<void>;
  handleEditorSave: (
    input:
      | CreatePolicyInput
      | { id: string; input: UpdatePolicyInput }
  ) => Promise<void>;
  validatePolicy: (regoSource: string) => Promise<PolicyValidationResult>;
}

export function PolicyRightColumn({
  policy,
  compliance,
  complianceCounts,
  compliancePct,
  donutSize,
  donutRadius,
  donutStroke,
  donutDashArray,
  donutDashOffset,
  now,
  showAssignPicker,
  editorOpen,
  savingEditor,
  savingEditorError,
  availableAgents,
  setShowAssignPicker,
  setEditorOpen,
  setSavingEditorError,
  handleAddAssignment,
  handleEditorSave,
  validatePolicy,
}: PolicyRightColumnProps) {
  return (
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
