// useChecks — manages check CRUD and live updates from the WebSocket.
//
// The hook exposes:
//   checks:         the current list (initial fetch + applied WS events)
//   isLoading:      true while the initial REST request is in flight
//   error:          any error from the initial fetch
//   refresh():      re-runs the REST fetch
//   status:         WebSocket connection status (cosmetic)
//   fetchCheck(id): fetch a single check detail on demand
//   createCheck:    POST a new check definition
//   updateCheck:    PATCH an existing check (name/config/enabled/interval)
//   deleteCheck:    DELETE a check
//   runCheck:       trigger a run-now for a check
//   assignAgent:    assign an agent to a check
//   unassignAgent:  remove an agent assignment
//   fetchResults:   fetch recent results for a check (for the detail page)
//
// WebSocket integration:
//   The "checks" channel delivers events like:
//     { type: "event", channel: "checks", event: "check.created",   data: Check }
//     { type: "event", channel: "checks", event: "check.updated",   data: Check }
//     { type: "event", channel: "checks", event: "check.deleted",   data: { id } }
//     { type: "event", channel: "checks", event: "check.result",    data: CheckResult }
//     { type: "event", channel: "checks", event: "check.completed", data: CheckResult }

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import { getWsClient, type WsEnvelope, type Status } from './websocket';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type CheckStatus = 'ok' | 'warning' | 'critical' | 'disabled';
export type CheckType =
  | 'http'
  | 'tcp'
  | 'ping'
  | 'disk_usage'
  | 'memory_usage'
  | 'cpu_usage'
  | 'process'
  | 'service'
  | 'tls_cert'
  | 'script'
  | 'log_watch';

export interface Check {
  id: string;
  name: string;
  type: CheckType;
  config: Record<string, unknown>;
  interval_secs: number;
  enabled: boolean;
  site_id?: string;
  created_at?: string;
  updated_at?: string;
  // Derived/aggregated fields the server may include in list responses:
  last_status?: CheckStatus;
  last_run?: string;
  assigned_agents?: number;
}

export interface CheckResult {
  id?: string;
  check_id: string;
  agent_id: string;
  timestamp: string;
  status: CheckStatus | string;
  value?: number;
  message?: string;
  duration_ms?: number;
}

export interface AgentAssignment {
  id: string;
  agent_id: string;
  check_id: string;
  hostname?: string;
  last_status?: CheckStatus | string;
  last_run?: string;
  enabled: boolean;
}

export interface UseChecksResult {
  checks: Check[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  status: Status;
  fetchCheck: (id: string) => Promise<Check>;
  createCheck: (input: CreateCheckInput) => Promise<Check>;
  updateCheck: (id: string, input: UpdateCheckInput) => Promise<Check>;
  deleteCheck: (id: string) => Promise<void>;
  runCheck: (id: string) => Promise<void>;
  assignAgent: (checkId: string, agentId: string) => Promise<void>;
  unassignAgent: (checkId: string, agentId: string) => Promise<void>;
  fetchResults: (checkId: string, limit?: number) => Promise<CheckResult[]>;
  fetchAssignments: (checkId: string) => Promise<AgentAssignment[]>;
}

export interface CreateCheckInput {
  name: string;
  type: CheckType;
  config: Record<string, unknown>;
  interval_secs: number;
  enabled?: boolean;
  site_id?: string;
}

export interface UpdateCheckInput {
  name?: string;
  config?: Record<string, unknown>;
  interval_secs?: number;
  enabled?: boolean;
}

interface CheckListResponse {
  checks: Check[];
  total: number;
  limit: number;
  offset: number;
}

// ---------------------------------------------------------------------------
// WebSocket envelope narrowing
// ---------------------------------------------------------------------------

type WsCheckEvent =
  | { event: 'check.created'; data: Check }
  | { event: 'check.updated'; data: Check }
  | { event: 'check.deleted'; data: { id: string } }
  | { event: 'check.result' | 'check.completed'; data: CheckResult };

function isCheckEvent(env: WsEnvelope): env is WsEnvelope & WsCheckEvent {
  if (env.type !== 'event' || env.channel !== 'checks') return false;
  const ev = env.event;
  if (ev !== 'check.created' && ev !== 'check.updated' && ev !== 'check.deleted' && ev !== 'check.result' && ev !== 'check.completed') {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useChecks(): UseChecksResult {
  const [checks, setChecks] = useState<Check[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [status, setStatus] = useState<Status>('closed');
  const mountedRef = useRef(true);

  const fetchChecks = useCallback(async () => {
    try {
      const res = await apiFetch<CheckListResponse>('/checks?limit=500');
      if (!mountedRef.current) return;
      setChecks(res.checks ?? []);
      setTotal(res.total ?? (res.checks?.length ?? 0));
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
    void fetchChecks();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchChecks]);

  // Subscribe to the "checks" channel.
  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());

    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const channelHandler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isCheckEvent(env)) return;

      const payload = env.data as unknown;

      if (env.event === 'check.created') {
        const c = payload as Check;
        setChecks((prev) => {
          if (prev.some((p) => p.id === c.id)) return prev;
          return [c, ...prev];
        });
        return;
      }

      if (env.event === 'check.updated') {
        const c = payload as Check;
        setChecks((prev) => prev.map((p) => (p.id === c.id ? { ...p, ...c } : p)));
        return;
      }

      if (env.event === 'check.deleted') {
        const { id } = payload as { id: string };
        setChecks((prev) => prev.filter((p) => p.id !== id));
        return;
      }

      if (env.event === 'check.result' || env.event === 'check.completed') {
        const r = payload as CheckResult;
        setChecks((prev) =>
          prev.map((p) =>
            p.id === r.check_id
              ? {
                  ...p,
                  last_status: (r.status as CheckStatus) ?? p.last_status,
                  last_run: r.timestamp ?? p.last_run,
                }
              : p
          )
        );
        return;
      }
    };

    const unsub = ws.subscribe('checks', channelHandler);
    return () => {
      clearInterval(statusInterval);
      unsub();
    };
  }, []);

  // -----------------------------------------------------------------------
  // Single-record + mutating actions
  // -----------------------------------------------------------------------

  const fetchCheck = useCallback(async (id: string): Promise<Check> => {
    const c = await apiFetch<Check>(`/checks/${encodeURIComponent(id)}`);
    // Merge into the list so the detail page reflects the latest state.
    setChecks((prev) => {
      const idx = prev.findIndex((p) => p.id === c.id);
      if (idx === -1) return [c, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...c };
      return next;
    });
    return c;
  }, []);

  const createCheck = useCallback(async (input: CreateCheckInput): Promise<Check> => {
    const c = await apiFetch<Check>('/checks', {
      method: 'POST',
      json: {
        enabled: true,
        ...input,
      },
    });
    setChecks((prev) => {
      if (prev.some((p) => p.id === c.id)) return prev;
      return [c, ...prev];
    });
    return c;
  }, []);

  const updateCheck = useCallback(
    async (id: string, input: UpdateCheckInput): Promise<Check> => {
      const c = await apiFetch<Check>(`/checks/${encodeURIComponent(id)}`, {
        method: 'PUT',
        json: input,
      });
      setChecks((prev) => prev.map((p) => (p.id === id ? { ...p, ...c } : p)));
      return c;
    },
    []
  );

  const deleteCheck = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/checks/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setChecks((prev) => prev.filter((p) => p.id !== id));
  }, []);

  const runCheck = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/checks/${encodeURIComponent(id)}/run`, { method: 'POST' });
  }, []);

  const assignAgent = useCallback(async (checkId: string, agentId: string): Promise<void> => {
    await apiFetch<void>(`/checks/${encodeURIComponent(checkId)}/assign`, {
      method: 'POST',
      json: { agent_id: agentId },
    });
    // Optimistic bump on the agent count.
    setChecks((prev) =>
      prev.map((p) =>
        p.id === checkId ? { ...p, assigned_agents: (p.assigned_agents ?? 0) + 1 } : p
      )
    );
  }, []);

  const unassignAgent = useCallback(async (checkId: string, agentId: string): Promise<void> => {
    await apiFetch<void>(`/checks/${encodeURIComponent(checkId)}/assign/${encodeURIComponent(agentId)}`, {
      method: 'DELETE',
    });
    setChecks((prev) =>
      prev.map((p) =>
        p.id === checkId
          ? { ...p, assigned_agents: Math.max(0, (p.assigned_agents ?? 1) - 1) }
          : p
      )
    );
  }, []);

  const fetchResults = useCallback(async (checkId: string, limit = 50): Promise<CheckResult[]> => {
    const res = await apiFetch<{ results: CheckResult[] } | CheckResult[]>(
      `/checks/${encodeURIComponent(checkId)}/results?limit=${limit}`
    );
    return Array.isArray(res) ? res : (res.results ?? []);
  }, []);

  const fetchAssignments = useCallback(async (checkId: string): Promise<AgentAssignment[]> => {
    const res = await apiFetch<{ agents: AgentAssignment[] } | AgentAssignment[]>(
      `/checks/${encodeURIComponent(checkId)}/assignments`
    );
    return Array.isArray(res) ? res : (res.agents ?? []);
  }, []);

  return {
    checks,
    total,
    isLoading,
    error,
    refresh: fetchChecks,
    status,
    fetchCheck,
    createCheck,
    updateCheck,
    deleteCheck,
    runCheck,
    assignAgent,
    unassignAgent,
    fetchResults,
    fetchAssignments,
  };
}
