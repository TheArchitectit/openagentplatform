package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
