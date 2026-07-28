package api

import (
	"log/slog"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/billing"
)

// TestSetBillingWiring verifies the SetBilling accessor wires the Stripe
// services onto the Server so the billing endpoints stop returning 503. This
// guards the server.go wiring that previously left BillingService /
// MeteringService / StripeClient nil for every deployment, so every billing
// endpoint always returned "billing_unavailable" and pending usage was never
// flushed.
func TestSetBillingWiring(t *testing.T) {
	s := &Server{log: slog.Default()}

	// Before wiring: all three are nil -> endpoints 503.
	if s.BillingService != nil || s.MeteringService != nil || s.StripeClient != nil {
		t.Fatal("expected nil billing services before SetBilling")
	}

	stripe := billing.NewStripeClientWithKey("sk_test_dummy")
	bs := billing.NewBillingService(stripe, slog.Default())
	ms := billing.NewMeteringService(stripe, slog.Default())
	s.SetBilling(stripe, bs, ms)

	if s.StripeClient != stripe {
		t.Error("StripeClient not wired")
	}
	if s.BillingService != bs {
		t.Error("BillingService not wired")
	}
	if s.MeteringService != ms {
		t.Error("MeteringService not wired")
	}

	// A nil SetBilling call must keep them nil (billing disabled path).
	s.SetBilling(nil, nil, nil)
	if s.BillingService != nil || s.MeteringService != nil || s.StripeClient != nil {
		t.Error("expected nil services after SetBilling(nil,nil,nil)")
	}
}
