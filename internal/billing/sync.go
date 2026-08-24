package billing

import (
	"context"
	"fmt"
	"time"
)

// SyncSubscription polls Stripe for the latest subscription state and
// refreshes the cached entry. Intended to be invoked on a ticker.
func (s *BillingService) SyncSubscription(ctx context.Context, orgID string) error {
	s.mu.RLock()
	st, ok := s.state[orgID]
	s.mu.RUnlock()
	if !ok || st.SubscriptionID == "" {
		return nil // nothing to sync
	}
	sub, err := s.client.GetSubscription(ctx, st.SubscriptionID)
	if err != nil {
		return fmt.Errorf("sync subscription: %w", err)
	}
	st.Status = string(sub.Status)
	st.CurrentPeriodEnd = time.Unix(sub.CurrentPeriodEnd, 0)
	if len(sub.Items.Data) > 0 {
		st.PriceID = sub.Items.Data[0].Price.ID
	}
	s.mu.Lock()
	s.state[orgID] = st
	s.mu.Unlock()
	s.persistState(st)
	return nil
}

// StartSyncLoop launches a goroutine that calls SyncSubscription every
// SyncInterval (15 minutes) for every known org. Cancel ctx to stop.
func (s *BillingService) StartSyncLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(SyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.RLock()
				orgs := make([]string, 0, len(s.state))
				for id := range s.state {
					orgs = append(orgs, id)
				}
				s.mu.RUnlock()
				for _, id := range orgs {
					if err := s.SyncSubscription(ctx, id); err != nil {
						s.log.Warn("billing sync failed",
							"org_id", id,
							"error", err.Error(),
						)
					}
				}
			}
		}
	}()
}
