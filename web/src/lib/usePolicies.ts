// usePolicies — manages policy CRUD, violations, and compliance summaries.
//
// Exposed operations:
//   fetchPolicies / fetchPolicy
//   createPolicy / updatePolicy / deletePolicy
//   validatePolicy (syntax check via POST /policies validate)
//   evaluatePolicy (trigger evaluation on all assigned agents)
//   fetchAssignments / assignAgent / unassignAgent
//   fetchViolations / dismissViolation
//   fetchComplianceSummary

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type PolicyCategory = 'security' | 'compliance' | 'configuration' | 'performance' | 'custom';
export type PolicyEnforcement = 'enforce' | 'audit' | 'report';
export type PolicySeverity = 'info' | 'warning' | 'critical' | 'emergency';

export interface Policy {
  id: string;
  name: string;
  description?: string;
  category: PolicyCategory;
  severity: PolicySeverity;
  enforcement: PolicyEnforcement;
  rego_source: string;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
  // Aggregated/derived fields the server may include:
  compliance_pct?: number;
  agent_count?: number;
}

export interface CreatePolicyInput {
  name: string;
  description?: string;
  category: PolicyCategory;
  severity: PolicySeverity;
  enforcement: PolicyEnforcement;
  rego_source: string;
  enabled?: boolean;
}

export interface UpdatePolicyInput {
  name?: string;
  description?: string;
  category?: PolicyCategory;
  severity?: PolicySeverity;
  enforcement?: PolicyEnforcement;
  rego_source?: string;
  enabled?: boolean;
}

export interface PolicyAssignment {
  id: string;
  policy_id: string;
  agent_id: string;
  hostname?: string;
  compliant?: boolean;
  last_evaluated?: string;
}

export type ViolationStatus = 'open' | 'acknowledged' | 'resolved' | 'dismissed';

export interface PolicyViolation {
  id: string;
  policy_id: string;
  policy_name?: string;
  agent_id: string;
  hostname?: string;
  severity: PolicySeverity;
  status: ViolationStatus;
  message?: string;
  detected_at: string;
  resolved_at?: string;
}

export interface ComplianceSummary {
  total_policies: number;
  enabled_policies: number;
  total_agents: number;
  compliant_agents: number;
  non_compliant_agents: number;
  overall_compliance_pct: number;
  by_category: Record<PolicyCategory, { compliant: number; non_compliant: number; total: number }>;
}

export interface PolicyValidationResult {
  valid: boolean;
  errors?: string[];
  warnings?: string[];
}

export interface UsePoliciesResult {
  policies: Policy[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  fetchPolicy: (id: string) => Promise<Policy>;
  createPolicy: (input: CreatePolicyInput) => Promise<Policy>;
  updatePolicy: (id: string, input: UpdatePolicyInput) => Promise<Policy>;
  deletePolicy: (id: string) => Promise<void>;
  validatePolicy: (regoSource: string) => Promise<PolicyValidationResult>;
  evaluatePolicy: (id: string) => Promise<void>;
  fetchAssignments: (policyId: string) => Promise<PolicyAssignment[]>;
  assignAgent: (policyId: string, agentId: string) => Promise<void>;
  unassignAgent: (policyId: string, agentId: string) => Promise<void>;
  fetchViolations: (policyId?: string) => Promise<PolicyViolation[]>;
  dismissViolation: (id: string, note?: string) => Promise<PolicyViolation>;
  fetchComplianceSummary: () => Promise<ComplianceSummary>;
}

interface PolicyListResponse {
  policies: Policy[];
  total: number;
  limit: number;
  offset: number;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function usePolicies(): UsePoliciesResult {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const fetchPolicies = useCallback(async () => {
    try {
      const res = await apiFetch<PolicyListResponse>('/policies?limit=500');
      if (!mountedRef.current) return;
      setPolicies(res.policies ?? []);
      setTotal(res.total ?? (res.policies?.length ?? 0));
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    void fetchPolicies();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchPolicies]);

  const fetchPolicy = useCallback(async (id: string): Promise<Policy> => {
    const p = await apiFetch<Policy>(`/policies/${encodeURIComponent(id)}`);
    setPolicies((prev) => {
      const idx = prev.findIndex((x) => x.id === p.id);
      if (idx === -1) return [p, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...p };
      return next;
    });
    return p;
  }, []);

  const createPolicy = useCallback(async (input: CreatePolicyInput): Promise<Policy> => {
    const p = await apiFetch<Policy>('/policies', {
      method: 'POST',
      json: {
        enabled: true,
        ...input,
      },
    });
    setPolicies((prev) => {
      if (prev.some((x) => x.id === p.id)) return prev;
      return [p, ...prev];
    });
    return p;
  }, []);

  const updatePolicy = useCallback(
    async (id: string, input: UpdatePolicyInput): Promise<Policy> => {
      const p = await apiFetch<Policy>(`/policies/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        json: input,
      });
      setPolicies((prev) => prev.map((x) => (x.id === id ? { ...x, ...p } : x)));
      return p;
    },
    []
  );

  const deletePolicy = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/policies/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setPolicies((prev) => prev.filter((x) => x.id !== id));
  }, []);

  const validatePolicy = useCallback(async (regoSource: string): Promise<PolicyValidationResult> => {
    return apiFetch<PolicyValidationResult>('/policies/validate', {
      method: 'POST',
      json: { rego_source: regoSource },
    });
  }, []);

  const evaluatePolicy = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/policies/${encodeURIComponent(id)}/evaluate`, { method: 'POST' });
  }, []);

  const fetchAssignments = useCallback(async (policyId: string): Promise<PolicyAssignment[]> => {
    const res = await apiFetch<{ assignments: PolicyAssignment[] } | PolicyAssignment[]>(
      `/policies/${encodeURIComponent(policyId)}/assign`
    );
    return Array.isArray(res) ? res : (res.assignments ?? []);
  }, []);

  const assignAgent = useCallback(async (policyId: string, agentId: string): Promise<void> => {
    await apiFetch<void>(`/policies/${encodeURIComponent(policyId)}/assign`, {
      method: 'POST',
      json: { agent_id: agentId },
    });
  }, []);

  const unassignAgent = useCallback(
    async (policyId: string, agentId: string): Promise<void> => {
      await apiFetch<void>(
        `/policies/${encodeURIComponent(policyId)}/assign/${encodeURIComponent(agentId)}`,
        { method: 'DELETE' }
      );
    },
    []
  );

  const fetchViolations = useCallback(async (policyId?: string): Promise<PolicyViolation[]> => {
    const path = policyId
      ? `/policies/${encodeURIComponent(policyId)}/violations?limit=200`
      : `/violations?limit=200`;
    const res = await apiFetch<{ violations: PolicyViolation[] } | PolicyViolation[]>(path);
    return Array.isArray(res) ? res : (res.violations ?? []);
  }, []);

  const dismissViolation = useCallback(
    async (id: string, note?: string): Promise<PolicyViolation> => {
      return apiFetch<PolicyViolation>(
        `/violations/${encodeURIComponent(id)}/dismiss`,
        {
          method: 'POST',
          json: note ? { note } : undefined,
        }
      );
    },
    []
  );

  const fetchComplianceSummary = useCallback(async (): Promise<ComplianceSummary> => {
    return apiFetch<ComplianceSummary>('/compliance/summary');
  }, []);

  return {
    policies,
    total,
    isLoading,
    error,
    refresh: fetchPolicies,
    fetchPolicy,
    createPolicy,
    updatePolicy,
    deletePolicy,
    validatePolicy,
    evaluatePolicy,
    fetchAssignments,
    assignAgent,
    unassignAgent,
    fetchViolations,
    dismissViolation,
    fetchComplianceSummary,
  };
}

export default usePolicies;
