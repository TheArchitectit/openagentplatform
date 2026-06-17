package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// TestValidTransitions verifies all legal transitions in the alert state machine
// move the alert from the expected source state to the correct destination state
// without returning an error.
func TestValidTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name   string
		from   string
		event  string
		wantTo string
	}{
		{"pending->open via check_failure", StatePending, EventCheckFailure, StateOpen},
		{"pending->acknowledged via acknowledge", StatePending, EventAcknowledge, StateAcknowledged},
		{"pending->snoozed via snooze", StatePending, EventSnooze, StateSnoozed},
		{"pending->resolved via check_recovery", StatePending, EventCheckRecovery, StateResolved},
		{"pending->closed via close", StatePending, EventClose, StateClosed},
		{"pending->closed via suppress", StatePending, EventSuppress, StateClosed},

		{"open->acknowledged via acknowledge", StateOpen, EventAcknowledge, StateAcknowledged},
		{"open->snoozed via snooze", StateOpen, EventSnooze, StateSnoozed},
		{"open->resolved via check_recovery", StateOpen, EventCheckRecovery, StateResolved},
		{"open->closed via close", StateOpen, EventClose, StateClosed},

		{"acknowledged->resolved via check_recovery", StateAcknowledged, EventCheckRecovery, StateResolved},
		{"acknowledged->closed via close", StateAcknowledged, EventClose, StateClosed},

		{"snoozed->open via snooze_expired", StateSnoozed, EventSnoozeExpired, StateOpen},
		{"snoozed->closed via close", StateSnoozed, EventClose, StateClosed},

		{"resolved->closed via close", StateResolved, EventClose, StateClosed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.Alert{ID: "alert-1", State: tc.from}
			rec, err := sm.Transition(context.Background(), TransitionInput{
				Alert:  a,
				Event:  tc.event,
				Actor:  "test-actor",
				Reason: "unit-test",
			})
			if err != nil {
				t.Fatalf("Transition(%q, %q) returned unexpected error: %v", tc.from, tc.event, err)
			}
			if a.State != tc.wantTo {
				t.Errorf("Transition(%q, %q): alert.State = %q, want %q", tc.from, tc.event, a.State, tc.wantTo)
			}
			if rec == nil {
				t.Errorf("Transition(%q, %q): expected non-nil record", tc.from, tc.event)
			}
		})
	}
}

// TestInvalidTransitions verifies that events not legal for a given source state
// are rejected with an error.
func TestInvalidTransitions(t *testing.T) {
	sm := NewStateMachine()

	tests := []struct {
		name  string
		from  string
		event string
	}{
		{"closed has no outbound events", StateClosed, EventCheckFailure},
		{"closed cannot acknowledge", StateClosed, EventAcknowledge},
		{"closed cannot snooze", StateClosed, EventSnooze},
		{"closed cannot recover", StateClosed, EventCheckRecovery},

		{"resolved cannot acknowledge", StateResolved, EventAcknowledge},
		{"resolved cannot snooze", StateResolved, EventSnooze},

		{"acknowledged cannot escalate", StateAcknowledged, EventEscalate},
		{"acknowledged cannot snooze_expired", StateAcknowledged, EventSnoozeExpired},

		{"open cannot escalate", StateOpen, EventEscalate},
		{"open cannot suppress", StateOpen, EventSuppress},

		{"unknown event rejected", StateOpen, "no_such_event"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &models.Alert{ID: "alert-1", State: tc.from}
			_, err := sm.Transition(context.Background(), TransitionInput{
				Alert: a,
				Event: tc.event,
				Actor: "test-actor",
			})
			if err == nil {
				t.Fatalf("Transition(%q, %q) expected error, got nil", tc.from, tc.event)
			}
		})
	}
}

// TestStateHistory verifies that the StateHistory helper reads transitions from a
// HistoryReader implementation in chronological order.
func TestStateHistory(t *testing.T) {
	now := time.Now().UTC()

	rows := []models.AlertStateMachine{
		{
			AlertID:   "alert-1",
			FromState: StatePending,
			ToState:   StateOpen,
			Event:     EventCheckFailure,
			Actor:     "agent-1",
			Reason:    "initial failure",
			CreatedAt: now.Add(-2 * time.Minute),
		},
		{
			AlertID:   "alert-1",
			FromState: StateOpen,
			ToState:   StateAcknowledged,
			Event:     EventAcknowledge,
			Actor:     "user-1",
			Reason:    "investigating",
			CreatedAt: now.Add(-1 * time.Minute),
		},
		{
			AlertID:   "alert-1",
			FromState: StateAcknowledged,
			ToState:   StateResolved,
			Event:     EventCheckRecovery,
			Actor:     "agent-1",
			Reason:    "service back",
			CreatedAt: now,
		},
	}

	store := &fakeHistoryStore{rows: rows}
	sm := NewStateMachine()
	hist, err := sm.StateHistory(context.Background(), store, "alert-1")
	if err != nil {
		t.Fatalf("StateHistory returned error: %v", err)
	}
	if len(hist) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(hist))
	}

	if hist[0].FromState != StatePending || hist[0].ToState != StateOpen {
		t.Errorf("entry[0]: got %s->%s, want pending->open", hist[0].FromState, hist[0].ToState)
	}
	if hist[1].FromState != StateOpen || hist[1].ToState != StateAcknowledged {
		t.Errorf("entry[1]: got %s->%s, want open->acknowledged", hist[1].FromState, hist[1].ToState)
	}
	if hist[2].Actor != "agent-1" || hist[2].Reason != "service back" {
		t.Errorf("entry[2]: got actor=%s reason=%s, want agent-1/service back", hist[2].Actor, hist[2].Reason)
	}
}

// fakeHistoryStore implements HistoryReader for the StateHistory test.
type fakeHistoryStore struct {
	rows []models.AlertStateMachine
}

func (f *fakeHistoryStore) GetStateHistory(_ context.Context, _ string) ([]models.AlertStateMachine, error) {
	return f.rows, nil
}
