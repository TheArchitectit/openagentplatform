// Package session records terminal input and output for audit playback.
package session

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Direction identifies the direction of terminal data.
type Direction string

const (
	DirectionInput  Direction = "input"
	DirectionOutput Direction = "output"
)

var (
	ErrAlreadyStarted = errors.New("session: recorder already started")
	ErrNotStarted     = errors.New("session: recorder not started")
	ErrStopped        = errors.New("session: recorder stopped")
)

// Event is one terminal input or output captured during a session.
type Event struct {
	Offset    time.Duration `json:"offset"`
	Direction Direction     `json:"direction"`
	Data      []byte        `json:"data"`
}

// Recording is the serializable representation of a terminal session.
type Recording struct {
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	StoppedAt time.Time `json:"stopped_at,omitempty"`
	Events    []Event   `json:"events"`
}

// SessionRecorder captures terminal input and output in memory.
type SessionRecorder struct {
	mu        sync.Mutex
	sessionID string
	startedAt time.Time
	stoppedAt time.Time
	events    []Event
	started   bool
	stopped   bool
}

// NewSessionRecorder constructs a recorder for sessionID.
func NewSessionRecorder(sessionID string) *SessionRecorder {
	return &SessionRecorder{sessionID: sessionID}
}

// Start begins recording at the supplied time.
func (r *SessionRecorder) Start(at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return ErrAlreadyStarted
	}
	if at.IsZero() {
		at = time.Now()
	}
	r.startedAt = at.UTC()
	r.started = true
	return nil
}

// WriteInput captures terminal input.
func (r *SessionRecorder) WriteInput(data []byte) error {
	return r.write(DirectionInput, data, time.Now())
}

// WriteOutput captures terminal output.
func (r *SessionRecorder) WriteOutput(data []byte) error {
	return r.write(DirectionOutput, data, time.Now())
}

func (r *SessionRecorder) write(direction Direction, data []byte, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return ErrNotStarted
	}
	if r.stopped {
		return ErrStopped
	}
	if len(data) == 0 {
		return nil
	}
	dataCopy := append([]byte(nil), data...)
	offset := at.Sub(r.startedAt)
	if offset < 0 {
		offset = 0
	}
	r.events = append(r.events, Event{Offset: offset, Direction: direction, Data: dataCopy})
	return nil
}

// Stop finishes recording at the supplied time. It is idempotent.
func (r *SessionRecorder) Stop(at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return ErrNotStarted
	}
	if r.stopped {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	if at.Before(r.startedAt) {
		at = r.startedAt
	}
	r.stoppedAt = at
	r.stopped = true
	return nil
}

// Recording returns a copy of the captured session.
func (r *SessionRecorder) Recording() Recording {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]Event, len(r.events))
	for i, event := range r.events {
		events[i] = event
		events[i].Data = append([]byte(nil), event.Data...)
	}
	return Recording{
		SessionID: r.sessionID,
		StartedAt: r.startedAt,
		StoppedAt: r.stoppedAt,
		Events:    events,
	}
}

// MarshalJSON serializes the current recording for persistence or playback.
func (r *SessionRecorder) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.Recording())
}
