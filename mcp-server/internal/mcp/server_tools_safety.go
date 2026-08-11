package mcp

import "github.com/mark3labs/mcp-go/mcp"

// safetyTools returns the safety/uncertainty/halt tool definitions.
func (s *MCPServer) safetyTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "guardrail_check_uncertainty",
			Description: "Force self-reflection when confidence in next step is low",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"current_plan": map[string]interface{}{
						"type":        "string",
						"description": "Description of the current plan",
					},
					"uncertainty_reason": map[string]interface{}{
						"type":        "string",
						"description": "Reason for uncertainty",
					},
				},
				Required: []string{"current_plan", "uncertainty_reason"},
			},
		},
		{
			Name:        "guardrail_check_halt_conditions",
			Description: "Evaluate if current state requires manual human escalation",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Current system/task status",
					},
				},
			},
		},
		{
			Name:        "guardrail_record_halt",
			Description: "Record a system-forced halt event",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Reason for the halt",
					},
				},
				Required: []string{"reason"},
			},
		},
		{
			Name:        "guardrail_acknowledge_halt",
			Description: "Acknowledged a previously recorded halt to resume operation",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"halt_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the halt being acknowledged",
					},
				},
				Required: []string{"halt_id"},
			},
		},
		{
			Name:        "guardrail_validate_production_first",
			Description: "Ensure production changes are prioritized or isolated correctly",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path being modified",
					},
				},
			},
		},
		{
			Name:        "guardrail_detect_feature_creep",
			Description: "Analyze if changes exceed original task scope",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Original task identifier",
					},
					"current_changes": map[string]interface{}{
						"type":        "string",
						"description": "Diff or summary of changes so far",
					},
				},
				Required: []string{"task_id", "current_changes"},
			},
		},
		{
			Name:        "guardrail_verify_fixes_intact",
			Description: "Ensure recent bugfixes haven't been regressed by new edits",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: mcp.ToolInputSchemaProperties{
					"bug_id": map[string]interface{}{
						"type":        "string",
						"description": "Known bug ID or description",
					},
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "File to check",
					},
				},
				Required: []string{"bug_id", "file_path"},
			},
		},
	}
}
