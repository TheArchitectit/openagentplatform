package reporting

import (
	"encoding/csv"
	"fmt"
	"io"
)

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
