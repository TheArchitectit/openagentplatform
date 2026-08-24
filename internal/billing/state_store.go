package billing

// state_store.go persists OrgBillingState so billing survives restarts.
// The in-memory map on BillingService remains the read cache; every
// mutation is written through to Postgres when a store is wired.

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StateStore persists org billing state. Implemented by PGStateStore.
type StateStore interface {
	UpsertState(ctx context.Context, st *OrgBillingState) error
	GetState(ctx context.Context, orgID string) (*OrgBillingState, error)
	ListStates(ctx context.Context) ([]*OrgBillingState, error)
}

// PGStateStore is the Postgres implementation of StateStore backed by the
// org_billing_state table (created by EnsureSchema).
type PGStateStore struct {
	pool *pgxpool.Pool
}

// NewPGStateStore constructs a PGStateStore.
func NewPGStateStore(pool *pgxpool.Pool) *PGStateStore {
	return &PGStateStore{pool: pool}
}

// EnsureSchema creates the org_billing_state table if missing. Idempotent.
func (s *PGStateStore) EnsureSchema(ctx context.Context) error {
	const q = `
		CREATE TABLE IF NOT EXISTS org_billing_state (
			org_id             TEXT PRIMARY KEY,
			stripe_customer_id TEXT NOT NULL DEFAULT '',
			subscription_id    TEXT NOT NULL DEFAULT '',
			price_id           TEXT NOT NULL DEFAULT '',
			tier               TEXT NOT NULL DEFAULT 'community',
			status             TEXT NOT NULL DEFAULT 'active',
			current_period_end TIMESTAMPTZ,
			updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
		)`
	_, err := s.pool.Exec(ctx, q)
	return err
}

// UpsertState writes the full state row for its org.
func (s *PGStateStore) UpsertState(ctx context.Context, st *OrgBillingState) error {
	const q = `
		INSERT INTO org_billing_state (
			org_id, stripe_customer_id, subscription_id, price_id,
			tier, status, current_period_end, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (org_id) DO UPDATE SET
			stripe_customer_id = EXCLUDED.stripe_customer_id,
			subscription_id    = EXCLUDED.subscription_id,
			price_id           = EXCLUDED.price_id,
			tier               = EXCLUDED.tier,
			status             = EXCLUDED.status,
			current_period_end = EXCLUDED.current_period_end,
			updated_at         = now()`
	var periodEnd any
	if !st.CurrentPeriodEnd.IsZero() {
		periodEnd = st.CurrentPeriodEnd
	}
	_, err := s.pool.Exec(ctx, q,
		st.OrgID, st.StripeCustomer, st.SubscriptionID, st.PriceID,
		st.Tier, st.Status, periodEnd)
	if err != nil {
		return fmt.Errorf("billing: upsert state: %w", err)
	}
	return nil
}

// GetState reads one org's state. Returns pgx.ErrNoRows when absent.
func (s *PGStateStore) GetState(ctx context.Context, orgID string) (*OrgBillingState, error) {
	const q = `
		SELECT org_id, stripe_customer_id, subscription_id, price_id,
		       tier, status, current_period_end
		FROM org_billing_state WHERE org_id = $1`
	st := &OrgBillingState{}
	var periodEnd *time.Time
	err := s.pool.QueryRow(ctx, q, orgID).Scan(
		&st.OrgID, &st.StripeCustomer, &st.SubscriptionID, &st.PriceID,
		&st.Tier, &st.Status, &periodEnd)
	if err != nil {
		return nil, err
	}
	if periodEnd != nil {
		st.CurrentPeriodEnd = *periodEnd
	}
	return st, nil
}

// ListStates reads all persisted org states — used to warm the cache at
// startup.
func (s *PGStateStore) ListStates(ctx context.Context) ([]*OrgBillingState, error) {
	const q = `
		SELECT org_id, stripe_customer_id, subscription_id, price_id,
		       tier, status, current_period_end
		FROM org_billing_state`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("billing: list states: %w", err)
	}
	defer rows.Close()
	var out []*OrgBillingState
	for rows.Next() {
		st := &OrgBillingState{}
		var periodEnd *time.Time
		if err := rows.Scan(
			&st.OrgID, &st.StripeCustomer, &st.SubscriptionID, &st.PriceID,
			&st.Tier, &st.Status, &periodEnd); err != nil {
			return nil, fmt.Errorf("billing: scan state: %w", err)
		}
		if periodEnd != nil {
			st.CurrentPeriodEnd = periodEnd.UTC()
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetStateStore wires the durable store and warms the in-memory cache from
// it. Call once after NewBillingService. With no store, behaviour is
// unchanged (in-memory only).
func (s *BillingService) SetStateStore(store StateStore) error {
	s.stateStore = store
	if store == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	states, err := store.ListStates(ctx)
	if err != nil {
		return fmt.Errorf("billing: warm cache: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range states {
		s.state[st.OrgID] = st
	}
	return nil
}

// persistState best-effort writes through to the durable store after an
// in-memory mutation.
func (s *BillingService) persistState(st *OrgBillingState) {
	if s.stateStore == nil || st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.stateStore.UpsertState(ctx, st); err != nil {
		s.log.Warn("billing: persist state failed", "org_id", st.OrgID, "error", err)
	}
}
