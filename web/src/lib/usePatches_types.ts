// usePatches — manages patch management operations across the platform.
//
// Patch operations exposed:
//   - Catalog:   list of available OS / vendor patches that can be applied
//   - Jobs:      deployment jobs that roll patches out to agents
//   - Scans:     on-demand scan results (missing patches per agent)
//   - Approvals: approval / rejection of pending jobs
//   - Reboots:   pending reboot coordination per agent
//   - WebSocket: real-time merge of patch / job / reboot events
//
// REST endpoints (server-of-record):
//   GET    /patches/catalog?os=&severity=&search=&page=&limit=
//   GET    /patches/jobs?status=&page=&limit=
//   POST   /patches/jobs
//   GET    /patches/jobs/:id
//   POST   /patches/jobs/:id/approve
//   POST   /patches/jobs/:id/reject
//   POST   /patches/jobs/:id/cancel
//   POST   /patches/jobs/:id/rollback
//   POST   /patches/jobs/:id/retry
//   GET    /patches/jobs/:id/targets
//   GET    /patches/jobs/:id/approvals
//   GET    /patches/jobs/:id/reboots
//   POST   /patches/jobs/:id/reboots/:agentId/reboot-now
//   POST   /patches/jobs/:id/reboots/:agentId/schedule
//   GET    /patches/scans?agent_id=&job_id=
//   POST   /patches/scans
//
// WebSocket event vocabulary (server -> client):
//   { channel: "patches", event: "patch.job.created",   data: PatchJob }
//   { channel: "patches", event: "patch.job.updated",   data: PatchJob }
//   { channel: "patches", event: "patch.job.state",     data: { id, status, stage?, timestamp? } }
//   { channel: "patches", event: "patch.target.updated", data: PatchTarget }
//   { channel: "patches", event: "patch.reboot",        data: PatchReboot }
//   { channel: "patches", event: "patch.scan.completed", data: PatchScan }

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import type { WsEnvelope, Status } from './websocket';
import type { Severity } from '@/components/severity-badge';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------


export type PatchJobStatus =
  | 'pending_approval'
  | 'approved'
  | 'rejected'
  | 'in_progress'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'rolled_back';

export type PatchSeverity = 'critical' | 'important' | 'moderate' | 'low' | Severity;
export type PatchCategory = 'security' | 'os' | 'application' | 'driver' | 'firmware' | 'other';

export type RebootStatus =
  | 'not_required'
  | 'pending'
  | 'scheduled'
  | 'in_progress'
  | 'completed'
  | 'failed';

export type InstallStatus =
  | 'pending'
  | 'downloading'
  | 'installing'
  | 'completed'
  | 'failed'
  | 'skipped'
  | 'rolled_back';

export type DeploymentStage = 'queued' | 'canary' | 'early' | 'majority' | 'complete';

export interface PatchCatalogItem {
  id: string;
  kb_number?: string;
  cve_ids?: string[];
  title: string;
  description?: string;
  os: string;
  category: PatchCategory;
  severity: PatchSeverity;
  release_date?: string;
  size_mb?: number;
  vendor?: string;
  product?: string;
  requires_reboot?: boolean;
  affected_agent_count?: number;
  cvss_score?: number;
}

export interface PatchTarget {
  id: string;
  job_id: string;
  agent_id: string;
  hostname: string;
  os: string;
  os_version?: string;
  current_version?: string;
  target_version?: string;
  install_status: InstallStatus;
  reboot_status: RebootStatus;
  error_message?: string;
  started_at?: string;
  completed_at?: string;
  scheduled_reboot_at?: string;
  rebooted_at?: string;
}

export interface PatchApproval {
  id: string;
  job_id: string;
  approver?: string;
  decision: 'approved' | 'rejected' | 'requested_changes';
  note?: string;
  created_at: string;
}

export interface PatchReboot {
  id: string;
  job_id: string;
  agent_id: string;
  hostname: string;
  status: RebootStatus;
  scheduled_at?: string;
  rebooted_at?: string;
  // Minutes from job creation to perform the reboot. Null = manual.
  delay_minutes?: number | null;
  // Index into the staggered timeline (0, 1, 2, …).
  stage_index?: number;
  last_error?: string;
}

export interface PatchJob {
  id: string;
  name: string;
  description?: string;
  status: PatchJobStatus;
  severity: PatchSeverity;
  patch_ids: string[];
  patch_count: number;
  // Target agent selection
  target_agent_ids: string[];
  target_tags?: string[];
  target_label?: string;
  // Counts (denormalized for fast UI rendering)
  total_agents: number;
  completed_agents: number;
  failed_agents: number;
  in_progress_agents: number;
  // Deployment strategy
  strategy?: 'immediate' | 'staged' | 'maintenance_window';
  stages?: DeploymentStage[];
  // Maintenance / reboot config
  maintenance_window_start?: string;
  maintenance_window_end?: string;
  reboot_policy?: 'never' | 'if_required' | 'always' | 'scheduled';
  // Audit
  created_by?: string;
  created_at: string;
  updated_at?: string;
  approved_at?: string;
  approved_by?: string;
  started_at?: string;
  completed_at?: string;
  progress_pct?: number;
  // Aggregated / derived
  targets?: PatchTarget[];
  reboots?: PatchReboot[];
  approvals?: PatchApproval[];
}

export interface PatchScanResult {
  id: string;
  agent_id: string;
  hostname: string;
  os: string;
  patch_id: string;
  kb_number?: string;
  cve_ids?: string[];
  severity: PatchSeverity;
  detected_at: string;
  job_id?: string;
  // Per-agent scan metadata
  missing?: boolean;
  installed?: boolean;
  current_version?: string;
  available_version?: string;
  release_date?: string;
  cvss_score?: number;
}

export interface CreatePatchJobInput {
  name: string;
  description?: string;
  patch_ids: string[];
  target_agent_ids: string[];
  target_tags?: string[];
  strategy?: 'immediate' | 'staged' | 'maintenance_window';
  maintenance_window_start?: string;
  maintenance_window_end?: string;
  reboot_policy?: 'never' | 'if_required' | 'always' | 'scheduled';
  // How many agents per batch (for staged rollouts). Default 10.
  batch_size?: number;
  // How many minutes between batch advances. Default 15.
  batch_interval_minutes?: number;
  // When the job should be queued for execution. ISO 8601.
  scheduled_at?: string;
}

export interface UsePatchesResult {
  // Catalog
  catalog: PatchCatalogItem[];
  catalogTotal: number;
  catalogLoading: boolean;
  catalogError: Error | null;
  fetchCatalog: (filters?: PatchCatalogFilters) => Promise<void>;
  scanMissing: (agentIds?: string[]) => Promise<PatchScanResult[]>;

  // Jobs
  jobs: PatchJob[];
  jobsTotal: number;
  isLoading: boolean;
  error: Error | null;
  status: Status;
  refresh: () => Promise<void>;
  fetchJob: (id: string) => Promise<PatchJob>;
  createJob: (input: CreatePatchJobInput) => Promise<PatchJob>;
  cancelJob: (id: string) => Promise<PatchJob>;
  rollbackJob: (id: string) => Promise<PatchJob>;
  retryJob: (id: string) => Promise<PatchJob>;
  approveJob: (id: string, note?: string) => Promise<PatchJob>;
  rejectJob: (id: string, note?: string) => Promise<PatchJob>;
  batchApprove: (ids: string[], note?: string) => Promise<{ succeeded: string[]; failed: string[] }>;
  batchReject: (ids: string[], note?: string) => Promise<{ succeeded: string[]; failed: string[] }>;

  // Job details
  fetchJobTargets: (jobId: string) => Promise<PatchTarget[]>;
  fetchJobApprovals: (jobId: string) => Promise<PatchApproval[]>;
  fetchJobReboots: (jobId: string) => Promise<PatchReboot[]>;
  rebootAgentNow: (jobId: string, agentId: string) => Promise<PatchReboot>;
  scheduleReboot: (
    jobId: string,
    agentId: string,
    scheduledAt: string
  ) => Promise<PatchReboot>;

  // Scans
  scans: PatchScanResult[];
  scansLoading: boolean;
  fetchScans: (filters?: { agent_id?: string; job_id?: string }) => Promise<PatchScanResult[]>;
}

export interface PatchCatalogFilters {
  os?: string;
  severity?: PatchSeverity;
  category?: PatchCategory;
  search?: string;
  limit?: number;
  offset?: number;
}

// ---------------------------------------------------------------------------
// WebSocket helpers
// ---------------------------------------------------------------------------

type WsPatchEvent =
  | { event: 'patch.job.created'; data: PatchJob }
  | { event: 'patch.job.updated'; data: PatchJob }
  | { event: 'patch.job.state'; data: { id: string; status: PatchJobStatus; stage?: DeploymentStage; timestamp?: string; previous_status?: string } }
  | { event: 'patch.target.updated'; data: PatchTarget }
  | { event: 'patch.reboot'; data: PatchReboot }
  | { event: 'patch.scan.completed'; data: PatchScanResult };

export function isPatchEvent(env: WsEnvelope): env is WsEnvelope & WsPatchEvent {
  if (env.type !== 'event' || env.channel !== 'patches') return false;
  const ev = env.event;
  if (
    ev !== 'patch.job.created' &&
    ev !== 'patch.job.updated' &&
    ev !== 'patch.job.state' &&
    ev !== 'patch.target.updated' &&
    ev !== 'patch.reboot' &&
    ev !== 'patch.scan.completed'
  ) {
    return false;
  }
  return typeof env.data === 'object' && env.data !== null;
}

// ---------------------------------------------------------------------------
