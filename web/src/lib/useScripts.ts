// useScripts — manages script CRUD and live run updates from the WebSocket.
//
// The hook exposes:
//   scripts:        the current list (initial fetch + applied WS events)
//   isLoading:      true while the initial REST request is in flight
//   error:          any error from the initial fetch
//   refresh():      re-runs the REST fetch
//   status:         WebSocket connection status (cosmetic)
//   fetchScript:    fetch a single script on demand
//   createScript:   POST a new script
//   updateScript:   PATCH an existing script
//   deleteScript:   DELETE a script
//   runScript:      trigger a run-now for a script (returns the run id)
//   cancelRun:      cancel an in-progress run
//   fetchRuns:      fetch recent runs for a script
//   fetchRun:       fetch a single run detail (with output)
//
// WebSocket integration:
//   The "scripts" channel delivers events like:
//     { type: "event", channel: "scripts", event: "script.created",   data: Script }
//     { type: "event", channel: "scripts", event: "script.updated",   data: Script }
//     { type: "event", channel: "scripts", event: "script.deleted",   data: { id } }
//     { type: "event", channel: "scripts", event: "script.run.started", data: ScriptRun }
//     { type: "event", channel: "scripts", event: "script.run.update",  data: ScriptRun }
//     { type: "event", channel: "scripts", event: "script.run.output",  data: { run_id, stream, data } }
//     { type: "event", channel: "scripts", event: "script.run.completed", data: ScriptRun }

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import { getWsClient, type WsEnvelope, type Status } from './websocket';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ScriptRuntime = 'bash' | 'powershell' | 'python' | 'node';

export interface Script {
  id: string;
  name: string;
  description?: string;
  runtime: ScriptRuntime;
  content: string;
  timeout_secs: number;
  tags?: string[];
  site_id?: string;
  created_at?: string;
  updated_at?: string;
  // Derived/aggregated fields the server may include in list responses:
  last_run?: string;
  last_status?: ScriptRunStatus;
  run_count?: number;
}

export type ScriptRunStatus =
  | 'pending'
  | 'in_progress'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'timeout';

export interface ScriptRun {
  id: string;
  script_id: string;
  agent_id: string;
  hostname?: string;
  status: ScriptRunStatus;
  started_at: string;
  finished_at?: string;
  duration_ms?: number;
  exit_code?: number;
  triggered_by?: string;
  stdout?: string;
  stderr?: string;
}

export interface CreateScriptInput {
  name: string;
  description?: string;
  runtime: ScriptRuntime;
  content: string;
  timeout_secs?: number;
  tags?: string[];
}

export interface UpdateScriptInput {
  name?: string;
  description?: string;
  runtime?: ScriptRuntime;
  content?: string;
  timeout_secs?: number;
  tags?: string[];
}

export interface UseScriptsResult {
  scripts: Script[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  status: Status;
  fetchScript: (id: string) => Promise<Script>;
  createScript: (input: CreateScriptInput) => Promise<Script>;
  updateScript: (id: string, input: UpdateScriptInput) => Promise<Script>;
  deleteScript: (id: string) => Promise<void>;
  runScript: (id: string, agentIds: string[]) => Promise<ScriptRun>;
  cancelRun: (runId: string) => Promise<void>;
  fetchRuns: (scriptId: string, limit?: number) => Promise<ScriptRun[]>;
  fetchRun: (runId: string) => Promise<ScriptRun>;
}

// ---------------------------------------------------------------------------
// WebSocket envelope narrowing
// ---------------------------------------------------------------------------

type WsScriptEvent =
  | { event: 'script.created'; data: Script }
  | { event: 'script.updated'; data: Script }
  | { event: 'script.deleted'; data: { id: string } }
  | { event: 'script.run.started' | 'script.run.update' | 'script.run.completed'; data: ScriptRun }
  | { event: 'script.run.output'; data: { run_id: string; stream: 'stdout' | 'stderr'; data: string } };

function isScriptEvent(env: WsEnvelope): env is WsEnvelope & WsScriptEvent {
  if (env.type !== 'event' || env.channel !== 'scripts') return false;
  const ev = env.event;
  if (
    ev !== 'script.created' &&
    ev !== 'script.updated' &&
    ev !== 'script.deleted' &&
    ev !== 'script.run.started' &&
    ev !== 'script.run.update' &&
    ev !== 'script.run.completed' &&
    ev !== 'script.run.output'
  ) {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useScripts(): UseScriptsResult {
  const [scripts, setScripts] = useState<Script[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [status, setStatus] = useState<Status>('closed');
  const mountedRef = useRef(true);

  const fetchScripts = useCallback(async () => {
    try {
      const res = await apiFetch<{ scripts: Script[]; total: number } | Script[]>(
        '/scripts?limit=500'
      );
      if (!mountedRef.current) return;
      const list = Array.isArray(res) ? res : (res.scripts ?? []);
      setScripts(list);
      setTotal(Array.isArray(res) ? res.length : (res.total ?? list.length));
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
    void fetchScripts();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchScripts]);

  // Subscribe to the "scripts" channel.
  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());

    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const channelHandler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isScriptEvent(env)) return;

      const payload = env.data as unknown;

      if (env.event === 'script.created') {
        const s = payload as Script;
        setScripts((prev) => {
          if (prev.some((p) => p.id === s.id)) return prev;
          return [s, ...prev];
        });
        return;
      }

      if (env.event === 'script.updated') {
        const s = payload as Script;
        setScripts((prev) => prev.map((p) => (p.id === s.id ? { ...p, ...s } : p)));
        return;
      }

      if (env.event === 'script.deleted') {
        const { id } = payload as { id: string };
        setScripts((prev) => prev.filter((p) => p.id !== id));
        return;
      }
    };

    const unsub = ws.subscribe('scripts', channelHandler);
    return () => {
      clearInterval(statusInterval);
      unsub();
    };
  }, []);

  // -----------------------------------------------------------------------
  // Single-record + mutating actions
  // -----------------------------------------------------------------------

  const fetchScript = useCallback(async (id: string): Promise<Script> => {
    const s = await apiFetch<Script>(`/scripts/${encodeURIComponent(id)}`);
    setScripts((prev) => {
      const idx = prev.findIndex((p) => p.id === s.id);
      if (idx === -1) return [s, ...prev];
      const next = prev.slice();
      next[idx] = { ...next[idx], ...s };
      return next;
    });
    return s;
  }, []);

  const createScript = useCallback(async (input: CreateScriptInput): Promise<Script> => {
    const s = await apiFetch<Script>('/scripts', {
      method: 'POST',
      json: {
        timeout_secs: 60,
        ...input,
      },
    });
    setScripts((prev) => {
      if (prev.some((p) => p.id === s.id)) return prev;
      return [s, ...prev];
    });
    return s;
  }, []);

  const updateScript = useCallback(
    async (id: string, input: UpdateScriptInput): Promise<Script> => {
      const s = await apiFetch<Script>(`/scripts/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        json: input,
      });
      setScripts((prev) => prev.map((p) => (p.id === id ? { ...p, ...s } : p)));
      return s;
    },
    []
  );

  const deleteScript = useCallback(async (id: string): Promise<void> => {
    await apiFetch<void>(`/scripts/${encodeURIComponent(id)}`, { method: 'DELETE' });
    setScripts((prev) => prev.filter((p) => p.id !== id));
  }, []);

  const runScript = useCallback(
    async (id: string, agentIds: string[]): Promise<ScriptRun> => {
      const res = await apiFetch<ScriptRun | { runs: ScriptRun[] }>(
        `/scripts/${encodeURIComponent(id)}/run`,
        {
          method: 'POST',
          json: { agent_ids: agentIds },
        }
      );
      // If the server returns a single run, return it; otherwise pick the
      // first run from the list.
      if (!Array.isArray(res) && 'runs' in res) {
        return res.runs[0];
      }
      return res as ScriptRun;
    },
    []
  );

  const cancelRun = useCallback(async (runId: string): Promise<void> => {
    await apiFetch<void>(`/script-runs/${encodeURIComponent(runId)}/cancel`, {
      method: 'POST',
    });
  }, []);

  const fetchRuns = useCallback(
    async (scriptId: string, limit = 50): Promise<ScriptRun[]> => {
      const res = await apiFetch<{ runs: ScriptRun[] } | ScriptRun[]>(
        `/scripts/${encodeURIComponent(scriptId)}/runs?limit=${limit}`
      );
      return Array.isArray(res) ? res : (res.runs ?? []);
    },
    []
  );

  const fetchRun = useCallback(async (runId: string): Promise<ScriptRun> => {
    return apiFetch<ScriptRun>(`/script-runs/${encodeURIComponent(runId)}`);
  }, []);

  return {
    scripts,
    total,
    isLoading,
    error,
    refresh: fetchScripts,
    status,
    fetchScript,
    createScript,
    updateScript,
    deleteScript,
    runScript,
    cancelRun,
    fetchRuns,
    fetchRun,
  };
}
