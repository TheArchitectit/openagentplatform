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
			ee.autoReject(req, now, "unknown action type", false)
			continue
		}

		switch {
		case cfg.OnTimeout == "escalate" && req.EscalationDepth < cfg.MaxEscalations:
			ee.escalate(req, cfg, now)
		case cfg.OnTimeout == "escalate":
			// R3.5: the depth cap was reached without a decision —
			// auto-reject and alert the admin.
			ee.autoReject(req, now, "max escalation depth reached", true)
		default:
			ee.autoReject(req, now, "timeout exceeded", false)
		}
	}
}

// autoReject rejects an expired approval. When alertAdmin is true (spec
// R3.5: max escalation depth exhausted) it also dispatches a timeout alert
// through the manager's notification seam.
func (ee *EscalationEngine) autoReject(req *ApprovalRequest, now time.Time, reason string, alertAdmin bool) {
	req.Status = StatusExpired
	req.DecidedBy = "system"
	req.DecidedAt = now
	req.DecisionNote = reason

	if alertAdmin {
		if notifier := ee.manager.notifier; notifier != nil {
			snapshot := *req
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := notifier.SendTimeoutAlert(ctx, &snapshot); err != nil {
					log.Printf("hitl: timeout alert for approval %s failed: %v", snapshot.ID, err)
				}
			}()
		}
		ee.manager.appendAudit(AuditEntry{
			ApprovalID: req.ID,
			Action:     "admin_alert",
			Actor:      "system",
			Reason:     reason,
			Timestamp:  now,
		})
	}

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

// NotificationService sends approval notifications through the platform's
// channel infrastructure (email, Slack, webhook).
type NotificationService interface {
	// SendApprovalRequest notifies configured channels of a new approval.
	SendApprovalRequest(ctx context.Context, req *ApprovalRequest) error

	// SendReminder re-notifies about a still-pending approval.
	SendReminder(ctx context.Context, req *ApprovalRequest) error

	// SendTimeoutAlert notifies that an approval expired at maximum
	// escalation depth (spec R3.5).
	SendTimeoutAlert(ctx context.Context, req *ApprovalRequest) error
}
