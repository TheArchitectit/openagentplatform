package gates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openagentplatform/openagentplatform/gate"
)

// SchemaScan validates JSON and basic YAML structure.
type SchemaScan struct{}

// NewSchemaScan creates a schema validation gate.
func NewSchemaScan() *SchemaScan { return &SchemaScan{} }

func (s *SchemaScan) Name() string { return "schema" }

// Check validates supported schema files.
func (s *SchemaScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	files, err := expandPaths(paths)
	if err != nil {
		return nil, err
	}
	var findings []gate.Finding
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return findings, readErr
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json":
			var document any
			if unmarshalErr := json.Unmarshal(content, &document); unmarshalErr != nil {
				line, column := jsonErrorPosition(content, unmarshalErr)
				findings = append(findings, gate.Finding{
					Gate: s.Name(), Path: path, Line: line, Column: column,
					Severity: gate.SeverityError, Message: unmarshalErr.Error(), Rule: "invalid-json",
				})
			}
		case ".yaml", ".yml":
			findings = append(findings, validateYAML(s.Name(), path, string(content))...)
		}
	}
	return findings, nil
}

func jsonErrorPosition(content []byte, err error) (int, int) {
	// Try *json.SyntaxError first (has byte offset).
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		offset := syntaxErr.Offset
		line, column := 1, 1
		for i, value := range content {
			if int64(i+1) >= offset {
				break
			}
			if value == '\n' {
				line++
				column = 1
			} else {
				column++
			}
		}
		return line, column
	}
	// Try *json.UnmarshalTypeError (has Offset since Go 1.21).
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Offset > 0 {
		line, column := 1, 1
		for i, value := range content {
			if int64(i+1) >= typeErr.Offset {
				break
			}
			if value == '\n' {
				line++
				column = 1
			} else {
				column++
			}
		}
		return line, column
	}
	return 1, 1
}

func validateYAML(name, path, content string) []gate.Finding {
	var findings []gate.Finding
	indentStack := []int{0}
	for index, raw := range strings.Split(content, "\n") {
		line := index + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" || trimmed == "..." {
			continue
		}
		if strings.Contains(raw, "\t") {
			findings = append(findings, schemaFinding(name, path, line, strings.IndexByte(raw, '\t')+1, "tabs are not valid YAML indentation", "yaml-tab"))
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if indent > indentStack[len(indentStack)-1] {
			indentStack = append(indentStack, indent)
		} else {
			for len(indentStack) > 1 && indent < indentStack[len(indentStack)-1] {
				indentStack = indentStack[:len(indentStack)-1]
			}
			if indent != indentStack[len(indentStack)-1] {
				findings = append(findings, schemaFinding(name, path, line, 1, "inconsistent YAML indentation", "yaml-indent"))
			}
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if value == "" {
			continue
		}
		colon := strings.Index(value, ":")
		if colon < 1 {
			// Scalar list items (e.g. "- apple") have no colon and are valid YAML.
			// Only flag it as an error if this line is NOT a list item (no leading '-').
			if !strings.HasPrefix(trimmed, "-") {
				findings = append(findings, schemaFinding(name, path, line, indent+1, "expected YAML mapping key and colon", "yaml-structure"))
			}
			continue
		}
		key := strings.TrimSpace(value[:colon])
		if key == "" {
			findings = append(findings, schemaFinding(name, path, line, indent+1, "empty YAML mapping key", "yaml-key"))
		}
		if strings.HasPrefix(key, "[") || strings.HasPrefix(key, "{") {
			findings = append(findings, schemaFinding(name, path, line, indent+1, fmt.Sprintf("unsupported complex YAML key %s", strconv.Quote(key)), "yaml-key"))
		}
	}
	return findings
}

func schemaFinding(name, path string, line, column int, message, rule string) gate.Finding {
	return gate.Finding{Gate: name, Path: path, Line: line, Column: column, Severity: gate.SeverityError, Message: message, Rule: rule}
}
