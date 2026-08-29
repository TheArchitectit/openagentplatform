// useApprovals — data layer for the HITL approval queue (spec R6).
//
// Responsibilities:
//   - list approvals by status (R6.1)
//   - fetch one approval with its decision history (R6.2)
//   - approve / reject with comment or reason (R6.3, R6.4 — the batch
//     UI drives one call per selection through the same mutation)
//   - live updates over the /a2a/approvals/events SSE stream (R6.5)
//
// The list endpoint has no server-side search, so urgency/requester
// filters are applied client-side in the page.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { apiFetch, ApiError } from './api';
import type {
  ApprovalDetail,
  ApprovalEvent,
  ApprovalListResponse,
  ApprovalRequest,
  ApprovalStatus,
} from './useApprovals_types';

export const APPROVALS_SSE_PATH = '/api/v1/a2a/approvals/events';

export type ApprovalFilterStatus = ApprovalStatus;

export interface UseApprovalsResult {
  approvals: ApprovalRequest[];
  status: ApprovalFilterStatus;
  setStatus: (s: ApprovalFilterStatus) => void;
  isLoading: boolean;
  isMutating: boolean;
  error: Error | null;
  sseConnected: boolean;
  approve: (id: string, comment?: string) => Promise<void>;
  reject: (id: string, reason: string) => Promise<void>;
  approveBatch: (ids: string[], comment?: string) => Promise<void>;
  rejectBatch: (ids: string[], reason: string) => Promise<void>;
  refresh: () => Promise<void>;
}

export function useApprovals(initial: ApprovalFilterStatus = 'pending'): UseApprovalsResult {
  const [status, setStatus] = useState<ApprovalFilterStatus>(initial);
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isMutating, setIsMutating] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [sseConnected, setSseConnected] = useState(false);

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    try {
      setError(null);
      const data = await apiFetch<ApprovalListResponse>(
        `/a2a/approvals?status=${encodeURIComponent(status)}`
      );
      if (!mountedRef.current) return;
      setApprovals(Array.isArray(data?.approvals) ? data.approvals : []);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
    }
  }, [status]);

  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    void load().finally(() => {
      if (!cancelled) setIsLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [load]);

  // R6.5: any lifecycle action touching an approval in view re-fetches
  // the list. Decisions made elsewhere (another admin, the API, a task
  // gate timeout) surface without polling.
  const lastEventRef = useRef(0);
  useEffect(() => {
    let es: EventSource | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    const refreshThrottle = () => {
      const now = Date.now();
      if (now - lastEventRef.current < 400) return;
      lastEventRef.current = now;
      void load();
    };

    const connect = () => {
      try {
        es = new EventSource(APPROVALS_SSE_PATH, { withCredentials: true } as EventSourceInit);
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
          setError((prev) => prev ?? new ApiError(0, 'SSEError', 'Approval-event stream disconnected'));
        }
        es?.close();
        scheduleReconnect();
      };
      // The stream uses named events ("approval" plus a "hello" frame).
      es.addEventListener('hello', () => {
        if (mountedRef.current) setSseConnected(true);
      });
      es.addEventListener('approval', (ev: MessageEvent) => {
        if (!mountedRef.current) return;
        try {
          const payload = JSON.parse(ev.data) as ApprovalEvent;
          if (!payload?.approval_id) return;
          refreshThrottle();
        } catch {
          /* ignore malformed */
        }
      });
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
  }, [load]);

  const decide = useCallback(
    async (ids: string[], path: (id: string) => string, body: Record<string, unknown>) => {
      if (ids.length === 0) return;
      setIsMutating(true);
      setError(null);
      try {
        // One call per selection; failures are collected so a partial
        // batch (e.g. one approval already decided elsewhere) still
        // applies the rest and reports what failed.
        const results = await Promise.allSettled(
          ids.map((id) => apiFetch<ApprovalRequest>(path(id), { method: 'POST', json: body }))
        );
        const failures = results.filter((r) => r.status === 'rejected');
        if (failures.length === results.length) {
          const first = failures[0] as PromiseRejectedResult;
          throw first.reason instanceof Error ? first.reason : new Error(String(first.reason));
        }
        // Reload first — load() clears the error slot — then surface a
        // partial-batch message over the refreshed list.
        await load();
        if (failures.length > 0) {
          setError(new Error(`${ids.length - failures.length} of ${ids.length} succeeded`));
        }
      } finally {
        if (mountedRef.current) setIsMutating(false);
      }
    },
    [load]
  );

  const approve = useCallback(
    (id: string, comment?: string) =>
      decide([id], (i) => `/a2a/approvals/${encodeURIComponent(i)}/approve`, { comment: comment ?? '' }),
    [decide]
  );

  const reject = useCallback(
    (id: string, reason: string) =>
      decide([id], (i) => `/a2a/approvals/${encodeURIComponent(i)}/reject`, { reason }),
    [decide]
  );

  const approveBatch = useCallback(
    (ids: string[], comment?: string) =>
      decide(ids, (i) => `/a2a/approvals/${encodeURIComponent(i)}/approve`, { comment: comment ?? '' }),
    [decide]
  );

  const rejectBatch = useCallback(
    (ids: string[], reason: string) =>
      decide(ids, (i) => `/a2a/approvals/${encodeURIComponent(i)}/reject`, { reason }),
    [decide]
  );

  const refresh = useCallback(async () => {
    setIsLoading(true);
    await load();
    setIsLoading(false);
  }, [load]);

  return useMemo(
    () => ({
      approvals,
      status,
      setStatus,
      isLoading,
      isMutating,
      error,
      sseConnected,
      approve,
      reject,
      approveBatch,
      rejectBatch,
      refresh,
    }),
    [approvals, status, isLoading, isMutating, error, sseConnected, approve, reject, approveBatch, rejectBatch, refresh]
  );
}

/** Fetch a single approval with its decision history (R6.2). */
export async function fetchApprovalDetail(id: string): Promise<ApprovalDetail> {
  return apiFetch<ApprovalDetail>(`/a2a/approvals/${encodeURIComponent(id)}`);
}
