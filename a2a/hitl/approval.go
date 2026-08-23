// Package hitl implements Human-in-the-Loop approval workflows for agent
// actions that require human authorization before execution.
package hitl

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================
// Errors
// ============================================================

var (
	ErrApprovalNotFound   = errors.New("hitl: approval not found")
	ErrAlreadyDecided     = errors.New("hitl: approval already decided")
	ErrInvalidTransition  = errors.New("hitl: invalid state transition")
	ErrEscalationDepthMax = errors.New("hitl: max escalation depth reached")
)

// ============================================================
// Approval Status
// ============================================================

// ApprovalStatus represents the lifecycle state of an approval request.
type ApprovalStatus string

const (
	StatusPending   ApprovalStatus = "pending"
	StatusApproved  ApprovalStatus = "approved"
	StatusRejected  ApprovalStatus = "rejected"
	StatusExpired   ApprovalStatus = "expired"
	StatusEscalated ApprovalStatus = "escalated"
)

// ValidStatuses lists all valid approval statuses.
var ValidStatuses = map[ApprovalStatus]bool{
	StatusPending:   true,
	StatusApproved:  true,
	StatusRejected:  true,
	StatusExpired:   true,
	StatusEscalated: true,
}

// ============================================================
// Approval Type
// ============================================================

// ApprovalTypeConfig defines defaults for a class of approval.
type ApprovalTypeConfig struct {
	Type             string        `json:"type"`
	TimeoutDuration  time.Duration `json:"timeout_duration"`
	OnTimeout        string        `json:"on_timeout"` // "reject" or "escalate"
	MaxEscalations   int           `json:"max_escalations"`
	EscalationGroups []string      `json:"escalation_groups,omitempty"`
}

// DefaultApprovalTypes returns the standard approval type configs.
func DefaultApprovalTypes() []ApprovalTypeConfig {
	return []ApprovalTypeConfig{
		{Type: "secret_access", TimeoutDuration: 1 * time.Hour, OnTimeout: "reject", MaxEscalations: 3},
		{Type: "patch_deploy", TimeoutDuration: 4 * time.Hour, OnTimeout: "escalate", MaxEscalations: 3},
		{Type: "policy_change", TimeoutDuration: 8 * time.Hour, OnTimeout: "reject", MaxEscalations: 3},
		{Type: "external_api", TimeoutDuration: 2 * time.Hour, OnTimeout: "reject", MaxEscalations: 3},
		{Type: "script_execute", TimeoutDuration: 30 * time.Minute, OnTimeout: "reject", MaxEscalations: 3},
		{Type: "config_change", TimeoutDuration: 4 * time.Hour, OnTimeout: "escalate", MaxEscalations: 3},
	}
}

// ============================================================
// Approval Request
// ============================================================

// ApprovalRequest represents a single human-in-the-loop approval request.
type ApprovalRequest struct {
	ID               string            `json:"id"`
	ActionType       string            `json:"action_type"`
	Payload          map[string]any    `json:"payload"`
	RequesterAgentID string            `json:"requester_agent_id"`
	Urgency          string            `json:"urgency"` // critical, high, medium, low
	Status           ApprovalStatus    `json:"status"`
	TaskID           string            `json:"task_id,omitempty"`

	// Escalation tracking.
	EscalationDepth int    `json:"escalation_depth"`
	EscalatedFrom   string `json:"escalated_from,omitempty"` // parent approval ID

	// Decision.
	DecidedBy    string    `json:"decided_by,omitempty"`
	DecidedAt    time.Time `json:"decided_at,omitempty"`
	DecisionNote string    `json:"decision_note,omitempty"`

	// Timestamps.
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// Notifications.
	NotificationsSent int `json:"notifications_sent"`
}

// IsTerminal returns true if the approval is in a final state.
func (a *ApprovalRequest) IsTerminal() bool {
	return a.Status == StatusApproved || a.Status == StatusRejected || a.Status == StatusExpired
}

// ============================================================
// Audit Entry
// ============================================================

// AuditEntry records a single action on an approval request.
type AuditEntry struct {
	ApprovalID string            `json:"approval_id"`
	Action     string            `json:"action"` // created, approved, rejected, escalated, expired, notified
	Actor      string            `json:"actor"`
	Reason     string            `json:"reason,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// ============================================================
// Decision
// ============================================================

// Decision represents a human's approval or rejection.
type Decision struct {
	Approver string `json:"approver"`
	Approved bool   `json:"approved"`
	Note     string `json:"note,omitempty"`
}

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
