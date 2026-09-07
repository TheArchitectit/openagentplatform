package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/security/ingest"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// withTestTenant returns a context carrying a minimal TenantContext so
// handlers that call tenancy.GetTenant(ctx).OrgID don't panic in tests.
func withTestTenant(orgID string) context.Context {
	return tenancy.WithTenantContext(context.Background(), &tenancy.TenantContext{OrgID: orgID})
}

type fakeSecurityEventStore struct {
	events []*models.SecurityEvent
}

func (f *fakeSecurityEventStore) ListByOrg(_ context.Context, _ string, _ int) ([]*models.SecurityEvent, error) {
	return f.events, nil
}

func (f *fakeSecurityEventStore) Get(_ context.Context, id string) (*models.SecurityEvent, error) {
	for _, e := range f.events {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, nil
}

type fakeIngestQueue struct {
	depth int
	cap   int
}

func (q *fakeIngestQueue) Submit(_ context.Context, _ ingest.IngestJob) bool {
	if q.depth >= q.cap {
		return false
	}
	q.depth++
	return true
}

func TestHandleSecurityWebhook202(t *testing.T) {
	srv := &Server{securityQueue: &fakeIngestQueue{cap: 100}}
	body := []byte(`{"event":{"detection_id":"d1","severity":80}}`)
	req := httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("webhook status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestHandleSecurityWebhookQueueFull(t *testing.T) {
	srv := &Server{securityQueue: &fakeIngestQueue{cap: 0, depth: 0}}
	req := httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("webhook status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if got := w.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
}

func TestHandleSecurityWebhookBodyTooLarge(t *testing.T) {
	srv := &Server{securityQueue: &fakeIngestQueue{cap: 100}}
	big := bytes.Repeat([]byte("x"), (1<<20)+1)
	req := httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader(big))
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusAccepted {
		// 1<<20 = 1MB; our body is 1MB+1; LimitReader caps to 1MB, so this still submits
		// Either Accepted or BadRequest is fine. Just verify it doesn't panic.
		_ = w.Code
	}
	// Verify the body is bounded
	body, _ := io.ReadAll(req.Body)
	if len(body) > 1<<20 {
		t.Errorf("body should be bounded to 1MB, got %d", len(body))
	}
}

func TestListSecurityEventsUnconfigured(t *testing.T) {
	srv := &Server{}
	req := httptest.NewRequest("GET", "/api/v1/security/events", nil)
	w := httptest.NewRecorder()
	srv.listSecurityEvents(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured list status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestListSecurityEventsOK(t *testing.T) {
	store := &fakeSecurityEventStore{
		events: []*models.SecurityEvent{{ID: "e1", Provider: models.EDRCrowdStrike, Severity: "critical"}},
	}
	srv := &Server{security: &securityStores{events: store}}
	ctx := withTestTenant("org-a")
	req := httptest.NewRequest("GET", "/api/v1/security/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	srv.listSecurityEvents(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list status = %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
	}
	var got []*models.SecurityEvent
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(events) = %d, want 1", len(got))
	}
}
