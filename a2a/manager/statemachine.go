// Package manager implements the A2A task lifecycle state machine.
// The state machine is the authoritative arbiter of legal task
// transitions; manager.go drives it from API calls and store.go
// persists the results.
package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Task state machine
// ============================================================

// ValidTransitions defines the legal (currentState, event) -> nextState
// mappings for task lifecycle transitions. Any (state, event) pair not
// present in the map is rejected by Transition.
//
// The A2A task lifecycle has 7 states:
//   pending           - task created, not yet started
//   working           - agent actively processing
//   input-required    - agent needs more input from the user
//   output-available  - agent has produced an output artifact
//   completed         - terminal: task finished successfully
//   failed            - terminal: task encountered an unrecoverable error
//   cancelled         - terminal: task was cancelled by user or system
var ValidTransitions = map[string]map[string]string{
	// pending: initial state, can start working or be cancelled
	models.TaskStatusPending: {
		EventStart:     models.TaskStatusWorking,
		EventCancel:    models.TaskStatusCancelled,
		EventFail:      models.TaskStatusFailed,
	},

	// working: agent is processing
	models.TaskStatusWorking: {
		EventRequestInput:   models.TaskStatusInputRequired,
		EventProduceOutput:  models.TaskStatusOutputAvailable,
		EventComplete:       models.TaskStatusCompleted,
		EventFail:           models.TaskStatusFailed,
		EventCancel:         models.TaskStatusCancelled,
	},

	// input-required: agent needs user input to continue
	models.TaskStatusInputRequired: {
		EventResume:  models.TaskStatusWorking,
		EventFail:    models.TaskStatusFailed,
		EventCancel:  models.TaskStatusCancelled,
	},

	// output-available: agent has produced output, may continue or finalize
	models.TaskStatusOutputAvailable: {
		EventContinue:  models.TaskStatusWorking,
		EventComplete:  models.TaskStatusCompleted,
		EventFail:      models.TaskStatusFailed,
		EventCancel:    models.TaskStatusCancelled,
	},

	// completed: terminal - no outgoing transitions
	// failed:    terminal - no outgoing transitions
	// cancelled: terminal - no outgoing transitions
}

// Event names that drive task state transitions.
const (
	EventStart         = "start"
	EventRequestInput  = "request_input"
	EventProduceOutput = "produce_output"
	EventComplete      = "complete"
	EventFail          = "fail"
	EventCancel        = "cancel"
	EventResume        = "resume"
	EventContinue      = "continue"
)

// ErrInvalidTransition is returned when an event is not valid for the
// current state.
var ErrInvalidTransition = errors.New("invalid task state transition")

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

// TransitionInput is the input to Transition. Actor identifies who or
// what triggered the event (user ID, "system", agent ID, etc.).
type TransitionInput struct {
	Task  *models.Task
	Event string
	Actor string
}

// Transition validates and applies a state transition to the task in-place.
// It mutates the task's Status and UpdatedAt fields, then returns the
// task. Callers must persist the updated task after a successful transition.
//
// Returns ErrInvalidTransition if the (currentStatus, event) pair is
// not in ValidTransitions. Returns an error if the task or event is nil/empty.
func Transition(_ context.Context, in TransitionInput) (*models.Task, error) {
	if in.Task == nil {
		return nil, errors.New("statemachine: nil task")
	}
	if in.Event == "" {
		return nil, errors.New("statemachine: empty event")
	}

	from := in.Task.Status
	to, err := NextState(from, in.Event)
	if err != nil {
		return nil, err
	}

	in.Task.Status = to
	// UpdatedAt is set by the caller (TaskManager) after persistence,
	// or by the model layer. We do not import time here to keep this
	// file dependency-light; the TaskManager sets UpdatedAt before calling
	// Transition, and the updated task is returned.
	return in.Task, nil
}
