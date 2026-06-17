// Package bridge - models.go defines Go structs that mirror the Python
// adapter service REST API contract. These types are used by the HTTP
// client (client.go) to serialize/deserialize requests and responses.
package bridge

import (
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Request types
// ============================================================

// InvokeRequest is the payload sent to POST /api/v1/adapters/invoke.
// It describes which adapter to invoke and the input message.
type InvokeRequest struct {
	// Adapter is the name of the adapter to invoke.
	Adapter string `json:"adapter"`

	// TaskID is the A2A task identifier (for correlation).
	TaskID string `json:"task_id,omitempty"`

	// ContextID groups related invocations.
	ContextID string `json:"context_id,omitempty"`

	// Message is the input message to send to the adapter.
	Message models.Message `json:"message"`

	// Metadata carries arbitrary key-value context (org_id, user_id, etc.).
	Metadata map[string]string `json:"metadata,omitempty"`

	// Stream indicates the caller wants streaming responses.
	Stream bool `json:"stream,omitempty"`
}

// StreamRequest is the payload sent to POST /api/v1/adapters/stream.
// It carries the same fields as InvokeRequest with stream=true.
type StreamRequest = InvokeRequest

// CancelRequest is the payload for POST /api/v1/adapters/{taskId}/cancel.
// The taskId is passed in the URL; the body may carry a reason.
type CancelRequest struct {
	// Reason is a human-readable explanation for the cancellation.
	Reason string `json:"reason,omitempty"`
}

// CostUsageRequest is the query for GET /api/v1/cost/usage.
type CostUsageRequest struct {
	// OrgID filters usage by organization. Empty = all orgs.
	OrgID string `json:"org_id,omitempty"`

	// From is the start of the time window (inclusive).
	From time.Time `json:"from"`

	// To is the end of the time window (exclusive).
	To time.Time `json:"to"`
}

// ============================================================
// Response types
// ============================================================

// InvokeResponse is the response from POST /api/v1/adapters/invoke.
type InvokeResponse struct {
	// TaskID is the adapter-side task identifier.
	TaskID string `json:"task_id"`

	// Status is the final task status (completed, failed, etc.).
	Status string `json:"status"`

	// Messages contains the conversation produced by the adapter.
	Messages []models.Message `json:"messages,omitempty"`

	// Artifacts contains any output artifacts produced.
	Artifacts []models.Artifact `json:"artifacts,omitempty"`

	// Usage contains cost/token usage for this invocation.
	Usage *UsageRecord `json:"usage,omitempty"`

	// Error is set when the adapter failed.
	Error string `json:"error,omitempty"`
}

// StreamEvent is a single Server-Sent Event from a streaming invocation.
type StreamEvent struct {
	// Type is the event type (message, artifact, status, error, done).
	Type string `json:"type"`

	// TaskID is the adapter-side task identifier.
	TaskID string `json:"task_id"`

	// Status is set on status events.
	Status string `json:"status,omitempty"`

	// Message is set on message events.
	Message *models.Message `json:"message,omitempty"`

	// Artifact is set on artifact events.
	Artifact *models.Artifact `json:"artifact,omitempty"`

	// Error is set on error events.
	Error string `json:"error,omitempty"`

	// Usage is set on the final usage event.
	Usage *UsageRecord `json:"usage,omitempty"`
}

// UsageRecord tracks cost and token usage for a single invocation.
type UsageRecord struct {
	// InputTokens is the number of input/prompt tokens consumed.
	InputTokens int64 `json:"input_tokens"`

	// OutputTokens is the number of output/completion tokens consumed.
	OutputTokens int64 `json:"output_tokens"`

	// CostUSD is the estimated cost in US dollars.
	CostUSD float64 `json:"cost_usd"`

	// Model is the model identifier used (if applicable).
	Model string `json:"model,omitempty"`

	// Adapter is the adapter name that produced this usage.
	Adapter string `json:"adapter,omitempty"`
}

// UsageReport is the response from GET /api/v1/cost/usage.
type UsageReport struct {
	// OrgID is the organization filter applied.
	OrgID string `json:"org_id,omitempty"`

	// From is the start of the queried window.
	From time.Time `json:"from"`

	// To is the end of the queried window.
	To time.Time `json:"to"`

	// Records is the list of per-invocation usage records.
	Records []UsageRecord `json:"records"`

	// TotalInputTokens is the sum of all input tokens.
	TotalInputTokens int64 `json:"total_input_tokens"`

	// TotalOutputTokens is the sum of all output tokens.
	TotalOutputTokens int64 `json:"total_output_tokens"`

	// TotalCostUSD is the sum of all costs.
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// BudgetInfo describes a single cost budget.
type BudgetInfo struct {
	// OrgID is the organization this budget applies to.
	OrgID string `json:"org_id"`

	// LimitUSD is the budget cap in US dollars.
	LimitUSD float64 `json:"limit_usd"`

	// UsedUSD is the amount consumed so far.
	UsedUSD float64 `json:"used_usd"`

	// Period is the reset period (daily, monthly, etc.).
	Period string `json:"period"`

	// ResetAt is when the budget resets.
	ResetAt time.Time `json:"reset_at"`
}

// AdapterInfo describes a single adapter exposed by the Python service.
type AdapterInfo struct {
	// Name is the unique adapter identifier.
	Name string `json:"name"`

	// Framework is the underlying agent framework (langchain, crewai, etc.).
	Framework string `json:"framework"`

	// Description is a human-readable description of the adapter.
	Description string `json:"description,omitempty"`

	// Version is the adapter version string.
	Version string `json:"version"`

	// Capabilities lists the adapter's capabilities (streaming, etc.).
	Capabilities []string `json:"capabilities,omitempty"`

	// Tags lists the adapter's skill tags for routing.
	Tags []string `json:"tags,omitempty"`

	// Healthy indicates whether the adapter is currently healthy.
	Healthy bool `json:"healthy"`
}

// HealthStatus is the response from GET /api/v1/adapters/{name}/health.
type HealthStatus struct {
	// Name is the adapter name.
	Name string `json:"name"`

	// Status is one of "healthy", "degraded", "unhealthy".
	Status string `json:"status"`

	// Message is a human-readable status description.
	Message string `json:"message,omitempty"`

	// LastChecked is when the health check was performed.
	LastChecked time.Time `json:"last_checked"`

	// LatencyMs is the last observed latency in milliseconds.
	LatencyMs int64 `json:"latency_ms,omitempty"`
}

// ============================================================
// AgentCard conversion
// ============================================================

// AgentCardFromAdapter converts a Python AdapterInfo into an A2A AgentCard.
// The endpoint is set to the adapter name (used as a routing key in the registry).
func AgentCardFromAdapter(info *AdapterInfo) *models.AgentCard {
	if info == nil {
		return nil
	}
	tags := info.Tags
	if tags == nil {
		tags = []string{}
	}
	caps := info.Capabilities
	if caps == nil {
		caps = []string{}
	}
	card := &models.AgentCard{
		ID:           info.Name,
		Name:         info.Name,
		Description:  info.Description,
		Version:      info.Version,
		Framework:    info.Framework,
		Endpoint:     info.Name,
		Capabilities: caps,
		Tags:         tags,
		Skills:       []models.Skill{},
	}
	return card
}
