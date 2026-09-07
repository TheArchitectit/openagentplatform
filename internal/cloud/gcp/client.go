package gcp

import (
	"context"
	"fmt"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type GCPClient struct {
	projectID string
}

func NewGCPClient(projectID string) *GCPClient {
	return &GCPClient{projectID: projectID}
}

func (c *GCPClient) Name() string { return "gcp" }

func (c *GCPClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
	return []cloud.CloudAccountInfo{{AccountID: c.projectID, DisplayName: c.projectID}}, nil
}

func (c *GCPClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]models.CloudResource, error) {
	computeSvc, err := compute.NewService(ctx, option.WithCredentialsFile(credRef))
	if err != nil {
		return nil, err
	}
	var resources []models.CloudResource
	list, err := computeSvc.Instances.List(c.projectID, region).Do()
	if err == nil {
		for _, inst := range list.Items {
			tags := make(map[string]string)
			for _, t := range inst.Tags.Items {
				tags[t] = t
			}
			resources = append(resources, models.CloudResource{
				ResourceID:   inst.Name,
				ResourceType: "gcp_compute_instance",
				AccountID:    accountID,
				Region:       region,
				Name:         inst.Name,
				Status:       inst.Status,
				Tags:         tags,
				Provider:     models.CloudProviderGCP,
			})
		}
	}
	return resources, nil
}

func (c *GCPClient) GetCost(ctx context.Context, credRef, accountID, period string) (cloud.CostInfo, error) {
	// GCP cost data requires either a BigQuery billing export dataset
	// (the canonical approach) or the Cloud Billing Catalog API (which
	// only lists SKU prices, not actual spend). Neither is a simple
	// credential-scoped call from a CloudProviderClient — both need
	// GCP-side configuration (export dataset creation, IAM roles, etc).
	// Return a clear error so the reconciler logs the gap rather than
	// silently recording 0 cost.
	return cloud.CostInfo{}, fmt.Errorf("gcp: cost fetch requires BigQuery billing export dataset — not yet implemented in CloudProviderClient (track via OAP roadmap)")
}
