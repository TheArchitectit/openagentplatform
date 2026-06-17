// useA2A — React hook for the A2A (Agent-to-Agent) protocol dashboard.
//
// Provides:
//   - REST methods: fetchAdapters, fetchAdapterCard, fetchAdapterHealth,
//     invokeAdapter, cancelTask, fetchTasks, fetchTask, fetchCostSummary
//   - SSE streaming: streamAdapter (returns a cancel function)
//   - Real-time task events: subscribeTaskEvents
//
// API paths are rooted at the A2A proxy base (/api/v1/a2a/...) which
// forwards to the Python adapter service. The apiFetch helper prepends
// /api/v1, so A2A paths are written as /a2a/... (relative to that prefix).

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface A2AAdapter {
  name: string;
  display_name?: string;
  version: string;
  description?: string;
  provider?: string;
  url?: string;
  icon?: string;
  health: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  streaming: boolean;
  skills: A2ASkill[];
  models: A2AModel[];
  uptime_secs?: number;
  active_tasks?: number;
  memory_mb?: number;
}

export interface A2ASkill {
  name: string;
  description: string;
  tags: string[];
  input_schema?: Record<string, unknown>;
  output_schema?: Record<string, unknown>;
  examples?: A2ASkillExample[];
}

export interface A2ASkillExample {
  name?: string;
  description?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
}

export interface A2AModel {
  name: string;
  input_cost_per_1k: number;
  output_cost_per_1k: number;
  currency?: string;
}

export interface A2ATask {
  id: string;
  adapter: string;
  status: A2ATaskStatus;
  messages: A2AMessage[];
  artifacts: A2AArtifact[];
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at?: string;
  completed_at?: string;
  duration_ms?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cost?: number;
  model?: string;
}

export type A2ATaskStatus =
  | 'pending'
  | 'working'
  | 'input_required'
  | 'completed'
  | 'failed'
  | 'cancelled';

export interface A2AMessage {
  role: 'user' | 'agent' | 'system';
  parts: A2APart[];
  timestamp?: string;
}

export interface A2APart {
  type: 'text' | 'file' | 'data';
  text?: string;
  url?: string;
  mime_type?: string;
  filename?: string;
  data?: Record<string, unknown>;
}

export interface A2AArtifact {
  id: string;
  name: string;
  description?: string;
  parts: A2APart[];
  created_at?: string;
}

export interface A2AInvokeResult {
  task_id: string;
  status: A2ATaskStatus;
  messages?: A2AMessage[];
  artifacts?: A2AArtifact[];
  error?: string;
}

export interface A2ACostSummary {
  total_cost: number;
  currency: string;
  by_adapter: A2ACostByAdapter[];
  by_model: A2ACostByModel[];
  by_day: A2ACostByDay[];
  by_org: A2ACostByOrg[];
  date_range: { start: string; end: string };
}

export interface A2ACostByAdapter {
  adapter: string;
  tasks: number;
  tokens: number;
  cost: number;
  percent_of_total: number;
}

export interface A2ACostByModel {
  model: string;
  tasks: number;
  tokens: number;
  cost: number;
  percent_of_total: number;
}

export interface A2ACostByDay {
  date: string;
  cost: number;
  tasks: number;
}

export interface A2ACostByOrg {
  org_id: string;
  org_name?: string;
  spend: number;
  budget: number;
  percent_used: number;
  status: 'ok' | 'warning' | 'critical' | 'exceeded';
}

// ---------------------------------------------------------------------------
// REST helpers — plain functions, usable outside of React components.
// ---------------------------------------------------------------------------

// A2A API base path. The frontend apiFetch prepends /api/v1, so the
// proxy routes are at /api/v1/a2a/... — we use /a2a/... as the relative
// segment. This matches the proxy handlers in internal/api/routes.go.
const A2A = '/a2a';

export async function fetchAdapters(): Promise<A2AAdapter[]> {
  const res = await apiFetch<{ adapters: A2AAdapter[] } | A2AAdapter[]>(`${A2A}/adapters`);
  return Array.isArray(res) ? res : (res.adapters ?? []);
}

export async function fetchAdapterCard(name: string): Promise<A2AAdapter> {
  return apiFetch<A2AAdapter>(`${A2A}/adapters/${encodeURIComponent(name)}/card`);
}

export async function fetchAdapterHealth(name: string): Promise<{
  status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
  uptime_secs: number;
  active_tasks: number;
  memory_mb: number;
}> {
  return apiFetch(`${A2A}/adapters/${encodeURIComponent(name)}/health`);
}

export interface InvokeInput {
  adapter: string;
  message: string;
  skill?: string;
  model?: string;
  metadata?: Record<string, unknown>;
}

export async function invokeAdapter(input: InvokeInput): Promise<A2AInvokeResult> {
  return apiFetch<A2AInvokeResult>(`${A2A}/invoke`, {
    method: 'POST',
    json: {
      adapter: input.adapter,
      message: input.message,
      skill: input.skill,
      model: input.model,
      metadata: input.metadata,
    },
  });
}

export async function cancelTask(taskId: string): Promise<void> {
  await apiFetch<void>(`${A2A}/tasks/${encodeURIComponent(taskId)}/cancel`, {
    method: 'POST',
  });
}

export interface FetchTasksParams {
  status?: A2ATaskStatus;
  adapter?: string;
  limit?: number;
  offset?: number;
}

export async function fetchTasks(params: FetchTasksParams = {}): Promise<A2ATask[]> {
  const qs = new URLSearchParams();
  if (params.status) qs.set('status', params.status);
  if (params.adapter) qs.set('adapter', params.adapter);
  if (params.limit) qs.set('limit', String(params.limit));
  if (params.offset) qs.set('offset', String(params.offset));
  const q = qs.toString();
  const path = q ? `${A2A}/tasks?${q}` : `${A2A}/tasks`;
  const res = await apiFetch<{ tasks: A2ATask[] } | A2ATask[]>(path);
  return Array.isArray(res) ? res : (res.tasks ?? []);
}

export async function fetchTask(taskId: string): Promise<A2ATask> {
  return apiFetch<A2ATask>(`${A2A}/tasks/${encodeURIComponent(taskId)}`);
}

export interface FetchCostParams {
  start?: string;
  end?: string;
}

export async function fetchCostSummary(params: FetchCostParams = {}): Promise<A2ACostSummary> {
  const qs = new URLSearchParams();
  if (params.start) qs.set('start', params.start);
  if (params.end) qs.set('end', params.end);
  const q = qs.toString();
  const path = q ? `${A2A}/costs/summary?${q}` : `${A2A}/costs/summary`;
  return apiFetch<A2ACostSummary>(path);
}

// ---------------------------------------------------------------------------
// SSE streaming
// ---------------------------------------------------------------------------

export interface StreamChunk {
  type: 'message' | 'artifact' | 'status' | 'error' | 'done';
  message?: A2AMessage;
  artifact?: A2AArtifact;
  status?: A2ATaskStatus;
  error?: string;
  task_id?: string;
}

export type StreamHandler = (chunk: StreamChunk) => void;

/**
 * Open a Server-Sent Events stream to the A2A gateway and invoke the given
 * adapter. Returns a cancel function that aborts the underlying connection.
 */
export function streamAdapter(
  input: InvokeInput,
  handler: StreamHandler
): () => void {
  const controller = new AbortController();
  // The stream endpoint is proxied at /api/v1/a2a/stream. We construct
  // the full path manually here because EventSource/fetch does not
  // go through apiFetch (which would JSON-encode the body).
  const url = `/api/v1${A2A}/stream`;

  // We use fetch + ReadableStream because the native EventSource API does
  // not support POST bodies. The response is expected to be text/event-stream.
  void (async () => {
    try {
      const res = await fetch(
        url.startsWith('http') ? url : url,
        {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
          body: JSON.stringify(input),
          signal: controller.signal,
        }
      );
      if (!res.ok || !res.body) {
        handler({ type: 'error', error: `Stream failed: ${res.status} ${res.statusText}` });
        return;
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        // SSE messages are separated by a blank line.
        const parts = buf.split('\n\n');
        buf = parts.pop() ?? '';
        for (const part of parts) {
          const line = part.trim();
          if (!line.startsWith('data:')) continue;
          const payload = line.slice(5).trim();
          if (payload === '[DONE]') {
            handler({ type: 'done' });
            return;
          }
          try {
            const chunk = JSON.parse(payload) as StreamChunk;
            handler(chunk);
          } catch {
            // Ignore malformed chunks.
          }
        }
      }
      handler({ type: 'done' });
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      handler({ type: 'error', error: (err as Error).message });
    }
  })();

  return () => controller.abort();
}

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
        setSseConnected(false);
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
