package patches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// IngestKBScan records a freshly-scanned KB article for an agent. It is
// idempotent: the first sighting upserts a row in the "scanned" (default)
// state; repeated scans with the same (agent_id, kb) are no-ops. After
// the upsert, the auto-approve policy is applied: a critical severity is
// auto-approved (scanned -> approved), while any other severity is queued
// for approval (scanned -> pending_approval). The resulting state is
// returned.
func (s *pgPatchStore) IngestKBScan(ctx context.Context, orgID, agentID, kb, severity string) (string, error) {
	if s.pool == nil {
		return "", errors.New("patches: nil pool")
	}
	if orgID == "" || agentID == "" || kb == "" {
		return "", errors.New("patches: org_id, agent_id, and kb are required")
	}

	now := time.Now().UTC()
	id := uuid.NewString()
	const upsert = `
		INSERT INTO winupdate_kb_state (id, org_id, agent_id, kb, state, result, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (agent_id, kb) DO NOTHING
	`
	if _, err := s.pool.Exec(ctx, upsert,
		id, orgID, agentID, kb, WinUpdateStateScanned, "", now, now,
	); err != nil {
		return "", fmt.Errorf("patches: kb scan upsert: %w", err)
	}

	// Read the current state (either the row we just inserted or the
	// pre-existing one). org_id is always part of the WHERE clause.
	cur, err := s.getKBState(ctx, orgID, agentID, kb)
	if err != nil {
		return "", err
	}

	// Auto-approve policy mirrors ApprovalWorkflow.ApplyPolicy: only
	// critical severity auto-approves. Everything else is queued for
	// human approval.
	event := WinUpdateEventQueue
	if severity == "critical" {
		event = WinUpdateEventApprove
	}
	next, err := WinUpdateNextState(cur.State, event)
	if err != nil {
		// Already past scanned (e.g. already approved/installed) ->
		// leave as-is and report current state. This keeps repeated
		// scans idempotent once a KB has advanced past the initial
		// auto-approve/queue decision.
		return cur.State, nil
	}
	if next != cur.State {
		if err := s.updateKBState(ctx, orgID, agentID, kb, next, ""); err != nil {
			return "", err
		}
		return next, nil
	}
	return cur.State, nil
}

// IngestKBInstall records an install outcome reported by the agent. It is
// tolerant of at-least-once delivery: if the current state is one that
// precedes installing (approved, scanned, pending_approval), it first
// transitions to installing, then applies the outcome:
//
//	success && rebootRequired -> reboot_required (event reboot)
//	success                   -> installed       (event complete)
//	!success                  -> failed          (event fail; errMsg in result)
func (s *pgPatchStore) IngestKBInstall(ctx context.Context, orgID, agentID, kb string, success, rebootRequired bool, errMsg string) (string, error) {
	if s.pool == nil {
		return "", errors.New("patches: nil pool")
	}
	if orgID == "" || agentID == "" || kb == "" {
		return "", errors.New("patches: org_id, agent_id, and kb are required")
	}

	cur, err := s.getKBState(ctx, orgID, agentID, kb)
	if err != nil {
		return "", err
	}

	// Determine the desired outcome for this delivery.
	var desired string
	switch {
	case success && rebootRequired:
		desired = WinUpdateStateRebootRequired
	case success:
		desired = WinUpdateStateInstalled
	default:
		desired = WinUpdateStateFailed
	}

	// Idempotent redelivery: if the row is already at the same terminal
	// outcome this delivery is reporting, treat it as a no-op and return
	// the existing state without any write. This keeps IngestKBInstall
	// safe under at-least-once NATS redelivery.
	if cur.State == desired {
		return cur.State, nil
	}

	// Walk from the current state to installing through the legal path.
	// install is only valid from approved/failed, so rows still in
	// scanned/pending_approval must first be approved (and that
	// transition is persisted so the audit trail is complete).
	if cur.State != WinUpdateStateInstalling {
		from := cur.State
		if from == WinUpdateStateScanned || from == WinUpdateStatePendingApproval {
			if err := s.updateKBState(ctx, orgID, agentID, kb, WinUpdateStateApproved, ""); err != nil {
				return "", err
			}
			from = WinUpdateStateApproved
		}
		if _, err := WinUpdateNextState(from, WinUpdateEventInstall); err != nil {
			return "", err
		}
		if err := s.updateKBState(ctx, orgID, agentID, kb, WinUpdateStateInstalling, ""); err != nil {
			return "", err
		}
	}

	resultVal := ""
	if !success {
		resultVal = errMsg
	}
	if err := s.updateKBState(ctx, orgID, agentID, kb, desired, resultVal); err != nil {
		return "", err
	}
	return desired, nil
}

// IngestKBRebootDone transitions each listed KB from reboot_required to
// installed. Already-installed KBs are left untouched (idempotent).
func (s *pgPatchStore) IngestKBRebootDone(ctx context.Context, orgID, agentID string, kbs []string) error {
	if s.pool == nil {
		return errors.New("patches: nil pool")
	}
	if orgID == "" || agentID == "" {
		return errors.New("patches: org_id and agent_id are required")
	}
	if len(kbs) == 0 {
		return nil
	}

	rows, err := s.getKBStatesByKBs(ctx, orgID, agentID, kbs)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.State == WinUpdateStateRebootRequired {
			if err := s.updateKBState(ctx, orgID, agentID, r.KB, WinUpdateStateInstalled, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// TransitionKB applies a single WinUpdate event to the current state of a
// KB. It returns ErrInvalidTransition for illegal moves.
func (s *pgPatchStore) TransitionKB(ctx context.Context, orgID, agentID, kb, event string) (string, error) {
	if s.pool == nil {
		return "", errors.New("patches: nil pool")
	}
	if orgID == "" || agentID == "" || kb == "" {
		return "", errors.New("patches: org_id, agent_id, and kb are required")
	}
	cur, err := s.getKBState(ctx, orgID, agentID, kb)
	if err != nil {
		return "", err
	}
	next, err := WinUpdateNextState(cur.State, event)
	if err != nil {
		return "", err
	}
	if err := s.updateKBState(ctx, orgID, agentID, kb, next, ""); err != nil {
		return "", err
	}
	return next, nil
}

// GetKBStatesByAgent returns WinUpdate KB states scoped to the caller's
// org. When agentID is empty the query is org-wide (no agent predicate);
// otherwise it is scoped to a single agent. Results are capped at 200
// rows. The state filter is applied in-memory by the API handler.
func (s *pgPatchStore) GetKBStatesByAgent(ctx context.Context, orgID, agentID string) ([]models.WinUpdateKBState, error) {
	if s.pool == nil {
		return nil, errors.New("patches: nil pool")
	}
	if orgID == "" {
		return nil, errors.New("patches: org_id is required")
	}

	var q string
	var args []any
	if agentID == "" {
		q = `
			SELECT id, org_id, agent_id, kb, state, COALESCE(result,''), created_at, updated_at
			FROM winupdate_kb_state
			WHERE org_id = $1
			ORDER BY agent_id ASC, kb ASC
			LIMIT 200
		`
		args = []any{orgID}
	} else {
		q = `
			SELECT id, org_id, agent_id, kb, state, COALESCE(result,''), created_at, updated_at
			FROM winupdate_kb_state
			WHERE org_id = $1 AND agent_id = $2
		 ORDER BY kb ASC
			LIMIT 200
		`
		args = []any{orgID, agentID}
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("patches: list kb states: %w", err)
	}
	defer rows.Close()
	return scanKBStates(rows)
}

// getKBState loads a single KB state scoped to org + agent.
func (s *pgPatchStore) getKBState(ctx context.Context, orgID, agentID, kb string) (*models.WinUpdateKBState, error) {
	const q = `
		SELECT id, org_id, agent_id, kb, state, COALESCE(result,''), created_at, updated_at
		FROM winupdate_kb_state
		WHERE org_id = $1 AND agent_id = $2 AND kb = $3
		LIMIT 1
	`
	row := s.pool.QueryRow(ctx, q, orgID, agentID, kb)
	var r models.WinUpdateKBState
	if err := row.Scan(&r.ID, &r.OrgID, &r.AgentID, &r.KB, &r.State, &r.Result, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("patches: kb state not found: %w", ErrWinUpdateKBNotFound)
		}
		return nil, fmt.Errorf("patches: get kb state: %w", err)
	}
	return &r, nil
}

// getKBStatesByKBs loads the listed KBs for an agent scoped to org.
func (s *pgPatchStore) getKBStatesByKBs(ctx context.Context, orgID, agentID string, kbs []string) ([]models.WinUpdateKBState, error) {
	const q = `
		SELECT id, org_id, agent_id, kb, state, COALESCE(result,''), created_at, updated_at
		FROM winupdate_kb_state
		WHERE org_id = $1 AND agent_id = $2 AND kb = ANY($3)
	`
	rows, err := s.pool.Query(ctx, q, orgID, agentID, kbs)
	if err != nil {
		return nil, fmt.Errorf("patches: list kb states by kbs: %w", err)
	}
	defer rows.Close()
	return scanKBStates(rows)
}

// updateKBState sets the state (and optionally result) for a single KB
// scoped to org + agent. It returns an error if no row matches.
func (s *pgPatchStore) updateKBState(ctx context.Context, orgID, agentID, kb, state, resultVal string) error {
	const q = `
		UPDATE winupdate_kb_state
		SET state = $4, result = $5, updated_at = $6
		WHERE org_id = $1 AND agent_id = $2 AND kb = $3
	`
	tag, err := s.pool.Exec(ctx, q, orgID, agentID, kb, state, resultVal, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("patches: update kb state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("patches: kb state not found: %w", ErrWinUpdateKBNotFound)
	}
	return nil
}

// scanKBStates iterates a result set of KB-state rows.
func scanKBStates(rows pgx.Rows) ([]models.WinUpdateKBState, error) {
	out := make([]models.WinUpdateKBState, 0, 8)
	for rows.Next() {
		var r models.WinUpdateKBState
		if err := rows.Scan(&r.ID, &r.OrgID, &r.AgentID, &r.KB, &r.State, &r.Result, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("patches: scan kb state: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ErrWinUpdateKBNotFound is returned when a KB state row does not exist.
var ErrWinUpdateKBNotFound = errors.New("winupdate kb state not found")
