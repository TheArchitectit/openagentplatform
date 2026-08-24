package hitl

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================
// Approval Manager
// ============================================================

// ApprovalManager coordinates approval lifecycle: creation, decision,
// timeout, escalation, and audit.
type ApprovalManager struct {
	mu       sync.Mutex
	byID     map[string]*ApprovalRequest
	auditLog []AuditEntry
	typeCfgs map[string]ApprovalTypeConfig
	store    Store // optional persistence
}

// NewApprovalManager creates a manager with the given type configs.
func NewApprovalManager(typeCfgs []ApprovalTypeConfig) *ApprovalManager {
	cfgs := make(map[string]ApprovalTypeConfig, len(typeCfgs))
	for _, c := range typeCfgs {
		cfgs[c.Type] = c
	}
	return &ApprovalManager{
		byID:     make(map[string]*ApprovalRequest),
		typeCfgs: cfgs,
	}
}

// SetStore attaches a persistence store (optional).
func (am *ApprovalManager) SetStore(s Store) {
	am.store = s
}

// CreateRequest creates a new approval request. Returns the request.
func (am *ApprovalManager) CreateRequest(id, actionType, requesterAgentID, urgency, taskID string, payload map[string]any) (*ApprovalRequest, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	cfg, ok := am.typeCfgs[actionType]
	if !ok {
		return nil, fmt.Errorf("hitl: unknown action type %q", actionType)
	}

	now := time.Now()
	req := &ApprovalRequest{
		ID:               id,
		ActionType:       actionType,
		Payload:          payload,
		RequesterAgentID: requesterAgentID,
		Urgency:          urgency,
		Status:           StatusPending,
		TaskID:           taskID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(cfg.TimeoutDuration),
	}

	am.byID[id] = req
	am.appendAudit(AuditEntry{
		ApprovalID: id,
		Action:     "created",
		Actor:      requesterAgentID,
		Timestamp:  now,
	})

	if am.store != nil {
		_ = am.store.SaveApproval(req)
		_ = am.store.AppendAudit(AuditEntry{
			ApprovalID: id, Action: "created", Actor: requesterAgentID, Timestamp: now,
		})
	}

	return req, nil
}

// Approve approves an approval request.
func (am *ApprovalManager) Approve(id, approver, note string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	req, ok := am.byID[id]
	if !ok {
		return ErrApprovalNotFound
	}
	if req.IsTerminal() {
		return ErrAlreadyDecided
	}

	now := time.Now()
	req.Status = StatusApproved
	req.DecidedBy = approver
	req.DecidedAt = now
	req.DecisionNote = note

	am.appendAudit(AuditEntry{
		ApprovalID: id,
		Action:     "approved",
		Actor:      approver,
		Reason:     note,
		Timestamp:  now,
	})

	if am.store != nil {
		_ = am.store.SaveApproval(req)
		_ = am.store.AppendAudit(AuditEntry{
			ApprovalID: id, Action: "approved", Actor: approver,
			Reason: note, Timestamp: now,
		})
	}
	return nil
}

// Reject rejects an approval request.
func (am *ApprovalManager) Reject(id, approver, reason string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	req, ok := am.byID[id]
	if !ok {
		return ErrApprovalNotFound
	}
	if req.IsTerminal() {
		return ErrAlreadyDecided
	}
	if reason == "" {
		return fmt.Errorf("hitl: rejection reason is required")
	}

	now := time.Now()
	req.Status = StatusRejected
	req.DecidedBy = approver
	req.DecidedAt = now
	req.DecisionNote = reason

	am.appendAudit(AuditEntry{
		ApprovalID: id,
		Action:     "rejected",
		Actor:      approver,
		Reason:     reason,
		Timestamp:  now,
	})

	if am.store != nil {
		_ = am.store.SaveApproval(req)
		_ = am.store.AppendAudit(AuditEntry{
			ApprovalID: id, Action: "rejected", Actor: approver,
			Reason: reason, Timestamp: now,
		})
	}
	return nil
}

// GetRequest returns the approval request by ID.
func (am *ApprovalManager) GetRequest(id string) (*ApprovalRequest, error) {
	am.mu.Lock()
	defer am.mu.Unlock()
	req, ok := am.byID[id]
	if !ok {
		return nil, ErrApprovalNotFound
	}
	return req, nil
}

// ListPending returns all pending approval requests.
func (am *ApprovalManager) ListPending() []*ApprovalRequest {
	am.mu.Lock()
	defer am.mu.Unlock()
	var pending []*ApprovalRequest
	for _, req := range am.byID {
		if req.Status == StatusPending {
			pending = append(pending, req)
		}
	}
	return pending
}

// ListByStatus returns approvals matching the given status.
func (am *ApprovalManager) ListByStatus(status ApprovalStatus) []*ApprovalRequest {
	am.mu.Lock()
	defer am.mu.Unlock()
	var result []*ApprovalRequest
	for _, req := range am.byID {
		if req.Status == status {
			result = append(result, req)
		}
	}
	return result
}

// AuditLog returns the full audit trail for an approval.
func (am *ApprovalManager) AuditLog(approvalID string) []AuditEntry {
	am.mu.Lock()
	defer am.mu.Unlock()
	var entries []AuditEntry
	for _, e := range am.auditLog {
		if e.ApprovalID == approvalID {
			entries = append(entries, e)
		}
	}
	return entries
}

func (am *ApprovalManager) appendAudit(entry AuditEntry) {
	am.auditLog = append(am.auditLog, entry)
}
