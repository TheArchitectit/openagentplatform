package reporting

import "fmt"

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
