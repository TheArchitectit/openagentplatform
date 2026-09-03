package models

import "time"

type CloudProvider string

const (
	CloudProviderAWS   CloudProvider = "aws"
	CloudProviderAzure CloudProvider = "azure"
	CloudProviderGCP   CloudProvider = "gcp"
)

type CloudAccount struct {
	ID          string        `json:"id"`
	OrgID       string        `json:"org_id"`
	Provider    CloudProvider `json:"provider"`
	AccountID   string        `json:"account_id"`
	DisplayName string        `json:"display_name"`
	IsMSPHub    bool          `json:"is_msp_hub"`
	Enabled     bool          `json:"enabled"`
	LastSeen    time.Time     `json:"last_seen"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type CloudResource struct {
	ID           string            `json:"id"`
	OrgID        string            `json:"org_id"`
	Provider     CloudProvider     `json:"provider"`
	AccountID    string            `json:"account_id"`
	ResourceID   string            `json:"resource_id"`
	ResourceType string            `json:"resource_type"`
	Region       string            `json:"region"`
	Name         string            `json:"name"`
	Status       string            `json:"status"`
	Tags         map[string]string `json:"tags"`
	ArchivedAt   *time.Time        `json:"archived_at,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type CloudPolicy struct {
	ID           string              `json:"id"`
	OrgID        string              `json:"org_id"`
	Provider     CloudProvider       `json:"provider"`
	AccountID    string              `json:"account_id"`
	RequiredTags []string            `json:"required_tags"`
	TagRules     map[string][]string `json:"tag_rules"`
	Enabled      bool                `json:"enabled"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
}

type CostSnapshot struct {
	ID            string             `json:"id"`
	OrgID         string             `json:"org_id"`
	Provider      CloudProvider      `json:"provider"`
	AccountID     string             `json:"account_id"`
	BillingPeriod string             `json:"billing_period"`
	TotalCostUSD  float64            `json:"total_cost_usd"`
	ServiceCosts  map[string]float64 `json:"service_costs"`
	CreatedAt     time.Time          `json:"created_at"`
}
