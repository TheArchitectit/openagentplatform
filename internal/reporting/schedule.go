package reporting

import (
	"fmt"
	"time"
)

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
