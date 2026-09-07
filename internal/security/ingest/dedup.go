// dedup.go — (provider, provider_event_id) dedup helper for the EDR
// security_events table. The unique constraint on
// (provider, provider_event_id) is the source of truth; this helper
// is a best-effort pre-check that avoids a round-trip when the event
// is already present. On a real collision (race between concurrent
// workers), the store's Insert must handle the unique-violation error.
package ingest

import (
	"context"
	"errors"
)

// EventStore is the minimal persistence seam for dedup-checking a
// security event before insert.
type EventStore interface {
	// GetByProviderEventID returns the event ID if (provider,
	// provider_event_id) already exists, or "" + nil if it does not.
	GetByProviderEventID(ctx context.Context, provider, providerEventID string) (string, error)
	// Insert persists the event. Must be tolerant of unique-constraint
	// violations (treat as a no-op duplicate).
	Insert(ctx context.Context, ev interface{}) error
}

// ErrDuplicate is returned when an event with the same (provider,
// provider_event_id) already exists.
var ErrDuplicate = errors.New("duplicate event")

// CheckAndInsert queries the store by (provider, provider_event_id) and
// returns ErrDuplicate if the event is already present. Otherwise the
// event is inserted. On real collisions at insert time (race between
// concurrent workers), the store's Insert handles the unique-violation.
func CheckAndInsert(ctx context.Context, store EventStore, provider, providerEventID string) error {
	if id, err := store.GetByProviderEventID(ctx, provider, providerEventID); err != nil {
		return err
	} else if id != "" {
		return ErrDuplicate
	}
	return nil
}
