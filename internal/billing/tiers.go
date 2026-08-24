package billing

import (
	"errors"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/license"
)

// Tier quota limits. Aligned with internal/license/license.go so that
// licensing and billing report the same numbers to operators.
type TierLimits struct {
	Tier         license.Tier
	MaxAgents    int // 0 means unlimited
	MaxUsers     int // 0 means unlimited
	MaxSites     int // 0 means unlimited
	MonthlyPrice int // USD cents; 0 for free tier
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
		MaxAgents:    0,     // unlimited
		MaxUsers:     0,     // unlimited
		MaxSites:     0,     // unlimited
		MonthlyPrice: 49900, // $499.00
	},
}

// OrgBillingState tracks an organisation's billing status in memory.
// In production this would be persisted to the database.
type OrgBillingState struct {
	OrgID            string    `json:"org_id"`
	StripeCustomer   string    `json:"stripe_customer_id,omitempty"`
	SubscriptionID   string    `json:"subscription_id,omitempty"`
	PriceID          string    `json:"price_id,omitempty"`
	Tier             string    `json:"tier"`
	Status           string    `json:"status"` // active, past_due, canceled, ...
	CurrentPeriodEnd time.Time `json:"current_period_end,omitempty"`
}

// Sentinel errors for the billing service.
var (
	ErrUnknownTier        = errors.New("unknown billing tier")
	ErrNoCustomer         = errors.New("no Stripe customer for organisation")
	ErrPriceIDNotResolved = errors.New("Stripe price IDs not configured (STRIPE_PRO_PRICE_ID, STRIPE_ENT_PRICE_ID)")
)
