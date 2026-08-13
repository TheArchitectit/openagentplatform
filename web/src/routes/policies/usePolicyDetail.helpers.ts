// Policy detail — shared types for usePolicyDetail (extracted for size gate).
import { useNavigate } from '@tanstack/react-router';
import type { Agent } from '@/lib/useAgents';
import {
  usePolicies,
  type Policy,
  type PolicyAssignment,
  type PolicyViolation,
  type ComplianceSummary,
  type PolicyValidationResult,
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
  setSavingEditorError: React.Dispatch<React.SetStateAction<string | null>>;
  validatePolicy: (regoSource: string) => Promise<PolicyValidationResult>;
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
