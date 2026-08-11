package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/thearchitectit/guardrail-mcp/internal/audit"
	"github.com/thearchitectit/guardrail-mcp/internal/cache"
	"github.com/thearchitectit/guardrail-mcp/internal/config"
	"github.com/thearchitectit/guardrail-mcp/internal/database"
	"github.com/thearchitectit/guardrail-mcp/internal/metrics"
	"github.com/thearchitectit/guardrail-mcp/internal/validation"
)

// MCPServer handles MCP protocol requests
type MCPServer struct {
	mcpServer   *server.MCPServer
	db          *database.DB
	cache       *cache.Cache
	metrics     *metrics.Metrics
	audit       *audit.AuditLogger
	validator   *validation.Engine
	config      *config.Config
	visionTools *VisionTools
}

// NewServer creates a new MCP server instance
func NewServer(db *database.DB, cache *cache.Cache, metrics *metrics.Metrics, audit *audit.AuditLogger, validator *validation.Engine, cfg *config.Config) *MCPServer {
	s := &MCPServer{
		mcpServer: server.NewMCPServer(
			"Guardrail Enforcement Server",
			cfg.Version,
			server.WithResourceCapabilities(true, true),
			server.WithLogging(),
		),
		db:        db,
		cache:     cache,
		metrics:   metrics,
		audit:     audit,
		validator: validator,
		config:    cfg,
	}

	// Initialize vision tools if configured
	if cfg.Vision.Enabled {
		s.visionTools = NewVisionTools(cfg)
	}

	s.setupHandlers()
	return s
}

func (s *MCPServer) setupHandlers() {
	// Register tools
	s.mcpServer.HandleListTools(func(ctx context.Context, cursor *string) (*mcp.ListToolsResult, error) {
		tools := append(s.sessionTools(), s.safetyTools()...)
		tools = append(tools, s.teamTools()...)

		if s.visionTools != nil {
			tools = append(tools, s.visionTools.visionToolList()...)
		}

		return &mcp.ListToolsResult{
			Tools: tools,
		}, nil
	})

	// Handle tool calls
	s.mcpServer.HandleCallTool(func(ctx context.Context, name string, arguments map[string]interface{}) (*mcp.CallToolResult, error) {
		// Try vision tools first if enabled
		if s.visionTools != nil {
			result, err := s.visionTools.dispatch(ctx, name, arguments)
			if err == nil {
				return result, nil
			}
		}
		return s.handleToolCall(ctx, name, arguments)
	})

	// Handle resource list requests
	s.mcpServer.HandleListResources(func(ctx context.Context, cursor *string) (*mcp.ListResourcesResult, error) {
		return &mcp.ListResourcesResult{
			Resources: []mcp.Resource{
				{
					URI:  "guardrail://config",
					Name: "Guardrail Configuration",
				},
				{
					URI:  "guardrail://stats",
					Name: "Guardrail Usage Stats",
				},
			},
		}, nil
	})

	// Handle resource read requests
	s.mcpServer.HandleReadResource(func(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
		if uri == "guardrail://config" {
			configJSON, _ := json.MarshalIndent(s.config, "", "  ")
			return &mcp.ReadResourceResult{
				Contents: []mcp.ResourceContent{
					{
						URI:      uri,
						MimeType: "application/json",
						Text:     string(configJSON),
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("resource not found: %s", uri)
	})
}
