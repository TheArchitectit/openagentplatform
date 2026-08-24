package billing

import (
	"context"
	"errors"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/license"
	stripe "github.com/stripe/stripe-go/v81"
)

// CreateCustomer provisions a Stripe customer for the given org.
func (s *BillingService) CreateCustomer(ctx context.Context, orgID, email, name string) (*OrgBillingState, error) {
	cust, err := s.client.CreateCustomer(ctx, CreateCustomerParams{
		OrgID: orgID,
		Email: email,
		Name:  name,
	})
	if err != nil {
		return nil, err
	}
	st := &OrgBillingState{
		OrgID:          orgID,
		StripeCustomer: cust.ID,
		Tier:           string(license.TierCommunity),
		Status:         "active",
	}
	s.mu.Lock()
	s.state[orgID] = st
	s.mu.Unlock()
	s.persistState(st)
	return st, nil
}

// CreateSubscription starts a Stripe subscription for an existing
// customer at the price matching the requested tier.
func (s *BillingService) CreateSubscription(ctx context.Context, orgID string, tier license.Tier) (*OrgBillingState, error) {
	limits, ok := TierCatalog[tier]
	if !ok {
		return nil, ErrUnknownTier
	}
	priceID, err := priceIDForTier(tier)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	st, exists := s.state[orgID]
	s.mu.RUnlock()
	if !exists || st.StripeCustomer == "" {
		return nil, ErrNoCustomer
	}
	sub, err := s.client.CreateSubscription(ctx, st.StripeCustomer, priceID)
	if err != nil {
		return nil, err
	}
	st.SubscriptionID = sub.ID
	st.PriceID = priceID
	st.Tier = string(tier)
	st.Status = string(sub.Status)
	st.CurrentPeriodEnd = time.Unix(sub.CurrentPeriodEnd, 0)
	s.mu.Lock()
	s.state[orgID] = st
	s.mu.Unlock()
	s.persistState(st)
	s.log.Info("billing subscription created",
		"org_id", orgID,
		"tier", limits.Tier,
		"subscription_id", sub.ID,
	)
	return st, nil
}

// UpdateSubscription swaps the tier on an existing subscription.
func (s *BillingService) UpdateSubscription(ctx context.Context, orgID string, tier license.Tier) (*OrgBillingState, error) {
	if _, ok := TierCatalog[tier]; !ok {
		return nil, ErrUnknownTier
	}
	priceID, err := priceIDForTier(tier)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	st, exists := s.state[orgID]
	s.mu.RUnlock()
	if !exists || st.SubscriptionID == "" {
		return nil, errors.New("no active subscription for organisation")
	}
	sub, err := s.client.UpdateSubscription(ctx, st.SubscriptionID, priceID)
	if err != nil {
		return nil, err
	}
	st.PriceID = priceID
	st.Tier = string(tier)
	st.Status = string(sub.Status)
	st.CurrentPeriodEnd = time.Unix(sub.CurrentPeriodEnd, 0)
	s.mu.Lock()
	s.state[orgID] = st
	s.mu.Unlock()
	s.persistState(st)
	return st, nil
}

// CancelSubscription cancels at period end.
func (s *BillingService) CancelSubscription(ctx context.Context, orgID string) (*OrgBillingState, error) {
	s.mu.RLock()
	st, exists := s.state[orgID]
	s.mu.RUnlock()
	if !exists || st.SubscriptionID == "" {
		return nil, errors.New("no active subscription for organisation")
	}
	sub, err := s.client.CancelSubscription(ctx, st.SubscriptionID)
	if err != nil {
		return nil, err
	}
	st.Status = string(sub.Status)
	st.CurrentPeriodEnd = time.Unix(sub.CurrentPeriodEnd, 0)
	s.mu.Lock()
	s.state[orgID] = st
	s.mu.Unlock()
	s.persistState(st)
	return st, nil
}

// GetSubscription returns the cached state for the org, falling back to
// the durable store on a cache miss.
func (s *BillingService) GetSubscription(ctx context.Context, orgID string) (*OrgBillingState, error) {
	s.mu.RLock()
	st, ok := s.state[orgID]
	store := s.stateStore
	s.mu.RUnlock()
	if ok {
		return st, nil
	}
	if store != nil {
		fresh, err := store.GetState(ctx, orgID)
		if err == nil {
			s.mu.Lock()
			s.state[orgID] = fresh
			s.mu.Unlock()
			return fresh, nil
		}
	}
	return nil, errors.New("organisation not found")
}

// GetInvoices fetches the most recent Stripe invoices for the org's
// customer.
func (s *BillingService) GetInvoices(ctx context.Context, orgID string, limit int) ([]*stripe.Invoice, error) {
	s.mu.RLock()
	st, ok := s.state[orgID]
	s.mu.RUnlock()
	if !ok || st.StripeCustomer == "" {
		return nil, ErrNoCustomer
	}
	return s.client.ListInvoices(ctx, st.StripeCustomer, limit)
}
