package gates

import (
	"context"
	"regexp"
	"strings"

	"github.com/openagentplatform/openagentplatform/gate"
)

type secretPattern struct {
	rule    string
	message string
	re      *regexp.Regexp
}

// SecretScan detects common hardcoded credentials.
type SecretScan struct {
	patterns []secretPattern
}

// NewSecretScan creates a secret scanning gate.
func NewSecretScan() *SecretScan {
	return &SecretScan{patterns: []secretPattern{
		{rule: "aws-access-key", message: "AWS access key detected", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
		{rule: "github-token", message: "GitHub token detected", re: regexp.MustCompile(`\bgh[opusr]_[A-Za-z0-9]{30,255}\b`)},
		{rule: "private-key", message: "private key detected", re: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
		{rule: "assigned-secret", message: "hardcoded credential detected", re: regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|password|passwd)\b\s*[:=]\s*["'][^"'\s]{8,}["']`)},
	}}
}

func (s *SecretScan) Name() string { return "secret-scan" }

// Check scans text files for secret patterns.
func (s *SecretScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	var findings []gate.Finding
	err := walkLines(ctx, paths, func(line sourceLine) {
		if isSecretPlaceholder(line.text) {
			return
		}
		for _, pattern := range s.patterns {
			location := pattern.re.FindStringIndex(line.text)
			if location == nil {
				continue
			}
			findings = append(findings, gate.Finding{
				Gate: s.Name(), Path: line.path, Line: line.number, Column: location[0] + 1,
				Severity: gate.SeverityCritical, Message: pattern.message, Rule: pattern.rule,
			})
		}
	})
	return findings, err
}

func isSecretPlaceholder(line string) bool {
	lower := strings.ToLower(line)
	placeholders := []string{"example", "placeholder", "changeme", "your_", "your-", "<token>", "${", "os.getenv", "os.environ", "getenv("}
	for _, placeholder := range placeholders {
		if strings.Contains(lower, placeholder) {
			return true
		}
	}
	return false
}
