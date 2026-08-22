// useA2A — React hook for the A2A (Agent-to-Agent) protocol dashboard.
//
// Provides: React hooks (useA2AAdapters, useA2ATasks, useA2ACost) plus the
// re-exported pure REST operations / SSE streaming helpers from
// useA2A.operations.ts.
//
// API paths are rooted at the A2A proxy base (/api/v1/a2a/...) which
// forwards to the Python adapter service. The apiFetch helper prepends
// /api/v1, so A2A paths are written as /a2a/... (relative to that prefix).

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';

import { A2A } from './useA2A_types'
import type {
  A2AAdapter, A2ATask, A2ATaskStatus, A2ACostSummary,
} from './useA2A_types'

export type {
  A2AAdapter, A2ASkill, A2ASkillExample, A2AModel,
  A2ATask, A2ATaskStatus, A2AMessage, A2APart,
  A2AArtifact, A2AInvokeResult, A2ACostSummary,
  A2ACostByAdapter, A2ACostByModel, A2ACostByDay, A2ACostByOrg,
} from './useA2A_types'

// Pure (non-React) operations and SSE streaming helpers.
export {
  fetchAdapters, fetchAdapterCard, fetchAdapterHealth,
  invokeAdapter, cancelTask, fetchTasks, fetchTask, fetchCostSummary,
  streamAdapter,
} from './useA2A.operations'
export type {
  InvokeInput, FetchTasksParams, FetchCostParams,
  StreamChunk, StreamHandler,
} from './useA2A.operations'

import {
  fetchAdapters, fetchTasks, fetchCostSummary,
} from './useA2A.operations'


// ---------------------------------------------------------------------------
// React hook for adapter list with optional real-time health pings.
// ---------------------------------------------------------------------------

export interface UseA2AAdaptersResult {
  adapters: A2AAdapter[];
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
}

export function useA2AAdapters(): UseA2AAdaptersResult {
  const [adapters, setAdapters] = useState<A2AAdapter[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const list = await fetchAdapters();
      if (!mountedRef.current) return;
      setAdapters(list);
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
    void refresh();
    return () => {
      mountedRef.current = false;
    };
  }, [refresh]);

  return { adapters, isLoading, error, refresh };
}

// ---------------------------------------------------------------------------
// React hook for the task list with real-time SSE updates.
// ---------------------------------------------------------------------------

export interface UseA2ATasksParams {
  status?: A2ATaskStatus;
  autoRefresh?: boolean;
}

export interface UseA2ATasksResult {
  tasks: A2ATask[];
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  sseConnected: boolean;
}

const TASK_SSE_PATH = `/api/v1${A2A}/tasks/events`;

export function useA2ATasks(params: UseA2ATasksParams = {}): UseA2ATasksResult {
  const { status, autoRefresh = true } = params;
  const [tasks, setTasks] = useState<A2ATask[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [sseConnected, setSseConnected] = useState(false);
  const mountedRef = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const list = await fetchTasks({ status, limit: 200 });
      if (!mountedRef.current) return;
      setTasks(list);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [status]);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    void refresh();
    return () => {
      mountedRef.current = false;
    };
  }, [refresh]);

  // Subscribe to task events via SSE for live status changes.
  useEffect(() => {
    if (!autoRefresh) return;
    let es: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      try {
        es = new EventSource(TASK_SSE_PATH, { withCredentials: true } as EventSourceInit);
      } catch {
        scheduleReconnect();
        return;
      }
      es.onopen = () => {
        if (mountedRef.current) setSseConnected(true);
      };
      es.onerror = () => {
        if (mountedRef.current) {
          setSseConnected(false);
          setError((prev) => prev ?? new ApiError(0, 'SSEError', 'Task-event stream disconnected'));
        }
        es?.close();
        scheduleReconnect();
      };
      es.onmessage = (ev) => {
        if (!mountedRef.current) return;
        try {
          const payload = JSON.parse(ev.data) as { task: A2ATask; event: string };
          if (!payload?.task) return;
          setTasks((prev) => {
            const idx = prev.findIndex((t) => t.id === payload.task.id);
            if (idx === -1) return [payload.task, ...prev];
            const next = prev.slice();
            next[idx] = { ...next[idx], ...payload.task };
            return next;
          });
        } catch {
          /* ignore malformed */
        }
      };
    };

    const scheduleReconnect = () => {
      if (reconnectTimer) return;
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null;
        if (mountedRef.current) connect();
      }, 3000);
    };

    connect();
    return () => {
      if (reconnectTimer) clearTimeout(reconnectTimer);
      es?.close();
      setSseConnected(false);
    };
  }, [autoRefresh]);

  return { tasks, isLoading, error, refresh, sseConnected };
}

// ---------------------------------------------------------------------------
// Cost summary hook
// ---------------------------------------------------------------------------

export interface UseA2ACostParams {
  start?: string;
  end?: string;
}

export function useA2ACost(params: UseA2ACostParams = {}) {
  const [summary, setSummary] = useState<A2ACostSummary | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const res = await fetchCostSummary(params);
      if (!mountedRef.current) return;
      setSummary(res);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new ApiError(0, 'Unknown', String(err)));
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [params.start, params.end]);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    void refresh();
    return () => {
      mountedRef.current = false;
    };
  }, [refresh]);

  return { summary, isLoading, error, refresh };
}
