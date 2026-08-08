package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/a2a/models"
)

func (s *pgStore) AddMessage(ctx context.Context, taskID string, msg models.Message, version int) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return errors.New("a2a: task ID required")
	}
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("a2a: invalid message: %w", err)
	}

	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("a2a: marshal message: %w", err)
	}

	const q = `
		UPDATE a2a_tasks SET
			messages   = messages || $2::jsonb,
			version    = version + 1,
			updated_at = NOW()
		WHERE id = $1 AND version = $3
	`
	tag, err := s.pool.Exec(ctx, q, taskID, msgJSON, version)
	if err != nil {
		return fmt.Errorf("a2a: add message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionMismatch
	}
	return nil
}

// GetMessages returns all messages for a task, ordered by insertion
// order (JSONB array order).
func (s *pgStore) GetMessages(ctx context.Context, taskID string) ([]models.Message, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	const q = `SELECT messages FROM a2a_tasks WHERE id = $1 LIMIT 1`
	var raw []byte
	err := s.pool.QueryRow(ctx, q, taskID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("a2a: get messages: %w", err)
	}
	if len(raw) == 0 {
		return []models.Message{}, nil
	}
	var msgs []models.Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return nil, fmt.Errorf("a2a: unmarshal messages: %w", err)
	}
	return msgs, nil
}

// ============================================================
// Artifact operations
// ============================================================

// InsertArtifact inserts a new artifact for a task. The artifact's
// ID, task_id, and created_at must be set by the caller.
func (s *pgStore) InsertArtifact(ctx context.Context, a *models.Artifact) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if a.ID == "" {
		return errors.New("a2a: artifact ID required")
	}
	if a.TaskID == "" {
		return errors.New("a2a: artifact task_id required")
	}

	partsJSON, err := json.Marshal(a.Parts)
	if err != nil {
		return fmt.Errorf("a2a: marshal parts: %w", err)
	}
	// The model Artifact does not have a top-level metadata field;
	// artifacts carry metadata via their parts or description. We store
	// a minimal JSON with name and description.
	metaMap := map[string]string{
		"name":        a.Name,
		"description": a.Description,
		"mime_type":   a.MimeType,
	}
	metaJSON, err := json.Marshal(metaMap)
	if err != nil {
		return fmt.Errorf("a2a: marshal artifact metadata: %w", err)
	}

	const q = `
		INSERT INTO a2a_artifacts (id, task_id, parts, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = s.pool.Exec(ctx, q, a.ID, a.TaskID, partsJSON, metaJSON, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("a2a: insert artifact: %w", err)
	}
	return nil
}

// GetArtifact fetches a single artifact by (id, task_id).
// Returns ErrArtifactNotFound if not found.
func (s *pgStore) GetArtifact(ctx context.Context, id string, taskID string) (*models.Artifact, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	const q = `
		SELECT id, task_id, parts, metadata, created_at
		FROM a2a_artifacts
		WHERE id = $1 AND task_id = $2
		LIMIT 1
	`
	var a models.Artifact
	var partsRaw []byte
	var metaRaw []byte
	err := s.pool.QueryRow(ctx, q, id, taskID).Scan(
		&a.ID, &a.TaskID, &partsRaw, &metaRaw, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("a2a: get artifact: %w", err)
	}
	if len(partsRaw) > 0 {
		_ = json.Unmarshal(partsRaw, &a.Parts)
	}
	if len(metaRaw) > 0 {
		var m map[string]string
		_ = json.Unmarshal(metaRaw, &m)
		a.Name = m["name"]
		a.Description = m["description"]
		a.MimeType = m["mime_type"]
	}
	return &a, nil
}

// ListArtifacts returns all artifacts for a task, ordered by created_at ASC.
func (s *pgStore) ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	if taskID == "" {
		return nil, errors.New("a2a: task ID required")
	}
	const q = `
		SELECT id, task_id, parts, metadata, created_at
		FROM a2a_artifacts
		WHERE task_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.pool.Query(ctx, q, taskID)
	if err != nil {
		return nil, fmt.Errorf("a2a: list artifacts: %w", err)
	}
	defer rows.Close()
	out := make([]models.Artifact, 0, 4)
	for rows.Next() {
		var a models.Artifact
		var partsRaw []byte
		var metaRaw []byte
		if err := rows.Scan(&a.ID, &a.TaskID, &partsRaw, &metaRaw, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("a2a: scan artifact: %w", err)
		}
		if len(partsRaw) > 0 {
			_ = json.Unmarshal(partsRaw, &a.Parts)
		}
		if len(metaRaw) > 0 {
			var m map[string]string
			_ = json.Unmarshal(metaRaw, &m)
			a.Name = m["name"]
			a.Description = m["description"]
			a.MimeType = m["mime_type"]
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteArtifact removes an artifact by (id, task_id).
func (s *pgStore) DeleteArtifact(ctx context.Context, id string, taskID string) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	const q = `DELETE FROM a2a_artifacts WHERE id = $1 AND task_id = $2`
	tag, err := s.pool.Exec(ctx, q, id, taskID)
	if err != nil {
		return fmt.Errorf("a2a: delete artifact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrArtifactNotFound
	}
	return nil
}

// ============================================================
// Errors
// ============================================================

// ErrTaskNotFound is returned when a task id does not exist.
var ErrTaskNotFound = errors.New("a2a: task not found")

// ErrArtifactNotFound is returned when an artifact (id, task_id) does not exist.
var ErrArtifactNotFound = errors.New("a2a: artifact not found")

// ErrVersionMismatch is returned when an optimistic concurrency check
// fails (the stored version does not match the expected version).
var ErrVersionMismatch = errors.New("a2a: version mismatch (concurrent modification)")

// ============================================================
// Helpers
// ============================================================

// jsonOrNull marshals v to JSON, or returns nil if v is empty/nil.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	// For maps, check if empty
	if m, ok := v.(map[string]string); ok && len(m) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(v)
}

// joinAnd joins SQL fragments with " AND ".
func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
