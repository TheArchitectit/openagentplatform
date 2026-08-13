// useA2A — pure (non-React) REST operations and SSE streaming helpers.
// Re-exports from useA2A.ts; see useA2A.ts for the React hooks.

import { apiFetch } from './api';
import { A2A } from './useA2A_types';
import type {
  A2AAdapter, A2ATask, A2ATaskStatus, A2AMessage,
  A2AArtifact, A2AInvokeResult, A2ACostSummary,
} from './useA2A_types';

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
