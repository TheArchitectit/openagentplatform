package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

type AWSClient struct {
	resolver *resolver.SecretResolver
}

func NewAWSClient(r *resolver.SecretResolver) *AWSClient {
	return &AWSClient{resolver: r}
}

func (c *AWSClient) Name() string { return "aws" }

func (c *AWSClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
	cfg, err := c.cfgForCred(ctx, credRef)
	if err != nil {
		return nil, err
	}
	client := organizations.NewFromConfig(cfg)
	out, err := client.ListAccounts(ctx, &organizations.ListAccountsInput{})
	if err != nil {
		return []cloud.CloudAccountInfo{{AccountID: "default", DisplayName: "default"}}, nil
	}
	var accounts []cloud.CloudAccountInfo
	for _, a := range out.Accounts {
		if a.Status == "ACTIVE" {
			accounts = append(accounts, cloud.CloudAccountInfo{
				AccountID:   *a.Id,
				DisplayName: *a.Name,
			})
		}
	}
	return accounts, nil
}

func (c *AWSClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]models.CloudResource, error) {
	cfg, err := c.cfgForAccount(ctx, credRef, accountID, region)
	if err != nil {
		return nil, err
	}
	tagClient := resourcegroupstaggingapi.NewFromConfig(cfg)
	var resources []models.CloudResource

	paginator := resourcegroupstaggingapi.NewGetResourcesPaginator(tagClient, &resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []string{
			"ec2:instance", "ec2:volume", "ec2:security-group",
			"rds:db", "lambda:function",
			"s3", "elasticloadbalancing:loadbalancer",
			"vpc", "ec2:subnet", "ec2:route-table", "ec2:internet-gateway",
			"ec2:natgateway", "ec2:vpn-gateway",
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, rp := range page.ResourceTagMappingList {
			resources = append(resources, awsResourceFromTagMapping(rp, accountID))
		}
	}
	return resources, nil
}

func (c *AWSClient) GetCost(ctx context.Context, credRef, accountID, period string) (cloud.CostInfo, error) {
	_, err := c.cfgForAccount(ctx, credRef, accountID, "us-east-1")
	if err != nil {
		return cloud.CostInfo{}, err
	}
	return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
}

func (c *AWSClient) cfgForCred(ctx context.Context, credRef string) (aws.Config, error) {
	secretVal, err := c.resolver.Resolve(ctx, credRef, nil)
	if err != nil {
		return aws.Config{}, err
	}
	accessKey, _ := secretVal.Data["access_key_id"].(string)
	secretKey, _ := secretVal.Data["secret_access_key"].(string)
	credsProvider := aws.CredentialsProviderFunc(
		func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
			}, nil
		},
	)
	return config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credsProvider),
	)
}

func (c *AWSClient) cfgForAccount(ctx context.Context, credRef, accountID, region string) (aws.Config, error) {
	cfg, err := c.cfgForCred(ctx, credRef)
	if err != nil {
		return aws.Config{}, err
	}
	if region != "" {
		cfg.Region = region
	}
	return cfg, nil
}

func awsResourceFromTagMapping(rp types.ResourceTagMapping, accountID string) models.CloudResource {
	tags := make(map[string]string)
	for _, t := range rp.Tags {
		tags[*t.Key] = *t.Value
	}
	arn := string(*rp.ResourceARN)
	parts := strings.Split(arn, ":")
	resourceType := ""
	if len(parts) >= 6 {
		resourceType = parts[2] + ":" + parts[5]
	}
	return models.CloudResource{
		ResourceID:   arn,
		ResourceType: resourceType,
		AccountID:    accountID,
		Name:         tags["Name"],
		Tags:         tags,
		Status:       "",
		Provider:     models.CloudProviderAWS,
	}
}
