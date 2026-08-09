// Check detail page state and effects — extracted for file-size compliance.

import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { toast } from 'sonner';
import { useAgents, type Agent } from '@/lib/useAgents';
import {
  useChecks,
  type Check,
  type CheckResult,
  type CheckStatus,
  type AgentAssignment,
} from '@/lib/useChecks';

export interface CheckDetailState {
  check: Check | null;
  assignments: AgentAssignment[];
  results: CheckResult[];
  isLoading: boolean;
  error: string | null;
  now: number;
  showAssign: boolean;
  showEdit: boolean;
  busy: boolean;
  agents: Agent[];
  navigate: ReturnType<typeof useNavigate>;
  setShowAssign: (v: boolean) => void;
  setShowEdit: (v: boolean) => void;
  onToggleEnabled: () => Promise<void>;
  onRunNow: () => Promise<void>;
  onDelete: () => Promise<void>;
  onSaveEdit: (patch: { name?: string; interval_secs?: number; config?: Record<string, unknown> }) => Promise<void>;
  onAssign: (agentId: string) => Promise<void>;
  onUnassign: (agentId: string) => Promise<void>;
  reload: () => Promise<void>;
}

export function useCheckDetail(checkId: string): CheckDetailState {
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

  return {
    check, assignments, results, isLoading, error, now,
    showAssign, showEdit, busy, agents, navigate,
    setShowAssign, setShowEdit,
    onToggleEnabled, onRunNow, onDelete, onSaveEdit, onAssign, onUnassign, reload,
  };
}
