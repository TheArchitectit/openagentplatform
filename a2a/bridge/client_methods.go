package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

func (c *AdapterClient) Invoke(ctx context.Context, adapter string, messages []Part) (*InvokeResponse, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	req := &InvokeRequest{
		AdapterName: adapter,
		Messages:    messages,
	}

	_, body, err := c.doRequest(ctx, http.MethodPost, "/api/v1/adapters/invoke", req)
	if err != nil {
		return nil, err
	}

	var resp InvokeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal invoke response: %w", err)
	}
	return &resp, nil
}

// InvokeEx sends an invocation using a fully-specified InvokeRequest,
// preserving any caller-supplied Metadata (P2-9). Invoke builds its own
// request and drops Metadata; use InvokeEx when metadata must round-trip.
func (c *AdapterClient) InvokeEx(ctx context.Context, req InvokeRequest) (*InvokeResponse, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	_, body, err := c.doRequest(ctx, http.MethodPost, "/api/v1/adapters/invoke", &req)
	if err != nil {
		return nil, err
	}

	var resp InvokeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal invoke response: %w", err)
	}
	return &resp, nil
}

// ============================================================
// Cancel
// ============================================================

// Cancel cancels a running adapter task.
// POST /api/v1/adapters/{taskId}/cancel
func (c *AdapterClient) Cancel(ctx context.Context, adapter, taskID string) (bool, error) {
	if c == nil {
		return false, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/cancel", url.PathEscape(taskID))
	body := &CancelRequest{Reason: "cancelled via A2A bridge"}

	_, respBody, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return false, err
	}

	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return false, fmt.Errorf("bridge: unmarshal cancel response: %w", err)
	}

	if !resp.Success && resp.Error != "" {
		return false, fmt.Errorf("bridge: cancel failed: %s", resp.Error)
	}
	return resp.Success, nil
}

// ============================================================
// Adapter discovery
// ============================================================

// ListAdapters returns all available adapters from the Python service.
// GET /api/v1/adapters
func (c *AdapterClient) ListAdapters(ctx context.Context) ([]AdapterInfo, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, "/api/v1/adapters", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Adapters []AdapterInfo `json:"adapters"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal adapters response: %w", err)
	}
	if resp.Adapters == nil {
		resp.Adapters = []AdapterInfo{}
	}
	return resp.Adapters, nil
}

// GetAdapterCard retrieves the AgentCard for a named adapter.
// GET /api/v1/adapters/{name}/card
func (c *AdapterClient) GetAdapterCard(ctx context.Context, name string) (*models.AgentCard, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/card", url.PathEscape(name))
	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var info AdapterInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal adapter card: %w", err)
	}
	return AgentCardFromAdapter(&info), nil
}

// GetAdapterHealth checks the health of a named adapter.
// GET /api/v1/adapters/{name}/health
func (c *AdapterClient) GetAdapterHealth(ctx context.Context, name string) (*HealthStatus, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	path := fmt.Sprintf("/api/v1/adapters/%s/health", url.PathEscape(name))
	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var health HealthStatus
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal health response: %w", err)
	}
	return &health, nil
}

// ============================================================
// Cost / Budget
// ============================================================

// GetCostUsage retrieves cost usage data for an org within a time window.
// GET /api/v1/cost/usage?org_id=...&from=...&to=...
func (c *AdapterClient) GetCostUsage(ctx context.Context, orgID string, from, to time.Time) (*UsageReport, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	q := url.Values{}
	if orgID != "" {
		q.Set("org_id", orgID)
	}
	// The service declares from/to as float Unix epoch (FastAPI Query
	// parser) — RFC 3339 strings fail validation with a 422.
	q.Set("from", strconv.FormatFloat(float64(from.UnixNano())/float64(time.Second), 'f', -1, 64))
	q.Set("to", strconv.FormatFloat(float64(to.UnixNano())/float64(time.Second), 'f', -1, 64))

	path := "/api/v1/cost/usage"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var report UsageReport
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal usage report: %w", err)
	}
	return &report, nil
}

// GetBudgetStatus returns all configured cost budgets.
// GET /api/v1/cost/budgets
func (c *AdapterClient) GetBudgetStatus(ctx context.Context) ([]BudgetInfo, error) {
	if c == nil {
		return nil, ErrClientNotConfigured
	}

	_, body, err := c.doRequest(ctx, http.MethodGet, "/api/v1/cost/budgets", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Budgets []BudgetInfo `json:"budgets"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bridge: unmarshal budgets response: %w", err)
	}
	if resp.Budgets == nil {
		resp.Budgets = []BudgetInfo{}
	}
	return resp.Budgets, nil
}
