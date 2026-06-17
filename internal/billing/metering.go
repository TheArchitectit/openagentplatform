// Package billing — metering.go records per-organisation usage and
// reports it to Stripe meter events on an hourly cadence.
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/billing/meterevent"
)

// Supported metric names. These map 1:1 to Stripe meter event names
// configured in the Stripe dashboard.
const (
	MetricAgentCountDays = "agent_count_days"
	MetricA2ATaskCount   = "a2a_task_count"
	MetricAPICallCount   = "api_call_count"
)

// MeterReportInterval is the cadence at which pending usage is flushed
// to Stripe as billing meter events.
const MeterReportInterval = 1 * time.Hour

// UsageRecord is a single pending usage event awaiting flush.
type UsageRecord struct {
	OrgID    string
	Metric   string
	Quantity int64
	Recorded time.Time
}

// MeteringService aggregates usage records and reports them to Stripe.
type MeteringService struct {
	client *StripeClient
	log    *slog.Logger

	mu      sync.Mutex
	pending map[string][]UsageRecord // keyed by org ID
}

// NewMeteringService constructs a MeteringService.
func NewMeteringService(client *StripeClient, log *slog.Logger) *MeteringService {
	if log == nil {
		log = slog.Default()
	}
	return &MeteringService{
		client:  client,
		log:     log,
		pending: make(map[string][]UsageRecord),
	}
}

// RecordUsage accumulates a usage event for the given org/metric.
// The record is held in memory until the next hourly flush.
func (m *MeteringService) RecordUsage(orgID, metric string, quantity int64) error {
	switch metric {
	case MetricAgentCountDays, MetricA2ATaskCount, MetricAPICallCount:
		// supported
	default:
		return fmt.Errorf("unknown metric: %s", metric)
	}
	m.mu.Lock()
	m.pending[orgID] = append(m.pending[orgID], UsageRecord{
		OrgID:    orgID,
		Metric:   metric,
		Quantity: quantity,
		Recorded: time.Now().UTC(),
	})
	m.mu.Unlock()
	return nil
}

// Flush reports all pending usage records to Stripe and clears the
// in-memory queue on success. Stripe errors leave the records in the
// queue for the next flush.
func (m *MeteringService) Flush(ctx context.Context) error {
	m.mu.Lock()
	pending := m.pending
	m.pending = make(map[string][]UsageRecord)
	m.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}
	var firstErr error
	for orgID, records := range pending {
		// Aggregate by metric so we emit one event per (org, metric).
		agg := make(map[string]int64)
		for _, r := range records {
			agg[r.Metric] += r.Quantity
		}
		for metric, qty := range agg {
			params := &stripe.BillingMeterEventParams{
				EventName: stripe.String(metric),
				Payload: map[string]string{
					"oap_org_id": orgID,
					"value":      fmt.Sprintf("%d", qty),
				},
			}
			_, err := meterevent.New(params)
			if err != nil {
				m.log.Warn("billing meter event failed",
					"org_id", orgID,
					"metric", metric,
					"error", err.Error(),
				)
				// Re-queue for retry.
				m.mu.Lock()
				m.pending[orgID] = append(m.pending[orgID], UsageRecord{
					OrgID:    orgID,
					Metric:   metric,
					Quantity: qty,
					Recorded: time.Now().UTC(),
				})
				m.mu.Unlock()
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// StartFlushLoop launches a goroutine that calls Flush every
// MeterReportInterval. Cancel ctx to stop.
func (m *MeteringService) StartFlushLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(MeterReportInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.Flush(ctx); err != nil {
					m.log.Warn("billing flush returned errors",
						"error", err.Error(),
					)
				}
			}
		}
	}()
}

// UsageSummary is the response shape for GET /billing/usage.
type UsageSummary struct {
	OrgID  string           `json:"org_id"`
	Period string           `json:"period"` // YYYY-MM
	Counts map[string]int64 `json:"counts"`
}

// GetUsage returns the queued usage counts for the current month. This
// is a snapshot of in-memory data; persisted history lives in Stripe.
func (m *MeteringService) GetUsage(orgID string) UsageSummary {
	now := time.Now().UTC()
	period := fmt.Sprintf("%04d-%02d", now.Year(), now.Month())
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := make(map[string]int64)
	for _, r := range m.pending[orgID] {
		if r.Recorded.Format("2006-01") == period {
			counts[r.Metric] += r.Quantity
		}
	}
	return UsageSummary{
		OrgID:  orgID,
		Period: period,
		Counts: counts,
	}
}
