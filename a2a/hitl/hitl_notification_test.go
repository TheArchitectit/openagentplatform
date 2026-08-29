package hitl

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingNotifier captures NotificationService calls so tests can assert
// R2.1 (create notification) and R2.4 (re-notification) behavior.
type recordingNotifier struct {
	mu            sync.Mutex
	createCalls   []string
	reminderCalls []string
	timeoutCalls  []string
	attempts      int // every call, including failed ones
	failNext      bool
}

func (n *recordingNotifier) SendApprovalRequest(_ context.Context, req *ApprovalRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.attempts++
	if n.failNext {
		n.failNext = false
		return context.DeadlineExceeded
	}
	n.createCalls = append(n.createCalls, req.ID)
	return nil
}

func (n *recordingNotifier) SendReminder(_ context.Context, req *ApprovalRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.attempts++
	n.reminderCalls = append(n.reminderCalls, req.ID)
	return nil
}

func (n *recordingNotifier) SendTimeoutAlert(_ context.Context, req *ApprovalRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.attempts++
	n.timeoutCalls = append(n.timeoutCalls, req.ID)
	return nil
}

func (n *recordingNotifier) counts() (int, int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.createCalls), len(n.reminderCalls)
}

func (n *recordingNotifier) attemptCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.attempts
}

// waitForAttempts polls until the notifier has been invoked at least n times.
func waitForAttempts(t *testing.T, notifier *recordingNotifier, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if notifier.attemptCount() >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("notifier never reached %d attempts (got %d)", n, notifier.attemptCount())
}

func waitForNotifier(t *testing.T, n *recordingNotifier, wantCreate, wantReminder int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, r := n.counts()
		if c == wantCreate && r == wantReminder {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	c, r := n.counts()
	t.Fatalf("notifier calls: create=%d reminder=%d, want %d/%d", c, r, wantCreate, wantReminder)
}

// waitForBookkeeping polls until the predicate on a request holds. Delivery
// bookkeeping happens on an async goroutine after the notifier call returns.
func waitForBookkeeping(t *testing.T, mgr *ApprovalManager, id string, pred func(*ApprovalRequest) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if req, err := mgr.GetRequest(id); err == nil && pred(req) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	req, _ := mgr.GetRequest(id)
	t.Fatalf("bookkeeping never landed: sent=%d next=%v status=%s", req.NotificationsSent, req.NextReminderAt, req.Status)
}

// TestCreateNotifies verifies R2.1: creating a request with a wired
// notifier delivers the initial notification asynchronously.
func TestCreateNotifies(t *testing.T) {
	notifier := &recordingNotifier{}
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetStore(NewMemStore())
	mgr.SetNotifier(notifier)
	mgr.SetDefaultReminderInterval(time.Hour)

	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	waitForNotifier(t, notifier, 1, 0)

	req, _ := mgr.GetRequest("a1")
	if req.NotificationsSent != 1 {
		t.Errorf("NotificationsSent = %d, want 1", req.NotificationsSent)
	}
	if req.NextReminderAt.IsZero() {
		t.Error("expected NextReminderAt to be set after the create notification")
	}
	if got := mgr.NotificationsSent("a1"); got != 1 {
		t.Errorf("manager.NotificationsSent = %d, want 1", got)
	}
	// The audit log gained a "notified" entry.
	found := false
	for _, e := range mgr.AuditLog("a1") {
		if e.Action == "notified" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'notified' audit entry")
	}
}

// TestReminderLoopCappedAtThree verifies R2.4: processReminders
// re-notifies still-pending approvals after the configured delay and stops
// after MaxRenotifications reminders.
func TestReminderLoopCappedAtThree(t *testing.T) {
	notifier := &recordingNotifier{}
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetStore(NewMemStore())
	mgr.SetNotifier(notifier)
	mgr.SetDefaultReminderInterval(time.Millisecond)

	if _, err := mgr.CreateRequest("a1", "script_execute", "agent-1", "medium", "", nil); err != nil {
		t.Fatal(err)
	}
	waitForNotifier(t, notifier, 1, 0)
	// Wait for the create-delivery goroutine to land bookkeeping.
	waitForBookkeeping(t, mgr, "a1", func(r *ApprovalRequest) bool {
		return r.NotificationsSent == 1 && !r.NextReminderAt.IsZero()
	})

	// Drive MaxRenotifications reminder rounds; each round's bookkeeping
	// must land before the next processReminders sees the request due.
	later := time.Now().Add(time.Hour)
	for round := 1; round <= MaxRenotifications; round++ {
		mgr.processReminders(later)
		wantSent := 1 + round
		waitForBookkeeping(t, mgr, "a1", func(r *ApprovalRequest) bool {
			return r.NotificationsSent == wantSent
		})
	}
	// Exactly MaxRenotifications reminders, not more: one more sweep must
	// add nothing even though NextReminderAt was cleared after the cap.
	mgr.processReminders(later)
	time.Sleep(20 * time.Millisecond)
	c, r := notifier.counts()
	if c != 1 || r != MaxRenotifications {
		t.Fatalf("create=%d reminder=%d, want 1/%d", c, r, MaxRenotifications)
	}

	req, _ := mgr.GetRequest("a1")
	if !req.NextReminderAt.IsZero() {
		t.Error("expected NextReminderAt cleared after the reminder cap was reached")
	}

	// Reminder rounds are audited.
	reminders := 0
	for _, e := range mgr.AuditLog("a1") {
		if e.Action == "reminder" {
			reminders++
		}
	}
	if reminders != MaxRenotifications {
		t.Errorf("reminder audit entries = %d, want %d", reminders, MaxRenotifications)
	}
}

// TestReminderNotDueBeforeInterval verifies processReminders honors the
// delay: nothing is sent while NextReminderAt is in the future.
func TestReminderNotDueBeforeInterval(t *testing.T) {
	notifier := &recordingNotifier{}
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetNotifier(notifier)
	mgr.SetDefaultReminderInterval(time.Hour) // due in the future

	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "low", "", nil); err != nil {
		t.Fatal(err)
	}
	waitForNotifier(t, notifier, 1, 0)

	mgr.processReminders(time.Now()) // reminder due in an hour → no-op
	time.Sleep(20 * time.Millisecond)
	c, r := notifier.counts()
	if c != 1 || r != 0 {
		t.Fatalf("expected no reminder before interval, got create=%d reminder=%d", c, r)
	}
}

// TestReminderSkipsDecided verifies decided approvals never re-notify.
func TestReminderSkipsDecided(t *testing.T) {
	notifier := &recordingNotifier{}
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetNotifier(notifier)
	mgr.SetDefaultReminderInterval(time.Millisecond)

	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "low", "", nil); err != nil {
		t.Fatal(err)
	}
	waitForNotifier(t, notifier, 1, 0)

	if err := mgr.Approve("a1", "human", ""); err != nil {
		t.Fatal(err)
	}
	mgr.processReminders(time.Now().Add(time.Hour))
	waitForNotifier(t, notifier, 1, 0)
}

// TestTimeoutAlertAtMaxDepth verifies R3.5: an approval that expires at
// maximum escalation depth is auto-rejected AND an admin timeout alert is
// dispatched through the notification seam.
func TestTimeoutAlertAtMaxDepth(t *testing.T) {
	notifier := &recordingNotifier{}
	// OnTimeout escalate with MaxEscalations 1: first timeout escalates
	// (depth 0→1, re-armed because one escalation group exists), second
	// timeout hits the depth cap → expired + admin alert.
	typeCfgs := []ApprovalTypeConfig{
		{Type: "t", TimeoutDuration: 10 * time.Millisecond, OnTimeout: "escalate", MaxEscalations: 1, EscalationGroups: []string{"g1"}},
	}
	mgr := NewApprovalManager(typeCfgs)
	mgr.SetNotifier(notifier)

	if _, err := mgr.CreateRequest("a1", "t", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}

	engine := NewEscalationEngine(mgr, 5*time.Millisecond)
	engine.Start(context.Background())

	// Wait for the approval to expire at max depth.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if req, err := mgr.GetRequest("a1"); err == nil && req.Status == StatusExpired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	engine.Stop()

	req, _ := mgr.GetRequest("a1")
	if req.Status != StatusExpired {
		t.Fatalf("expected expired at max depth, got %s", req.Status)
	}
	if req.DecisionNote != "max escalation depth reached" {
		t.Errorf("decision note = %q", req.DecisionNote)
	}

	// The timeout alert was dispatched.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		notifier.mu.Lock()
		n := len(notifier.timeoutCalls)
		notifier.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	notifier.mu.Lock()
	got := len(notifier.timeoutCalls)
	notifier.mu.Unlock()
	if got != 1 {
		t.Errorf("timeout alerts = %d, want 1", got)
	}

	// Both the admin_alert and expired audit entries exist.
	var sawAlert, sawExpired bool
	for _, e := range mgr.AuditLog("a1") {
		if e.Action == "admin_alert" {
			sawAlert = true
		}
		if e.Action == "expired" {
			sawExpired = true
		}
	}
	if !sawAlert || !sawExpired {
		t.Errorf("audit entries: admin_alert=%v expired=%v, want both true", sawAlert, sawExpired)
	}
}

// TestNoTimeoutAlertOnPlainExpiry verifies the admin alert only fires at the
// escalation-depth cap, not on ordinary timeout rejection.
func TestNoTimeoutAlertOnPlainExpiry(t *testing.T) {
	notifier := &recordingNotifier{}
	typeCfgs := []ApprovalTypeConfig{
		{Type: "t", TimeoutDuration: 10 * time.Millisecond, OnTimeout: "reject", MaxEscalations: 3},
	}
	mgr := NewApprovalManager(typeCfgs)
	mgr.SetNotifier(notifier)

	if _, err := mgr.CreateRequest("a1", "t", "agent-1", "high", "", nil); err != nil {
		t.Fatal(err)
	}
	engine := NewEscalationEngine(mgr, 5*time.Millisecond)
	engine.Start(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if req, err := mgr.GetRequest("a1"); err == nil && req.Status == StatusExpired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	engine.Stop()
	time.Sleep(20 * time.Millisecond) // allow any stray alert to land

	notifier.mu.Lock()
	got := len(notifier.timeoutCalls)
	notifier.mu.Unlock()
	if got != 0 {
		t.Errorf("plain timeout dispatched %d timeout alerts, want 0", got)
	}
}

// TestNotificationFailureDoesNotAdvance verifies a failed delivery leaves
// bookkeeping untouched so the reminder loop can retry later.
func TestNotificationFailureDoesNotAdvance(t *testing.T) {
	notifier := &recordingNotifier{}
	notifier.failNext = true
	mgr := NewApprovalManager(DefaultApprovalTypes())
	mgr.SetNotifier(notifier)

	if _, err := mgr.CreateRequest("a1", "secret_access", "agent-1", "low", "", nil); err != nil {
		t.Fatal(err)
	}
	// Give the async delivery goroutine time to run and fail. A failed
	// delivery must not advance the counter or arm a reminder.
	time.Sleep(50 * time.Millisecond)
	req, _ := mgr.GetRequest("a1")
	if req.NotificationsSent != 0 || !req.NextReminderAt.IsZero() {
		t.Errorf("failed delivery advanced bookkeeping: sent=%d next=%v", req.NotificationsSent, req.NextReminderAt)
	}
}
