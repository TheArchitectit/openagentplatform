// Package alerts implements the alert rule engine with a 6-state
// lifecycle machine. The state machine is the authoritative arbiter
// of legal alert transitions; the engine (engine.go) drives it from
// NATS messages and the store (store.go) persists the results.
package alerts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Alert states. These are the six states in the lifecycle machine.
const (
	StatePending      = "pending"
	StateOpen         = "open"
	StateAcknowledged = "acknowledged"
	StateSnoozed      = "snoozed"
	StateResolved     = "resolved"
	StateClosed       = "closed"
)

// Events that drive state transitions.
const (
	EventCheckFailure   = "check_failure"
	EventAcknowledge    = "acknowledge"
	EventSnooze         = "snooze"
	EventCheckRecovery  = "check_recovery"
	EventClose          = "close"
	EventSnoozeExpired  = "snooze_expired"
	EventEscalate       = "escalate"
	EventSuppress       = "suppress"
)

// ErrInvalidTransition is returned when an event is not valid for the
// current state.
var ErrInvalidTransition = errors.New("invalid state transition")

// ValidTransitions defines the legal (state, event) -> nextState mappings.
// Any (state, event) pair not present in the map is rejected by Transition.
var ValidTransitions = map[string]map[string]string{
	StatePending: {
		EventCheckFailure:  StateOpen,        // escalate pending -> open
		EventAcknowledge:   StateAcknowledged, // user acks before escalation
		EventSnooze:        StateSnoozed,
		EventCheckRecovery: StateResolved,
		EventClose:         StateClosed,
		EventSuppress:      StateClosed,      // suppressed alerts go straight to closed
	},
	StateOpen: {
		EventAcknowledge:   StateAcknowledged,
		EventSnooze:        StateSnoozed,
		EventCheckRecovery: StateResolved,
		EventClose:         StateClosed,
	},
	StateAcknowledged: {
		EventSnooze:        StateSnoozed,
		EventCheckRecovery: StateResolved,
		EventClose:         StateClosed,
		// re-failure reopens
		EventCheckFailure:  StateOpen,
	},
	StateSnoozed: {
		EventSnoozeExpired: StateOpen,        // auto on expiry
		EventAcknowledge:   StateAcknowledged,
		EventCheckRecovery: StateResolved,
		EventClose:         StateClosed,
		EventCheckFailure:  StateOpen,
	},
	StateResolved: {
		EventClose: StateClosed, // manual or timeout close
		// re-failure within the close window reopens
		EventCheckFailure: StateOpen,
	},
	// StateClosed is terminal: no outgoing transitions.
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

// StateMachine executes state transitions for a single alert. It is
// stateless beyond the alert it is operating on; callers are responsible
// for loading/persisting the alert via Store.
type StateMachine struct {
	// Now is the time source. Defaults to time.Now.
	Now func() time.Time
}

// NewStateMachine constructs a StateMachine with the default clock.
func NewStateMachine() *StateMachine {
	return &StateMachine{Now: time.Now}
}

// TransitionInput is the input to Transition. Actor identifies who or
// what triggered the event (user ID, "system", "escalator", etc.).
// For snooze events, Duration specifies the snooze length.
type TransitionInput struct {
	Alert    *models.Alert
	Event    string
	Actor    string
	Reason   string
	Duration time.Duration // for EventSnooze
}

// Transition validates and applies a state transition to the alert in-place.
// It returns the transition record that should be written to the audit log.
// Callers must persist both the updated alert and the returned record.
func (sm *StateMachine) Transition(_ context.Context, in TransitionInput) (*models.AlertStateMachine, error) {
	if in.Alert == nil {
		return nil, errors.New("statemachine: nil alert")
	}
	if in.Event == "" {
		return nil, errors.New("statemachine: empty event")
	}

	from := in.Alert.State
	to, err := NextState(from, in.Event)
	if err != nil {
		return nil, err
	}

	now := sm.now()
	alert := in.Alert
	alert.State = to
	alert.UpdatedAt = now

	switch in.Event {
	case EventAcknowledge:
		alert.AcknowledgedBy = in.Actor
	case EventSnooze:
		until := now.Add(in.Duration)
		alert.SnoozedUntil = &until
	case EventSnoozeExpired:
		alert.SnoozedUntil = nil
	case EventCheckRecovery:
		t := now
		alert.ResolvedAt = &t
	case EventClose:
		t := now
		alert.ClosedAt = &t
	}

	rec := &models.AlertStateMachine{
		AlertID:   alert.ID,
		FromState: from,
		ToState:   to,
		Event:     in.Event,
		Actor:     in.Actor,
		Reason:    in.Reason,
		CreatedAt: now,
	}
	return rec, nil
}

// StateHistory returns the timeline of state transitions for a given alert,
// ordered from oldest to newest. It is a thin wrapper around Store.StateHistory
// to keep the state-machine API self-contained.
type StateHistoryEntry struct {
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	Event     string    `json:"event"`
	Actor     string    `json:"actor"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// StateHistory reads the transition history for an alert and returns it
// as a flat timeline. The store is the source of truth; this is a
// convenience for API responses.
func (sm *StateMachine) StateHistory(ctx context.Context, store HistoryReader, alertID string) ([]StateHistoryEntry, error) {
	if store == nil {
		return nil, errors.New("statemachine: nil store")
	}
	rows, err := store.GetStateHistory(ctx, alertID)
	if err != nil {
		return nil, err
	}
	out := make([]StateHistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, StateHistoryEntry{
			FromState: r.FromState,
			ToState:   r.ToState,
			Event:     r.Event,
			Actor:     r.Actor,
			Reason:    r.Reason,
			Timestamp: r.CreatedAt,
		})
	}
	return out, nil
}

// HistoryReader is the minimal store interface used by StateHistory.
// The full Store interface lives in store.go.
type HistoryReader interface {
	GetStateHistory(ctx context.Context, alertID string) ([]models.AlertStateMachine, error)
}

func (sm *StateMachine) now() time.Time {
	if sm.Now != nil {
		return sm.Now()
	}
	return time.Now()
}
