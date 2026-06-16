// useAgents — load the agent list from REST, then merge real-time
// heartbeat updates from the WebSocket into the in-memory state.
//
// The hook returns:
//   agents:     the current list (initial fetch + applied updates)
//   isLoading:  true while the initial REST request is in flight
//   error:      any error from the initial fetch
//   refresh():  re-runs the REST fetch
//   status:     ws connection status (cosmetic)

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import { getWsClient, type Channel, type WsEnvelope, type Status } from './websocket';

export interface Agent {
  id: string;
  site_id: string;
  org_id?: string;
  hostname: string;
  os: string;
  arch?: string;
  platform?: string;
  cpu_count: number;
  total_memory_mb: number;
  total_disk_gb: number;
  agent_version: string;
  version?: string;
  status: string;
  last_seen: string;
  tags?: string[];
  metadata?: Record<string, unknown>;
  // Real-time metrics pushed over WS, present once a heartbeat lands.
  cpu_percent?: number;
  mem_percent?: number;
  disk_percent?: number;
  uptime_secs?: number;
}

interface AgentListResponse {
  agents: Agent[];
  total: number;
  limit: number;
  offset: number;
}

export interface UseAgentsResult {
  agents: Agent[];
  total: number;
  isLoading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  status: Status;
}

interface HeartbeatEvent {
  agent_id: string;
  timestamp: string;
  cpu_percent: number;
  mem_percent: number;
  disk_percent: number;
  uptime_secs: number;
  version?: string;
}

function isHeartbeatEvent(env: WsEnvelope): env is WsEnvelope & { data: HeartbeatEvent } {
  return (
    env.type === 'event' &&
    env.channel === 'agents' &&
    env.event === 'heartbeat' &&
    typeof env.data === 'object' &&
    env.data !== null &&
    typeof (env.data as HeartbeatEvent).agent_id === 'string'
  );
}

export function useAgents(): UseAgentsResult {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [status, setStatus] = useState<Status>('closed');
  const mountedRef = useRef(true);

  const fetchAgents = useCallback(async () => {
    try {
      const res = await apiFetch<AgentListResponse>('/agents?limit=500');
      if (!mountedRef.current) return;
      setAgents(res.agents ?? []);
      setTotal(res.total ?? (res.agents?.length ?? 0));
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
    void fetchAgents();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchAgents]);

  // Subscribe to the "agents" channel and merge heartbeats into state.
  useEffect(() => {
    const ws = getWsClient();
    setStatus(ws.getStatus());

    const statusHandler = (s: Status) => {
      if (mountedRef.current) setStatus(s);
    };
    // We poll the status once per render via the snapshot above; in
    // addition we can listen for changes by re-reading on a low-rate
    // interval. This avoids needing to expose the singleton's listener
    // registry publicly.
    const statusInterval = setInterval(() => {
      if (!mountedRef.current) return;
      setStatus(ws.getStatus());
    }, 1000);

    const channelHandler = (env: WsEnvelope) => {
      if (!mountedRef.current) return;
      if (!isHeartbeatEvent(env)) return;
      const hb = env.data;
      setAgents((prev) => {
        let found = false;
        const next = prev.map((a) => {
          if (a.id !== hb.agent_id) return a;
          found = true;
          return {
            ...a,
            status: 'online',
            last_seen: hb.timestamp,
            cpu_percent: hb.cpu_percent,
            mem_percent: hb.mem_percent,
            disk_percent: hb.disk_percent,
            uptime_secs: hb.uptime_secs,
            version: hb.version ?? a.version,
          };
        });
        // Unknown agent — append a minimal record so the UI still
        // reflects the live state.
        if (!found) {
          next.push({
            id: hb.agent_id,
            site_id: '',
            hostname: hb.agent_id,
            os: '',
            cpu_count: 0,
            total_memory_mb: 0,
            total_disk_gb: 0,
            agent_version: hb.version ?? '',
            status: 'online',
            last_seen: hb.timestamp,
            cpu_percent: hb.cpu_percent,
            mem_percent: hb.mem_percent,
            disk_percent: hb.disk_percent,
            uptime_secs: hb.uptime_secs,
          });
        }
        return next;
      });
    };

    const unsub = ws.subscribe('agents' as Channel, channelHandler);
    return () => {
      clearInterval(statusInterval);
      unsub();
      // Note: we intentionally do not call statusHandler here; the
      // WsClient singleton lives for the lifetime of the tab.
      void statusHandler;
    };
  }, []);

  return { agents, total, isLoading, error, refresh: fetchAgents, status };
}
