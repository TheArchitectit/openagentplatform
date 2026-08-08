package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

// ============================================================
// Stream
// ============================================================

// Stream sends a streaming invocation request. It returns a channel of
// StreamEvent and a cancel function. The channel is closed when the
// stream completes or an error occurs. The cancel function aborts the
// underlying HTTP request.
//
// POST /api/v1/adapters/stream (SSE response)
func (c *AdapterClient) Stream(ctx context.Context, adapter string, messages []Part) (<-chan StreamEvent, func(), error) {
	if c == nil {
		return nil, nil, ErrClientNotConfigured
	}

	if !c.cb.allow() {
		return nil, nil, ErrCircuitOpen
	}

	req := &StreamRequest{
		AdapterName: adapter,
		Messages:    messages,
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: marshal stream request: %w", err)
	}

	fullURL := c.baseURL + "/api/v1/adapters/stream"
	reqID := c.requestID.Add(1)

	// Use a separate context for the HTTP request so we can cancel
	// it independently of the caller's context.
	streamCtx, cancel := context.WithCancel(ctx)

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		cancel()
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: create stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	httpReq.Header.Set("X-Request-ID", fmt.Sprintf("bridge-stream-%d", reqID))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		c.cb.recordFailure()
		return nil, nil, fmt.Errorf("bridge: stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode >= 500 {
			c.cb.recordFailure()
		}
		return nil, nil, fmt.Errorf("bridge: stream status %d: %s", resp.StatusCode, string(respBody))
	}

	c.cb.recordSuccess()

	events := make(chan StreamEvent, 32)

	go func() {
		defer close(events)
		defer cancel()
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			// SSE format: "data: <json>"
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				if c.log != nil {
					c.log.Warn("bridge: parse SSE event",
						"req_id", reqID,
						"err", err,
					)
				}
				continue
			}

			select {
			case events <- event:
			case <-streamCtx.Done():
				return
			}

			// Terminal events end the stream
			if event.EventType == "done" || event.EventType == "error" {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if c.log != nil {
				c.log.Warn("bridge: stream read error",
					"req_id", reqID,
					"err", err,
				)
			}
		}
	}()

	cancelFunc := func() {
		cancel()
	}

	return events, cancelFunc, nil
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
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))

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
