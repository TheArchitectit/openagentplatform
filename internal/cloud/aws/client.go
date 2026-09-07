package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	ceTypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	rgTypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/pkg/models"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

type AWSClient struct {
	resolver *resolver.SecretResolver
	log      *slog.Logger
}

func NewAWSClient(r *resolver.SecretResolver) *AWSClient {
	return &AWSClient{resolver: r, log: slog.Default()}
}

func NewAWSClientWithLog(r *resolver.SecretResolver, log *slog.Logger) *AWSClient {
	if log == nil {
		log = slog.Default()
	}
	return &AWSClient{resolver: r, log: log}
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
	// Cost Explorer is a global service, so us-east-1 is the standard endpoint.
	cfg, err := c.cfgForAccount(ctx, credRef, accountID, "us-east-1")
	if err != nil {
		return cloud.CostInfo{}, fmt.Errorf("aws: cost config: %w", err)
	}
	client := costexplorer.NewFromConfig(cfg)

	start, end, err := awsPeriodRange(period)
	if err != nil {
		return cloud.CostInfo{}, fmt.Errorf("aws: parse period %q: %w", period, err)
	}

	out, err := client.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		TimePeriod: &ceTypes.DateInterval{Start: aws.String(start), End: aws.String(end)},
		Granularity: ceTypes.GranularityMonthly,
		Metrics:     []string{"BlendedCost"},
		GroupBy: []ceTypes.GroupDefinition{{
			Key:  aws.String("SERVICE"),
			Type: ceTypes.GroupDefinitionTypeDimension,
		}},
	})
	if err != nil {
		// Cost Explorer may not be enabled for this account, or credentials
		// may lack ce:GetCostAndUsage. Non-fatal — reconciler continues.
		c.log.Warn("aws: GetCostAndUsage failed",
			"account", accountID, "period", period, "err", err)
		return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
	}

	result := cloud.CostInfo{
		BillingPeriod: period,
		ServiceCosts:  map[string]float64{},
	}
	for _, rbt := range out.ResultsByTime {
		if v, ok := rbt.Total["BlendedCost"]; ok && v.Amount != nil {
			if n, err := strconv.ParseFloat(*v.Amount, 64); err == nil {
				result.TotalCostUSD = n
			}
		}
		for _, g := range rbt.Groups {
			if len(g.Keys) == 0 {
				continue
			}
			svc := g.Keys[0]
			if mv, ok := g.Metrics["BlendedCost"]; ok && mv.Amount != nil {
				if n, err := strconv.ParseFloat(*mv.Amount, 64); err == nil {
					result.ServiceCosts[svc] = n
				}
			}
		}
	}
	return result, nil
}

// awsPeriodRange converts "YYYY-MM" into a [start, end) DateInterval where
// end is the first day of the following month (per AWS's exclusive-end rule).
func awsPeriodRange(period string) (string, string, error) {
	t, err := time.Parse("2006-01", period)
	if err != nil {
		return "", "", err
	}
	start := t.Format("2006-01-02")
	end := t.AddDate(0, 1, 0).Format("2006-01-02")
	return start, end, nil
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

func awsResourceFromTagMapping(rp rgTypes.ResourceTagMapping, accountID string) models.CloudResource {
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
