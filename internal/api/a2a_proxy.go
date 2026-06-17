package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

// adapterBaseURL is the default base URL for the Python adapter service.
// In production this would come from configuration; the default matches
// the local development setup where the adapter service runs on 8001.
const adapterBaseURL = "http://localhost:8001"

// adapterHTTPClient is used to proxy requests to the adapter service.
// A short timeout keeps the frontend responsive even when the adapter
// service is down.
var adapterHTTPClient = &http.Client{Timeout: 10 * time.Second}

// handleA2AListAdapters proxies GET /api/v1/a2a/adapters to the
// adapter service's /adapters endpoint.
func (s *Server) handleA2AListAdapters(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, "GET", "/adapters", nil)
}

// handleA2AAdapterCard proxies GET /api/v1/a2a/adapters/{name}/card
// to the adapter service's /adapters/{name}/card endpoint.
func (s *Server) handleA2AAdapterCard(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.proxyAdapter(w, r, "GET", fmt.Sprintf("/adapters/%s/card", url.PathEscape(name)), nil)
}

// handleA2AAdapterHealth proxies GET /api/v1/a2a/adapters/{name}/health
// to the adapter service's /adapters/{name}/health endpoint.
func (s *Server) handleA2AAdapterHealth(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.proxyAdapter(w, r, "GET", fmt.Sprintf("/adapters/%s/health", url.PathEscape(name)), nil)
}

// handleA2AListTasks proxies GET /api/v1/a2a/tasks to the adapter
// service's /tasks endpoint with query parameters forwarded.
func (s *Server) handleA2AListTasks(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, "GET", "/tasks", r.URL.Query())
}

// handleA2AGetTask proxies GET /api/v1/a2a/tasks/{id} to the
// adapter service's /tasks/{id} endpoint.
func (s *Server) handleA2AGetTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.proxyAdapter(w, r, "GET", fmt.Sprintf("/tasks/%s", url.PathEscape(id)), nil)
}

// handleA2ACostSummary proxies GET /api/v1/a2a/costs/summary to the
// adapter service's /costs/summary endpoint.
func (s *Server) handleA2ACostSummary(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, "GET", "/costs/summary", r.URL.Query())
}

// handleA2AInvoke proxies POST /api/v1/a2a/invoke to the adapter
// service's /invoke endpoint. The request body is forwarded as-is.
func (s *Server) handleA2AInvoke(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, "POST", "/invoke", nil)
}

// handleA2AStream proxies POST /api/v1/a2a/stream to the adapter
// service's /stream endpoint for SSE streaming of invoke results.
func (s *Server) handleA2AStream(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, "POST", "/stream", nil)
}

// handleA2ACancelTask proxies POST /api/v1/a2a/tasks/{id}/cancel to
// the adapter service's /tasks/{id}/cancel endpoint.
func (s *Server) handleA2ACancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.proxyAdapter(w, r, "POST", fmt.Sprintf("/tasks/%s/cancel", url.PathEscape(id)), nil)
}

// handleA2ATaskEvents proxies GET /api/v1/a2a/tasks/events to the
// adapter service's /tasks/events SSE stream. Unlike the standard
// proxyAdapter, this handler is designed for long-lived Server-Sent
// Events connections and does not set a short timeout.
func (s *Server) handleA2ATaskEvents(w http.ResponseWriter, r *http.Request) {
	target := adapterBaseURL + "/tasks/events"

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "proxy: build request: "+err.Error())
		return
	}

	// Use a client with no timeout for SSE streams.
	sseClient := &http.Client{Timeout: 0}
	resp, err := sseClient.Do(req)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy SSE-relevant headers.
	for _, h := range []string{"Content-Type", "Cache-Control", "Connection"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream the SSE body to the client until the connection drops.
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// proxyAdapter forwards a request to the adapter service and copies
// the response back to the client. It is intentionally simple: it
// preserves the method and body, copies the status code, and pipes
// JSON responses through unchanged.
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
	// Forward content type so the adapter service can parse the body.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	resp, err := adapterHTTPClient.Do(req)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// Copy the status code and relevant headers.
	for _, h := range []string{"Content-Type", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream the body back to the client. If the response is not JSON,
	// still forward it transparently.
	_, _ = io.Copy(w, resp.Body)
}
