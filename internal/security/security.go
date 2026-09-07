// Package security implements active security/EDR ingest (a2a-security spec):
// webhook receivers for CrowdStrike/Defender/SentinelOne, reconciliation
// polling, dedup, edr→agent correlation, and SIEM forwarders (Splunk/ELK/
// generic CEF/LEEF).
package security

import (
	"context"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// EDRProvider parses vendor-specific EDR payloads (webhook + API pull).
type EDRProvider interface {
	Name() models.EDRProviderType

	// VerifyWebhookSignature returns true if the webhook payload was
	// signed by the configured integration secret.
	VerifyWebhookSignature(payload []byte, signature string) bool

	// ParseWebhookEvent converts a vendor-specific webhook payload to a
	// normalized SecurityEvent. IngestionMethod is set to "webhook".
	ParseWebhookEvent(payload []byte) (*models.SecurityEvent, error)

	// PullRecentEvents fetches events from the vendor API since the
	// given time. Used for reconciliation (catches anything missed
	// during webhook outages).
	PullRecentEvents(ctx context.Context, since time.Time) ([]*models.SecurityEvent, error)
}

// SIEMForwarder ships a batch of security events to a SIEM endpoint.
type SIEMForwarder interface {
	Name() models.SIEMType

	// BatchSize returns the maximum number of events per flush.
	BatchSize() int

	// Flush sends a batch of events to the SIEM. A non-nil error
	// indicates the batch must be retried.
	Flush(ctx context.Context, events []*models.SecurityEvent) error
}
