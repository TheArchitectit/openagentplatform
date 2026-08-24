package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
)

// adapterBaseURL is the default base URL for the Python adapter service.
// Default matches the local dev setup (adapter service on 8001). Declared as
// a var (not const) so tests can point it at a mock upstream.
var adapterBaseURL = "http://localhost:8001"

// adapterHTTPClient proxies to the adapter service; a short timeout keeps the
// frontend responsive when the adapter service is down.
var adapterHTTPClient = &http.Client{Timeout: 10 * time.Second}

// handleA2AListAdapters proxies GET /api/v1/a2a/adapters to the adapter
// service's /adapters. The service already returns the {adapters:[...]}
// envelope the frontend parses, so this is a direct pass-through.
func (s *Server) handleA2AListAdapters(w http.ResponseWriter, r *http.Request) {
	s.proxyAdapter(w, r, http.MethodGet, "/api/v1/adapters", nil)
}

// handleA2AAdapterCard proxies GET /api/v1/a2a/adapters/{name}/card and
// enriches it with the adapter's cost models from /adapters/{name}/models
// (the frontend renders card.models).
func (s *Server) handleA2AAdapterCard(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cardBody, cardStatus, cardErr := doAdapterRequest(r.Context(), http.MethodGet, fmt.Sprintf("/api/v1/adapters/%s/card", url.PathEscape(name)), nil)
	if cardErr != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+cardErr.Error())
		return
	}
	if cardStatus != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(cardStatus)
		_, _ = w.Write(cardBody)
		return
	}
	var card map[string]any
	if err := json.Unmarshal(cardBody, &card); err != nil {
		writeJSONError(w, http.StatusBadGateway, "invalid card response: "+err.Error())
		return
	}
	if modelsBody, modelsStatus, mErr := doAdapterRequest(r.Context(), http.MethodGet, fmt.Sprintf("/api/v1/adapters/%s/models", url.PathEscape(name)), nil); mErr == nil && modelsStatus == http.StatusOK {
		var modelsResp struct {
			Models []map[string]any `json:"models"`
		}
		if json.Unmarshal(modelsBody, &modelsResp) == nil {
			card["models"] = modelsResp.Models
		}
	}
	writeRESTJSON(w, http.StatusOK, card)
}

// handleA2AAdapterModels proxies GET /api/v1/a2a/adapters/{name}/models. The
// upstream already returns {models:[...]}.
func (s *Server) handleA2AAdapterModels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.proxyAdapter(w, r, http.MethodGet, fmt.Sprintf("/api/v1/adapters/%s/models", url.PathEscape(name)), nil)
}

// handleA2AAdapterHealth proxies GET /api/v1/a2a/adapters/{name}/health.
func (s *Server) handleA2AAdapterHealth(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.proxyAdapter(w, r, http.MethodGet, fmt.Sprintf("/api/v1/adapters/%s/health", url.PathEscape(name)), nil)
}

// handleA2AListTasks delegates GET /api/v1/a2a/tasks to the Go A2A gateway
// (tasks live in the gateway, not the adapter service).
func (s *Server) handleA2AListTasks(w http.ResponseWriter, r *http.Request) {
	if s.a2aGateway == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "a2a gateway not configured")
		return
	}
	gateway.RESTTasksHandler(s.a2aGateway).ServeHTTP(w, r)
}

// handleA2AGetTask delegates GET /api/v1/a2a/tasks/{id} to the Go A2A gateway.
func (s *Server) handleA2AGetTask(w http.ResponseWriter, r *http.Request) {
	if s.a2aGateway == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "a2a gateway not configured")
		return
	}
	gateway.RESTTaskHandler(s.a2aGateway).ServeHTTP(w, r)
}

// handleA2ACostSummary proxies GET /api/v1/a2a/costs/summary to the adapter
// service's /cost/usage endpoint (frontend uses start/end; the service uses
// from/to) and translates into the frontend's A2ACostSummary shape (P2-5/P2-7).
func (s *Server) handleA2ACostSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := parseCostWindow(q.Get("start"), q.Get("end"))
	fwd := url.Values{}
	if org := q.Get("org"); org != "" {
		fwd.Set("org_id", org)
	}
	// The adapter service expects Unix epoch floats for from/to, not
	// RFC3339 (FAIL-A2A-010: RFC3339 params 422 on FastAPI's float
	// Query parser).
	fwd.Set("from", strconv.FormatFloat(float64(from.UnixNano())/float64(time.Second), 'f', -1, 64))
	fwd.Set("to", strconv.FormatFloat(float64(to.UnixNano())/float64(time.Second), 'f', -1, 64))

	body, status, err := doAdapterRequest(r.Context(), http.MethodGet, "/api/v1/cost/usage?"+fwd.Encode(), nil)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	if status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}
	var report bridge.UsageReport
	if err := json.Unmarshal(body, &report); err != nil {
		writeJSONError(w, http.StatusBadGateway, "invalid cost response: "+err.Error())
		return
	}
	summary := map[string]any{
		"org_id":            report.OrgID,
		"total_cost":        report.TotalCost,
		"currency":          report.Currency,
		"prompt_tokens":     report.PromptTokens,
		"completion_tokens": report.CompletionTokens,
		"task_count":        report.TaskCount,
		"from":              report.From,
		"to":                report.To,
		"by_org": []map[string]any{{
			"org_id":            report.OrgID,
			"total_cost":        report.TotalCost,
			"task_count":        report.TaskCount,
			"prompt_tokens":     report.PromptTokens,
			"completion_tokens": report.CompletionTokens,
		}},
		"by_adapter": []map[string]any{},
		"by_model":   []map[string]any{},
		"by_day":     []map[string]any{},
	}
	if report.Adapter != "" {
		summary["by_adapter"] = []map[string]any{{
			"adapter":           report.Adapter,
			"total_cost":        report.TotalCost,
			"task_count":        report.TaskCount,
			"prompt_tokens":     report.PromptTokens,
			"completion_tokens": report.CompletionTokens,
		}}
	}
	writeRESTJSON(w, http.StatusOK, summary)
}

// a2AInvokeRequest is the frontend's invoke body. The frontend sends a single
// `message` string and an `adapter` name; the service expects
// `messages: [{type,text}]` and `adapter_name`. We translate here (P2-3/P2-9).
type a2AInvokeRequest struct {
	Adapter  string         `json:"adapter"`
	Message  string         `json:"message"`
	Skill    string         `json:"skill,omitempty"`
	Model    string         `json:"model,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// handleA2AInvoke translates the frontend invoke body into the adapter
// service's InvokeRequest and forwards it.
func (s *Server) handleA2AInvoke(w http.ResponseWriter, r *http.Request) {
	var req a2AInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Adapter == "" {
		writeJSONError(w, http.StatusBadRequest, "adapter required")
		return
	}
	if req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "message required")
		return
	}
	invokeReq := bridge.InvokeRequest{
		AdapterName: req.Adapter,
		Messages:    []bridge.Part{{Type: "text", Text: req.Message}},
		Metadata:    req.Metadata,
	}
	body, _ := json.Marshal(invokeReq)
	respBody, status, err := doAdapterRequest(r.Context(), http.MethodPost, "/api/v1/adapters/invoke", body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "adapter service unavailable: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(respBody)
}

// handleA2ACancelTask proxies POST /api/v1/a2a/tasks/{id}/cancel to the
// adapter service's /adapters/{id}/cancel endpoint.
func (s *Server) handleA2ACancelTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.proxyAdapter(w, r, http.MethodPost, fmt.Sprintf("/api/v1/adapters/%s/cancel", url.PathEscape(id)), nil)
}
