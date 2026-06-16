// Package patches implements the patch approval workflow and its RBAC.
// The ApprovalWorkflow struct is the authoritative arbiter of legal
// state transitions for a PatchJob; the store (store.go) persists
// state, and the API handlers (api/patches.go) invoke the workflow.
package patches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Patch job states. These are the eight states in the lifecycle machine.
const (
	StatePendingApproval = "pending_approval"
	StateApproved        = "approved"
	StateRejected        = "rejected"
	StateScheduled       = "scheduled"
	StateInProgress      = "in_progress"
	StateCompleted       = "completed"
	StateFailed          = "failed"
	StateRolledBack      = "rolled_back"
)

// Events that drive state transitions.
const (
	EventApprove     = "approve"
	EventReject      = "reject"
	EventSchedule    = "schedule"
	EventCancel      = "cancel"
	EventStart       = "start"       // auto on schedule time
	EventComplete    = "complete"    // auto on success
	EventFail        = "fail"        // auto on failure
	EventRollback    = "rollback"
	EventTimeout     = "timeout"     // auto-approve on timeout
	EventAdminReject = "admin_reject" // admin override of approved state
)

// RBAC permissions for patch operations.
const (
	PermApprove  = "patch:approve"
	PermReject   = "patch:reject"
	PermSchedule = "patch:schedule"
	PermCancel   = "patch:cancel"
	PermRollback = "patch:rollback"
	PermCreate   = "patch:create"
)

// ErrInvalidTransition is returned when an event is not valid for the
// current state.
var ErrInvalidTransition = errors.New("invalid patch state transition")

// ErrInsufficientApprovals is returned when the number of approvals
// recorded is less than the required count for the patch severity.
var ErrInsufficientApprovals = errors.New("insufficient approvals")

// ErrPermissionDenied is returned when the actor lacks the RBAC
// permission required for the requested transition.
var ErrPermissionDenied = errors.New("permission denied")

// ErrOutsideMaintenanceWindow is returned when a scheduled patch
// falls outside the configured maintenance window.
var ErrOutsideMaintenanceWindow = errors.New("scheduled time outside maintenance window")

// DefaultApprovalTimeout is the default deadline for approval when
// the job's ApprovalTimeout is not set. 72 hours per the spec.
const DefaultApprovalTimeout = 72 * time.Hour

// RequiredApprovals maps patch severity to the minimum number of
// distinct approvers required before a patch may move to scheduled.
var RequiredApprovals = map[models.PatchSeverity]int{
	models.PatchSeverityStandard: 1,
	models.PatchSeverityMajorOS:  2,
}

// ValidTransitions defines the legal (state, event) -> nextState mappings.
// Any (state, event) pair not present in the map is rejected by Transition.
var ValidTransitions = map[string]map[string]string{
	StatePendingApproval: {
		EventApprove: StateApproved,
		EventReject:  StateRejected,
		EventTimeout: StateApproved, // auto-approve on 72h timeout
	},
	StateApproved: {
		EventSchedule:    StateScheduled,
		EventAdminReject: StateRejected, // admin override
	},
	StateScheduled: {
		EventStart:  StateInProgress, // auto on schedule time
		EventCancel: StateRejected,   // by admin
	},
	StateInProgress: {
		EventComplete: StateCompleted, // auto on success
		EventFail:     StateFailed,    // auto on failure
	},
	StateFailed: {
		EventApprove:  StatePendingApproval, // retry: go back through approval
		EventRollback: StateRolledBack,      // auto/manual rollback
	},
	StateCompleted: {
		EventRollback: StateRolledBack, // manual rollback
	},
	// StateRejected and StateRolledBack are terminal: no outgoing transitions.
}

// CanTransition returns true if the (state, event) pair has a defined
// next state. It does not perform side effects.
func CanTransition(state, event string) bool {
	events, ok := ValidTransitions[state]
	if !ok {
		return false
	}
	_, ok = events[event]
	return ok
}

// NextState returns the destination state for a (state, event) pair.
// Returns ErrInvalidTransition if the pair is not in the map.
func NextState(state, event string) (string, error) {
	events, ok := ValidTransitions[state]
	if !ok {
		return "", fmt.Errorf("%w: unknown state %q", ErrInvalidTransition, state)
	}
	next, ok := events[event]
	if !ok {
		return "", fmt.Errorf("%w: event %q not valid in state %q", ErrInvalidTransition, event, state)
	}
	return next, nil
}

// PermissionForEvent returns the RBAC permission required to invoke
// the given event as a user-initiated action. System-driven events
// (start, complete, fail, timeout) return empty string.
func PermissionForEvent(event string) string {
	switch event {
	case EventApprove:
		return PermApprove
	case EventReject:
		return PermReject
	case EventSchedule:
		return PermSchedule
	case EventCancel:
		return PermCancel
	case EventRollback:
		return PermRollback
	case EventAdminReject:
		return PermReject
	}
	return "" // system events
}

// IsSystemEvent returns true if the event is driven by the system
// rather than a user action. System events skip RBAC checks.
func IsSystemEvent(event string) bool {
	switch event {
	case EventStart, EventComplete, EventFail, EventTimeout:
		return true
	}
	return false
}

// ApprovalWorkflow executes state transitions for patch jobs. It is
// stateless beyond the job it is operating on; callers are responsible
// for loading/persisting the job via Store.
type ApprovalWorkflow struct {
	// Now is the time source. Defaults to time.Now.
	Now func() time.Time
}

// NewApprovalWorkflow constructs an ApprovalWorkflow with the default clock.
func NewApprovalWorkflow() *ApprovalWorkflow {
	return &ApprovalWorkflow{Now: time.Now}
}

// Actor represents a user or system entity performing a workflow action.
// Permissions lists RBAC permission strings the actor holds.
type Actor struct {
	ID          string
	Name        string
	Permissions []string
}

// HasPermission returns true if the actor holds the given permission.
func (a *Actor) HasPermission(perm string) bool {
	if a == nil {
		return false
	}
	for _, p := range a.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// TransitionInput is the input to Transition. Actor is required for
// user-driven events. Comment is optional and persisted in the audit
// record. For schedule events, ScheduledAt and MaintenanceWindow set
// the deployment window.
type TransitionInput struct {
	Job              *models.PatchJob
	Event            string
	Actor            *Actor
	Comment          string
	ScheduledAt      *time.Time
	MaintenanceStart *time.Time
	MaintenanceEnd   *time.Time
	FailureReason    string
}

// TransitionResult contains the side-effects of a successful transition.
type TransitionResult struct {
	Job            *models.PatchJob
	ApprovalRecord *models.ApprovalRecord
	AutoApproved   bool // true if approval was granted by the timeout rule
}

// Transition validates and applies a state transition to a patch job
// in-place. It enforces RBAC, approval-count rules, and maintenance
// window constraints. The returned result contains the updated job and
// any approval record that should be persisted.
func (w *ApprovalWorkflow) Transition(_ context.Context, in TransitionInput) (*TransitionResult, error) {
	if in.Job == nil {
		return nil, errors.New("patches: nil job")
	}
	if in.Event == "" {
		return nil, errors.New("patches: empty event")
	}

	from := in.Job.State
	to, err := NextState(from, in.Event)
	if err != nil {
		return nil, err
	}

	now := w.now()
	job := in.Job

	// RBAC check for user-driven events.
	if !IsSystemEvent(in.Event) {
		perm := PermissionForEvent(in.Event)
		if perm == "" {
			return nil, fmt.Errorf("%w: no permission mapped for event %q", ErrPermissionDenied, in.Event)
		}
		if in.Actor == nil || !in.Actor.HasPermission(perm) {
			return nil, fmt.Errorf("%w: missing %s", ErrPermissionDenied, perm)
		}
	}

	// Build optional approval record for approve/reject events.
	var rec *models.ApprovalRecord
	autoApproved := false
	switch in.Event {
	case EventApprove:
		rec = &models.ApprovalRecord{
			PatchJobID:   job.ID,
			ApproverID:   actorID(in.Actor),
			ApproverName: actorName(in.Actor),
			Decision:     "approved",
			Comment:      in.Comment,
			CreatedAt:    now,
		}
		job.Approvals = append(job.Approvals, *rec)
	case EventReject:
		rec = &models.ApprovalRecord{
			PatchJobID:   job.ID,
			ApproverID:   actorID(in.Actor),
			ApproverName: actorName(in.Actor),
			Decision:     "rejected",
			Comment:      in.Comment,
			CreatedAt:    now,
		}
		job.Approvals = append(job.Approvals, *rec)
	case EventTimeout:
		autoApproved = true
		rec = &models.ApprovalRecord{
			PatchJobID:   job.ID,
			ApproverID:   "system",
			ApproverName: "auto-approval",
			Decision:     "approved",
			Comment:      "auto-approved after approval timeout",
			CreatedAt:    now,
		}
		job.Approvals = append(job.Approvals, *rec)
	}

	// For approve events, check that the required number of approvals
	// is met before moving to the approved state. The pending_approval
	// state is kept until the threshold is reached; the caller is
	// responsible for re-invoking Transition when more approvals arrive.
	// For the auto-approve and admin paths we always move to approved.
	if in.Event == EventApprove {
		required := job.RequiredApprovals
		if required <= 0 {
			required = RequiredApprovals[job.Severity]
		}
		approvedCount := 0
		for _, a := range job.Approvals {
			if a.Decision == "approved" {
				approvedCount++
			}
		}
		if approvedCount < required {
			// Stay in pending_approval; do not move state.
			job.UpdatedAt = now
			if rec != nil {
				recCopy := *rec
				return &TransitionResult{Job: job, ApprovalRecord: &recCopy}, nil
			}
			return &TransitionResult{Job: job}, nil
		}
	}

	// For schedule events, validate the maintenance window.
	if in.Event == EventSchedule {
		if in.ScheduledAt == nil {
			return nil, errors.New("patches: scheduled_at required for schedule event")
		}
		if in.MaintenanceStart != nil && in.ScheduledAt.Before(*in.MaintenanceStart) {
			return nil, ErrOutsideMaintenanceWindow
		}
		if in.MaintenanceEnd != nil && in.ScheduledAt.After(*in.MaintenanceEnd) {
			return nil, ErrOutsideMaintenanceWindow
		}
		job.ScheduledAt = in.ScheduledAt
		if in.MaintenanceStart != nil {
			job.MaintenanceWindowStart = in.MaintenanceStart
		}
		if in.MaintenanceEnd != nil {
			job.MaintenanceWindowEnd = in.MaintenanceEnd
		}
	}

	// For fail events, record the failure reason.
	if in.Event == EventFail {
		if in.FailureReason != "" {
			job.FailureReason = in.FailureReason
		}
	}

	// For complete events, set completed_at.
	if in.Event == EventComplete {
		t := now
		job.CompletedAt = &t
	}

	job.State = to
	job.UpdatedAt = now

	result := &TransitionResult{Job: job, AutoApproved: autoApproved}
	if rec != nil {
		recCopy := *rec
		result.ApprovalRecord = &recCopy
	}
	return result, nil
}

// ApplyPolicy applies the approval rules for a freshly created patch
// job. Critical patches are auto-approved. Standard patches require 1
// approver, major OS upgrades require 2. If the job has no
// ApprovalTimeout set, a default 72h deadline is applied when
// auto-approve-on-timeout is true.
func (w *ApprovalWorkflow) ApplyPolicy(job *models.PatchJob) {
	if job == nil {
		return
	}
	now := w.now()
	switch job.Severity {
	case models.PatchSeverityCritical:
		// Critical patches auto-approve. A system approval record is
		// added so the audit trail is complete.
		job.State = StateApproved
		job.RequiredApprovals = 0
		job.AutoApproveOnTimeout = true
		job.Approvals = append(job.Approvals, models.ApprovalRecord{
			PatchJobID:   job.ID,
			ApproverID:   "system",
			ApproverName: "auto-approval (critical)",
			Decision:     "approved",
			Comment:      "critical severity patch auto-approved",
			CreatedAt:    now,
		})
	case models.PatchSeverityStandard:
		if job.RequiredApprovals <= 0 {
			job.RequiredApprovals = 1
		}
		job.AutoApproveOnTimeout = true
	case models.PatchSeverityMajorOS:
		if job.RequiredApprovals <= 0 {
			job.RequiredApprovals = 2
		}
		job.AutoApproveOnTimeout = true
	}
	// Apply default approval timeout if not set and auto-approve is enabled.
	if job.AutoApproveOnTimeout && job.ApprovalTimeout == nil {
		t := now.Add(DefaultApprovalTimeout)
		job.ApprovalTimeout = &t
	}
	job.UpdatedAt = now
}

// CountApprovals returns the number of approval records with decision
// "approved" in the job's approval history.
func CountApprovals(job *models.PatchJob) int {
	if job == nil {
		return 0
	}
	n := 0
	for _, a := range job.Approvals {
		if a.Decision == "approved" {
			n++
		}
	}
	return n
}

// RequiredApprovalCount returns the configured required-approvals
// value for the job, falling back to the severity default.
func RequiredApprovalCount(job *models.PatchJob) int {
	if job == nil {
		return 0
	}
	if job.RequiredApprovals > 0 {
		return job.RequiredApprovals
	}
	return RequiredApprovals[job.Severity]
}

// HistoryReader is the minimal store interface used by workflow
// queries. The full Store interface lives in store.go.
type HistoryReader interface {
	GetApprovalHistory(ctx context.Context, jobID string) ([]models.ApprovalRecord, error)
}

func (w *ApprovalWorkflow) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func actorID(a *Actor) string {
	if a == nil {
		return "system"
	}
	return a.ID
}

func actorName(a *Actor) string {
	if a == nil || a.Name == "" {
		return actorID(a)
	}
	return a.Name
}
