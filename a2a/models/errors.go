package models

import "errors"

// Sentinel errors for the A2A module.
var (
	// ErrTaskNotFound is returned when a requested task does not exist.
	ErrTaskNotFound = errors.New("a2a: task not found")

	// ErrInvalidTransition is returned when a task state transition is not allowed.
	ErrInvalidTransition = errors.New("a2a: invalid task status transition")

	// ErrNoMatchingAgent is returned when no agent matches the required skills.
	ErrNoMatchingAgent = errors.New("a2a: no matching agent found")
)
