// Package billing — billing.go provides the BillingService: the
// application-facing façade that orchestrates Stripe calls and maps
// commercial tiers to Stripe price IDs.
//
// Tier definitions intentionally mirror internal/license/license.go so
// that the license engine (offline Ed25519-signed keys) and the billing
// engine (online Stripe subscriptions) agree on limits.
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/openagentplatform/openagentplatform/internal/license"
)

// Tier quota limits. Aligned with internal/license/license.go so that
// licensing and billing report the same numbers to operators.
type TierLimits struct {
	Tier         license.Tier
	MaxAgents    int  // 0 means unlimited
	MaxUsers     int  // 0 means unlimited
	MaxSites     int  // 0 means unlimited
	MonthlyPrice int  // USD cents; 0 for free tier
}

// TierCatalog is the canonical tier table. Keep in sync with the
// license package's IsCommunity / IsProfessional / IsEnterprise helpers.
var TierCatalog = map[license.Tier]TierLimits{
	license.TierCommunity: {
		Tier:         license.TierCommunity,
		MaxAgents:    10,
		MaxUsers:     2,
		MaxSites:     1,
		MonthlyPrice: 0,
	},
	license.TierProfessional: {
		Tier:         license.TierProfessional,
		MaxAgents:    100,
		MaxUsers:     10,
		MaxSites:     5,
		MonthlyPrice: 9900, // $99.00
	},
	license.TierEnterprise: {
		Tier:         license.TierEnterprise,
		MaxAgents:    0, // unlimited
		MaxUsers:     0, // unlimited
		MaxSites:     0, // unlimited
		MonthlyPrice: 49900, // $499.00
	},
}

// OrgBillingState tracks an organisation's billing status in memory.
// In production this would be persisted to the database.
type OrgBillingState struct {
	OrgID          string    `json:"org_id"`
	StripeCustomer string    `json:"stripe_customer_id,omitempty"`
	SubscriptionID string    `json:"subscription_id,omitempty"`
	PriceID        string    `json:"price_id,omitempty"`
	Tier           string    `json:"tier"`
	Status         string    `json:"status"` // active, past_due, canceled, ...
	CurrentPeriodEnd time.Time `json:"current_period_end,omitempty"`
}

// Sentinel errors for the billing service.
var (
	ErrUnknownTier        = errors.New("unknown billing tier")
	ErrNoCustomer         = errors.New("no Stripe customer for organisation")
	ErrPriceIDNotResolved = errors.New("Stripe price IDs not configured (STRIPE_PRO_PRICE_ID, STRIPE_ENT_PRICE_ID)")
)

// BillingService is the application-facing billing façade.
type BillingService struct {
	client *StripeClient
	log    *slog.Logger

	mu    sync.RWMutex
	state map[string]*OrgBillingState // keyed by org ID
}

// NewBillingService wires a BillingService to a StripeClient.
func NewBillingService(client *StripeClient, log *slog.Logger) *BillingService {
	if log == nil {
		log = slog.Default()
	}
	return &BillingService{
		client: client,
		log:    log,
		state:  make(map[string]*OrgBillingState),
	}
}

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
	return st, nil
}

// GetSubscription returns the cached state for the org.
func (s *BillingService) GetSubscription(ctx context.Context, orgID string) (*OrgBillingState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.state[orgID]
	if !ok {
		return nil, errors.New("organisation not found")
	}
	return st, nil
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

// priceIDForTier resolves a Stripe price ID from the environment. The
// Community tier is free and has no price ID.
func priceIDForTier(tier license.Tier) (string, error) {
	switch tier {
	case license.TierCommunity:
		return "", nil // free tier
	case license.TierProfessional:
		pro, enterprise, err := PriceIDs()
		if err != nil {
			return "", err
		}
		_ = enterprise
		return pro, nil
	case license.TierEnterprise:
		_, enterprise, err := PriceIDs()
		if err != nil {
			return "", err
		}
		return enterprise, nil
	default:
		return "", ErrUnknownTier
	}
}
