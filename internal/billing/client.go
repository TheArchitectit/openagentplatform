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

// Env vars consumed by the Stripe client. Centralised so callers can
// reference them without scattering string literals.
const (
	EnvStripeSecretKey = "STRIPE_SECRET_KEY"
	EnvProPriceID      = "STRIPE_PRO_PRICE_ID"
	EnvEnterprisePrice = "STRIPE_ENT_PRICE_ID"
	EnvWebhookSecret   = "STRIPE_WEBHOOK_SECRET"
)

// ErrSecretKeyMissing is returned when STRIPE_SECRET_KEY is not set.
var ErrSecretKeyMissing = fmt.Errorf("STRIPE_SECRET_KEY environment variable not set")

// StripeClient wraps the Stripe SDK for testability.
type StripeClient struct {
	apiKey string
}

// NewStripeClient creates a new Stripe client from the STRIPE_SECRET_KEY env var.
func NewStripeClient() (*StripeClient, error) {
	key := os.Getenv(EnvStripeSecretKey)
	if key == "" {
		return nil, ErrSecretKeyMissing
	}
	stripe.Key = key
	return &StripeClient{apiKey: key}, nil
}

// NewStripeClientWithKey creates a Stripe client with an explicit API key.
func NewStripeClientWithKey(apiKey string) *StripeClient {
	stripe.Key = apiKey
	return &StripeClient{apiKey: apiKey}
}

// CreateCustomerParams are parameters for creating a Stripe customer.
type CreateCustomerParams struct {
	Email string
	Name  string
	OrgID string
}

// CreateCustomer creates a Stripe customer.
func (c *StripeClient) CreateCustomer(ctx context.Context, params CreateCustomerParams) (*stripe.Customer, error) {
	cust, err := customer.New(&stripe.CustomerParams{
		Email: stripe.String(params.Email),
		Name:  stripe.String(params.Name),
		Params: stripe.Params{
			Context: ctx,
			Metadata: map[string]string{
				"oap_org_id": params.OrgID,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Stripe customer: %w", err)
	}
	return cust, nil
}

// GetCustomer retrieves a Stripe customer by ID.
func (c *StripeClient) GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, error) {
	cust, err := customer.Get(customerID, &stripe.CustomerParams{
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Stripe customer: %w", err)
	}
	return cust, nil
}

// CreateSubscription creates a Stripe subscription.
func (c *StripeClient) CreateSubscription(ctx context.Context, customerID, priceID string) (*stripe.Subscription, error) {
	subParams := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Price: stripe.String(priceID),
			},
		},
		Params: stripe.Params{
			Context: ctx,
		},
	}

	sub, err := subscription.New(subParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create Stripe subscription: %w", err)
	}
	return sub, nil
}

// UpdateSubscription swaps the price on an existing subscription. The
// current subscription is retrieved first so the update targets the
// existing item ID rather than appending a second item.
func (c *StripeClient) UpdateSubscription(ctx context.Context, subscriptionID, priceID string) (*stripe.Subscription, error) {
	current, err := c.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	if len(current.Items.Data) == 0 {
		return nil, errors.New("stripe subscription has no items")
	}
	sub, err := subscription.Update(subscriptionID, &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
				ID:    stripe.String(current.Items.Data[0].ID),
				Price: stripe.String(priceID),
			},
		},
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update Stripe subscription: %w", err)
	}
	return sub, nil
}

// CancelSubscription cancels a Stripe subscription at period end.
func (c *StripeClient) CancelSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Update(subscriptionID, &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(true),
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to cancel Stripe subscription: %w", err)
	}
	return sub, nil
}

// GetSubscription retrieves a Stripe subscription by ID.
func (c *StripeClient) GetSubscription(ctx context.Context, subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Get(subscriptionID, &stripe.SubscriptionParams{
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Stripe subscription: %w", err)
	}
	return sub, nil
}

// ListInvoices returns the most recent invoices for a customer. The
// limit is clamped to the 1..100 range with a default of 20.
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
		return nil, fmt.Errorf("failed to list Stripe invoices: %w", err)
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

// SyncInterval is the interval at which billing state is synced with Stripe.
const SyncInterval = 15 * time.Minute

// PriceIDs returns the Stripe price IDs for Pro and Enterprise tiers.
func PriceIDs() (pro, enterprise string, err error) {
	pro = os.Getenv(EnvProPriceID)
	enterprise = os.Getenv(EnvEnterprisePrice)
	if pro == "" || enterprise == "" {
		return "", "", ErrPriceIDNotResolved
	}
	return pro, enterprise, nil
}
