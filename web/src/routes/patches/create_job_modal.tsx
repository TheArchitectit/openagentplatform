import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Check,
  X,
  Loader2,
  ChevronLeft,
  ChevronRight,
  Send,
} from 'lucide-react';
import {
  usePatches,
  type PatchJob,
  type PatchCatalogItem,
} from '@/lib/usePatches';
import { useAgents, type Agent } from '@/lib/useAgents';
import { PatchesStep, TargetsStep } from './create_job_steps_select';
import { ConfigureStep, ReviewStep } from './create_job_steps_config';

// ---------------------------------------------------------------------------
// Create-job modal (multi-step wizard)
// ---------------------------------------------------------------------------

export type WizardStep = 'patches' | 'targets' | 'configure' | 'review';

export const STEP_LABELS: Record<WizardStep, string> = {
  patches: 'Select Patches',
  targets: 'Select Targets',
  configure: 'Configure',
  review: 'Review & Submit',
};

export const STEPS: WizardStep[] = ['patches', 'targets', 'configure', 'review'];

export function CreateJobModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (job: PatchJob) => void;
}) {
  const { catalog, fetchCatalog, catalogLoading, createJob } = usePatches();
  const { agents } = useAgents();

  const [step, setStep] = useState<WizardStep>('patches');
  const [search, setSearch] = useState('');
  const [catalogFilter, setCatalogFilter] = useState<{
    severity?: string;
    category?: string;
    os?: string;
  }>({});

  const [selectedPatches, setSelectedPatches] = useState<Set<string>>(new Set());
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(new Set());
  const [agentQuery, setAgentQuery] = useState('');

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [strategy, setStrategy] = useState<'immediate' | 'staged' | 'maintenance_window'>(
    'staged'
  );
  const [batchSize, setBatchSize] = useState(10);
  const [batchIntervalMinutes, setBatchIntervalMinutes] = useState(15);
  const [rebootPolicy, setRebootPolicy] = useState<
    'never' | 'if_required' | 'always' | 'scheduled'
  >('if_required');
  const [maintenanceStart, setMaintenanceStart] = useState('');
  const [maintenanceEnd, setMaintenanceEnd] = useState('');

  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Lazy-load catalog on first open.
  useEffect(() => {
    if (catalog.length === 0 && !catalogLoading) {
      void fetchCatalog();
    }
  }, [catalog.length, catalogLoading, fetchCatalog]);

  // Reset state on close.
  useEffect(() => {
    function onEsc(e: KeyboardEvent) {
      if (e.key === 'Escape' && !submitting) onClose();
    }
    document.addEventListener('keydown', onEsc);
    return () => document.removeEventListener('keydown', onEsc);
  }, [onClose, submitting]);

  // derived lists
  const catalogPatched = useMemo(() => {
    const q = search.trim().toLowerCase();
    return catalog.filter((c) => {
      if (catalogFilter.severity && c.severity !== catalogFilter.severity) return false;
      if (catalogFilter.category && c.category !== catalogFilter.category) return false;
      if (catalogFilter.os && c.os !== catalogFilter.os) return false;
      if (!q) return true;
      if (c.title?.toLowerCase().includes(q)) return true;
      if (c.kb_number?.toLowerCase().includes(q)) return true;
      if (c.cve_ids?.some((id) => id.toLowerCase().includes(q))) return true;
      return false;
    });
  }, [catalog, search, catalogFilter]);

  const agentList = useMemo(() => {
    const q = agentQuery.trim().toLowerCase();
    if (!q) return agents;
    return agents.filter(
      (a) =>
        a.hostname?.toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q) ||
        a.os?.toLowerCase().includes(q)
    );
  }, [agents, agentQuery]);

  const stepIndex = STEPS.indexOf(step);
  const canGoNext = (() => {
    if (step === 'patches') return selectedPatches.size > 0;
    if (step === 'targets') return selectedAgents.size > 0;
    if (step === 'configure') return name.trim().length > 0;
    return true;
  })();

  const goNext = useCallback(() => {
    const idx = STEPS.indexOf(step);
    if (idx < STEPS.length - 1) setStep(STEPS[idx + 1]);
  }, [step]);

  const goBack = useCallback(() => {
    const idx = STEPS.indexOf(step);
    if (idx > 0) setStep(STEPS[idx - 1]);
  }, [step]);

  const togglePatch = useCallback((id: string) => {
    setSelectedPatches((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleAgent = useCallback((id: string) => {
    setSelectedAgents((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const selectAllAgents = useCallback(() => {
    setSelectedAgents(new Set(agentList.map((a) => a.id)));
  }, [agentList]);

  const clearAllAgents = useCallback(() => {
    setSelectedAgents(new Set());
  }, []);

  const submit = useCallback(async () => {
    setSubmitError(null);
    setSubmitting(true);
    try {
      const job = await createJob({
        name: name.trim(),
        description: description.trim() || undefined,
        patch_ids: Array.from(selectedPatches),
        target_agent_ids: Array.from(selectedAgents),
        strategy,
        reboot_policy: rebootPolicy,
        batch_size: batchSize,
        batch_interval_minutes: batchIntervalMinutes,
        ...(strategy === 'maintenance_window'
          ? {
              maintenance_window_start: maintenanceStart || undefined,
              maintenance_window_end: maintenanceEnd || undefined,
            }
          : {}),
      });
      onCreated(job);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  }, [
    name,
    description,
    selectedPatches,
    selectedAgents,
    strategy,
    rebootPolicy,
    batchSize,
    batchIntervalMinutes,
    maintenanceStart,
    maintenanceEnd,
    createJob,
    onCreated,
  ]);

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
              onSearchChange={setSearch}
              catalogFilter={catalogFilter}
              onCatalogFilterChange={setCatalogFilter}
              selected={selectedPatches}
              onToggle={togglePatch}
            />
          )}
          {step === 'targets' && (
            <TargetsStep
              agents={agentList}
              isLoading={false}
              search={agentQuery}
              onSearchChange={setAgentQuery}
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
