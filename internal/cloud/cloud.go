package cloud

import (
	"context"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type AccountStore interface {
	Create(ctx context.Context, a *models.CloudAccount) error
	Get(ctx context.Context, id string) (*models.CloudAccount, error)
	ListByOrg(ctx context.Context, orgID string) ([]*models.CloudAccount, error)
	Delete(ctx context.Context, id string) error
}

type ResourceStore interface {
	Upsert(ctx context.Context, r *models.CloudResource) error
	Get(ctx context.Context, id string) (*models.CloudResource, error)
	ListByOrg(ctx context.Context, orgID string, filter ResourceFilter) ([]*models.CloudResource, error)
	Archive(ctx context.Context, id string) error
}

type ResourceFilter struct {
	Provider  string
	AccountID string
	Type      string
	Archived  bool
}

type PolicyStore interface {
	Create(ctx context.Context, p *models.CloudPolicy) error
	Get(ctx context.Context, id string) (*models.CloudPolicy, error)
	ListByOrg(ctx context.Context, orgID string) ([]*models.CloudPolicy, error)
	Delete(ctx context.Context, id string) error
}

type CostStore interface {
	Insert(ctx context.Context, c *models.CostSnapshot) error
	GetLatest(ctx context.Context, orgID, provider, accountID string) (*models.CostSnapshot, error)
}
