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
