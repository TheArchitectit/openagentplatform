package reporting

import "time"

// ReportType represents the type of report.
type ReportType string

const (
	ReportTypeCompliance        ReportType = "compliance"
	ReportTypePatchStatus       ReportType = "patch_status"
	ReportTypeAlertHistory      ReportType = "alert_history"
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
