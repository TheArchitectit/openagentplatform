package hitl

import (
	"context"
	"log"
	"sync"
	"time"
)

// ============================================================
// Escalation Engine — timeout + escalation background loop.
// ============================================================

// EscalationEngine periodically checks for expired approvals and
// either auto-rejects or escalates them based on type config.
type EscalationEngine struct {
	mu      sync.Mutex
	manager *ApprovalManager
	ticker  *time.Ticker
	stopCh  chan struct{}
}

// NewEscalationEngine creates an engine that checks every interval.
func NewEscalationEngine(manager *ApprovalManager, checkInterval time.Duration) *EscalationEngine {
	return &EscalationEngine{
		manager: manager,
		ticker:  time.NewTicker(checkInterval),
		stopCh:  make(chan struct{}),
	}
}

// Start begins the background check loop. Pass context.Background()
// if no cancellation is needed.
func (ee *EscalationEngine) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ee.ticker.C:
				now := time.Now()
				ee.checkExpired(now)
				// Same loop drives re-notification (R2.4).
				ee.manager.processReminders(now)
			case <-ee.stopCh:
				ee.ticker.Stop()
				return
			case <-doneChan(ctx):
				ee.ticker.Stop()
				return
			}
		}
	}()
}

// doneChan returns a channel that closes when ctx is done,
// or nil if ctx is nil (avoids nil-pointer dereference).
func doneChan(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// Stop halts the background loop.
func (ee *EscalationEngine) Stop() {
	close(ee.stopCh)
}

// checkExpired scans all pending approvals and handles timeouts.
func (ee *EscalationEngine) checkExpired(now time.Time) {
	ee.manager.mu.Lock()
	defer ee.manager.mu.Unlock()
	for _, req := range ee.manager.byID {
		if req.Status != StatusPending {
			continue
		}
		if now.Before(req.ExpiresAt) {
			continue
		}

		cfg, ok := ee.manager.typeCfgs[req.ActionType]
		if !ok {
			// Unknown type — auto-reject.
			ee.autoReject(req, now, "unknown action type")
			continue
		}

		if cfg.OnTimeout == "escalate" && req.EscalationDepth < cfg.MaxEscalations {
			ee.escalate(req, cfg, now)
		} else {
			ee.autoReject(req, now, "timeout exceeded")
		}
	}
}

// autoReject rejects an expired approval.
func (ee *EscalationEngine) autoReject(req *ApprovalRequest, now time.Time, reason string) {
	req.Status = StatusExpired
	req.DecidedBy = "system"
	req.DecidedAt = now
	req.DecisionNote = reason

	ee.manager.appendAudit(AuditEntry{
		ApprovalID: req.ID,
		Action:     "expired",
		Actor:      "system",
		Reason:     reason,
		Timestamp:  now,
	})

	log.Printf("hitl: approval %s expired (type=%s, depth=%d)", req.ID, req.ActionType, req.EscalationDepth)
}

// escalate moves an approval to the next escalation group.
func (ee *EscalationEngine) escalate(req *ApprovalRequest, cfg ApprovalTypeConfig, now time.Time) {
	req.Status = StatusEscalated
	req.EscalationDepth++

	ee.manager.appendAudit(AuditEntry{
		ApprovalID: req.ID,
		Action:     "escalated",
		Actor:      "system",
		Reason:     "timeout — escalated to next group",
		Timestamp:  now,
		Metadata: map[string]string{
			"escalation_depth":  string(rune('0' + req.EscalationDepth)),
			"escalation_groups": joinGroups(cfg.EscalationGroups),
		},
	})

	log.Printf("hitl: approval %s escalated to depth %d", req.ID, req.EscalationDepth)

	// Create a new pending approval in the next escalation group.
	nextDepth := req.EscalationDepth
	if nextDepth <= len(cfg.EscalationGroups) {
		req.Status = StatusPending
		req.ExpiresAt = now.Add(cfg.TimeoutDuration)
		req.NotificationsSent = 0 // reset for new round
	}
}

func joinGroups(groups []string) string {
	result := ""
	for i, g := range groups {
		if i > 0 {
			result += ","
		}
		result += g
	}
	return result
}

// ============================================================
// Notification — stub for Sprint 2.7 stories (email/Slack/webhook).
// ============================================================

// NotificationService sends approval notifications. The actual
// delivery (email, Slack, webhook) is implemented in a later story.
type NotificationService interface {
	// SendApprovalRequest notifies configured channels of a new approval.
	SendApprovalRequest(ctx context.Context, req *ApprovalRequest) error

	// SendReminder re-notifies about a still-pending approval.
	SendReminder(ctx context.Context, req *ApprovalRequest) error
}
