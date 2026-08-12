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


// A2A type definitions

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
export const A2A = '/a2a';
