// Policy detail page state and effects — extracted for file-size compliance.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAgents, type Agent } from '@/lib/useAgents';
import {
  usePolicies,
  type Policy,
  type PolicyAssignment,
  type PolicyViolation,
  type ComplianceSummary,
} from '@/lib/usePolicies';
import type { ApiError } from '@/lib/api';

export interface PolicyDetailState {
  policy: Policy | null;
  enabled: boolean;
  isLoading: boolean;
  loadError: string | null;
  editMode: boolean;
  editorOpen: boolean;
  savingToggle: boolean;
  evaluating: boolean;
  assignments: PolicyAssignment[];
  violations: PolicyViolation[];
  compliance: ComplianceSummary | null;
  showAssignPicker: boolean;
  savingEditor: boolean;
  savingEditorError: string | null;
  now: number;
  navigate: ReturnType<typeof useNavigate>;
  complianceCounts: { compliant: number; nonCompliant: number; total: number };
  compliancePct: number | undefined;
  donutDashArray: number;
  donutDashOffset: number;
  donutRadius: number;
  donutStroke: number;
  donutSize: number;
  availableAgents: Agent[];
  setEnabled: React.Dispatch<React.SetStateAction<boolean>>;
  setEditMode: React.Dispatch<React.SetStateAction<boolean>>;
  setEditorOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setShowAssignPicker: React.Dispatch<React.SetStateAction<boolean>>;
  handleToggleEnabled: () => Promise<void>;
  handleEvaluate: () => Promise<void>;
  handleDelete: () => Promise<void>;
  handleRemoveAssignment: (agentId: string) => Promise<void>;
  handleAddAssignment: (agentId: string) => Promise<void>;
  handleDismissViolation: (id: string) => Promise<void>;
  handleEditorSave: (
    input:
      | Parameters<ReturnType<typeof usePolicies>['createPolicy']>[0]
      | { id: string; input: Parameters<ReturnType<typeof usePolicies>['updatePolicy']>[1] }
  ) => Promise<void>;
}

export function usePolicyDetail(policyId: string): PolicyDetailState {
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

  return {
    policy, enabled, isLoading, loadError, editMode, editorOpen,
    savingToggle, evaluating, assignments, violations, compliance,
    showAssignPicker, savingEditor, savingEditorError, now, navigate,
    complianceCounts, compliancePct,
    donutDashArray, donutDashOffset, donutRadius, donutStroke, donutSize,
    availableAgents,
    setEnabled, setEditMode, setEditorOpen, setShowAssignPicker,
    handleToggleEnabled, handleEvaluate, handleDelete,
    handleRemoveAssignment, handleAddAssignment, handleDismissViolation,
    handleEditorSave,
  };
}
