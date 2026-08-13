// useChecks — types and WebSocket envelope narrowing helpers.
// Re-exported from useChecks.ts.

import type { WsEnvelope } from './websocket';

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

export function isCheckEvent(env: WsEnvelope): env is WsEnvelope & WsCheckEvent {
  if (env.type !== 'event' || env.channel !== 'checks') return false;
  const ev = env.event;
  if (
    ev !== 'check.created' &&
    ev !== 'check.updated' &&
    ev !== 'check.deleted' &&
    ev !== 'check.result' &&
    ev !== 'check.completed'
  ) {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}
