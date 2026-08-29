package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestA2AProxyLiveE2E makes one real request through the Go /api/v1/a2a/*
// proxy against a running adapter service and asserts the round-trip is
// valid JSON. It is the "live end-to-end runtime check" the adapter-service
// spec lists as the subsystem's remaining gap (Known Limitations): unit
// coverage of body/envelope translation already lives in
// a2a_proxy_test.go with httptest upstreams; this test proves the same
// handlers work against the real FastAPI process.
//
// Enable by starting the adapter service and setting:
//
//	OAP_TEST_ADAPTER_E2E=http://localhost:8001
//
// The service is started in the repo root with:
//
//	cd py && .venv/bin/python -m uvicorn oap.app:app --port 8001
//
// When unset (or the service is unreachable), the test skips: a
// framework-SDK-less CI box cannot run the Python service, and an
// adapter-less service still answers the read-only paths (empty list,
// zero cost), which is exactly the degraded contract under test.
func TestA2AProxyLiveE2E(t *testing.T) {
	base := os.Getenv("OAP_TEST_ADAPTER_E2E")
	if base == "" {
		t.Skip("OAP_TEST_ADAPTER_E2E not set; skipping live adapter-service e2e")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Liveness probe first so an unstarted service yields a skip, not a
	// misleading FAIL.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("adapter service not reachable at %s: %v", base, err)
	}
	resp.Body.Close()

	orig := adapterBaseURL
	adapterBaseURL = base
	defer func() { adapterBaseURL = orig }()

	s := &Server{}

	t.Run("list_adapters", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/adapters", nil)
		w := httptest.NewRecorder()
		s.handleA2AListAdapters(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/a2a/adapters: got %d, body=%s", w.Code, w.Body.String())
		}
		var envelope struct {
			Adapters []map[string]any `json:"adapters"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("list adapters body is not the {adapters:[...]} envelope: %v (body=%s)", err, w.Body.String())
		}
		t.Logf("service reachable; %d adapters registered (0 is valid when no framework SDKs installed)", len(envelope.Adapters))
	})

	t.Run("cost_summary", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/a2a/costs/summary?start=2020-01-01T00:00:00Z&end=2099-01-01T00:00:00Z", nil)
		w := httptest.NewRecorder()
		s.handleA2ACostSummary(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/a2a/costs/summary: got %d, body=%s", w.Code, w.Body.String())
		}
		var summary map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
			t.Fatalf("cost summary is not JSON: %v (body=%s)", err, w.Body.String())
		}
		// The Go handler reshapes the Python report into the frontend
		// summary contract; key fields MUST be present on a real round-trip.
		for _, k := range []string{"total_cost", "by_org", "by_adapter", "from", "to"} {
			if _, ok := summary[k]; !ok {
				t.Errorf("cost summary missing %q (body=%s)", k, w.Body.String())
			}
		}
	})
}
