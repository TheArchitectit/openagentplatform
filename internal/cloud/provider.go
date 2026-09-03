package cloud

import "context"

type CloudAccountInfo struct {
	AccountID   string
	DisplayName string
	Region      string
}

type CostInfo struct {
	BillingPeriod string
	TotalCostUSD  float64
	ServiceCosts  map[string]float64
}

type ProviderClient interface {
	Name() string
	ListAccounts(ctx context.Context, credRef string) ([]CloudAccountInfo, error)
	ListResources(ctx context.Context, credRef, accountID, region string) ([]CloudResource, error)
	GetCost(ctx context.Context, credRef, accountID, period string) (CostInfo, error)
}
