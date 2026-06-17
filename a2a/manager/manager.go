// Package manager - manager.go implements the TaskManager, the
// business-logic layer for A2A task operations. It wraps the Store
// and enforces the task state machine with optimistic concurrency
// control.
package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// TaskManager
// ============================================================

// TaskManager is the high-level task management API. It uses a Store
// for persistence and enforces the task state machine with optimistic
// concurrency via a version column.
type TaskManager struct {
	store Store
}

// NewTaskManager constructs a TaskManager backed by a pgx connection pool.
func NewTaskManager(pool *pgxpool.Pool) *TaskManager {
	return &TaskManager{store: NewPGStore(pool)}
}

// NewTaskManagerWithStore constructs a TaskManager with a custom Store
// implementation (useful for testing with a mock store).
func NewTaskManagerWithStore(store Store) *TaskManager {
	return &TaskManager{store: store}
}

// ============================================================
// CreateTask
// ============================================================

// CreateTask creates a new A2A task in the "pending" state. The task
// ID is auto-generated as a UUID if not provided. The initial version
// is 1. Returns the persisted task.
func (m *TaskManager) CreateTask(ctx context.Context, sessionID, agentCardURL string, metadata map[string]string) (*models.Task, error) {
	now := time.Now().UTC()
	t := &models.Task{
		ID:         uuid.NewString(),
		ContextID:  sessionID,
		AgentID:    agentCardURL,
		Status:     models.TaskStatusPending,
		Metadata:   metadata,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := t.Validate(); err != nil {
		return nil, fmt.Errorf("a2a: validate task: %w", err)
	}
	if err := m.store.InsertTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// ============================================================
// GetTask
// ============================================================

// GetTask fetches a task by id, including its artifacts.
func (m *TaskManager) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if id == "" {
		return nil, errors.New("a2a: task ID required")
	}
	return m.store.GetTask(ctx, id)
}

// ============================================================
// ListTasks
// ============================================================

// ListTasks returns a filtered, paginated list of tasks.
func (m *TaskManager) ListTasks(ctx context.Context, f TaskFilter) ([]models.Task, int, error) {
	return m.store.ListTasks(ctx, f)
}

// ============================================================
// CancelTask
// ============================================================

// CancelTask transitions a task to "cancelled" state. Only non-terminal
// tasks can be cancelled. Uses optimistic concurrency: the caller must
// provide the expected version. Returns ErrInvalidTransition if the
// task is already in a terminal state.
func (m *TaskManager) CancelTask(ctx context.Context, id string, version int) (*models.Task, error) {
	if id == "" {
		return nil, errors.New("a2a: task ID required")
	}

	// Fetch current task to check state machine validity
	current, err := m.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	if models.IsTerminal(current.Status) {
		return nil, fmt.Errorf("%w: cannot cancel task in terminal state %q", ErrInvalidTransition, current.Status)
	}

	// Apply state machine transition
	in := TransitionInput{
		Task:  current,
		Event: EventCancel,
	}
	updated, err := Transition(ctx, in)
	if err != nil {
		return nil, err
	}

	// Persist with optimistic concurrency
	if err := m.store.UpdateTaskStatus(ctx, updated.ID, updated.Status, version); err != nil {
		return nil, err
	}

	// Re-fetch to get the new version
	return m.store.GetTask(ctx, id)
}

// ============================================================
// UpdateStatus
// ============================================================

// UpdateStatus transitions a task to a new status via an event.
// The caller specifies the event (e.g., EventStart, EventComplete,
// EventFail) and the expected version. The state machine validates
// the transition. On success, the version is incremented.
func (m *TaskManager) UpdateStatus(ctx context.Context, id string, event string, version int) (*models.Task, error) {
	if id == "" {
		return nil, errors.New("a2a: task ID required")
	}
	if event == "" {
		return nil, errors.New("a2a: event required")
	}

	// Fetch current task
	current, err := m.store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply state machine transition
	in := TransitionInput{
		Task:  current,
		Event: event,
	}
	updated, err := Transition(ctx, in)
	if err != nil {
		return nil, err
	}

	// Persist with optimistic concurrency
	if err := m.store.UpdateTaskStatus(ctx, updated.ID, updated.Status, version); err != nil {
		return nil, err
	}

	// Re-fetch to get the new version
	return m.store.GetTask(ctx, id)
}

// ============================================================
// AddMessage
// ============================================================

// AddMessage appends a message to a task's conversation history.
// Uses optimistic concurrency: the caller must provide the expected
// version. On success, the version is incremented.
func (m *TaskManager) AddMessage(ctx context.Context, taskID string, msg models.Message, version int) (*models.Task, error) {
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	if err := msg.Validate(); err != nil {
		return nil, fmt.Errorf("a2a: validate message: %w", err)
	}

	if err := m.store.AddMessage(ctx, taskID, msg, version); err != nil {
		return nil, err
	}

	// Re-fetch to return updated task
	return m.store.GetTask(ctx, taskID)
}

// ============================================================
// AddArtifact
// ============================================================

// AddArtifact attaches an artifact to a task. Artifacts are immutable
// once created. The artifact ID is auto-generated if not provided.
func (m *TaskManager) AddArtifact(ctx context.Context, taskID string, name, description, mimeType string, parts []models.Part) (*models.Artifact, error) {
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	if len(parts) == 0 {
		return nil, errors.New("a2a: artifact must have at least one part")
	}

	// Verify the task exists
	if _, err := m.store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}

	a := &models.Artifact{
		ID:          uuid.NewString(),
		TaskID:      taskID,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
		Parts:       parts,
		CreatedAt:   time.Now().UTC(),
	}
	if err := m.store.InsertArtifact(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}
