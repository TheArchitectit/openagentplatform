package gates

import (
	"context"
	"regexp"

	"github.com/openagentplatform/openagentplatform/gate"
)

// RegressionPattern describes a known source pattern that must not return.
type RegressionPattern struct {
	ID       string
	Message  string
	Severity gate.Severity
	Pattern  string
}

type compiledRegression struct {
	RegressionPattern
	re *regexp.Regexp
}

// RegressionScan checks source files against known regression signatures.
type RegressionScan struct {
	patterns []compiledRegression
}

// NewRegressionScan compiles the supplied known regression patterns.
func NewRegressionScan(patterns []RegressionPattern) (*RegressionScan, error) {
	scan := &RegressionScan{patterns: make([]compiledRegression, 0, len(patterns))}
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern.Pattern)
		if err != nil {
			return nil, err
		}
		if pattern.Severity == "" {
			pattern.Severity = gate.SeverityError
		}
		scan.patterns = append(scan.patterns, compiledRegression{RegressionPattern: pattern, re: re})
	}
	return scan, nil
}

func (s *RegressionScan) Name() string { return "regression" }

// Check reports every known regression signature.
func (s *RegressionScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	var findings []gate.Finding
	err := walkLines(ctx, paths, func(line sourceLine) {
		for _, pattern := range s.patterns {
			location := pattern.re.FindStringIndex(line.text)
			if location == nil {
				continue
			}
			findings = append(findings, gate.Finding{
				Gate: s.Name(), Path: line.path, Line: line.number, Column: location[0] + 1,
				Severity: pattern.Severity, Message: pattern.Message, Rule: pattern.ID,
			})
		}
	})
	return findings, err
}
