package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/bridge"
)

// proxyAdapter forwards a request to the adapter service and copies the
// response back to the client. It preserves the method and body, copies the
// status code, and pipes JSON responses through unchanged.
func (s *Server) proxyAdapter(w http.ResponseWriter, r *http.Request, method, path string, query url.Values) {
	var body io.Reader
	if r.Body != nil && method != http.MethodGet && method != http.MethodHead {
		body = r.Body
		defer r.Body.Close()
	}

	target := adapterBaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(r.Context(), method, target, body)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy: build request: "+err.Error())
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	var resp *http.Response
	err = s.callAdapter(func() error {
		var derr error
		resp, derr = adapterHTTPClient.Do(req)
		return derr
	})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// doAdapterRequest performs an adapter-service call through the circuit
// breaker and returns the raw body, status code, and transport error.
func (s *Server) doAdapterRequest(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	var (
		out    []byte
		status int
	)
	err := s.callAdapter(func() error {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, rerr := http.NewRequestWithContext(ctx, method, adapterBaseURL+path, reader)
		if rerr != nil {
			return rerr
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, derr := adapterHTTPClient.Do(req)
		if derr != nil {
			return derr
		}
		defer resp.Body.Close()
		out, _ = io.ReadAll(resp.Body)
		status = resp.StatusCode
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, status, nil
}

// writeRESTJSON encodes v as JSON with the given status code.
func writeRESTJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleA2AStream translates the frontend invoke body and proxies the SSE
// stream from the adapter service's /adapters/stream endpoint.
func (s *Server) handleA2AStream(w http.ResponseWriter, r *http.Request) {
	var req a2AInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Adapter == "" || req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "adapter and message required")
		return
	}
	invokeReq := bridge.InvokeRequest{
		AdapterName: req.Adapter,
		Messages:    []bridge.Part{{Type: "text", Text: req.Message}},
		Metadata:    req.Metadata,
	}
	body, _ := json.Marshal(invokeReq)

	req2, err := http.NewRequestWithContext(r.Context(), http.MethodPost, adapterBaseURL+"/api/v1/adapters/stream", bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy: build request: "+err.Error())
		return
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Accept", "text/event-stream")

	sseClient := &http.Client{Timeout: 0}
	var resp *http.Response
	err = s.callAdapter(func() error {
		var derr error
		resp, derr = sseClient.Do(req2)
		return derr
	})
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Cache-Control", "Connection"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// handleA2ATaskEvents proxies GET /api/v1/a2a/tasks/events to the adapter
// service's /adapters/tasks/events SSE stream. If the adapter service has no
// global task-event feed it degrades to a keep-alive stream (P2-4).
func (s *Server) handleA2ATaskEvents(w http.ResponseWriter, r *http.Request) {
	target := adapterBaseURL + "/api/v1/adapters/tasks/events"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy: build request: "+err.Error())
		return
	}
	sseClient := &http.Client{Timeout: 0}
	resp, err := sseClient.Do(req)
	if err != nil {
		s.handleA2ATaskEventsFallback(w, r)
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Cache-Control", "Connection"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

// handleA2ATaskEventsFallback emits a keep-alive SSE stream when the adapter
// service has no global task-event feed, so the UI can distinguish "connected,
// no events" from a hard failure.
func (s *Server) handleA2ATaskEventsFallback(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for i := 0; i < 30; i++ {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			_, _ = w.Write([]byte(": keep-alive\n\n"))
			flusher.Flush()
		}
	}
}

// queryString renders query params as a leading "?" string, or "".
func queryString(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// parseCostWindow converts the frontend's start/end query params (RFC3339 or
// empty) into a [from, to] window, defaulting to the last 30 days.
func parseCostWindow(start, end string) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -30)
	if end != "" {
		if t, err := time.Parse(time.RFC3339, end); err == nil {
			to = t
		}
	}
	if start != "" {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			from = t
		}
	}
	return from, to
}
