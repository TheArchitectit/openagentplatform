package gate

import "context"

// Severity describes the impact of a finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Finding describes a problem found by a gate.
type Finding struct {
	Gate     string
	Path     string
	Line     int
	Column   int
	Severity Severity
	Message  string
	Rule     string
}

// Gate checks files for a class of problems.
type Gate interface {
	Name() string
	Check(ctx context.Context, paths []string) ([]Finding, error)
}
