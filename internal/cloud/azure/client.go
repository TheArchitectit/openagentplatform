package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type AzureClient struct {
	subscriptionID string
}

func NewAzureClient(subscriptionID string) *AzureClient {
	return &AzureClient{subscriptionID: subscriptionID}
}

func (c *AzureClient) Name() string { return "azure" }

func (c *AzureClient) cred() (*azidentity.ClientSecretCredential, error) {
	return azidentity.NewClientSecretCredential("", "", "", nil)
}

func (c *AzureClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
	_, err := c.cred()
	if err != nil {
		return nil, err
	}
	return []cloud.CloudAccountInfo{{AccountID: c.subscriptionID, DisplayName: c.subscriptionID}}, nil
}

func (c *AzureClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]models.CloudResource, error) {
	cred, err := c.cred()
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
	return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
}
