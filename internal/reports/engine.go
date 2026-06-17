// Package reports - engine.go implements the report generation engine.
// It defines the Report struct, 7 built-in templates, and the
// ReportEngine that aggregates data and produces formatted output.
package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// ReportFormat identifies the output format for a generated report.
type ReportFormat string

const (
	FormatJSON ReportFormat = "json"
	FormatCSV  ReportFormat = "csv"
	FormatPDF  ReportFormat = "pdf"
)

// DeliveryStatus tracks where a report has been delivered.
type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
	DeliveryDownload  DeliveryStatus = "download"
)

// Report is the result of a single report generation.
type Report struct {
	ID            string          `json:"id"`
	OrgID         string          `json:"org_id"`
	TemplateID    string          `json:"template_id"`
	Title         string          `json:"title"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Data          json.RawMessage `json:"data"`
	Format        ReportFormat    `json:"format"`
	DeliveryStatus DeliveryStatus `json:"delivery_status"`
}

// TemplateID values for the 7 built-in report types.
const (
	TemplateAgentInventory   = "agent_inventory"
	TemplateCheckCompliance  = "check_compliance"
	TemplateAlertSummary     = "alert_summary"
	TemplatePatchCompliance  = "patch_compliance"
	TemplateAuditTrail       = "audit_trail"
	TemplateUsageSummary     = "usage_summary"
	TemplateExecutiveSummary = "executive_summary"
)

// AllTemplates lists every supported template ID.
var AllTemplates = []string{
	TemplateAgentInventory,
	TemplateCheckCompliance,
	TemplateAlertSummary,
	TemplatePatchCompliance,
	TemplateAuditTrail,
	TemplateUsageSummary,
	TemplateExecutiveSummary,
}

// DataAggregator fetches the raw data needed to build a report.
// Implementations are typically thin wrappers around existing store
// or service interfaces (agents, checks, alerts, patches, etc.).
type DataAggregator interface {
	// AggregateAgentInventory returns per-agent summary data for the org.
	AggregateAgentInventory(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregateCheckCompliance returns check pass/fail summaries.
	AggregateCheckCompliance(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregateAlertSummary returns alert counts and severities.
	AggregateAlertSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregatePatchCompliance returns patch deployment status.
	AggregatePatchCompliance(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregateAuditTrail returns audit events for the given window.
	AggregateAuditTrail(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregateUsageSummary returns API call counts and billing metrics.
	AggregateUsageSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
	// AggregateExecutiveSummary returns a high-level roll-up.
	AggregateExecutiveSummary(ctx context.Context, orgID string, params map[string]string) (json.RawMessage, error)
}

// ReportEngine generates reports from templates and aggregated data.
type ReportEngine struct {
	aggregator DataAggregator
	log        *slog.Logger
}

// NewReportEngine constructs a ReportEngine.
func NewReportEngine(agg DataAggregator, log *slog.Logger) *ReportEngine {
	if log == nil {
		log = slog.Default()
	}
	return &ReportEngine{aggregator: agg, log: log}
}

// GenerateReport produces a Report for the given template and params.
// The returned Report contains the aggregated data and a delivery
// status of DeliveryPending — the scheduler/delivery layer is
// responsible for actually transmitting the output.
func (e *ReportEngine) GenerateReport(ctx context.Context, orgID, templateID string, params map[string]string, format ReportFormat) (*Report, error) {
	if orgID == "" {
		return nil, fmt.Errorf("org_id is required")
	}
	if !validTemplate(templateID) {
		return nil, fmt.Errorf("unknown template: %s", templateID)
	}
	if format == "" {
		format = FormatJSON
	}

	data, err := e.aggregate(ctx, orgID, templateID, params)
	if err != nil {
		return nil, fmt.Errorf("aggregate: %w", err)
	}

	rpt := &Report{
		ID:             uuid.NewString(),
		OrgID:          orgID,
		TemplateID:     templateID,
		Title:          titleFor(templateID),
		GeneratedAt:    time.Now().UTC(),
		Data:           data,
		Format:         format,
		DeliveryStatus: DeliveryPending,
	}
	e.log.Info("report generated",
		"report_id", rpt.ID,
		"org_id", orgID,
		"template", templateID,
		"format", format,
	)
	return rpt, nil
}

// aggregate dispatches to the correct aggregator method.
func (e *ReportEngine) aggregate(ctx context.Context, orgID, templateID string, params map[string]string) (json.RawMessage, error) {
	switch templateID {
	case TemplateAgentInventory:
		return e.aggregator.AggregateAgentInventory(ctx, orgID, params)
	case TemplateCheckCompliance:
		return e.aggregator.AggregateCheckCompliance(ctx, orgID, params)
	case TemplateAlertSummary:
		return e.aggregator.AggregateAlertSummary(ctx, orgID, params)
	case TemplatePatchCompliance:
		return e.aggregator.AggregatePatchCompliance(ctx, orgID, params)
	case TemplateAuditTrail:
		return e.aggregator.AggregateAuditTrail(ctx, orgID, params)
	case TemplateUsageSummary:
		return e.aggregator.AggregateUsageSummary(ctx, orgID, params)
	case TemplateExecutiveSummary:
		return e.aggregator.AggregateExecutiveSummary(ctx, orgID, params)
	}
	return nil, fmt.Errorf("unsupported template: %s", templateID)
}

func validTemplate(id string) bool {
	return AllTemplatesContains(id)
}

// AllTemplatesContains reports whether id is one of the built-in
// template identifiers. Exported for use by API handlers.
func AllTemplatesContains(id string) bool {
	for _, t := range AllTemplates {
		if t == id {
			return true
		}
	}
	return false
}

func titleFor(templateID string) string {
	switch templateID {
	case TemplateAgentInventory:
		return "Agent Inventory Report"
	case TemplateCheckCompliance:
		return "Check Compliance Report"
	case TemplateAlertSummary:
		return "Alert Summary Report"
	case TemplatePatchCompliance:
		return "Patch Compliance Report"
	case TemplateAuditTrail:
		return "Audit Trail Report"
	case TemplateUsageSummary:
		return "Usage Summary Report"
	case TemplateExecutiveSummary:
		return "Executive Summary Report"
	}
	return "Report"
}
