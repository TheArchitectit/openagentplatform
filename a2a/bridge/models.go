// Package bridge - models.go defines Go structs that mirror the Python
// adapter service REST API contract. These types are used by the HTTP
// client (client.go) to serialize/deserialize requests and responses.
package bridge

import (
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Part — a single content unit (text, file, or data).
// ============================================================

// Part is a single content unit carried in messages and artifacts.
// Matches Python oap.adapters.types.Part.
type Part struct {
	// Type is the kind of content: "text", "file", or "data".
	Type string `json:"type"`

	// Text is the text content (populated when Type == "text").
	Text string `json:"text,omitempty"`

	// FileURL is the URL or path to a file (populated when Type == "file").
	FileURL string `json:"file_url,omitempty"`

	// FileMIME is the MIME type of the file (populated when Type == "file").
	FileMIME string `json:"file_mime,omitempty"`

	// Data is arbitrary structured data (populated when Type == "data").
	Data map[string]any `json:"data,omitempty"`
}

// ============================================================
// CostRecord — per-invocation cost and token usage.
// ============================================================

// CostRecord tracks cost and token usage for a single invocation.
// Matches Python oap.adapters.types.CostRecord.
type CostRecord struct {
	// TaskID is the A2A task identifier (for correlation).
	TaskID string `json:"task_id,omitempty"`

	// Framework is the underlying agent framework.
	Framework string `json:"framework,omitempty"`

	// Model is the model identifier used.
	Model string `json:"model,omitempty"`

	// PromptTokens is the number of input/prompt tokens consumed.
	PromptTokens float64 `json:"prompt_tokens"`

	// CompletionTokens is the number of output/completion tokens consumed.
	CompletionTokens float64 `json:"completion_tokens"`

	// TotalCost is the estimated cost.
	TotalCost float64 `json:"total_cost"`

	// Currency is the currency code for TotalCost (e.g. "USD").
	Currency string `json:"currency,omitempty"`
}

// ============================================================
// Request types
// ============================================================

// InvokeRequest is the payload sent to POST /api/v1/adapters/invoke.
// Matches Python InvokeRequestModel.
type InvokeRequest struct {
	// AdapterName is the registry name of the preferred adapter.
	AdapterName string `json:"adapter_name"`

	// TaskID is the A2A task identifier (for correlation).
	TaskID string `json:"task_id,omitempty"`

	// Messages is the ordered list of message Parts.
	Messages []Part `json:"messages"`
}

// StreamRequest is the payload sent to POST /api/v1/adapters/stream.
// It carries the same fields as InvokeRequest. Streaming is determined
// by the endpoint, not a flag.
type StreamRequest = InvokeRequest

// ============================================================
// Response types
// ============================================================

// InvokeResponse is the response from POST /api/v1/adapters/invoke.
// Matches Python InvokeResponse.
type InvokeResponse struct {
	// TaskID is the A2A task identifier.
	TaskID string `json:"task_id"`

	// Adapter is the adapter that handled the request.
	Adapter string `json:"adapter,omitempty"`

	// Messages is the list of response Parts.
	Messages []Part `json:"messages"`

	// TokensUsed is the total number of tokens consumed.
	TokensUsed int `json:"tokens_used,omitempty"`

	// DurationMs is the wall-clock duration in milliseconds.
	DurationMs int64 `json:"duration_ms,omitempty"`

	// Cost is the per-invocation cost record.
	Cost *CostRecord `json:"cost,omitempty"`

	// ErrorMessage is set on failure.
	ErrorMessage string `json:"error_message,omitempty"`
}

// StreamResponse is the response from POST /api/v1/adapters/stream.
type StreamResponse struct {
	// TaskID is the A2A task identifier.
	TaskID string `json:"task_id"`

	// Adapter is the adapter that handled the request.
	Adapter string `json:"adapter,omitempty"`

	// Messages is the list of response Parts (accumulated).
	Messages []Part `json:"messages"`

	// Done indicates the stream is complete.
	Done bool `json:"done,omitempty"`

	// Cost is the per-invocation cost record (set on final event).
	Cost *CostRecord `json:"cost,omitempty"`
}

// StreamEvent is a single Server-Sent Event from a streaming invocation.
type StreamEvent struct {
	// TaskID is the A2A task identifier.
	TaskID string `json:"task_id"`

	// EventType is the event kind: "delta", "status", "error", or "done".
	EventType string `json:"event_type"`

	// Delta is the content delta carried by this event (populated for
	// EventType == "delta").
	Delta *Part `json:"delta,omitempty"`

	// Artifact is set on artifact events (nullable, included when
	// streaming a complete artifact outside the main delta channel).
	Artifact *models.Artifact `json:"artifact,omitempty"`

	// Status is a status string (populated for EventType == "status").
	Status string `json:"status,omitempty"`

	// ErrorMessage is set on error events.
	ErrorMessage string `json:"error_message,omitempty"`

	// Metadata carries arbitrary event-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ============================================================
// Cost / Budget types
// ============================================================

// UsageReport is the response from GET /api/v1/cost/usage.
// Matches Python UsageReport.
type UsageReport struct {
	// OrgID is the organization filter applied.
	OrgID string `json:"org_id,omitempty"`

	// Adapter is the adapter filter applied.
	Adapter string `json:"adapter,omitempty"`

	// Model is the model filter applied.
	Model string `json:"model,omitempty"`

	// TaskCount is the number of tasks in the report.
	TaskCount int `json:"task_count"`

	// PromptTokens is the total prompt tokens.
	PromptTokens int64 `json:"prompt_tokens"`

	// CompletionTokens is the total completion tokens.
	CompletionTokens int64 `json:"completion_tokens"`

	// TotalCost is the total cost.
	TotalCost float64 `json:"total_cost"`

	// Currency is the currency code.
	Currency string `json:"currency,omitempty"`

	// From is the start of the queried window.
	From time.Time `json:"from"`

	// To is the end of the queried window.
	To time.Time `json:"to"`
}

// BudgetInfo describes a single cost budget.
// Matches Python BudgetInfo.
type BudgetInfo struct {
	// OrgID is the organization this budget applies to.
	OrgID string `json:"org_id"`

	// BudgetLimit is the budget cap.
	BudgetLimit float64 `json:"budget_limit"`

	// CurrentSpend is the amount consumed so far.
	CurrentSpend float64 `json:"current_spend"`

	// AlertThresholds maps threshold names to spend amounts.
	AlertThresholds map[string]float64 `json:"alert_thresholds,omitempty"`

	// Status is the budget status (ok, warning, exceeded).
	Status string `json:"status,omitempty"`
}

// CancelRequest is the body sent to POST /api/v1/adapters/{taskId}/cancel.
type CancelRequest struct {
	// Reason is a human-readable reason for cancellation.
	Reason string `json:"reason,omitempty"`
}

// ============================================================
// Adapter discovery types
// ============================================================

// AdapterInfo describes a single adapter exposed by the Python service.
// Matches Python AdapterListEntry.
type AdapterInfo struct {
	// Name is the unique adapter identifier.
	Name string `json:"name"`

	// AgentCard is the nested A2A agent card for this adapter.
	AgentCard *models.AgentCard `json:"agent_card"`

	// Healthy indicates whether the adapter is currently healthy.
	Healthy bool `json:"healthy"`
}

// HealthStatus is the response from GET /api/v1/adapters/{name}/health.
// Matches Python HealthStatus.
type HealthStatus struct {
	// Healthy indicates whether the adapter is healthy.
	Healthy bool `json:"healthy"`

	// LastError is the last error message (empty if none).
	LastError string `json:"last_error,omitempty"`

	// UptimeSeconds is how long the adapter has been running.
	UptimeSeconds float64 `json:"uptime_seconds,omitempty"`

	// ActiveTasks is the number of currently active tasks.
	ActiveTasks int `json:"active_tasks,omitempty"`

	// MemoryMb is the memory usage in megabytes.
	MemoryMb float64 `json:"memory_mb,omitempty"`
}

// ============================================================
// AgentCard conversion
// ============================================================

// AgentCardFromAdapter extracts the AgentCard from a Python AdapterInfo.
// The AgentCard is now nested inside AdapterInfo (per Python contract).
func AgentCardFromAdapter(info *AdapterInfo) *models.AgentCard {
	if info == nil || info.AgentCard == nil {
		return nil
	}
	return info.AgentCard
}
