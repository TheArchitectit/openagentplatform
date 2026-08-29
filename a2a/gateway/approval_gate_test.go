// Package gateway - approval_gate_test.go covers the hitl-approval spec R5
// task integration end-to-end through the gateway: declaration -> hold,
// approve -> resume with approval context, reject/expire -> failed.
package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

type gateTestEnv struct {
	gw    *Gateway
	mgr   *hitl.ApprovalManager
	store *memStore
	gate  *ApprovalGate
	id    *Identity
}

func newGateTestEnv(t *testing.T) *gateTestEnv {
	t.Helper()
	ms := newMemStore()
	mgr := hitl.NewApprovalManager([]hitl.ApprovalTypeConfig{
		{Type: "patch_deploy", TimeoutDuration: time.Hour, OnTimeout: "escalate", MaxEscalations: 1, EscalationGroups: []string{"oncall"}},
		{Type: "script_execute", TimeoutDuration: time.Hour, OnTimeout: "reject"},
	})
	gw, err := NewGateway(manager.NewTaskManagerWithStore(ms), newTestRegistry(t), newTestRouter(t), Config{RequireAuth: false})
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	gate, err := NewApprovalGate(mgr, gw, nil)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	gate.Start()
	gw.SetApprovalGate(gate)
	return &gateTestEnv{
		gw:    gw,
		mgr:   mgr,
		store: ms,
		gate:  gate,
		id:    &Identity{Subject: "agent-requester-7", Scopes: []string{PermSend}, Metadata: map[string]string{"org_id": "org-a"}},
	}
}

func approvalTask() *models.Task {
	return &models.Task{
		AgentID: "http://agents.test/patcher",
		Message: models.Message{
			Role:  "user",
			Parts: []models.Part{{Text: "deploy patch KB5034441 to ws-01"}},
		},
		Metadata: map[string]string{
			MetaRequiresApproval:   "true",
			MetaApprovalActionType: "patch_deploy",
			MetaApprovalUrgency:    "high",
		},
	}
}

func waitForTaskStatus(t *testing.T, ms *memStore, taskID, want string) *models.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := ms.GetTask(context.Background(), taskID)
		if err == nil && task.Status == want {
			return task
		}
		time.Sleep(2 * time.Millisecond)
	}
	task, _ := ms.GetTask(context.Background(), taskID)
	got := "<missing>"
	if task != nil {
		got = task.Status
	}
	t.Fatalf("task %s never reached status %q (last: %q)", taskID, want, got)
	return nil
}

// R5.1 + R5.2: declaring requires_approval creates the linked request and
// parks the task in input-required.
func TestSendTaskHeldForApproval(t *testing.T) {
	env := newGateTestEnv(t)
	created, err := env.gw.SendTask(context.Background(), env.id, approvalTask())
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	if created.Status != models.TaskStatusInputRequired {
		t.Fatalf("status = %q, want %q", created.Status, models.TaskStatusInputRequired)
	}
	approvalID := created.Metadata[MetaApprovalID]
	if approvalID == "" {
		t.Fatalf("metadata missing %s: %+v", MetaApprovalID, created.Metadata)
	}

	req, err := env.mgr.GetRequest(approvalID)
	if err != nil {
		t.Fatalf("GetRequest(%s): %v", approvalID, err)
	}
	if req.TaskID != created.ID {
		t.Errorf("approval TaskID = %q, want %q", req.TaskID, created.ID)
	}
	if req.Status != hitl.StatusPending {
		t.Errorf("approval status = %q, want pending", req.Status)
	}
	if req.OrgID != "org-a" {
		t.Errorf("approval OrgID = %q, want org-a", req.OrgID)
	}
	if req.RequesterAgentID != env.id.Subject {
		t.Errorf("approval requester = %q, want %q", req.RequesterAgentID, env.id.Subject)
	}
	if req.Urgency != "high" {
		t.Errorf("approval urgency = %q, want high", req.Urgency)
	}
	if parts, _ := req.Payload["request_parts"].(string); !strings.Contains(parts, "KB5034441") {
		t.Errorf("payload request_parts = %v, want the task's initiating message", req.Payload["request_parts"])
	}
	if tid, _ := req.Payload["task_id"].(string); tid != created.ID {
		t.Errorf("payload task_id = %v, want %s", req.Payload["task_id"], created.ID)
	}
}

// R5.1 guardrail: a declaration without an installed gate must not create
// an unheld task.
func TestSendTaskNoGateRejectsDeclaration(t *testing.T) {
	ms := newMemStore()
	gw, err := NewGateway(manager.NewTaskManagerWithStore(ms), newTestRegistry(t), newTestRouter(t), Config{RequireAuth: false})
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	if _, err := gw.SendTask(context.Background(), nil, approvalTask()); err == nil {
		t.Fatal("expected error when gate is not configured, got nil")
	} else if !strings.Contains(err.Error(), "no approval gate") {
		t.Fatalf("error = %v, want mention of missing gate", err)
	}
	if _, total, _ := ms.ListTasks(context.Background(), manager.TaskFilter{}); total != 0 {
		t.Fatalf("store has %d task(s), want none", total)
	}
}

// R5.1 guardrail: declaration without an action type is rejected outright.
func TestSendTaskMissingActionTypeRejected(t *testing.T) {
	env := newGateTestEnv(t)
	task := approvalTask()
	delete(task.Metadata, MetaApprovalActionType)
	if _, err := env.gw.SendTask(context.Background(), env.id, task); err == nil {
		t.Fatal("expected error for missing approval_action_type, got nil")
	}
}

// Control: tasks that say nothing about approval behave exactly as before.
func TestSendTaskPlainTaskUnaffected(t *testing.T) {
	env := newGateTestEnv(t)
	created, err := env.gw.SendTask(context.Background(), env.id, &models.Task{
		AgentID: "http://agents.test/plain",
		Message: models.Message{Role: "user", Parts: []models.Part{{Text: "hello"}}},
	})
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	if created.Status != models.TaskStatusPending {
		t.Fatalf("status = %q, want pending", created.Status)
	}
	if _, ok := created.Metadata[MetaApprovalID]; ok {
		t.Fatalf("plain task should not carry approval_id, got %v", created.Metadata)
	}
	if pending := env.mgr.ListPending(); len(pending) != 0 {
		t.Fatalf("manager has %d pending approvals, want none", len(pending))
	}
}

// R5.3: approval resumes the task and appends approval context in message
// parts (text summary + structured data part).
func TestApprovalResumeWithApprovalContext(t *testing.T) {
	env := newGateTestEnv(t)
	created, err := env.gw.SendTask(context.Background(), env.id, approvalTask())
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	approvalID := created.Metadata[MetaApprovalID]

	if err := env.mgr.Approve(approvalID, "admin@corp", "within window"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	waitForTaskStatus(t, env.store, created.ID, models.TaskStatusWorking)

	msgs, err := env.store.GetMessages(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	var (
		sawText bool
		ctxData map[string]string
	)
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "granted by admin@corp") && strings.Contains(p.Text, approvalID) {
				sawText = true
			}
			if len(p.Data) > 0 {
				var d map[string]string
				if err := json.Unmarshal(p.Data, &d); err == nil && d["type"] == approvalContextPartType {
					ctxData = d
				}
			}
		}
	}
	if !sawText {
		t.Fatalf("no approval-context text part found in %d message(s)", len(msgs))
	}
	if ctxData == nil {
		t.Fatalf("no approval_context data part found in %d message(s)", len(msgs))
	}
	if ctxData["approval_id"] != approvalID || ctxData["decision"] != "approved" ||
		ctxData["decided_by"] != "admin@corp" || ctxData["note"] != "within window" {
		t.Fatalf("approval context = %+v, want approval_id/decision/decided_by/note", ctxData)
	}
}

// R5.4: rejection fails the task with the rejection reason recorded.
func TestApprovalRejectionFailsTask(t *testing.T) {
	env := newGateTestEnv(t)
	created, err := env.gw.SendTask(context.Background(), env.id, approvalTask())
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	approvalID := created.Metadata[MetaApprovalID]

	if err := env.mgr.Reject(approvalID, "admin@corp", "not in change window"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	waitForTaskStatus(t, env.store, created.ID, models.TaskStatusFailed)

	msgs, err := env.store.GetMessages(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	var reason string
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "Task failed") {
				reason = p.Text
			}
		}
	}
	if reason == "" || !strings.Contains(reason, "not in change window") {
		t.Fatalf("rejection reason part = %q, want it to carry the reject reason", reason)
	}
}

// R5.5: an expired approval (engine auto-rejected on timeout) fails the
// linked task with the timeout reason. The engine-side hook firing is
// covered in a2a/hitl; here we drive the dispatch path directly.
func TestApprovalExpiryFailsTask(t *testing.T) {
	env := newGateTestEnv(t)
	created, err := env.gw.SendTask(context.Background(), env.id, approvalTask())
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}
	approvalID := created.Metadata[MetaApprovalID]

	env.gate.onDecision(hitl.ApprovalRequest{
		ID:           approvalID,
		TaskID:       created.ID,
		Status:       hitl.StatusExpired,
		DecisionNote: "timeout exceeded",
	})
	waitForTaskStatus(t, env.store, created.ID, models.TaskStatusFailed)

	msgs, _ := env.store.GetMessages(context.Background(), created.ID)
	var found bool
	for _, m := range msgs {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "approval expired") && strings.Contains(p.Text, "timeout exceeded") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no expiry-reason message part found in %d message(s)", len(msgs))
	}
}

// A decision on an approval with no linked task (created directly via the
// R1 API) must not touch any task.
func TestDecisionWithoutTaskIsNoop(t *testing.T) {
	env := newGateTestEnv(t)
	req, err := env.mgr.CreateRequest("manual-1", "script_execute", "agent-x", "low", "", nil)
	if err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if req.TaskID != "" {
		t.Fatalf("TaskID = %q, want empty", req.TaskID)
	}
	// onDecision returns immediately for unlinked requests; nothing to
	// assert beyond "no panic and no task state changes".
	env.gate.onDecision(*req)
}
