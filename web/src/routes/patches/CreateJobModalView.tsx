import {
  Check,
  X,
  Loader2,
  ChevronLeft,
  ChevronRight,
  Send,
} from 'lucide-react';
import {
  type PatchCatalogItem,
} from '@/lib/usePatches';
import { type Agent } from '@/lib/useAgents';
import { PatchesStep, TargetsStep } from './create_job_steps_select';
import { ConfigureStep, ReviewStep } from './create_job_steps_config';
import {
  type WizardStep,
  STEP_LABELS,
  STEPS,
  type CatalogFilterInput,
} from './create_job_modal.config';

export function CreateJobModalView({
  step,
  submitting,
  submitError,
  catalogPatched,
  catalogLoading,
  catalog,
  catalogFilter,
  search,
  onSearchChange,
  onCatalogFilterChange,
  selectedPatches,
  togglePatch,
  agentList,
  agentQuery,
  onAgentQueryChange,
  selectedAgents,
  toggleAgent,
  selectAllAgents,
  clearAllAgents,
  agents,
  name,
  setName,
  description,
  setDescription,
  strategy,
  setStrategy,
  batchSize,
  setBatchSize,
  batchIntervalMinutes,
  setBatchIntervalMinutes,
  rebootPolicy,
  setRebootPolicy,
  maintenanceStart,
  setMaintenanceStart,
  maintenanceEnd,
  setMaintenanceEnd,
  onClose,
  goBack,
  goNext,
  canGoNext,
  submit,
}: {
  step: WizardStep;
  submitting: boolean;
  submitError: string | null;
  catalogPatched: PatchCatalogItem[];
  catalogLoading: boolean;
  catalog: PatchCatalogItem[];
  catalogFilter: CatalogFilterInput;
  search: string;
  onSearchChange: (v: string) => void;
  onCatalogFilterChange: (v: CatalogFilterInput) => void;
  selectedPatches: Set<string>;
  togglePatch: (id: string) => void;
  agentList: Agent[];
  agentQuery: string;
  onAgentQueryChange: (v: string) => void;
  selectedAgents: Set<string>;
  toggleAgent: (id: string) => void;
  selectAllAgents: () => void;
  clearAllAgents: () => void;
  agents: Agent[];
  name: string;
  setName: (v: string) => void;
  description: string;
  setDescription: (v: string) => void;
  strategy: 'immediate' | 'staged' | 'maintenance_window';
  setStrategy: (v: 'immediate' | 'staged' | 'maintenance_window') => void;
  batchSize: number;
  setBatchSize: (v: number) => void;
  batchIntervalMinutes: number;
  setBatchIntervalMinutes: (v: number) => void;
  rebootPolicy: 'never' | 'if_required' | 'always' | 'scheduled';
  setRebootPolicy: (v: 'never' | 'if_required' | 'always' | 'scheduled') => void;
  maintenanceStart: string;
  setMaintenanceStart: (v: string) => void;
  maintenanceEnd: string;
  setMaintenanceEnd: (v: string) => void;
  onClose: () => void;
  goBack: () => void;
  goNext: () => void;
  canGoNext: boolean;
  submit: () => void;
}) {
  const stepIndex = STEPS.indexOf(step);
  return (
    <div
      className="fixed inset-0 z-50 bg-slate-950/70 flex items-center justify-center p-4 overflow-y-auto"
      onClick={() => {
        if (!submitting) onClose();
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-full max-w-3xl rounded-lg border border-slate-800 bg-slate-900 shadow-2xl"
      >
        {/* Header */}
        <div className="px-5 py-4 border-b border-slate-800 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-white">Create patch job</h2>
            <p className="text-xs text-gray-400 mt-0.5">
              Step {stepIndex + 1} of {STEPS.length} — {STEP_LABELS[step]}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="text-gray-300 hover:text-white transition-colors"
            title="Close"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Stepper */}
        <div className="px-5 py-3 border-b border-slate-800 flex items-center gap-2">
          {STEPS.map((s, idx) => {
            const active = s === step;
            const done = idx < stepIndex;
            return (
              <div key={s} className="flex items-center gap-2 flex-1">
                <div
                  className={
                    'h-6 w-6 rounded-full flex items-center justify-center text-xs font-medium border ' +
                    (done
                      ? 'bg-green-500/15 border-green-800 text-green-400'
                      : active
                      ? 'bg-blue-600/15 border-blue-500/40 text-blue-400'
                      : 'bg-slate-800 border-slate-700 text-gray-400')
                  }
                >
                  {done ? <Check className="h-3.5 w-3.5" /> : idx + 1}
                </div>
                <span
                  className={
                    'text-sm ' + (active ? 'text-white' : done ? 'text-gray-300' : 'text-gray-400')
                  }
                >
                  {STEP_LABELS[s]}
                </span>
                {idx < STEPS.length - 1 && (
                  <ChevronRight className="h-4 w-4 text-gray-400 ml-auto" />
                )}
              </div>
            );
          })}
        </div>

        {/* Step body */}
        <div className="p-5 min-h-[20rem] max-h-[60vh] overflow-y-auto">
          {step === 'patches' && (
            <PatchesStep
              catalog={catalogPatched}
              isLoading={catalogLoading}
              search={search}
              onSearchChange={onSearchChange}
              catalogFilter={catalogFilter}
              onCatalogFilterChange={onCatalogFilterChange}
              selected={selectedPatches}
              onToggle={togglePatch}
            />
          )}
          {step === 'targets' && (
            <TargetsStep
              agents={agentList}
              isLoading={false}
              search={agentQuery}
              onSearchChange={onAgentQueryChange}
              selected={selectedAgents}
              onToggle={toggleAgent}
              onSelectAll={selectAllAgents}
              onClear={clearAllAgents}
            />
          )}
          {step === 'configure' && (
            <ConfigureStep
              name={name}
              onNameChange={setName}
              description={description}
              onDescriptionChange={setDescription}
              strategy={strategy}
              onStrategyChange={setStrategy}
              batchSize={batchSize}
              onBatchSizeChange={setBatchSize}
              batchIntervalMinutes={batchIntervalMinutes}
              onBatchIntervalChange={setBatchIntervalMinutes}
              rebootPolicy={rebootPolicy}
              onRebootPolicyChange={setRebootPolicy}
              maintenanceStart={maintenanceStart}
              onMaintenanceStartChange={setMaintenanceStart}
              maintenanceEnd={maintenanceEnd}
              onMaintenanceEndChange={setMaintenanceEnd}
            />
          )}
          {step === 'review' && (
            <ReviewStep
              patchCount={selectedPatches.size}
              agentCount={selectedAgents.size}
              name={name}
              description={description}
              strategy={strategy}
              batchSize={batchSize}
              batchIntervalMinutes={batchIntervalMinutes}
              rebootPolicy={rebootPolicy}
              maintenanceStart={maintenanceStart}
              maintenanceEnd={maintenanceEnd}
              catalog={catalog}
              selectedPatchIds={Array.from(selectedPatches)}
              agents={agents}
              selectedAgentIds={Array.from(selectedAgents)}
            />
          )}
        </div>

        {submitError && (
          <div className="mx-5 mb-2 rounded-md border border-red-800 bg-red-500/5 p-3 text-red-400 text-sm">
            {submitError}
          </div>
        )}

        {/* Footer */}
        <div className="px-5 py-3 border-t border-slate-800 flex items-center justify-between">
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="text-sm text-gray-300 hover:text-white transition-colors"
          >
            Cancel
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={goBack}
              disabled={stepIndex === 0 || submitting}
              className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-white text-sm hover:bg-slate-700 disabled:opacity-40 transition-colors"
            >
              <ChevronLeft className="h-4 w-4" />
              <span>Back</span>
            </button>
            {step === 'review' ? (
              <button
                type="button"
                onClick={() => void submit()}
                disabled={submitting}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 border border-blue-500 text-white text-sm disabled:opacity-50 transition-colors"
              >
                {submitting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Send className="h-4 w-4" />
                )}
                <span>Submit</span>
              </button>
            ) : (
              <button
                type="button"
                onClick={goNext}
                disabled={!canGoNext}
                className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-600 border border-blue-500 text-white text-sm disabled:opacity-40 transition-colors"
              >
                <span>Next</span>
                <ChevronRight className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
