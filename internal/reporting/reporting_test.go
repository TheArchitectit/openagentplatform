package reporting

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestReportService_GetTemplate(t *testing.T) {
	svc := NewReportService()

	tests := []struct {
		templateID string
		wantErr    bool
	}{
		{"compliance", false},
		{"patch_status", false},
		{"alert_history", false},
		{"endpoint_inventory", false},
		{"nonexistent", true},
	}

	for _, tt := range tests {
		t.Run(tt.templateID, func(t *testing.T) {
			tmpl, err := svc.GetTemplate(tt.templateID)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tmpl.ID != tt.templateID {
				t.Errorf("expected template ID %q, got %q", tt.templateID, tmpl.ID)
			}
		})
	}
}

func TestReportService_ListTemplates(t *testing.T) {
	svc := NewReportService()

	templates := svc.ListTemplates()
	if len(templates) != 4 {
		t.Errorf("expected 4 templates, got %d", len(templates))
	}
}

func TestReportService_GenerateReport(t *testing.T) {
	svc := NewReportService()

	period := ReportPeriod{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now(),
	}

	report, err := svc.GenerateReport(context.Background(), "tenant-1", "compliance", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.TenantID != "tenant-1" {
		t.Errorf("expected tenant ID 'tenant-1', got %q", report.TenantID)
	}
	if report.TemplateID != "compliance" {
		t.Errorf("expected template ID 'compliance', got %q", report.TemplateID)
	}
	if report.Format != ReportFormatCSV {
		t.Errorf("expected format 'csv', got %q", report.Format)
	}
	if report.Data == nil {
		t.Error("expected non-nil data")
	}
}

func TestReportService_GenerateReport_InvalidTemplate(t *testing.T) {
	svc := NewReportService()

	period := ReportPeriod{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now(),
	}

	_, err := svc.GenerateReport(context.Background(), "tenant-1", "nonexistent", period, ReportFormatCSV)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestReportService_GetReport(t *testing.T) {
	svc := NewReportService()

	period := ReportPeriod{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now(),
	}

	created, err := svc.GenerateReport(context.Background(), "tenant-1", "compliance", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report, err := svc.GetReport(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.ID != created.ID {
		t.Errorf("expected report ID %s, got %s", created.ID, report.ID)
	}
}

func TestReportService_GetReport_NotFound(t *testing.T) {
	svc := NewReportService()

	_, err := svc.GetReport("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent report")
	}
}

func TestReportService_ListReports(t *testing.T) {
	svc := NewReportService()

	period := ReportPeriod{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now(),
	}

	// Generate reports for different tenants
	_, err := svc.GenerateReport(context.Background(), "tenant-1", "compliance", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GenerateReport(context.Background(), "tenant-1", "patch_status", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GenerateReport(context.Background(), "tenant-2", "compliance", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reports := svc.ListReports("tenant-1")
	if len(reports) != 2 {
		t.Errorf("expected 2 reports for tenant-1, got %d", len(reports))
	}

	reports = svc.ListReports("tenant-2")
	if len(reports) != 1 {
		t.Errorf("expected 1 report for tenant-2, got %d", len(reports))
	}
}

func TestReportService_ExportCSV(t *testing.T) {
	svc := NewReportService()

	period := ReportPeriod{
		Start: time.Now().Add(-24 * time.Hour),
		End:   time.Now(),
	}

	report, err := svc.GenerateReport(context.Background(), "tenant-1", "compliance", period, ReportFormatCSV)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var buf bytes.Buffer
	err = svc.ExportCSV(report, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify CSV output
	output := buf.String()
	if output == "" {
		t.Error("expected non-empty CSV output")
	}

	// Should contain header
	if !bytes.Contains(buf.Bytes(), []byte("endpoint_id")) {
		t.Error("expected CSV to contain header 'endpoint_id'")
	}
}

func TestReportService_CreateSchedule(t *testing.T) {
	svc := NewReportService()

	schedule, err := svc.CreateSchedule("tenant-1", "compliance", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if schedule.TenantID != "tenant-1" {
		t.Errorf("expected tenant ID 'tenant-1', got %q", schedule.TenantID)
	}
	if schedule.TemplateID != "compliance" {
		t.Errorf("expected template ID 'compliance', got %q", schedule.TemplateID)
	}
	if schedule.Frequency != ScheduleWeekly {
		t.Errorf("expected frequency 'weekly', got %q", schedule.Frequency)
	}
	if schedule.Format != ReportFormatPDF {
		t.Errorf("expected format 'pdf', got %q", schedule.Format)
	}
	if !schedule.Enabled {
		t.Error("expected schedule to be enabled")
	}
	if len(schedule.Recipients) != 1 {
		t.Errorf("expected 1 recipient, got %d", len(schedule.Recipients))
	}
}

func TestReportService_CreateSchedule_InvalidTemplate(t *testing.T) {
	svc := NewReportService()

	_, err := svc.CreateSchedule("tenant-1", "nonexistent", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestReportService_GetSchedule(t *testing.T) {
	svc := NewReportService()

	created, err := svc.CreateSchedule("tenant-1", "compliance", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schedule, err := svc.GetSchedule(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if schedule.ID != created.ID {
		t.Errorf("expected schedule ID %s, got %s", created.ID, schedule.ID)
	}
}

func TestReportService_GetSchedule_NotFound(t *testing.T) {
	svc := NewReportService()

	_, err := svc.GetSchedule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent schedule")
	}
}

func TestReportService_ListSchedules(t *testing.T) {
	svc := NewReportService()

	_, err := svc.CreateSchedule("tenant-1", "compliance", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.CreateSchedule("tenant-1", "patch_status", ScheduleDaily, ReportFormatCSV, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.CreateSchedule("tenant-2", "compliance", ScheduleMonthly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schedules := svc.ListSchedules("tenant-1")
	if len(schedules) != 2 {
		t.Errorf("expected 2 schedules for tenant-1, got %d", len(schedules))
	}

	schedules = svc.ListSchedules("tenant-2")
	if len(schedules) != 1 {
		t.Errorf("expected 1 schedule for tenant-2, got %d", len(schedules))
	}
}

func TestReportService_DeleteSchedule(t *testing.T) {
	svc := NewReportService()

	created, err := svc.CreateSchedule("tenant-1", "compliance", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.DeleteSchedule(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetSchedule(created.ID)
	if err == nil {
		t.Error("expected error for deleted schedule")
	}
}

func TestReportService_DeleteSchedule_NotFound(t *testing.T) {
	svc := NewReportService()

	err := svc.DeleteSchedule("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent schedule")
	}
}

func TestReportService_EnableDisableSchedule(t *testing.T) {
	svc := NewReportService()

	created, err := svc.CreateSchedule("tenant-1", "compliance", ScheduleWeekly, ReportFormatPDF, []string{"admin@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Disable
	err = svc.DisableSchedule(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schedule, _ := svc.GetSchedule(created.ID)
	if schedule.Enabled {
		t.Error("expected schedule to be disabled")
	}

	// Enable
	err = svc.EnableSchedule(created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schedule, _ = svc.GetSchedule(created.ID)
	if !schedule.Enabled {
		t.Error("expected schedule to be enabled")
	}
}

func TestReportType_Constants(t *testing.T) {
	tests := []struct {
		reportType ReportType
		expected   string
	}{
		{ReportTypeCompliance, "compliance"},
		{ReportTypePatchStatus, "patch_status"},
		{ReportTypeAlertHistory, "alert_history"},
		{ReportTypeEndpointInventory, "endpoint_inventory"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.reportType) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.reportType))
			}
		})
	}
}

func TestReportFormat_Constants(t *testing.T) {
	tests := []struct {
		format   ReportFormat
		expected string
	}{
		{ReportFormatPDF, "pdf"},
		{ReportFormatCSV, "csv"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.format) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.format))
			}
		})
	}
}

func TestScheduleFrequency_Constants(t *testing.T) {
	tests := []struct {
		frequency ScheduleFrequency
		expected  string
	}{
		{ScheduleDaily, "daily"},
		{ScheduleWeekly, "weekly"},
		{ScheduleMonthly, "monthly"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.frequency) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.frequency))
			}
		})
	}
}
