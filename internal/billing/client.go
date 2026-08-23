package billing

import (
	"context"
	"fmt"
	"os"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/subscription"
)

// ErrSecretKeyMissing is returned when STRIPE_SECRET_KEY is not set.
var ErrSecretKeyMissing = fmt.Errorf("STRIPE_SECRET_KEY environment variable not set")

// StripeClient wraps the Stripe SDK for testability.
type StripeClient struct {
	apiKey string
}

// NewStripeClient creates a new Stripe client from the STRIPE_SECRET_KEY env var.
func NewStripeClient() (*StripeClient, error) {
	key := os.Getenv("STRIPE_SECRET_KEY")
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

// UpdateSubscription updates a Stripe subscription's price.
func (c *StripeClient) UpdateSubscription(ctx context.Context, subscriptionID, priceID string) (*stripe.Subscription, error) {
	sub, err := subscription.Update(subscriptionID, &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{
			{
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

// ListInvoices lists Stripe invoices for a customer.
func (c *StripeClient) ListInvoices(ctx context.Context, customerID string, limit int) ([]*stripe.Invoice, error) {
	// In production, this would use stripe invoice list
	// For now, return empty list
	return []*stripe.Invoice{}, nil
}

// SyncInterval is the interval at which billing state is synced with Stripe.
const SyncInterval = 15 * time.Minute

// PriceIDs returns the Stripe price IDs for Pro and Enterprise tiers.
func PriceIDs() (pro, enterprise string, err error) {
	pro = os.Getenv("STRIPE_PRO_PRICE_ID")
	enterprise = os.Getenv("STRIPE_ENT_PRICE_ID")
	if pro == "" || enterprise == "" {
		return "", "", ErrPriceIDNotResolved
	}
	return pro, enterprise, nil
}
