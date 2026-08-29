package hitl

import (
	"context"
	"fmt"
	"log"
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
	// notifier is the optional delivery seam used on create (R2.1) and
	// by the reminder loop (R2.4). Delivery is fire-and-forget: a failed
	// notification never fails the request lifecycle.
	notifier NotificationService
	// auditSinks are optional external audit integrations (spec R4.4).
	// Every entry appended to the engine's own audit log is forwarded to
	// them asynchronously — the tamper-evident trail and the SSE approval
	// stream are both consumers.
	auditSinks []func(AuditEntry)
	// defaultReminderInterval is the fallback re-notification delay for
	// types without a per-type ReminderInterval.
	defaultReminderInterval time.Duration
	// decisionHooks are notified (asynchronously, with a value snapshot)
	// whenever a request reaches a terminal decision: approved, rejected,
	// or expired. Used by the A2A task gate (hitl-approval spec R5) to
	// resume or fail the linked task.
	decisionHooks []func(ApprovalRequest)
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

// SetNotifier attaches the notification delivery seam. Create (R2.1) and
// the reminder loop (R2.4) dispatch through it asynchronously; delivery
// failure never affects the approval lifecycle.
func (am *ApprovalManager) SetNotifier(n NotificationService) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.notifier = n
}

// SetDefaultReminderInterval sets the re-notification delay used for
// approval types without a per-type ReminderInterval (spec R2.4).
func (am *ApprovalManager) SetDefaultReminderInterval(d time.Duration) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.defaultReminderInterval = d
}

// reminderIntervalFor resolves the re-notification delay for an action
// type: the per-type override if set, else the manager default.
func (am *ApprovalManager) reminderIntervalFor(actionType string) time.Duration {
	if cfg, ok := am.typeCfgs[actionType]; ok && cfg.ReminderInterval > 0 {
		return cfg.ReminderInterval
	}
	return am.defaultReminderInterval
}

// CreateRequest creates a new unscoped approval request. Returns the request.
func (am *ApprovalManager) CreateRequest(id, actionType, requesterAgentID, urgency, taskID string, payload map[string]any) (*ApprovalRequest, error) {
	return am.CreateRequestWithOrg(id, actionType, requesterAgentID, urgency, taskID, "", payload)
}

// CreateRequestWithOrg creates a new approval request scoped to orgID for
// notification fan-out (may be empty = unscoped). Returns the request.
func (am *ApprovalManager) CreateRequestWithOrg(id, actionType, requesterAgentID, urgency, taskID, orgID string, payload map[string]any) (*ApprovalRequest, error) {
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
		OrgID:            orgID,
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

	// R2.1: notify configured channels asynchronously (fire-and-forget).
	notifier := am.notifier
	reminderInterval := am.reminderIntervalFor(actionType)
	if notifier != nil {
		snapshot := *req
		go am.deliverNotification(&snapshot, notifier, reminderInterval, false)
	}

	return req, nil
}

// deliverNotification sends one notification round through the notifier and
// updates the request's notification bookkeeping (NotificationsSent,
// NextReminderAt) on success. isReminder distinguishes the "notified" audit
// actions. Safe to call concurrently; takes the manager lock itself.
func (am *ApprovalManager) deliverNotification(snapshot *ApprovalRequest, notifier NotificationService, reminderInterval time.Duration, isReminder bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	if isReminder {
		err = notifier.SendReminder(ctx, snapshot)
	} else {
		err = notifier.SendApprovalRequest(ctx, snapshot)
	}
	if err != nil {
		log.Printf("hitl: notification for approval %s failed: %v", snapshot.ID, err)
		return
	}

	am.mu.Lock()
	defer am.mu.Unlock()
	req, ok := am.byID[snapshot.ID]
	if !ok || req.Status != StatusPending {
		return
	}
	req.NotificationsSent++
	if reminderInterval > 0 && req.NotificationsSent <= MaxRenotifications {
		req.NextReminderAt = time.Now().Add(reminderInterval)
	} else {
		req.NextReminderAt = time.Time{}
	}
	if isReminder {
		am.appendAudit(AuditEntry{
			ApprovalID: req.ID, Action: "reminder", Actor: "system",
			Metadata:  map[string]string{"notification_round": fmt.Sprintf("%d", req.NotificationsSent)},
			Timestamp: time.Now(),
		})
	} else {
		am.appendAudit(AuditEntry{
			ApprovalID: req.ID, Action: "notified", Actor: "system", Timestamp: time.Now(),
		})
	}
	if am.store != nil {
		_ = am.store.SaveApproval(req)
	}
}

// processReminders is called by the escalation loop on each tick: it finds
// pending approvals whose reminder is due (R2.4) and dispatches a
// re-notification. The NotificationsSent counter bounds the loop at
// MaxRenotifications reminders.
func (am *ApprovalManager) processReminders(now time.Time) {
	am.mu.Lock()
	notifier := am.notifier
	if notifier == nil {
		am.mu.Unlock()
		return
	}
	type due struct {
		snapshot *ApprovalRequest
		interval time.Duration
	}
	var dueList []due
	for _, req := range am.byID {
		if req.Status != StatusPending || req.NotificationsSent > MaxRenotifications {
			continue
		}
		if req.NextReminderAt.IsZero() || now.Before(req.NextReminderAt) {
			continue
		}
		s := *req
		dueList = append(dueList, due{snapshot: &s, interval: am.reminderIntervalFor(req.ActionType)})
	}
	am.mu.Unlock()

	for _, d := range dueList {
		// Mark the due point consumed first so a slow delivery cannot
		// cause double-dispatch on the next tick.
		am.mu.Lock()
		if req, ok := am.byID[d.snapshot.ID]; ok && req.Status == StatusPending {
			req.NextReminderAt = time.Time{}
		}
		am.mu.Unlock()
		go am.deliverNotification(d.snapshot, notifier, d.interval, true)
	}
}

// NotificationsSent returns the notification round count for an approval.
func (am *ApprovalManager) NotificationsSent(id string) int {
	am.mu.Lock()
	defer am.mu.Unlock()
	if req, ok := am.byID[id]; ok {
		return req.NotificationsSent
	}
	return 0
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
	am.notifyDecisionLocked(req)
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
	am.notifyDecisionLocked(req)
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
	// R4.4: mirror every lifecycle action into the external audit trail.
	// Dispatch asynchronously with a value snapshot so sinks never run
	// under the manager lock.
	if len(am.auditSinks) > 0 {
		sinks := make([]func(AuditEntry), len(am.auditSinks))
		copy(sinks, am.auditSinks)
		snapshot := entry
		if len(entry.Metadata) > 0 {
			snapshot.Metadata = make(map[string]string, len(entry.Metadata))
			for k, v := range entry.Metadata {
				snapshot.Metadata[k] = v
			}
		}
		for _, sink := range sinks {
			go sink(snapshot)
		}
	}
}

// AddAuditSink attaches an external audit integration (spec R4.4). The
// sink is called (asynchronously) for every entry appended to the
// engine's own audit log. Sinks are additive — the audit trail writer and
// the SSE approval stream can both consume lifecycle events.
func (am *ApprovalManager) AddAuditSink(fn func(AuditEntry)) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.auditSinks = append(am.auditSinks, fn)
}

// AddDecisionHook registers a callback invoked (asynchronously, with a
// value snapshot) whenever a request reaches a terminal status:
// approved, rejected, or expired. Hooks are additive; the manager keeps
// no ordering guarantee between them. Used by the A2A task gate to
// resume or fail the linked task (spec R5.3–R5.5).
func (am *ApprovalManager) AddDecisionHook(fn func(ApprovalRequest)) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.decisionHooks = append(am.decisionHooks, fn)
}

// notifyDecisionLocked fires the decision hooks for a request that just
// reached a terminal status. Callers must hold am.mu. The snapshot is
// dispatched asynchronously so hooks never run under the manager lock
// (same pattern as appendAudit's sink dispatch).
func (am *ApprovalManager) notifyDecisionLocked(req *ApprovalRequest) {
	if len(am.decisionHooks) == 0 || !req.IsTerminal() {
		return
	}
	snapshot := *req
	if len(req.Payload) > 0 {
		snapshot.Payload = make(map[string]any, len(req.Payload))
		for k, v := range req.Payload {
			snapshot.Payload[k] = v
		}
	}
	hooks := make([]func(ApprovalRequest), len(am.decisionHooks))
	copy(hooks, am.decisionHooks)
	for _, fn := range hooks {
		hook := fn
		go hook(snapshot)
	}
}
