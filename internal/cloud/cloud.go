package cloud

import "context"

type AccountStore interface {
	Create(ctx context.Context, a *CloudAccount) error
	Get(ctx context.Context, id string) (*CloudAccount, error)
	ListByOrg(ctx context.Context, orgID string) ([]*CloudAccount, error)
	Delete(ctx context.Context, id string) error
}

type ResourceStore interface {
	Upsert(ctx context.Context, r *CloudResource) error
	Get(ctx context.Context, id string) (*CloudResource, error)
	ListByOrg(ctx context.Context, orgID string, filter ResourceFilter) ([]*CloudResource, error)
	Archive(ctx context.Context, id string) error
}

type ResourceFilter struct {
	Provider  string
	AccountID string
	Type      string
	Archived  bool
}

type PolicyStore interface {
	Create(ctx context.Context, p *CloudPolicy) error
	Get(ctx context.Context, id string) (*CloudPolicy, error)
	ListByOrg(ctx context.Context, orgID string) ([]*CloudPolicy, error)
	Delete(ctx context.Context, id string) error
}

type CostStore interface {
	Insert(ctx context.Context, c *CostSnapshot) error
	GetLatest(ctx context.Context, orgID, provider, accountID string) (*CostSnapshot, error)
}
