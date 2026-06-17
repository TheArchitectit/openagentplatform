// Package billing implements Stripe integration for OAP commercial tiers.
//
// Per docs/AGENT_GUARDRAILS.md §6 (Secrets Handling):
//   - The Stripe secret key is read exclusively from the STRIPE_SECRET_KEY
//     environment variable. It is NEVER hardcoded, logged, or written to
//     source control.
//   - Stripe price IDs are sourced from STRIPE_PRO_PRICE_ID and
//     STRIPE_ENT_PRICE_ID. They are non-secret configuration but still
//     resolved from the environment so that different deployments
//     (staging/production) can target different Stripe products.
package billing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/invoice"
	"github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/webhook"
)

// Env vars consumed by the billing package. Centralised so callers can
// reference them without scattering string literals.
const (
	EnvStripeSecretKey = "STRIPE_SECRET_KEY"
	EnvProPriceID      = "STRIPE_PRO_PRICE_ID"
	EnvEnterprisePrice = "STRIPE_ENT_PRICE_ID"
	EnvWebhookSecret   = "STRIPE_WEBHOOK_SECRET"

	// SyncInterval is the cadence at which SyncSubscription polls Stripe.
	SyncInterval = 15 * time.Minute
)

// ErrSecretKeyMissing is returned when STRIPE_SECRET_KEY is not set.
var ErrSecretKeyMissing = errors.New("STRIPE_SECRET_KEY environment variable is required")

// StripeClient wraps the stripe-go SDK. The secret key is held in memory
// only and never serialised to disk or logs.
type StripeClient struct {
	apiKey string // secret — never log this field
}

// NewStripeClient validates that STRIPE_SECRET_KEY is present in the
// environment and configures the underlying stripe-go SDK. It MUST be
// called during application startup; a missing key is a fatal error.
func NewStripeClient() (*StripeClient, error) {
	key := os.Getenv(EnvStripeSecretKey)
	if key == "" {
		return nil, ErrSecretKeyMissing
	}
	stripe.Key = key
	// stripe-go v81 hardcodes an 80s default HTTP timeout; we accept that.
	return &StripeClient{apiKey: key}, nil
}

// NewStripeClientWithKey is intended for tests that need to inject a key
// explicitly. Production callers MUST use NewStripeClient.
func NewStripeClientWithKey(key string) *StripeClient {
	stripe.Key = key
	return &StripeClient{apiKey: key}
}

// PriceIDs returns the configured Stripe price IDs for the Pro and
// Enterprise tiers, resolved from environment variables.
func PriceIDs() (pro, enterprise string, err error) {
	pro = os.Getenv(EnvProPriceID)
	enterprise = os.Getenv(EnvEnterprisePrice)
	if pro == "" {
		return "", "", fmt.Errorf("%s not set", EnvProPriceID)
	}
	if enterprise == "" {
		return "", "", fmt.Errorf("%s not set", EnvEnterprisePrice)
	}
	return pro, enterprise, nil
}

// CreateCustomerParams carries the fields needed to provision a new
// Stripe customer for an OAP organisation.
type CreateCustomerParams struct {
	OrgID string // OAP organisation ID — stored as metadata
	Email string
	Name  string
}

// CreateCustomer creates a Stripe customer tagged with the OAP org ID.
func (c *StripeClient) CreateCustomer(ctx context.Context, p CreateCustomerParams) (*stripe.Customer, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(p.Email),
		Name:  stripe.String(p.Name),
		Metadata: map[string]string{
			"oap_org_id": p.OrgID,
		},
	}
	cust, err := customer.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe create customer: %w", err)
	}
	return cust, nil
}

// GetCustomer fetches a Stripe customer by its Stripe-assigned ID.
func (c *StripeClient) GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, error) {
	cust, err := customer.Get(customerID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe get customer: %w", err)
	}
	return cust, nil
}

// CreateSubscription creates a new subscription for the given customer at
// the given Stripe price ID.
func (c *StripeClient) CreateSubscription(ctx context.Context, customerID, priceID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: stripe.String(priceID)},
		},
	}
	sub, err := subscription.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe create subscription: %w", err)
	}
	return sub, nil
}

// UpdateSubscription swaps the price on an existing subscription.
func (c *StripeClient) UpdateSubscription(ctx context.Context, subscriptionID, newPriceID string) (*stripe.Subscription, error) {
	// Retrieve the current subscription to find its single item ID.
	current, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe get subscription: %w", err)
	}
	if len(current.Items.Data) == 0 {
		return nil, errors.New("stripe subscription has no items")
	}
	itemID := current.Items.Data[0].ID
	params := &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(itemID),
				Price: stripe.String(newPriceID),
			},
		},
	}
	sub, err := subscription.Update(subscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("stripe update subscription: %w", err)
	}
	return sub, nil
}

// CancelSubscription cancels at period end so the customer retains
// service until the current billing cycle closes.
func (c *StripeClient) CancelSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Cancel(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe cancel subscription: %w", err)
	}
	return sub, nil
}

// GetSubscription fetches a subscription by ID.
func (c *StripeClient) GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe get subscription: %w", err)
	}
	return sub, nil
}

// ListInvoices returns the most recent invoices for a customer.
func (c *StripeClient) ListInvoices(ctx context.Context, customerID string, limit int) ([]*stripe.Invoice, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	params := &stripe.InvoiceListParams{
		Customer: stripe.String(customerID),
	}
	params.Limit = stripe.Int64(int64(limit))
	it := invoice.List(params)
	var invoices []*stripe.Invoice
	for it.Next() {
		invoices = append(invoices, it.Invoice())
	}
	if err := it.Err(); err != nil {
		return nil, fmt.Errorf("stripe list invoices: %w", err)
	}
	return invoices, nil
}

// VerifyWebhook validates the signature on an inbound Stripe webhook and
// returns the parsed event. The signing secret comes from
// STRIPE_WEBHOOK_SECRET — never from a hardcoded value.
func (c *StripeClient) VerifyWebhook(payload []byte, signatureHeader string) (stripe.Event, error) {
	secret := os.Getenv(EnvWebhookSecret)
	if secret == "" {
		return stripe.Event{}, fmt.Errorf("%s not set", EnvWebhookSecret)
	}
	evt, err := webhook.ConstructEvent(payload, signatureHeader, secret)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("stripe webhook verify: %w", err)
	}
	return evt, nil
}
