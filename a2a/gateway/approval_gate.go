// Package gateway - approval_gate.go implements the hitl-approval spec R5
// task integration: an A2A task can declare requires_approval metadata and
// is then held in input-required until a human decides. The gate owns the
// task side of the linkage; the hitl engine owns the approval side, and
// decisions flow back through the manager's decision hooks.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/a2a/manager"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// Task metadata keys the gate reads (R5.1). Values are strings so they
// survive the JSONB metadata round-trip without schema changes.
const (
	// MetaRequiresApproval declares a task needs human approval before it
	// may run. "true"/"1" enables the gate.
	MetaRequiresApproval = "requires_approval"
	// MetaApprovalActionType names the hitl approval type (one of
	// hitl.DefaultApprovalTypes). Required when the gate is enabled.
	MetaApprovalActionType = "approval_action_type"
	// MetaApprovalUrgency overrides the request urgency (critical/high/
	// medium/low). Empty defaults to medium.
	MetaApprovalUrgency = "approval_urgency"
	// MetaApprovalID records the approval created for this task so the
	// linkage is visible in task metadata (R5.1, R1.3 cross-reference).
	MetaApprovalID = "approval_id"
)

// approvalContextPartType tags the data part injected on resume (R5.3).
const approvalContextPartType = "approval_context"

// declaresApproval reports whether task metadata opts the task into the
// HITL gate (R5.1).
func declaresApproval(meta map[string]string) bool {
	return meta[MetaRequiresApproval] == "true" || meta[MetaRequiresApproval] == "1"
}

// ApprovalGate holds A2A tasks in input-required until a human approval
// decision arrives. Construct with NewApprovalGate and install on the
// gateway with Gateway.SetApprovalGate — wiring is optional; without it
// the gateway never consults the metadata and behaves as before.
type ApprovalGate struct {
	manager *hitl.ApprovalManager
	gw      *Gateway
	log     *slog.Logger
}

// NewApprovalGate binds the gate to an approval manager and gateway. The
// manager must not be nil; log may be nil (slog.Default is used).
func NewApprovalGate(manager *hitl.ApprovalManager, gw *Gateway, log *slog.Logger) (*ApprovalGate, error) {
	if manager == nil {
		return nil, fmt.Errorf("approval gate: nil manager")
	}
	if gw == nil {
		return nil, fmt.Errorf("approval gate: nil gateway")
	}
	if log == nil {
		log = slog.Default()
	}
	return &ApprovalGate{manager: manager, gw: gw, log: log}, nil
}

// Start installs the decision hook on the manager. Call after wiring both
// components (hook registration is additive and idempotent per call —
// call once).
func (gate *ApprovalGate) Start() {
	gate.manager.AddDecisionHook(gate.onDecision)
}

// gateTask inspects the requested task's metadata and, when approval is
// required, creates the linked approval request against the persisted task
// (R5.1). Returns held=true when the caller must park the task instead of
// dispatching. The created task's metadata map is annotated with
// approval_id before return; persisting that is the caller's job.
func (gate *ApprovalGate) gateTask(t *models.Task, created *models.Task, id *Identity) (held bool, err error) {
	if !declaresApproval(created.Metadata) {
		return false, nil
	}
	actionType := created.Metadata[MetaApprovalActionType]
	if actionType == "" {
		return false, fmt.Errorf("a2a gateway: task declares %s but no %s", MetaRequiresApproval, MetaApprovalActionType)
	}
	urgency := created.Metadata[MetaApprovalUrgency]
	if urgency == "" {
		urgency = "medium"
	}
	orgID := ""
	requester := ""
	if id != nil {
		orgID = id.Metadata["org_id"]
		requester = id.Subject
	}

	payload := map[string]any{"task_id": created.ID}
	if created.ContextID != "" {
		payload["context_id"] = created.ContextID
	}
	if created.AgentID != "" {
		payload["agent_id"] = created.AgentID
	}
	// Carry the task's initiating message so the approver can see what the
	// agent intends to do (R1.3 detail view, R2.2 notification content).
	if len(t.Message.Parts) > 0 {
		if parts, jerr := json.Marshal(t.Message.Parts); jerr == nil {
			payload["request_parts"] = string(parts)
		}
	}

	approvalID := uuid.NewString()
	if _, err := gate.manager.CreateRequestWithOrg(approvalID, actionType, requester, urgency, created.ID, orgID, payload); err != nil {
		return false, fmt.Errorf("a2a gateway: create approval: %w", err)
	}
	created.Metadata[MetaApprovalID] = approvalID
	return true, nil
}

// onDecision is the hitl decision hook (registered via Start). It drives
// the linked task: approve -> resume with approval context (R5.3),
// reject -> failed (R5.4), expired -> configured timeout action (R5.5).
//
// Hook dispatch is asynchronous and the snapshot is a value copy, so this
// runs off the manager lock. Failures are logged, never returned — the
// approval decision itself already succeeded.
func (gate *ApprovalGate) onDecision(req hitl.ApprovalRequest) {
	if req.TaskID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch req.Status {
	case hitl.StatusApproved:
		gate.resumeTask(ctx, req)
	case hitl.StatusRejected:
		gate.failTask(ctx, req, fmt.Sprintf("approval rejected: %s", req.DecisionNote))
	case hitl.StatusExpired:
		// R5.5: an expired approval follows the type's timeout action.
		// Expired-by-timeout reaches this hook only when the engine
		// auto-rejected (OnTimeout=reject, or escalation cap exhausted —
		// see R3.5); both mean the action was not authorised, so the
		// task fails with the timeout reason.
		gate.failTask(ctx, req, fmt.Sprintf("approval expired: %s", req.DecisionNote))
	}
}

// resumeTask transitions input-required -> working and appends a message
// whose parts carry the approval context (R5.3).
func (gate *ApprovalGate) resumeTask(ctx context.Context, req hitl.ApprovalRequest) {
	task, err := gate.gw.GetTaskInternal(ctx, req.TaskID)
	if err != nil {
		gate.log.Warn("hitl gate: resume: task not found", "task_id", req.TaskID, "approval_id", req.ID, "err", err)
		return
	}
	if task.Status != models.TaskStatusInputRequired {
		gate.log.Warn("hitl gate: resume: task no longer awaiting input",
			"task_id", task.ID, "status", task.Status, "approval_id", req.ID)
		return
	}
	// EventResume moves the task back to working (R5.2 exit).
	working, err := gate.gw.UpdateTaskStatus(ctx, task.ID, manager.EventResume, int(task.Version))
	if err != nil {
		gate.log.Warn("hitl gate: resume transition failed", "task_id", task.ID, "approval_id", req.ID, "err", err)
		return
	}

	// Approval context in message parts (R5.3): a text summary plus a
	// structured data part so adapters can consume it mechanically.
	ctxData := map[string]string{
		"type":        approvalContextPartType,
		"approval_id": req.ID,
		"action_type": req.ActionType,
		"decision":    "approved",
		"decided_by":  req.DecidedBy,
	}
	if req.DecisionNote != "" {
		ctxData["note"] = req.DecisionNote
	}
	encoded, jerr := json.Marshal(ctxData)
	if jerr != nil {
		gate.log.Warn("hitl gate: marshal approval context", "approval_id", req.ID, "err", jerr)
		return
	}
	msg := models.Message{
		Role: "user",
		Parts: []models.Part{
			{Text: fmt.Sprintf("Approval %s (%s) granted by %s; task may proceed.", req.ID, req.ActionType, req.DecidedBy)},
			{Data: encoded},
		},
	}
	if _, err := gate.gw.AddMessage(ctx, working.ID, msg, int(working.Version)); err != nil {
		gate.log.Warn("hitl gate: add approval context failed", "task_id", task.ID, "approval_id", req.ID, "err", err)
	}
	gate.log.Info("hitl gate: task resumed with approval context",
		"task_id", task.ID, "approval_id", req.ID, "decided_by", req.DecidedBy)
}

// failTask transitions a non-terminal task to failed with the given reason
// (R5.4, R5.5).
func (gate *ApprovalGate) failTask(ctx context.Context, req hitl.ApprovalRequest, reason string) {
	task, err := gate.gw.GetTaskInternal(ctx, req.TaskID)
	if err != nil {
		gate.log.Warn("hitl gate: fail: task not found", "task_id", req.TaskID, "approval_id", req.ID, "err", err)
		return
	}
	if models.IsTerminal(task.Status) {
		return
	}
	if _, err := gate.gw.UpdateTaskStatus(ctx, task.ID, manager.EventFail, int(task.Version)); err != nil {
		gate.log.Warn("hitl gate: fail transition failed", "task_id", task.ID, "approval_id", req.ID, "err", err)
		return
	}
	msg := models.Message{
		Role:  "user",
		Parts: []models.Part{{Text: fmt.Sprintf("Task failed: %s", reason)}},
	}
	if failed, err := gate.gw.GetTaskInternal(ctx, task.ID); err == nil {
		if _, err := gate.gw.AddMessage(ctx, failed.ID, msg, int(failed.Version)); err != nil {
			gate.log.Warn("hitl gate: add failure reason failed", "task_id", task.ID, "approval_id", req.ID, "err", err)
		}
	}
	gate.log.Info("hitl gate: task failed on approval decision",
		"task_id", task.ID, "approval_id", req.ID, "status", req.Status)
}
