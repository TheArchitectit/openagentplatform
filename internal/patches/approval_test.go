package patches

import (
	"context"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// TestValidTransitions drives each legal (state, event) -> nextState pair
// through the workflow and asserts the job lands in the expected destination
// state. System events (start/complete/fail) are exercised without an actor;
// user-driven events use an actor with the matching RBAC permission.
func TestValidTransitions(t *testing.T) {
	w := NewApprovalWorkflow()

	approveActor := &Actor{ID: "user-1", Name: "User One", Permissions: []string{PermApprove}}
	scheduleActor := &Actor{ID: "user-2", Name: "User Two", Permissions: []string{PermSchedule}}
	rejectActor := &Actor{ID: "user-rj", Name: "Rejector", Permissions: []string{PermReject}}
	rollbackActor := &Actor{ID: "user-4", Name: "User Four", Permissions: []string{PermRollback}}

	future := time.Now().Add(1 * time.Hour)

	tests := []struct {
		name  string
		from  string
		event string
		actor *Actor
		prep  func(j *models.PatchJob)
		want  string
	}{
		{
			name:  "pending_approval -> approved (1 of 1 approvers)",
			from:  StatePendingApproval,
			event: EventApprove,
			actor: approveActor,
			prep: func(j *models.PatchJob) { j.RequiredApprovals = 1 },
			want:  StateApproved,
		},
		{
			name:  "pending_approval -> rejected",
			from:  StatePendingApproval,
			event: EventReject,
			actor: rejectActor,
			want:  StateRejected,
		},
		{
			name:  "approved -> scheduled",
			from:  StateApproved,
			event: EventSchedule,
			actor: scheduleActor,
			want:  StateScheduled,
		},
		{
			name:  "scheduled -> in_progress (system start)",
			from:  StateScheduled,
			event: EventStart,
			actor: nil, // system event
			want:  StateInProgress,
		},
		{
			name:  "in_progress -> completed (system)",
			from:  StateInProgress,
			event: EventComplete,
			actor: nil,
			want:  StateCompleted,
		},
		{
			name:  "in_progress -> failed (system)",
			from:  StateInProgress,
			event: EventFail,
			actor: nil,
			prep: func(j *models.PatchJob) {
				j.FailureReason = "unit-test failure"
			},
			want: StateFailed,
		},
		{
			name:  "completed -> rolled_back",
			from:  StateCompleted,
			event: EventRollback,
			actor: rollbackActor,
			want:  StateRolledBack,
		},
		{
			name:  "approved -> rejected (admin override)",
			from:  StateApproved,
			event: EventAdminReject,
			actor: rejectActor, // admin_reject requires PermReject
			want:  StateRejected,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := &models.PatchJob{
				ID:     "job-1",
				State:  tc.from,
				OrgID:  "org-1",
				Title:  "unit test job",
				Approvals: []models.ApprovalRecord{
					// Pre-seed an approval to satisfy standard patches.
					{PatchJobID: "job-1", ApproverID: "seed", Decision: "approved"},
				},
			}
			if tc.prep != nil {
				tc.prep(job)
			}

			in := TransitionInput{
				Job:     job,
				Event:   tc.event,
				Actor:   tc.actor,
				Comment: "unit-test",
			}
			if tc.event == EventSchedule {
				in.ScheduledAt = &future
				in.MaintenanceStart = &[]time.Time{time.Now()}[0]
				in.MaintenanceEnd = &[]time.Time{future.Add(time.Hour)}[0]
			}

			res, err := w.Transition(context.Background(), in)
			if err != nil {
				t.Fatalf("Transition(%s, %s): %v", tc.from, tc.event, err)
			}
			if res.Job.State != tc.want {
				t.Errorf("Transition(%s, %s): got state %s, want %s", tc.from, tc.event, res.Job.State, tc.want)
			}
		})
	}
}

// TestInvalidTransitions verifies that illegal (state, event) pairs and
// RBAC violations are rejected with an error.
func TestInvalidTransitions(t *testing.T) {
	w := NewApprovalWorkflow()

	approveActor := &Actor{ID: "user-1", Permissions: []string{PermApprove}}

	tests := []struct {
		name  string
		from  string
		event string
		actor *Actor
	}{
		{"rolled_back is terminal", StateRolledBack, EventApprove, approveActor},
		{"completed -> approve is illegal", StateCompleted, EventApprove, approveActor},
		{"in_progress -> reject is illegal", StateInProgress, EventReject, approveActor},
		{"rolled_back -> start is illegal", StateRolledBack, EventStart, nil},
		{"unknown event rejected", StatePendingApproval, "no_such_event", approveActor},
		{"approve without permission denied", StatePendingApproval, EventApprove,
			&Actor{ID: "user-no-perm", Permissions: []string{}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := &models.PatchJob{ID: "job-x", State: tc.from, RequiredApprovals: 1}
			_, err := w.Transition(context.Background(), TransitionInput{
				Job:   job,
				Event: tc.event,
				Actor: tc.actor,
			})
			if err == nil {
				t.Errorf("Transition(%s, %s): expected error, got nil", tc.from, tc.event)
			}
		})
	}
}

// TestAutoApproveCritical verifies that ApplyPolicy immediately moves a
// freshly-created critical-severity patch job to the approved state with a
// synthetic system approval record.
func TestAutoApproveCritical(t *testing.T) {
	w := NewApprovalWorkflow()

	job := &models.PatchJob{
		ID:       "job-critical",
		Severity: models.PatchSeverityCritical,
		State:    StatePendingApproval,
		OrgID:    "org-1",
	}

	w.ApplyPolicy(job)

	if job.State != StateApproved {
		t.Errorf("critical patch: got state %s, want %s", job.State, StateApproved)
	}
	if job.RequiredApprovals != 0 {
		t.Errorf("critical patch: RequiredApprovals = %d, want 0", job.RequiredApprovals)
	}
	if !job.AutoApproveOnTimeout {
		t.Error("critical patch: AutoApproveOnTimeout should be true")
	}
	if job.ApprovalTimeout == nil {
		t.Error("critical patch: ApprovalTimeout should be populated when auto-approve is on")
	}
	if CountApprovals(job) != 1 {
		t.Errorf("critical patch: expected exactly 1 system approval record, got %d", CountApprovals(job))
	}
}

// TestRequiresApproval verifies that standard-severity patches stay in
// pending_approval until the required number of human approvers have signed
// off; a single approval is sufficient for a standard patch.
func TestRequiresApproval(t *testing.T) {
	w := NewApprovalWorkflow()

	job := &models.PatchJob{
		ID:       "job-standard",
		Severity: models.PatchSeverityStandard,
		State:    StatePendingApproval,
		OrgID:    "org-1",
	}

	w.ApplyPolicy(job)
	if job.State != StatePendingApproval {
		t.Fatalf("ApplyPolicy: standard patch should stay in pending_approval, got %s", job.State)
	}
	if job.RequiredApprovals != 1 {
		t.Fatalf("standard patch: RequiredApprovals = %d, want 1", job.RequiredApprovals)
	}

	// First (and only required) approval: workflow must move to approved.
	actor := &Actor{ID: "user-approver", Name: "Approver", Permissions: []string{PermApprove}}
	res, err := w.Transition(context.Background(), TransitionInput{
		Job:     job,
		Event:   EventApprove,
		Actor:   actor,
		Comment: "lgtm",
	})
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if res.Job.State != StateApproved {
		t.Errorf("after 1 approval: got state %s, want %s", res.Job.State, StateApproved)
	}
	if CountApprovals(res.Job) != 1 {
		t.Errorf("expected exactly 1 approval record, got %d", CountApprovals(res.Job))
	}
}

// TestRequiresTwoApprovalsForMajorOS verifies that a major_os upgrade stays in
// pending_approval after one approval and only transitions to approved once
// the second distinct approver signs off.
func TestRequiresTwoApprovalsForMajorOS(t *testing.T) {
	w := NewApprovalWorkflow()

	job := &models.PatchJob{
		ID:       "job-majoros",
		Severity: models.PatchSeverityMajorOS,
		State:    StatePendingApproval,
		OrgID:    "org-1",
	}
	w.ApplyPolicy(job)
	if job.RequiredApprovals != 2 {
		t.Fatalf("major_os: RequiredApprovals = %d, want 2", job.RequiredApprovals)
	}

	a1 := &Actor{ID: "user-a", Permissions: []string{PermApprove}}
	a2 := &Actor{ID: "user-b", Permissions: []string{PermApprove}}

	res, err := w.Transition(context.Background(), TransitionInput{
		Job: job, Event: EventApprove, Actor: a1,
	})
	if err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if res.Job.State != StatePendingApproval {
		t.Errorf("after 1 approval: got %s, want pending_approval", res.Job.State)
	}

	res, err = w.Transition(context.Background(), TransitionInput{
		Job: res.Job, Event: EventApprove, Actor: a2,
	})
	if err != nil {
		t.Fatalf("second approval: %v", err)
	}
	if res.Job.State != StateApproved {
		t.Errorf("after 2 approvals: got %s, want approved", res.Job.State)
	}
}
