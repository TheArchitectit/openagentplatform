package patches

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/pashagolub/pgxmock/v4"
)

// newTestKBStore builds a pgPatchStore backed by a pgxmock pool.
func newTestKBStore(t *testing.T) (*pgPatchStore, pgxmock.PgxPoolIface, func()) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	s := &pgPatchStore{pool: pool}
	return s, pool, pool.Close
}

// TestIngestKBScan_AutoApproveCritical verifies that a critical-severity
// scan auto-approves (scanned -> approved) and that the upsert query
// includes org_id in the WHERE clause.
func TestIngestKBScan_AutoApproveCritical(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	// The upsert is idempotent on (agent_id, kb) and always inserts the
	// initial "scanned" state. The auto-approve happens in a follow-up
	// UPDATE, asserted below.
	pool.ExpectExec(`INSERT INTO winupdate_kb_state`).
		WithArgs(
			// id, org_id, agent_id, kb, state, result, created_at, updated_at
			pgxmock.AnyArg(), orgID, agentID, "KB5001234", "scanned", "", pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// After the upsert, the store reads back the newly inserted row.
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
			AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "scanned", "", time.Now(), time.Now()))
	// Auto-approve UPDATE: scanned -> approved.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "approved", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBScan(context.Background(), orgID, agentID, "KB5001234", "critical")
	if err != nil {
		t.Fatalf("IngestKBScan: %v", err)
	}
	if state != "approved" {
		t.Errorf("critical scan: got state %q, want approved", state)
	}
}

// TestIngestKBScan_NonCriticalPending verifies that a non-critical scan
// lands in pending_approval (scanned -> queue -> pending_approval).
func TestIngestKBScan_NonCriticalPending(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	pool.ExpectExec(`INSERT INTO winupdate_kb_state`).
		WithArgs(
			pgxmock.AnyArg(), orgID, agentID, "KB5001234", "scanned", "", pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// After the upsert, the store reads back the newly inserted row.
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
			AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "scanned", "", time.Now(), time.Now()))
	// Queue UPDATE: scanned -> pending_approval.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "pending_approval", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBScan(context.Background(), orgID, agentID, "KB5001234", "important")
	if err != nil {
		t.Fatalf("IngestKBScan: %v", err)
	}
	if state != "pending_approval" {
		t.Errorf("important scan: got state %q, want pending_approval", state)
	}
}

// TestIngestKBScan_Idempotent verifies that a second scan with the same
// inputs is a no-op (ON CONFLICT DO NOTHING) and returns the existing
// state without error.
func TestIngestKBScan_Idempotent(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	// First insert succeeds (always inserts "scanned").
	pool.ExpectExec(`INSERT INTO winupdate_kb_state`).
		WithArgs(
			pgxmock.AnyArg(), orgID, agentID, "KB5001234", "scanned", "", pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	// After the upsert, the store reads back the newly inserted row.
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
			AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "scanned", "", time.Now(), time.Now()))
	// Auto-approve UPDATE: scanned -> approved.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "approved", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Second insert hits the unique constraint: 0 rows affected, and the
	// existing "approved" row is read back. Because approved cannot be
	// re-approved (illegal transition), no follow-up UPDATE is issued.
	pool.ExpectExec(`INSERT INTO winupdate_kb_state`).
		WithArgs(
			pgxmock.AnyArg(), orgID, agentID, "KB5001234", "scanned", "", pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 0))
	// After a no-op upsert, the store reads back the current row.
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
			AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "approved", "", time.Now(), time.Now()))

	state1, err := s.IngestKBScan(context.Background(), orgID, agentID, "KB5001234", "critical")
	if err != nil {
		t.Fatalf("first IngestKBScan: %v", err)
	}
	state2, err := s.IngestKBScan(context.Background(), orgID, agentID, "KB5001234", "critical")
	if err != nil {
		t.Fatalf("second IngestKBScan: %v", err)
	}
	if state1 != state2 {
		t.Errorf("idempotency: state1=%q state2=%q", state1, state2)
	}
}

// TestIngestKBInstall_Success verifies a successful install transitions
// approved -> installing -> installed.
func TestIngestKBInstall_Success(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	// Pre-existing approved row.
	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "approved", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)
	// Update to installing.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installing", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Update to installed.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installed", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBInstall(context.Background(), orgID, agentID, "KB5001234", true, false, "")
	if err != nil {
		t.Fatalf("IngestKBInstall: %v", err)
	}
	if state != "installed" {
		t.Errorf("success install: got state %q, want installed", state)
	}
}

// TestIngestKBInstall_RebootRequired verifies success+rebootRequired lands
// in reboot_required.
func TestIngestKBInstall_RebootRequired(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "approved", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installing", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "reboot_required", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBInstall(context.Background(), orgID, agentID, "KB5001234", true, true, "")
	if err != nil {
		t.Fatalf("IngestKBInstall: %v", err)
	}
	if state != "reboot_required" {
		t.Errorf("reboot install: got state %q, want reboot_required", state)
	}
}

// TestIngestKBInstall_Failure verifies a failed install lands in failed
// and stores the error message in result.
func TestIngestKBInstall_Failure(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "approved", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)
	// Step into installing.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installing", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// Failure update stores the error message in result.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "failed", "0x80070005", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBInstall(context.Background(), orgID, agentID, "KB5001234", false, false, "0x80070005")
	if err != nil {
		t.Fatalf("IngestKBInstall: %v", err)
	}
	if state != "failed" {
		t.Errorf("failed install: got state %q, want failed", state)
	}
}

// TestIngestKBRebootDone verifies reboot_required -> installed and that
// already-installed is a no-op.
func TestIngestKBRebootDone(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	// One row in reboot_required, one already installed.
	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "reboot_required", "", time.Now(), time.Now()).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001235", "installed", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = ANY\(\$3\)`).
		WithArgs(orgID, agentID, []string{"KB5001234", "KB5001235"}).
		WillReturnRows(rows)
	// Only the reboot_required row is updated.
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installed", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := s.IngestKBRebootDone(context.Background(), orgID, agentID, []string{"KB5001234", "KB5001235"}); err != nil {
		t.Fatalf("IngestKBRebootDone: %v", err)
	}
}

// TestTransitionKB_Invalid verifies that an illegal transition returns
// ErrInvalidTransition.
func TestTransitionKB_Invalid(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "installed", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)

	_, err := s.TransitionKB(context.Background(), orgID, agentID, "KB5001234", "approve")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("TransitionKB(installed,approve): expected ErrInvalidTransition, got %v", err)
	}
}

// TestGetKBStatesByAgent_OrgScoped verifies the list query filters by
// org_id and returns the expected models.
func TestGetKBStatesByAgent_OrgScoped(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	now := time.Now()
	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "approved", "", now, now)
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2`).
		WithArgs(orgID, agentID).
		WillReturnRows(rows)

	got, err := s.GetKBStatesByAgent(context.Background(), orgID, agentID)
	if err != nil {
		t.Fatalf("GetKBStatesByAgent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetKBStatesByAgent: got %d rows, want 1", len(got))
	}
	if got[0].KB != "KB5001234" || got[0].State != "approved" || got[0].OrgID != orgID {
		t.Errorf("GetKBStatesByAgent: unexpected row %+v", got[0])
	}
}

// TestGetKBStatesByAgent_OrgWide verifies that an empty agent_id returns
// all KB states for the org (no agent predicate in the WHERE clause).
func TestGetKBStatesByAgent_OrgWide(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	now := time.Now()

	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, "agent-1", "KB5001234", "approved", "", now, now).
		AddRow(uuid.NewString(), orgID, "agent-2", "KB5001235", "installed", "", now, now)
	// Org-wide query: WHERE org_id = $1 only (no agent predicate).
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1`).
		WithArgs(orgID).
		WillReturnRows(rows)

	got, err := s.GetKBStatesByAgent(context.Background(), orgID, "")
	if err != nil {
		t.Fatalf("GetKBStatesByAgent(org-wide): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("GetKBStatesByAgent(org-wide): got %d rows, want 2", len(got))
	}
	for _, r := range got {
		if r.OrgID != orgID {
			t.Errorf("cross-org leak: %+v", r)
		}
	}
}

// TestIngestKBInstall_IdempotentRedelivery verifies that a second
// identical install delivery to an already-terminal row returns the same
// state and performs no UPDATE (safe under NATS redelivery).
func TestIngestKBInstall_IdempotentRedelivery(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	// Row already at "installed".
	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "installed", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)
	// No ExpectExec: the idempotent path does not write.

	state, err := s.IngestKBInstall(context.Background(), orgID, agentID, "KB5001234", true, false, "")
	if err != nil {
		t.Fatalf("IngestKBInstall (redelivery): %v", err)
	}
	if state != "installed" {
		t.Errorf("redelivery: got state %q, want installed", state)
	}
}

// TestIngestKBInstall_FirstDeliveryFromScanned verifies a first-ever
// install delivery to a row still in "scanned" walks the legal path
// (scanned -> approved -> installing -> installed) and reaches installed.
func TestIngestKBInstall_FirstDeliveryFromScanned(t *testing.T) {
	s, pool, closeFn := newTestKBStore(t)
	defer closeFn()

	orgID := uuid.NewString()
	agentID := uuid.NewString()

	rows := pgxmock.NewRows([]string{"id", "org_id", "agent_id", "kb", "state", "result", "created_at", "updated_at"}).
		AddRow(uuid.NewString(), orgID, agentID, "KB5001234", "scanned", "", time.Now(), time.Now())
	pool.ExpectQuery(`SELECT .+ FROM winupdate_kb_state WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234").
		WillReturnRows(rows)
	// scanned -> approved
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "approved", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// approved -> installing
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installing", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	// installing -> installed
	pool.ExpectExec(`UPDATE winupdate_kb_state SET state = \$4, result = \$5, updated_at = \$6 WHERE org_id = \$1 AND agent_id = \$2 AND kb = \$3`).
		WithArgs(orgID, agentID, "KB5001234", "installed", "", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	state, err := s.IngestKBInstall(context.Background(), orgID, agentID, "KB5001234", true, false, "")
	if err != nil {
		t.Fatalf("IngestKBInstall (first from scanned): %v", err)
	}
	if state != "installed" {
		t.Errorf("first from scanned: got state %q, want installed", state)
	}
}

// silence unused import guard
var _ = models.WinUpdateKBState{}
