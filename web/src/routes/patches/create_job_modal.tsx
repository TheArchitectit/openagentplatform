import { useCallback, useEffect, useMemo, useState } from 'react';
import { usePatches, type PatchJob } from '@/lib/usePatches';
import { useAgents, type Agent } from '@/lib/useAgents';
import { CreateJobModalView } from './CreateJobModalView';
import {
  type WizardStep,
  STEP_LABELS,
  STEPS,
} from './create_job_modal.config';

export type { WizardStep } from './create_job_modal.config';
export { STEP_LABELS, STEPS } from './create_job_modal.config';

// ---------------------------------------------------------------------------
// Create-job modal (multi-step wizard)
// ---------------------------------------------------------------------------

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
    <CreateJobModalView
      step={step}
      submitting={submitting}
      submitError={submitError}
      catalogPatched={catalogPatched}
      catalogLoading={catalogLoading}
      catalog={catalog}
      catalogFilter={catalogFilter}
      search={search}
      onSearchChange={setSearch}
      onCatalogFilterChange={setCatalogFilter}
      selectedPatches={selectedPatches}
      togglePatch={togglePatch}
      agentList={agentList}
      agentQuery={agentQuery}
      onAgentQueryChange={setAgentQuery}
      selectedAgents={selectedAgents}
      toggleAgent={toggleAgent}
      selectAllAgents={selectAllAgents}
      clearAllAgents={clearAllAgents}
      agents={agents}
      name={name}
      setName={setName}
      description={description}
      setDescription={setDescription}
      strategy={strategy}
      setStrategy={setStrategy}
      batchSize={batchSize}
      setBatchSize={setBatchSize}
      batchIntervalMinutes={batchIntervalMinutes}
      setBatchIntervalMinutes={setBatchIntervalMinutes}
      rebootPolicy={rebootPolicy}
      setRebootPolicy={setRebootPolicy}
      maintenanceStart={maintenanceStart}
      setMaintenanceStart={setMaintenanceStart}
      maintenanceEnd={maintenanceEnd}
      setMaintenanceEnd={setMaintenanceEnd}
      onClose={onClose}
      goBack={goBack}
      goNext={goNext}
      canGoNext={canGoNext}
      submit={submit}
    />
  );
}
