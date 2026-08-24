package reporting

import (
	"testing"
)

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
