package gates

import (
	"context"
	"regexp"
	"strings"

	"github.com/openagentplatform/openagentplatform/gate"
)

// PatternScan reports unfinished-work markers in production source files.
type PatternScan struct {
	marker *regexp.Regexp
}

// NewPatternScan creates a problematic pattern gate.
func NewPatternScan() *PatternScan {
	return &PatternScan{marker: regexp.MustCompile(`(?i)\b(TODO|FIXME|HACK)\b`)}
}

func (s *PatternScan) Name() string { return "pattern-scan" }

// Check scans production source lines for TODO, FIXME, and HACK markers.
func (s *PatternScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	var findings []gate.Finding
	err := walkLines(ctx, paths, func(line sourceLine) {
		match := s.marker.FindStringSubmatchIndex(line.text)
		if match == nil {
			return
		}
		marker := strings.ToUpper(line.text[match[2]:match[3]])
		findings = append(findings, gate.Finding{
			Gate: s.Name(), Path: line.path, Line: line.number, Column: match[0] + 1,
			Severity: gate.SeverityError, Message: marker + " marker in production code", Rule: strings.ToLower(marker),
		})
	})
	return findings, err
}
