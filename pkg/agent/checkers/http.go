package checkers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPChecker performs an HTTP GET and optionally matches the body.
type HTTPChecker struct {
	client *http.Client
}

func (h *HTTPChecker) Name() string { return "http" }

// Metadata describes the HTTP checker.
func (h *HTTPChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:        "http",
		Version:     "1.0.0",
		Description: "Performs an HTTP GET against the target URL and optionally matches a substring in the body.",
		SupportedPlatforms: []string{
			"linux", "darwin", "freebsd", "windows",
		},
	}
}

func (h *HTTPChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "http check requires target"}
	}
	timeout := 10 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	client := h.client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.Target, nil)
	if err != nil {
		return &Result{OK: false, Error: err.Error()}
	}
	httpReq.Header.Set("User-Agent", "oap-agent/"+agentVersion())
	resp, err := client.Do(httpReq)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	if req.Expected != "" && !strings.Contains(string(body), req.Expected) {
		ok = false
	}

	return &Result{
		OK:      ok,
		Status:  resp.Status,
		Value:   map[string]interface{}{"status_code": resp.StatusCode, "body_size": len(body)},
		Message: string(body),
		Duration: time.Since(start).Milliseconds(),
	}
}

func agentVersion() string { return "0.1.0" }
