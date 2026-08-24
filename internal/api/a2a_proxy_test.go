package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestA2AInvokeBodyTranslation verifies the proxy translates the frontend's
// {adapter, message} envelope into the adapter service's
// {adapter_name, messages:[{type,text}]} shape (P2-3/P2-9 / FAIL-A2A-003).
func TestA2AInvokeBodyTranslation(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/adapters/invoke" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"task_id":"t1","adapter":"langgraph","messages":[]}`))
	}))
	defer srv.Close()

	orig := adapterBaseURL
	adapterBaseURL = srv.URL
	defer func() { adapterBaseURL = orig }()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/a2a/invoke",
		strings.NewReader(`{"adapter":"langgraph","message":"hello","metadata":{"k":"v"}}`))
	rec := httptest.NewRecorder()

	// Build a minimal Server; handlers only need adapterBaseURL + the
	// httptest upstream, not the full gateway wiring.
	s := &Server{}
	s.handleA2AInvoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if gotBody["adapter_name"] != "langgraph" {
		t.Errorf("expected adapter_name=langgraph, got %v", gotBody["adapter_name"])
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("expected messages array of length 1, got %v", gotBody["messages"])
	}
	msg := msgs[0].(map[string]any)
	if msg["type"] != "text" || msg["text"] != "hello" {
		t.Errorf("expected message {type:text,text:hello}, got %v", msg)
	}
	meta, ok := gotBody["metadata"].(map[string]any)
	if !ok || meta["k"] != "v" {
		t.Errorf("expected metadata.k=v to round-trip, got %v", gotBody["metadata"])
	}
}

// TestA2AListAdaptersEnvelope verifies GET /adapters is proxied and the
// response is wrapped in the {adapters:[...]} envelope the frontend parses
// (P2-1 / FAIL-A2A-001).
func TestA2AListAdaptersEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/adapters" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"adapters":[{"name":"langgraph"}]}`))
	}))
	defer srv.Close()

	orig := adapterBaseURL
	adapterBaseURL = srv.URL
	defer func() { adapterBaseURL = orig }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/adapters", nil)
	rec := httptest.NewRecorder()
	s := &Server{}
	s.handleA2AListAdapters(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out struct {
		Adapters []map[string]any `json:"adapters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response not {adapters:[...]}: %v", err)
	}
	if len(out.Adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(out.Adapters))
	}
}

// TestA2ACostSummaryTranslation verifies GET /costs/summary proxies to the
// adapter service's /cost/usage endpoint and translates the UsageReport into
// the frontend's A2ACostSummary shape (P2-5/P2-7 / FAIL-A2A-005/007).
func TestA2ACostSummaryTranslation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cost/usage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"org_id":"o1","adapter":"langgraph","total_cost":1.5,"currency":"USD","prompt_tokens":10,"completion_tokens":20,"task_count":3}`))
	}))
	defer srv.Close()

	orig := adapterBaseURL
	adapterBaseURL = srv.URL
	defer func() { adapterBaseURL = orig }()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/costs/summary?start=2026-01-01T00:00:00Z&end=2026-02-01T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	s := &Server{}
	s.handleA2ACostSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var summary map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("invalid summary JSON: %v", err)
	}
	if summary["total_cost"] != 1.5 {
		t.Errorf("expected total_cost=1.5, got %v", summary["total_cost"])
	}
	byAdapter, ok := summary["by_adapter"].([]any)
	if !ok || len(byAdapter) != 1 {
		t.Errorf("expected by_adapter with 1 entry, got %v", summary["by_adapter"])
	}
}
