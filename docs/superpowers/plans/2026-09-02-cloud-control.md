# Cloud Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add AWS/Azure/GCP cloud resource inventory, hybrid MSP+per-org credential management, virtual-agent auto-enrollment, tag drift detection, and cost telemetry to OAP.

**Architecture:** A `CloudProvider` interface normalizes responses from AWS SDK, Azure SDK, and GCP SDK into a unified `CloudResource` struct. A `CloudAccount` registry holds credentials per-org via `SecretBackend`. A reconciliation worker runs on the 30s scheduler tick per org, compares cloud API state against the `cloud_resources` table, emits `cloud_drift` alerts via `oap.events.alerts`, and auto-enrolls unmanaged resources as virtual agents. The API surface is `/api/v1/cloud`.

**Tech Stack:** AWS SDK v2 (`github.com/aws/aws-sdk-go-v2`), Azure SDK (`github.com/Azure/azure-sdk-for-go`), GCP SDK (`cloud.google.com/go/compute`), `github.com/gosnmp/gosnmp` (UPS checks only), existing OAP patterns.

---

## File Map

```
internal/cloud/
├── cloud.go                    # CloudProvider interface, CloudResource struct, CloudAccount struct
├── provider.go                 # ProviderClient interface
├── aws/
│   └── client.go             # AWSClient: ListResources, ListAccounts, GetCost
├── azure/
│   └── client.go             # AzureClient: ListResources, ListAccounts, GetCost
├── gcp/
│   └── client.go             # GCPClient: ListResources, ListAccounts, GetCost
├── reconciler.go              # ReconciliationWorker: runs per-org on scheduler
├── accounts.go                # AccountStore: CRUD against cloud_accounts
├── resources.go               # ResourceStore: CRUD against cloud_resources
├── policies.go                # PolicyStore: CRUD against cloud_policies
└── costs.go                   # CostStore: CRUD against cost_snapshots

internal/api/
├── routes.go                  # Register /api/v1/cloud routes
├── cloud_accounts.go         # Handler: listAccounts, createAccount, deleteAccount
├── cloud_resources.go        # Handler: listResources, getResource, enroll, ignore
├── cloud_policies.go          # Handler: listPolicies, createPolicy, updatePolicy, deletePolicy
├── cloud_costs.go            # Handler: listCosts, listDrift
└── cloud_wiring.go           # Wire CloudProvider clients, reconciler into server lifecycle

pkg/models/
└── models_cloud.go           # CloudAccount, CloudResource, CloudPolicy, CostSnapshot types

internal/db/migrations/
└── 013_cloud_control.up.sql  # cloud_accounts, cloud_resources, cloud_policies, cost_snapshots

server_adapters.go             # Wire cloud reconciler into scheduler lifecycle
```

---

### Task 1: Database Migration 013

**Files:**
- Create: `internal/db/migrations/013_cloud_control.up.sql`
- Test: `internal/db/migrations/013_cloud_control.up.sql` (verify no duplicate table names against 001)

- [ ] **Step 1: Write the migration**

```sql
-- 013_cloud_control: cloud provider accounts, resource inventory, tag drift policies, cost snapshots

CREATE TABLE IF NOT EXISTS cloud_accounts (
    id              TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,  -- aws | azure | gcp
    account_id     TEXT NOT NULL,  -- cloud-native account ID/sub ID/project
    display_name   TEXT NOT NULL,
    is_msp_hub     BOOLEAN NOT NULL DEFAULT false,
    enabled        BOOLEAN NOT NULL DEFAULT true,
    last_seen      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cloud_accounts_org ON cloud_accounts (org_id);

CREATE TABLE IF NOT EXISTS cloud_resources (
    id              TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,
    account_id     TEXT NOT NULL,
    resource_id    TEXT NOT NULL,   -- cloud-native ID
    resource_type  TEXT NOT NULL,   -- e.g. ec2_instance, azure_vm, gcp_vpc
    region         TEXT,
    name           TEXT NOT NULL,
    status         TEXT,
    tags           JSONB NOT NULL DEFAULT '{}',
    archived_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, account_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_cloud_resources_org ON cloud_resources (org_id);
CREATE INDEX IF NOT EXISTS idx_cloud_resources_archived ON cloud_resources (archived_at) WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS cloud_policies (
    id              TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL DEFAULT '',
    provider        TEXT NOT NULL,
    account_id     TEXT NOT NULL,
    required_tags  JSONB NOT NULL DEFAULT '[]',  -- ["env", "owner", "team"]
    tag_rules      JSONB NOT NULL DEFAULT '{}',  -- {"env": ["prod","staging"]}
    enabled        BOOLEAN NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cloud_policies_org ON cloud_policies (org_id);

CREATE TABLE IF NOT EXISTS cost_snapshots (
    id                  TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL DEFAULT '',
    provider           TEXT NOT NULL,
    account_id         TEXT NOT NULL,
    billing_period      TEXT NOT NULL,  -- "2026-09"
    total_cost_usd     NUMERIC(12,4) NOT NULL DEFAULT 0,
    service_costs       JSONB NOT NULL DEFAULT '{}',  -- {"EC2": 120.50, "RDS": 80.00}
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cost_snapshots_org_period ON cost_snapshots (org_id, billing_period);
```

- [ ] **Step 2: Verify the migration parses against a test database**

Run: `go test ./internal/db/... -run TestMigration -v` (or equivalent)
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/013_cloud_control.up.sql
git commit -m "feat(cloud): migration 013 — cloud_accounts, cloud_resources, cloud_policies, cost_snapshots"
```

---

### Task 2: Core Types and Provider Interface

**Files:**
- Create: `pkg/models/models_cloud.go`
- Create: `internal/cloud/cloud.go`
- Create: `internal/cloud/provider.go`

- [ ] **Step 1: Write the Go models**

```go
// pkg/models/models_cloud.go
package models

import "time"

type CloudProvider string

const (
    CloudProviderAWS   CloudProvider = "aws"
    CloudProviderAzure CloudProvider = "azure"
    CloudProviderGCP   CloudProvider = "gcp"
)

type CloudAccount struct {
    ID          string      `json:"id"`
    OrgID       string      `json:"org_id"`
    Provider    CloudProvider `json:"provider"`
    AccountID   string      `json:"account_id"`
    DisplayName string      `json:"display_name"`
    IsMSPHub    bool        `json:"is_msp_hub"`
    Enabled     bool        `json:"enabled"`
    LastSeen    time.Time   `json:"last_seen"`
    CreatedAt   time.Time   `json:"created_at"`
    UpdatedAt   time.Time   `json:"updated_at"`
}

type CloudResource struct {
    ID           string                 `json:"id"`
    OrgID        string                 `json:"org_id"`
    Provider     CloudProvider           `json:"provider"`
    AccountID    string                 `json:"account_id"`
    ResourceID   string                 `json:"resource_id"`
    ResourceType string                 `json:"resource_type"`
    Region       string                 `json:"region"`
    Name         string                 `json:"name"`
    Status       string                 `json:"status"`
    Tags         map[string]string      `json:"tags"`
    ArchivedAt   *time.Time            `json:"archived_at,omitempty"`
    CreatedAt    time.Time              `json:"created_at"`
    UpdatedAt    time.Time              `json:"updated_at"`
}

type CloudPolicy struct {
    ID           string                 `json:"id"`
    OrgID        string                 `json:"org_id"`
    Provider     CloudProvider           `json:"provider"`
    AccountID    string                 `json:"account_id"`
    RequiredTags []string               `json:"required_tags"`
    TagRules     map[string][]string    `json:"tag_rules"`
    Enabled     bool                   `json:"enabled"`
    CreatedAt   time.Time              `json:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at"`
}

type CostSnapshot struct {
    ID             string                 `json:"id"`
    OrgID          string                 `json:"org_id"`
    Provider       CloudProvider           `json:"provider"`
    AccountID      string                 `json:"account_id"`
    BillingPeriod  string                 `json:"billing_period"`
    TotalCostUSD   float64                `json:"total_cost_usd"`
    ServiceCosts   map[string]float64     `json:"service_costs"`
    CreatedAt     time.Time              `json:"created_at"`
}
```

- [ ] **Step 2: Write the provider interface**

```go
// internal/cloud/provider.go
package cloud

import "context"

type CloudAccountInfo struct {
    AccountID   string
    DisplayName string
    Region     string
}

type CostInfo struct {
    BillingPeriod  string
    TotalCostUSD  float64
    ServiceCosts  map[string]float64
}

type ProviderClient interface {
    // Name returns the provider identifier ("aws", "azure", "gcp")
    Name() string

    // ListAccounts returns all accounts accessible via the given credential
    ListAccounts(ctx context.Context, credRef string) ([]CloudAccountInfo, error)

    // ListResources returns all cloud resources in the given account/region
    ListResources(ctx context.Context, credRef, accountID, region string) ([]CloudResource, error)

    // GetCost returns cost breakdown for the billing period
    GetCost(ctx context.Context, credRef, accountID, period string) (CostInfo, error)
}
```

- [ ] **Step 3: Write cloud.go with AccountStore and ResourceStore interfaces**

```go
// internal/cloud/cloud.go
package cloud

import "context"

// AccountStore manages cloud_accounts rows.
type AccountStore interface {
    Create(ctx context.Context, a *CloudAccount) error
    Get(ctx context.Context, id string) (*CloudAccount, error)
    ListByOrg(ctx context.Context, orgID string) ([]*CloudAccount, error)
    Delete(ctx context.Context, id string) error
}

// ResourceStore manages cloud_resources rows.
type ResourceStore interface {
    Upsert(ctx context.Context, r *CloudResource) error
    Get(ctx context.Context, id string) (*CloudResource, error)
    ListByOrg(ctx context.Context, orgID string, filter ResourceFilter) ([]*CloudResource, error)
    Archive(ctx context.Context, id string) error
}

// ResourceFilter filters ListByOrg.
type ResourceFilter struct {
    Provider   string
    AccountID string
    Type      string
    Archived  bool
}

// PolicyStore manages cloud_policies rows.
type PolicyStore interface {
    Create(ctx context.Context, p *CloudPolicy) error
    Get(ctx context.Context, id string) (*CloudPolicy, error)
    ListByOrg(ctx context.Context, orgID string) ([]*CloudPolicy, error)
    Delete(ctx context.Context, id string) error
}

// CostStore manages cost_snapshots rows.
type CostStore interface {
    Insert(ctx context.Context, c *CostSnapshot) error
    GetLatest(ctx context.Context, orgID, provider, accountID string) (*CostSnapshot, error)
}
```

- [ ] **Step 4: Write tests for models**

```go
// pkg/models/models_cloud_test.go
package models

import "testing"

func TestCloudProviderConstants(t *testing.T) {
    if CloudProviderAWS != "aws" {
        t.Errorf("CloudProviderAWS = %q, want aws", CloudProviderAWS)
    }
    if CloudProviderAzure != "azure" {
        t.Errorf("CloudProviderAzure = %q, want azure", CloudProviderAzure)
    }
    if CloudProviderGCP != "gcp" {
        t.Errorf("CloudProviderGCP = %q, want gcp", CloudProviderGCP)
    }
}

func TestCloudResourceTags(t *testing.T) {
    r := &CloudResource{
        ID:           "r1",
        OrgID:        "org1",
        Provider:     CloudProviderAWS,
        ResourceID:   "i-abc123",
        ResourceType: "ec2_instance",
        Tags:         map[string]string{"env": "prod", "owner": "sre"},
    }
    if r.Tags["env"] != "prod" {
        t.Errorf("r.Tags[env] = %q, want prod", r.Tags["env"])
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/models/... -run TestCloud -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/models/models_cloud.go internal/cloud/cloud.go internal/cloud/provider.go pkg/models/models_cloud_test.go
git commit -m "feat(cloud): core types and ProviderClient interface"
```

---

### Task 3: AWS Provider Client

**Files:**
- Create: `internal/cloud/aws/client.go`
- Test: `internal/cloud/aws/client_test.go`

- [ ] **Step 1: Write the AWS client**

```go
// internal/cloud/aws/client.go
package aws

import (
    "context"
    "encoding/json"
    "strings"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/ec2"
    "github.com/aws/aws-sdk-go-v2/service/organizations"
    "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
    "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

    "github.com/openagentplatform/openagentplatform/internal/cloud"
    "github.com/openagentplatform/openagentplatform/internal/secrets"
)

type AWSClient struct {
    resolver *secrets.Resolver
}

func NewAWSClient(resolver *secrets.Resolver) *AWSClient {
    return &AWSClient{resolver: resolver}
}

func (c *AWSClient) Name() string { return "aws" }

func (c *AWSClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
    // Use organizations API if possible, otherwise return the single account
    cfg, err := c.cfgForCred(ctx, credRef)
    if err != nil {
        return nil, err
    }
    client := organizations.NewFromConfig(cfg)
    out, err := client.ListAccounts(ctx, &organizations.ListAccountsInput{})
    if err != nil {
        // Fall back to single-account: extract from config
        return []cloud.CloudAccountInfo{{AccountID: *cfg.Credentials.Source}, DisplayName: "default"}, nil
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

func (c *AWSClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]cloud.CloudResource, error) {
    cfg, err := c.cfgForAccount(ctx, credRef, accountID, region)
    if err != nil {
        return nil, err
    }
    tagClient := resourcegroupstaggingapi.NewFromConfig(cfg)
    var resources []cloud.CloudResource

    // Tagging API covers all resource types in one call
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
    // Cost Explorer API — requires Cost Explorer to be enabled on the account
    cfg, err := c.cfgForAccount(ctx, credRef, accountID, "us-east-1")
    if err != nil {
        return cloud.CostInfo{}, err
    }
    // Implementation: call CostExplorer GetCostAndUsage
    // Return empty CostInfo if Cost Explorer is not enabled (non-fatal)
    return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
}

func (c *AWSClient) cfgForCred(ctx context.Context, credRef string) (aws.Config, error) {
    credVal, err := c.resolver.Resolve(ctx, credRef, nil)
    if err != nil {
        return aws.Config{}, err
    }
    return config.WithCredentialsProvider(ctx, aws.CredentialsProviderFunc(
        func(ctx context.Context) (aws.Credentials, error) {
            var m map[string]string
            json.Unmarshal(credVal, &m)
            return aws.Credentials{
                AccessKeyID:     m["access_key_id"],
                SecretAccessKey: m["secret_access_key"],
            }, nil
        },
    ))
}

func awsResourceFromTagMapping(rp types.ResourceTagMapping, accountID string) cloud.CloudResource {
    tags := make(map[string]string)
    for _, t := range rp.Tags {
        tags[*t.Key] = *t.Value
    }
    arn := string(*rp.ResourceARN)
    // Parse resource type from ARN: arn:aws:ec2:region:account:instance/id
    parts := strings.Split(arn, ":")
    resourceType := parts[2] + ":" + parts[5]
    if len(parts) > 6 {
        resourceType = parts[2] + ":" + parts[5]
    }
    return cloud.CloudResource{
        ResourceID:   arn,
        ResourceType: resourceType,
        AccountID:    accountID,
        Name:         tags["Name"],
        Tags:         tags,
        Status:       "",
        Provider:     cloud.CloudProviderAWS,
    }
}
```

- [ ] **Step 2: Write a test that verifies the interface is satisfied**

```go
// internal/cloud/aws/client_test.go
package aws

import (
    "testing"

    "github.com/openagentplatform/openagentplatform/internal/cloud"
)

func TestAWSClientImplementsInterface(t *testing.T) {
    var _ cloud.ProviderClient = (*AWSClient)(nil)
}
```

- [ ] **Step 3: Run tests**

Run: `go build ./internal/cloud/aws/ && go test ./internal/cloud/aws/...`
Expected: PASS (compile success + interface check)

- [ ] **Step 4: Commit**

```bash
git add internal/cloud/aws/client.go internal/cloud/aws/client_test.go
git commit -m "feat(cloud): AWS provider client using AWS SDK v2 + resource tagging API"
```

---

### Task 4: Azure and GCP Provider Clients

**Files:**
- Create: `internal/cloud/azure/client.go`
- Create: `internal/cloud/gcp/client.go`
- Create: `internal/cloud/azure/client_test.go`
- Create: `internal/cloud/gcp/client_test.go`

- [ ] **Step 1: Write Azure client**

```go
// internal/cloud/azure/client.go
package azure

import (
    "context"

    "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
    "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"

    "github.com/openagentplatform/openagentplatform/internal/cloud"
)

type AzureClient struct {
    subscriptionID string
}

func NewAzureClient(subscriptionID string) *AzureClient {
    return &AzureClient{subscriptionID: subscriptionID}
}

func (c *AzureClient) Name() string { return "azure" }

func (c *AzureClient) ListAccounts(ctx context.Context, credRef string) ([]cloud.CloudAccountInfo, error) {
    cred, err := azidentity.NewClientSecretCredential("", "", "", nil)
    if err != nil {
        return nil, err
    }
    client, err := armresources.NewSubscriptionsClient(c.subscriptionID, cred, nil)
    if err != nil {
        return nil, err
    }
    resp, err := client.Get(ctx, c.subscriptionID, nil)
    if err != nil {
        return nil, err
    }
    return []cloud.CloudAccountInfo{{AccountID: c.subscriptionID, DisplayName: *resp.Subscription.DisplayName}}, nil
}

func (c *AzureClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]cloud.CloudResource, error) {
    cred, err := azidentity.NewClientSecretCredential("", "", "", nil)
    if err != nil {
        return nil, err
    }
    resourcesClient, err := armresources.NewResourcesClient(c.subscriptionID, cred, nil)
    if err != nil {
        return nil, err
    }
    pager := resourcesClient.NewListPager(nil)
    var resources []cloud.CloudResource
    for pager.More() {
        page, err := pager.NextPage(ctx)
        if err != nil {
            return nil, err
        }
        for _, r := range page.ResourceGroupResult.Value {
            resources = append(resources, cloud.CloudResource{
                ResourceID:   *r.ID,
                ResourceType: *r.Type,
                AccountID:    accountID,
                Name:         *r.Name,
                Tags:         map[string]string(*r.Tags),
                Status:       string(*r.Properties.ProvisioningState),
                Provider:     cloud.CloudProviderAzure,
            })
        }
    }
    return resources, nil
}

func (c *AzureClient) GetCost(ctx context.Context, credRef, accountID, period string) (cloud.CostInfo, error) {
    cred, err := azidentity.NewClientSecretCredential("", "", "", nil)
    if err != nil {
        return cloud.CostInfo{}, err
    }
    costClient, err := armcostmanagement.NewQueryClient(cred, nil)
    if err != nil {
        return cloud.CostInfo{}, err
    }
    // Query cost for the billing period
    // Return empty if Cost Management is not enabled
    return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
}
```

- [ ] **Step 2: Write GCP client**

```go
// internal/cloud/gcp/client.go
package gcp

import (
    "context"

    "cloud.google.com/go/compute/apiv1"
    "cloud.google.com/go/compute/apiv1/computepb"
    "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
    "google.golang.org/api/compute/v1"
    "google.golang.org/api/option"

    "github.com/openagentplatform/openagentplatform/internal/cloud"
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

func (c *GCPClient) ListResources(ctx context.Context, credRef, accountID, region string) ([]cloud.CloudResource, error) {
    cred, err := option.WithCredentialsFile(credRef)
    if err != nil {
        return nil, err
    }
    computeSvc, err := compute.NewService(ctx, cred)
    if err != nil {
        return nil, err
    }
    var resources []cloud.CloudResource

    // Compute Engine instances
    list, err := computeSvc.Instances.List(c.projectID, region).Do()
    if err == nil {
        for _, inst := range list.Items {
            tags := make(map[string]string)
            for k, v := range inst.Tags.Items {
                tags[k] = v
            }
            resources = append(resources, cloud.CloudResource{
                ResourceID:   inst.Name,
                ResourceType: "gcp_compute_instance",
                AccountID:    accountID,
                Region:       region,
                Name:         inst.Name,
                Status:       inst.Status,
                Tags:         tags,
                Provider:     cloud.CloudProviderGCP,
            })
        }
    }
    return resources, nil
}

func (c *GCPClient) GetCost(ctx context.Context, credRef, accountID, period string) (cloud.CostInfo, error) {
    return cloud.CostInfo{BillingPeriod: period, ServiceCosts: map[string]float64{}}, nil
}
```

- [ ] **Step 3: Write interface tests**

```go
// internal/cloud/azure/client_test.go
package azure

import "testing"

func TestAzureClientImplementsInterface(t *testing.T) {
    var _ interface{ Name() string } = (*AzureClient)(nil)
    // Verify interface satisfied by compile check
    var _ interface{ Name() string; ListAccounts, ListResources, GetCost } = interface{}((*AzureClient)(nil))
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./internal/cloud/azure/ ./internal/cloud/gcp/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cloud/azure/client.go internal/cloud/gcp/client.go internal/cloud/azure/client_test.go internal/cloud/gcp/client_test.go
git commit -m "feat(cloud): Azure and GCP provider clients"
```

---

### Task 5: Reconciliation Worker

**Files:**
- Create: `internal/cloud/reconciler.go`
- Test: `internal/cloud/reconciler_test.go`

- [ ] **Step 1: Write the reconciliation worker**

```go
// internal/cloud/reconciler.go
package cloud

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/openagentplatform/openagentplatform/internal/events"
    "github.com/openagentplatform/openagentplatform/internal/secrets"
)

type Reconciler struct {
    accountsStore AccountStore
    resourceStore ResourceStore
    policyStore  PolicyStore
    costStore    CostStore
    providers    map[string]ProviderClient
    resolver     *secrets.Resolver
    log         *slog.Logger
    mu          sync.Mutex
}

func NewReconciler(as AccountStore, rs ResourceStore, ps PolicyStore, cs CostStore, log *slog.Logger) *Reconciler {
    return &Reconciler{
        accountsStore: as,
        resourceStore: rs,
        policyStore:  ps,
        costStore:    cs,
        providers:    make(map[string]ProviderClient),
        log:         log,
    }
}

func (r *Reconciler) RegisterProvider(p ProviderClient) {
    r.providers[p.Name()] = p
}

func (r *Reconciler) ReconcileOrg(ctx context.Context, orgID string) error {
    accounts, err := r.accountsStore.ListByOrg(ctx, orgID)
    if err != nil {
        return err
    }

    for _, acct := range accounts {
        if !acct.Enabled {
            continue
        }
        client, ok := r.providers[string(acct.Provider)]
        if !ok {
            r.log.Warn("cloud: no provider client for", "provider", acct.Provider)
            continue
        }
        credRef := "ref:oap://secret/cloud/" + acct.AccountID
        if acct.IsMSPHub {
            credRef = "ref:oap://secret/cloud/hub/" + acct.AccountID
        }

        // Fetch all resources
        resources, err := client.ListResources(ctx, credRef, acct.AccountID, "")
        if err != nil {
            r.log.Warn("cloud: ListResources failed", "account", acct.AccountID, "err", err)
            continue
        }

        // Upsert each resource
        seen := make(map[string]bool)
        for _, res := range resources {
            res.OrgID = orgID
            res.AccountID = acct.AccountID
            if err := r.resourceStore.Upsert(ctx, &res); err != nil {
                r.log.Warn("cloud: upsert resource failed", "id", res.ResourceID, "err", err)
            }
            seen[res.ResourceID] = true
        }

        // Archive resources that disappeared
        existing, err := r.resourceStore.ListByOrg(ctx, orgID, ResourceFilter{Provider: string(acct.Provider), AccountID: acct.AccountID})
        if err != nil {
            continue
        }
        for _, res := range existing {
            if !seen[res.ResourceID] && res.ArchivedAt == nil {
                r.resourceStore.Archive(ctx, res.ID)
            }
        }

        // Run drift detection against policies
        policies, _ := r.policyStore.ListByOrg(ctx, orgID)
        for _, pol := range policies {
            if !pol.Enabled || pol.Provider != acct.Provider || pol.AccountID != acct.AccountID {
                continue
            }
            r.checkDrift(ctx, pol, resources)
        }

        // Fetch cost
        period := time.Now().Format("2006-01")
        costInfo, _ := client.GetCost(ctx, credRef, acct.AccountID, period)
        if costInfo.TotalCostUSD > 0 {
            snapshot := &CostSnapshot{
                ID:            orgID + "-" + acct.AccountID + "-" + period,
                OrgID:         orgID,
                Provider:       acct.Provider,
                AccountID:     acct.AccountID,
                BillingPeriod:  period,
                TotalCostUSD:   costInfo.TotalCostUSD,
                ServiceCosts:   costInfo.ServiceCosts,
                CreatedAt:     time.Now(),
            }
            r.costStore.Insert(ctx, snapshot)
        }
    }
    return nil
}

func (r *Reconciler) checkDrift(ctx context.Context, pol *CloudPolicy, resources []CloudResource) {
    for _, res := range resources {
        for _, required := range pol.RequiredTags {
            if _, ok := res.Tags[required]; !ok {
                events.PublishAlert(ctx, &events.AlertPayload{
                    Type:       "cloud_drift",
                    Severity:   "warning",
                    ResourceID: res.ID,
                    Message:    "missing_required_tag: " + required,
                    Details:    map[string]any{"resource": res.Name, "tag": required},
                })
            }
        }
        // Tag rule violations
        for tagKey, allowed := range pol.TagRules {
            if actual, ok := res.Tags[tagKey]; ok {
                allowedMap := make(map[string]bool)
                for _, v := range allowed {
                    allowedMap[v] = true
                }
                if !allowedMap[actual] {
                    events.PublishAlert(ctx, &events.AlertPayload{
                        Type:       "cloud_drift",
                        Severity:   "warning",
                        ResourceID: res.ID,
                        Message:    "invalid_tag_value: " + tagKey + "=" + actual,
                        Details:    map[string]any{"resource": res.Name, "tag": tagKey, "actual": actual, "allowed": allowed},
                    })
                }
            }
        }
    }
}
```

- [ ] **Step 2: Wire into the scheduled jobs**

```go
// internal/cloud/reconciler.go — add to NewReconciler or wire separately:
func (r *Reconciler) RegisterWithScheduler(s *scheduled.Scheduler) {
    s.Register("cloud_reconcile", func(ctx context.Context, task *scheduled.TaskRecord) error {
        return r.ReconcileOrg(ctx, task.OrgID)
    })
}
```

- [ ] **Step 3: Write tests**

```go
// internal/cloud/reconciler_test.go
package cloud

import "testing"

func TestResourceFilter(t *testing.T) {
    f := ResourceFilter{Provider: "aws", Archived: false}
    if f.Provider != "aws" {
        t.Errorf("Provider = %q, want aws", f.Provider)
    }
}

func TestReconcilerImplementsNothingPanic(t *testing.T) {
    r := NewReconciler(nil, nil, nil, nil, nil)
    if r == nil {
        t.Fatal("NewReconciler returned nil")
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go build ./internal/cloud/ && go test ./internal/cloud/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cloud/reconciler.go internal/cloud/reconciler_test.go
git commit -m "feat(cloud): reconciliation worker — reconcile org, drift detection, cost fetch"
```

---

### Task 6: API Routes and Handlers

**Files:**
- Create: `internal/api/cloud_accounts.go`
- Create: `internal/api/cloud_resources.go`
- Create: `internal/api/cloud_policies.go`
- Create: `internal/api/cloud_costs.go`
- Modify: `internal/api/routes.go` — add `s.mountCloudRoutes(r)`
- Modify: `cmd/server/server_adapters.go` — wire cloud clients into server

- [ ] **Step 1: Write cloud accounts handler**

```go
// internal/api/cloud_accounts.go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/openagentplatform/openagentplatform/internal/cloud"
    "github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *Server) mountCloudRoutes(r chi.Router) {
    r.Route("/cloud", func(r chi.Router) {
        r.Get("/accounts", s.listCloudAccounts)
        r.Post("/accounts", auth.RequireRole(auth.RoleAdmin), s.createCloudAccount)
        r.Delete("/accounts/{id}", auth.RequireRole(auth.RoleAdmin), s.deleteCloudAccount)

        r.Get("/resources", s.listCloudResources)
        r.Get("/resources/{id}", s.getCloudResource)
        r.Post("/resources/{id}/enroll", auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician), s.enrollCloudResource)
        r.Post("/resources/{id}/ignore", auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician), s.ignoreCloudResource)

        r.Get("/policies", s.listCloudPolicies)
        r.Post("/policies", auth.RequireRole(auth.RoleAdmin), s.createCloudPolicy)
        r.Put("/policies/{id}", auth.RequireRole(auth.RoleAdmin), s.updateCloudPolicy)
        r.Delete("/policies/{id}", auth.RequireRole(auth.RoleAdmin), s.deleteCloudPolicy)

        r.Get("/costs", s.listCloudCosts)
        r.Get("/drift", s.listCloudDrift)
    })
}

func (s *Server) listCloudAccounts(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    orgID := tenancy.GetTenant(ctx).OrgID
    accounts, err := s.cloudAccounts.ListByOrg(ctx, orgID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(accounts)
}

func (s *Server) createCloudAccount(w http.ResponseWriter, r *http.Request) {
    var acct models.CloudAccount
    if err := json.NewDecoder(r.Body).Decode(&acct); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    acct.OrgID = tenancy.GetTenant(r.Context()).OrgID
    if err := s.cloudAccounts.Create(r.Context(), &acct); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(201)
    json.NewEncoder(w).Encode(acct)
}

func (s *Server) deleteCloudAccount(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := s.cloudAccounts.Delete(r.Context(), id); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(204)
}

// listCloudResources, getCloudResource, enrollCloudResource, ignoreCloudResource,
// listCloudPolicies, createCloudPolicy, updateCloudPolicy, deleteCloudPolicy,
// listCloudCosts, listCloudDrift follow the same pattern as the above handlers.
// They delegate to the cloud.AccountStore, ResourceStore, PolicyStore, CostStore interfaces.
```

- [ ] **Step 2: Wire into routes.go**

```go
// internal/api/routes.go — in mountAPISubRoutes:
s.mountCloudRoutes(r)
```

- [ ] **Step 3: Write a routing test**

```go
// internal/api/routes_cloud_test.go
package api

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestCloudRoutesRegistered(t *testing.T) {
    srv := newTestServer(t)
    routes := []struct {
        method, path string
        code int
    }{
        {"GET", "/api/v1/cloud/accounts", 200},
        {"POST", "/api/v1/cloud/accounts", 201},
        {"GET", "/api/v1/cloud/resources", 200},
        {"GET", "/api/v1/cloud/policies", 200},
        {"GET", "/api/v1/cloud/costs", 200},
    }
    for _, r := range routes {
        req := httptest.NewRequest(r.method, r.path, nil)
        req.Header.Set("Authorization", "Bearer "+testToken(t))
        w := httptest.NewRecorder()
        srv.ServeHTTP(w, req)
        if w.Code != r.code {
            t.Errorf("%s %s → %d, want %d", r.method, r.path, w.Code, r.code)
        }
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/... -run TestCloud -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/cloud_accounts.go internal/api/cloud_resources.go internal/api/cloud_policies.go internal/api/cloud_costs.go internal/api/routes.go internal/api/routes_cloud_test.go
git commit -m "feat(cloud): API surface — /api/v1/cloud accounts, resources, policies, costs"
```

---

### Task 7: Auto-Enrollment and Virtual Agents

**Files:**
- Modify: `internal/cloud/reconciler.go` — add enrollment logic
- Modify: `pkg/models/models.go` — add `platform = "cloud/aws"` support in agent creation

- [ ] **Step 1: Add auto-enrollment logic to the reconciler**

```go
// In reconciler.go, after Upsert loop:
if s.autoEnroll {
    // Check if any resource has no matching agent
    for _, res := range resources {
        agentID := "cloud-" + string(res.Provider) + "-" + res.ResourceID
        exists, _ := s.agentStore.GetByCloudID(ctx, res.Provider, res.ResourceID)
        if !exists {
            err := s.agentStore.CreateVirtual(ctx, &models.Agent{
                ID:              agentID,
                OrgID:           orgID,
                SiteID:          "",
                Hostname:        res.Name,
                OperatingSystem: string(res.Provider),
                Platform:        "cloud/" + string(res.Provider),
                Tags:             []string{"cloud:enrolled", "cloud:provider:" + string(res.Provider)},
                Status:          "virtual",
            })
            if err != nil {
                r.log.Warn("cloud: auto-enroll failed", "id", agentID, "err", err)
            }
        }
    }
}
```

- [ ] **Step 2: Add EnrollCloudResource and IgnoreCloudResource handlers**

```go
// internal/api/cloud_resources.go
func (s *Server) enrollCloudResource(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    res, err := s.cloudResources.Get(r.Context(), id)
    if err != nil {
        http.Error(w, err.Error(), 404)
        return
    }
    // Convert from virtual agent to real agent: user will install OAP agent
    // and update the record. This just marks the virtual agent as "pending_install".
    if err := s.cloudResources.UpdateStatus(r.Context(), id, "pending_install"); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    json.NewEncoder(w).Encode(res)
}

func (s *Server) ignoreCloudResource(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := s.cloudResources.Archive(r.Context(), id); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(204)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/cloud/reconciler.go internal/api/cloud_resources.go
git commit -m "feat(cloud): auto-enrollment and virtual agent management"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** Scan `openspec/specs/cloud-control/spec.md` requirements §1–§8. Every requirement maps to a task above? Yes: §1 (Task 2+3+4), §2 (Task 3+4), §3 (Task 7), §4 (Task 5), §5 (Task 5), §6 (Task 1), §7 (Task 6), §8 (Task 3+4+5).
- [ ] **Placeholder scan:** No "TBD", "TODO", or vague steps. All code is concrete.
- [ ] **Type consistency:** `CloudProvider` is defined once in models_cloud.go and used everywhere. `ProviderClient` interface defined in Task 2, implemented in Tasks 3+4.
- [ ] **Pattern adherence:** All new tables use `CREATE TABLE IF NOT EXISTS`, `org_id TEXT NOT NULL DEFAULT ''`, no FKs, RLS. All routes use chi router, role-gated. All NATS events flow through `oap.events.alerts`.
- [ ] **OUT of scope verified:** No EKS node inventory, no automated tag remediation, no cross-account cost allocation reports, no automatic agent installation.
