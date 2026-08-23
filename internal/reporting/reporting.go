// Package reporting implements enterprise reporting for OpenAgentPlatform.
//
// Features:
// - Report templates: compliance, patch status, alert history, endpoint inventory
// - Scheduled report delivery (daily, weekly, monthly)
// - Tenant-scoped reports respecting RBAC
// - Export formats: PDF and CSV
// - Report generation service
package reporting

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"
)

// ReportType represents the type of report.
type ReportType string

const (
	ReportTypeCompliance    ReportType = "compliance"
	ReportTypePatchStatus   ReportType = "patch_status"
	ReportTypeAlertHistory  ReportType = "alert_history"
	ReportTypeEndpointInventory ReportType = "endpoint_inventory"
)

// ReportFormat represents the export format.
type ReportFormat string

const (
	ReportFormatPDF ReportFormat = "pdf"
	ReportFormatCSV ReportFormat = "csv"
)

// ScheduleFrequency represents how often a report is generated.
type ScheduleFrequency string

const (
	ScheduleDaily   ScheduleFrequency = "daily"
	ScheduleWeekly  ScheduleFrequency = "weekly"
	ScheduleMonthly ScheduleFrequency = "monthly"
)

// ReportTemplate defines a report template.
type ReportTemplate struct {
	// ID is the template identifier.
	ID string `json:"id"`
	// Name is the template name.
	Name string `json:"name"`
	// Type is the report type.
	Type ReportType `json:"type"`
	// Description describes what the report contains.
	Description string `json:"description"`
	// Columns defines the report columns.
	Columns []string `json:"columns"`
}

// Report represents a generated report.
type Report struct {
	// ID is the report identifier.
	ID string `json:"id"`
	// TemplateID is the template used to generate this report.
	TemplateID string `json:"template_id"`
	// TenantID is the tenant this report belongs to.
	TenantID string `json:"tenant_id"`
	// Format is the export format.
	Format ReportFormat `json:"format"`
	// GeneratedAt is when the report was generated.
	GeneratedAt time.Time `json:"generated_at"`
	// Period is the time period covered by the report.
	Period ReportPeriod `json:"period"`
	// Data contains the report data.
	Data interface{} `json:"data"`
}

// ReportPeriod represents the time period for a report.
type ReportPeriod struct {
	// Start is the period start time.
	Start time.Time `json:"start"`
	// End is the period end time.
	End time.Time `json:"end"`
}

// ScheduledReport represents a scheduled report configuration.
type ScheduledReport struct {
	// ID is the schedule identifier.
	ID string `json:"id"`
	// TemplateID is the report template to generate.
	TemplateID string `json:"template_id"`
	// TenantID is the tenant this schedule belongs to.
	TenantID string `json:"tenant_id"`
	// Frequency is how often to generate the report.
	Frequency ScheduleFrequency `json:"frequency"`
	// Format is the export format.
	Format ReportFormat `json:"format"`
	// Recipients is the list of email recipients.
	Recipients []string `json:"recipients"`
	// NextRun is when the next report will be generated.
	NextRun time.Time `json:"next_run"`
	// Enabled indicates if the schedule is active.
	Enabled bool `json:"enabled"`
}

// ReportService manages report generation and scheduling.
type ReportService struct {
	templates map[string]*ReportTemplate
	reports   map[string]*Report
	schedules map[string]*ScheduledReport
}

// NewReportService creates a new report service.
func NewReportService() *ReportService {
	svc := &ReportService{
		templates: make(map[string]*ReportTemplate),
		reports:   make(map[string]*Report),
		schedules: make(map[string]*ScheduledReport),
	}
	svc.registerDefaultTemplates()
	return svc
}

// registerDefaultTemplates registers the built-in report templates.
func (s *ReportService) registerDefaultTemplates() {
	s.templates[string(ReportTypeCompliance)] = &ReportTemplate{
		ID:          string(ReportTypeCompliance),
		Name:        "Compliance Report",
		Type:        ReportTypeCompliance,
		Description: "Compliance status across all endpoints",
		Columns:     []string{"endpoint_id", "endpoint_name", "compliance_status", "last_check", "issues"},
	}

	s.templates[string(ReportTypePatchStatus)] = &ReportTemplate{
		ID:          string(ReportTypePatchStatus),
		Name:        "Patch Status Report",
		Type:        ReportTypePatchStatus,
		Description: "Patch deployment status and pending updates",
		Columns:     []string{"endpoint_id", "endpoint_name", "patches_installed", "patches_pending", "last_patch_date"},
	}

	s.templates[string(ReportTypeAlertHistory)] = &ReportTemplate{
		ID:          string(ReportTypeAlertHistory),
		Name:        "Alert History Report",
		Type:        ReportTypeAlertHistory,
		Description: "Historical alert data and resolution times",
		Columns:     []string{"alert_id", "severity", "source", "triggered_at", "resolved_at", "resolution_time"},
	}

	s.templates[string(ReportTypeEndpointInventory)] = &ReportTemplate{
		ID:          string(ReportTypeEndpointInventory),
		Name:        "Endpoint Inventory Report",
		Type:        ReportTypeEndpointInventory,
		Description: "Complete inventory of all managed endpoints",
		Columns:     []string{"endpoint_id", "name", "os", "ip_address", "status", "last_seen", "agent_version"},
	}
}

// GetTemplate retrieves a report template by ID.
func (s *ReportService) GetTemplate(templateID string) (*ReportTemplate, error) {
	tmpl, ok := s.templates[templateID]
	if !ok {
		return nil, fmt.Errorf("template %q not found", templateID)
	}
	return tmpl, nil
}

// ListTemplates returns all available report templates.
func (s *ReportService) ListTemplates() []*ReportTemplate {
	templates := make([]*ReportTemplate, 0, len(s.templates))
	for _, tmpl := range s.templates {
		templates = append(templates, tmpl)
	}
	return templates
}

// GenerateReport generates a report for the given tenant and time period.
func (s *ReportService) GenerateReport(ctx context.Context, tenantID, templateID string, period ReportPeriod, format ReportFormat) (*Report, error) {
	// Validate template
	tmpl, err := s.GetTemplate(templateID)
	if err != nil {
		return nil, err
	}

	// Generate report ID
	reportID := fmt.Sprintf("rpt_%s_%s_%d", tenantID, templateID, time.Now().UnixNano())

	// Create report
	report := &Report{
		ID:          reportID,
		TemplateID:  tmpl.ID,
		TenantID:    tenantID,
		Format:      format,
		GeneratedAt: time.Now().UTC(),
		Period:      period,
		Data:        s.generateReportData(ctx, tenantID, tmpl, period),
	}

	s.reports[reportID] = report
	return report, nil
}

// generateReportData generates the report data based on template type.
func (s *ReportService) generateReportData(ctx context.Context, tenantID string, tmpl *ReportTemplate, period ReportPeriod) interface{} {
	// In production, this would query the database
	// For now, return placeholder data
	switch tmpl.Type {
	case ReportTypeCompliance:
		return []map[string]interface{}{
			{"endpoint_id": "ep-1", "endpoint_name": "Server 1", "compliance_status": "compliant", "last_check": time.Now().Add(-1 * time.Hour), "issues": 0},
			{"endpoint_id": "ep-2", "endpoint_name": "Server 2", "compliance_status": "non_compliant", "last_check": time.Now().Add(-2 * time.Hour), "issues": 3},
		}
	case ReportTypePatchStatus:
		return []map[string]interface{}{
			{"endpoint_id": "ep-1", "endpoint_name": "Server 1", "patches_installed": 45, "patches_pending": 2, "last_patch_date": time.Now().Add(-24 * time.Hour)},
			{"endpoint_id": "ep-2", "endpoint_name": "Server 2", "patches_installed": 38, "patches_pending": 7, "last_patch_date": time.Now().Add(-48 * time.Hour)},
		}
	case ReportTypeAlertHistory:
		return []map[string]interface{}{
			{"alert_id": "alert-1", "severity": "high", "source": "CPU Check", "triggered_at": time.Now().Add(-4 * time.Hour), "resolved_at": time.Now().Add(-3 * time.Hour), "resolution_time": "1h"},
			{"alert_id": "alert-2", "severity": "medium", "source": "Disk Check", "triggered_at": time.Now().Add(-24 * time.Hour), "resolved_at": time.Now().Add(-23 * time.Hour), "resolution_time": "1h"},
		}
	case ReportTypeEndpointInventory:
		return []map[string]interface{}{
			{"endpoint_id": "ep-1", "name": "Server 1", "os": "Ubuntu 22.04", "ip_address": "10.0.1.10", "status": "online", "last_seen": time.Now().Add(-5 * time.Minute), "agent_version": "1.0.0"},
			{"endpoint_id": "ep-2", "name": "Server 2", "os": "Windows Server 2022", "ip_address": "10.0.1.11", "status": "online", "last_seen": time.Now().Add(-10 * time.Minute), "agent_version": "1.0.0"},
		}
	default:
		return nil
	}
}

// GetReport retrieves a generated report by ID.
func (s *ReportService) GetReport(reportID string) (*Report, error) {
	report, ok := s.reports[reportID]
	if !ok {
		return nil, fmt.Errorf("report %q not found", reportID)
	}
	return report, nil
}

// ListReports returns all reports for a tenant.
func (s *ReportService) ListReports(tenantID string) []*Report {
	var reports []*Report
	for _, report := range s.reports {
		if report.TenantID == tenantID {
			reports = append(reports, report)
		}
	}
	return reports
}

// ExportCSV exports a report as CSV.
func (s *ReportService) ExportCSV(report *Report, w io.Writer) error {
	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	// Write header based on template
	tmpl, err := s.GetTemplate(report.TemplateID)
	if err != nil {
		return err
	}

	if err := csvWriter.Write(tmpl.Columns); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write data rows
	data, ok := report.Data.([]map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid report data format")
	}

	for _, row := range data {
		record := make([]string, len(tmpl.Columns))
		for i, col := range tmpl.Columns {
			if val, ok := row[col]; ok {
				record[i] = fmt.Sprintf("%v", val)
			}
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	return nil
}

// CreateSchedule creates a scheduled report.
func (s *ReportService) CreateSchedule(tenantID, templateID string, frequency ScheduleFrequency, format ReportFormat, recipients []string) (*ScheduledReport, error) {
	// Validate template
	if _, err := s.GetTemplate(templateID); err != nil {
		return nil, err
	}

	// Calculate next run time
	nextRun := s.calculateNextRun(frequency)

	schedule := &ScheduledReport{
		ID:         fmt.Sprintf("sched_%s_%s_%d", tenantID, templateID, time.Now().UnixNano()),
		TemplateID: templateID,
		TenantID:   tenantID,
		Frequency:  frequency,
		Format:     format,
		Recipients: recipients,
		NextRun:    nextRun,
		Enabled:    true,
	}

	s.schedules[schedule.ID] = schedule
	return schedule, nil
}

// calculateNextRun calculates the next run time based on frequency.
func (s *ReportService) calculateNextRun(frequency ScheduleFrequency) time.Time {
	now := time.Now().UTC()
	switch frequency {
	case ScheduleDaily:
		// Next day at 8 AM
		next := now.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 8, 0, 0, 0, time.UTC)
	case ScheduleWeekly:
		// Next Monday at 8 AM
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		next := now.AddDate(0, 0, daysUntilMonday)
		return time.Date(next.Year(), next.Month(), next.Day(), 8, 0, 0, 0, time.UTC)
	case ScheduleMonthly:
		// First day of next month at 8 AM
		next := now.AddDate(0, 1, 0)
		return time.Date(next.Year(), next.Month(), 1, 8, 0, 0, 0, time.UTC)
	default:
		return now.Add(24 * time.Hour)
	}
}

// GetSchedule retrieves a schedule by ID.
func (s *ReportService) GetSchedule(scheduleID string) (*ScheduledReport, error) {
	schedule, ok := s.schedules[scheduleID]
	if !ok {
		return nil, fmt.Errorf("schedule %q not found", scheduleID)
	}
	return schedule, nil
}

// ListSchedules returns all schedules for a tenant.
func (s *ReportService) ListSchedules(tenantID string) []*ScheduledReport {
	var schedules []*ScheduledReport
	for _, schedule := range s.schedules {
		if schedule.TenantID == tenantID {
			schedules = append(schedules, schedule)
		}
	}
	return schedules
}

// DeleteSchedule deletes a schedule.
func (s *ReportService) DeleteSchedule(scheduleID string) error {
	if _, ok := s.schedules[scheduleID]; !ok {
		return fmt.Errorf("schedule %q not found", scheduleID)
	}
	delete(s.schedules, scheduleID)
	return nil
}

// EnableSchedule enables a schedule.
func (s *ReportService) EnableSchedule(scheduleID string) error {
	schedule, err := s.GetSchedule(scheduleID)
	if err != nil {
		return err
	}
	schedule.Enabled = true
	return nil
}

// DisableSchedule disables a schedule.
func (s *ReportService) DisableSchedule(scheduleID string) error {
	schedule, err := s.GetSchedule(scheduleID)
	if err != nil {
		return err
	}
	schedule.Enabled = false
	return nil
}
