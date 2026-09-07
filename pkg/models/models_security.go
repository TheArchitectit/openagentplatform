package models

import "time"

type EDRProviderType string

const (
	EDRCrowdStrike EDRProviderType = "crowdstrike"
	EDRDefender    EDRProviderType = "defender"
	EDRSentinelOne EDRProviderType = "sentinelone"
)

type EDRIntegration struct {
	ID                  string          `json:"id"`
	OrgID               string          `json:"org_id"`
	Provider            EDRProviderType `json:"provider"`
	Name                string          `json:"name"`
	CredentialRef       string          `json:"credential_ref"`
	WebhookSecret       string          `json:"webhook_secret,omitempty"`
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	Enabled             bool            `json:"enabled"`
	LastPollAt          *time.Time      `json:"last_poll_at,omitempty"`
	LastEventAt         *time.Time      `json:"last_event_at,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type EDRAgentMapping struct {
	ID        string          `json:"id"`
	OrgID     string          `json:"org_id"`
	Provider  EDRProviderType `json:"provider"`
	EDRHostID string          `json:"edr_host_id"`
	AgentID   string          `json:"agent_id"`
	Hostname  string          `json:"hostname"`
	LastSeen  time.Time       `json:"last_seen"`
}

type SecurityEvent struct {
	ID              string                 `json:"id"`
	OrgID           string                 `json:"org_id"`
	Provider        EDRProviderType        `json:"provider"`
	ProviderEventID string                 `json:"provider_event_id"`
	AgentID         string                 `json:"agent_id,omitempty"`
	Severity        string                 `json:"severity"`
	Tactic          string                 `json:"tactic,omitempty"`
	Technique       string                 `json:"technique,omitempty"`
	DetectionType   string                 `json:"detection_type,omitempty"`
	Payload         map[string]interface{} `json:"payload"`
	OccurredAt      time.Time              `json:"occurred_at"`
	IngestedAt      time.Time              `json:"ingested_at"`
	IngestionMethod string                 `json:"ingestion_method"`
}

type SIEMType string

const (
	SIEMSplunk  SIEMType = "splunk"
	SIEMElastic SIEMType = "elastic"
	SIEMGeneric SIEMType = "generic"
)

type SIEMForwarder struct {
	ID                   string    `json:"id"`
	OrgID                string    `json:"org_id"`
	Name                 string    `json:"name"`
	SIEMType             SIEMType  `json:"siem_type"`
	Endpoint             string    `json:"endpoint"`
	CredentialRef        string    `json:"credential_ref"`
	BatchSize            int       `json:"batch_size"`
	BatchIntervalSeconds int       `json:"batch_interval_seconds"`
	Enabled              bool      `json:"enabled"`
	LastFlushAt          *time.Time `json:"last_flush_at,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
