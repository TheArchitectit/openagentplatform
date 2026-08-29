// HITL approval queue types (hitl-approval spec R6).
//
// REST endpoints (Go server, under /api/v1):
//   GET  /a2a/approvals             ?status=pending|approved|rejected|expired|escalated
//   GET  /a2a/approvals/{id}        -> { ...request, history: [] }
//   POST /a2a/approvals/{id}/approve  { comment? }
//   POST /a2a/approvals/{id}/reject   { reason }   (reason required)
//   GET  /a2a/approvals/events      SSE: named "approval" events
//
// The wire shape mirrors the Go hitl.ApprovalRequest JSON tags.

export type ApprovalStatus = 'pending' | 'approved' | 'rejected' | 'expired' | 'escalated';

export type ApprovalUrgency = 'critical' | 'high' | 'medium' | 'low';

export interface ApprovalRequest {
  id: string;
  action_type: string;
  payload: Record<string, unknown> | null;
  requester_agent_id: string;
  urgency: string;
  status: ApprovalStatus;
  task_id?: string;
  org_id?: string;
  escalation_depth: number;
  escalated_from?: string;
  decided_by?: string;
  decided_at?: string;
  decision_note?: string;
  created_at: string;
  expires_at: string;
  notifications_sent: number;
}

export interface ApprovalHistoryEntry {
  approval_id: string;
  action: string;
  actor: string;
  reason?: string;
  metadata?: Record<string, string>;
  timestamp: string;
}

export interface ApprovalDetail extends ApprovalRequest {
  history: ApprovalHistoryEntry[];
}

/** One SSE frame on /a2a/approvals/events (hitl.AuditEntry mirror). */
export interface ApprovalEvent {
  approval_id: string;
  action: string; // created | approved | rejected | escalated | expired | notified | reminder | admin_alert
  actor?: string;
  reason?: string;
  metadata?: Record<string, string>;
  timestamp: string;
}

/** Wire shape of GET /a2a/approvals (Go handler normalises nil to []). */
export interface ApprovalListResponse {
  approvals: ApprovalRequest[];
}
