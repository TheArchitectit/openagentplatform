// Package billing — billing.go provides the BillingService: the
// application-facing façade that orchestrates Stripe calls and maps
// commercial tiers to Stripe price IDs.
//
// Tier definitions intentionally mirror internal/license/license.go so
// that the license engine (offline Ed25519-signed keys) and the billing
// engine (online Stripe subscriptions) agree on limits.
package billing

import (
	"log/slog"
	"sync"
)

// BillingService is the application-facing billing façade.
type BillingService struct {
	client *StripeClient
	log    *slog.Logger

	mu         sync.RWMutex
	state      map[string]*OrgBillingState // keyed by org ID; cache over stateStore
	stateStore StateStore                  // may be nil: memory-only mode
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
