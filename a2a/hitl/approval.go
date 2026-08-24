// Package hitl implements Human-in-the-Loop approval workflows for agent
// actions that require human authorization before execution.
package hitl

import (
	"errors"
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
	ID               string         `json:"id"`
	ActionType       string         `json:"action_type"`
	Payload          map[string]any `json:"payload"`
	RequesterAgentID string         `json:"requester_agent_id"`
	Urgency          string         `json:"urgency"` // critical, high, medium, low
	Status           ApprovalStatus `json:"status"`
	TaskID           string         `json:"task_id,omitempty"`

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
