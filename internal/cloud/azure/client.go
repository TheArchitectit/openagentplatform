package azure

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

type AzureClient struct {
	subscriptionID string
	resolver       *resolver.SecretResolver
	log            *slog.Logger
}

func NewAzureClient(subscriptionID string, r *resolver.SecretResolver) *AzureClient {
	return &AzureClient{subscriptionID: subscriptionID, resolver: r, log: slog.Default()}
}

func NewAzureClientWithLog(subscriptionID string, r *resolver.SecretResolver, log *slog.Logger) *AzureClient {
	if log == nil {
		log = slog.Default()
	}
	return &AzureClient{subscriptionID: subscriptionID, resolver: r, log: log}
}

func (c *AzureClient) Name() string { return "azure" }

// resolveCred extracts tenantID/clientID/clientSecret from the secret map.
func (c *AzureClient) resolveCred(ctx context.Context, credRef string) (*azidentity.ClientSecretCredential, error) {
	if c.resolver == nil {
		return nil, fmt.Errorf("azure: no secret resolver configured")
	}
	sv, err := c.resolver.Resolve(ctx, credRef, nil)
	if err != nil {
		return nil, fmt.Errorf("azure: resolve secret: %w", err)
	}
	tenantID, _ := sv.Data["tenant_id"].(string)
	clientID, _ := sv.Data["client_id"].(string)
	clientSecret, _ := sv.Data["client_secret"].(string)
	if tenantID == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("azure: secret missing tenant_id/client_id/client_secret")
	}
	return azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
}

func (c *AzureClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
	if _, err := c.resolveCred(ctx, credRef); err != nil {
		return nil, err
	}
	return []cloud.CloudAccountInfo{{AccountID: c.subscriptionID, DisplayName: c.subscriptionID}}, nil
}

func (c *AzureClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]models.CloudResource, error) {
	cred, err := c.resolveCred(ctx, credRef)
	if err != nil {
		return nil, err
	}
	resourcesClient, err := armresources.NewClient(c.subscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}
	pager := resourcesClient.NewListPager(nil)
	var resources []models.CloudResource
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Value {
			resources = append(resources, models.CloudResource{
				ResourceID:   *r.ID,
				ResourceType: *r.Type,
				AccountID:    accountID,
				Name:         *r.Name,
				Tags:         map[string]string{},
				Status:       "",
				Provider:     models.CloudProviderAzure,
			})
		}
	}
	return resources, nil
}

func (c *AzureClient) GetCost(ctx context.Context, credRef, accountID, period string) (cloud.CostInfo, error) {
	cred, err := c.resolveCred(ctx, credRef)
	if err != nil {
		return cloud.CostInfo{}, fmt.Errorf("azure: cost creds: %w", err)
	}
	client, err := armcostmanagement.NewQueryClient(cred, nil)
	if err != nil {
		return cloud.CostInfo{}, fmt.Errorf("azure: cost client: %w", err)
	}

	from, to, err := azurePeriodRange(period)
	if err != nil {
		return cloud.CostInfo{}, fmt.Errorf("azure: parse period %q: %w", period, err)
	}

	query := armcostmanagement.QueryDefinition{
		Type:      toPtr(armcostmanagement.ExportTypeUsage),
		Timeframe: toPtr(armcostmanagement.TimeframeTypeCustom),
		TimePeriod: &armcostmanagement.QueryTimePeriod{
			From: from,
			To:   to,
		},
		Dataset: &armcostmanagement.QueryDataset{
			Granularity: toPtrGran(),
			Aggregation: map[string]*armcostmanagement.QueryAggregation{
				"totalCost": {Name: toPtr("PreTaxCost"), Function: toPtr(armcostmanagement.FunctionTypeSum)},
			},
			Grouping: []*armcostmanagement.QueryGrouping{
				{Type: toPtr(armcostmanagement.QueryColumnTypeDimension), Name: toPtr("ServiceName")},
			},
		},
	}

	resp, err := client.Usage(ctx, "/subscriptions/"+accountID, query, nil)
	if err != nil {
		c.log.Warn("azure: cost query failed",
			"subscription", accountID, "period", period, "err", err)
		return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
	}

	result := cloud.CostInfo{
		BillingPeriod: period,
		ServiceCosts:  map[string]float64{},
	}
	if resp.Properties == nil {
		return result, nil
	}
	colIdx := azureColumnIndex(resp.Properties.Columns)
	for _, row := range resp.Properties.Rows {
		if cost, ok := azureRowFloat(row, colIdx, "PreTaxCost"); ok {
			result.TotalCostUSD += cost
		}
		if svc, ok := azureRowString(row, colIdx, "ServiceName"); ok {
			if cost, ok := azureRowFloat(row, colIdx, "PreTaxCost"); ok {
				result.ServiceCosts[svc] += cost
			}
		}
	}
	return result, nil
}

// azurePeriodRange parses "YYYY-MM" into a [from, to) time range.
func azurePeriodRange(period string) (*time.Time, *time.Time, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return nil, nil, err
	}
	from := t
	to := t.AddDate(0, 1, 0)
	return &from, &to, nil
}

func azureColumnIndex(cols []*armcostmanagement.QueryColumn) map[string]int {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		if c.Name != nil {
			idx[*c.Name] = i
		}
	}
	return idx
}

func azureRowFloat(row []any, idx map[string]int, col string) (float64, bool) {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return 0, false
	}
	v, ok := row[i].(float64)
	if !ok {
		// Azure sometimes returns json.Number; convert.
		if s, ok := row[i].(string); ok {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return 0, false
			}
			return f, true
		}
		return 0, false
	}
	return v, true
}

func azureRowString(row []any, idx map[string]int, col string) (string, bool) {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return "", false
	}
	s, ok := row[i].(string)
	return s, ok
}

func toPtr[T any](v T) *T { return &v }
func toPtrGran() *armcostmanagement.GranularityType { g := armcostmanagement.GranularityType("None"); return &g }

// Ensure _ = azcore.TokenCredential typecheck is satisfied.
var _ azcore.TokenCredential = (*azidentity.ClientSecretCredential)(nil)
