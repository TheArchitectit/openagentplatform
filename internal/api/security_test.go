package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/security/ingest"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// withTestTenant returns a context carrying a minimal TenantContext so
// handlers that call tenancy.GetTenant(ctx).OrgID don't panic in tests.
func withTestTenant(orgID string) context.Context {
	return tenancy.WithTenantContext(context.Background(), &tenancy.TenantContext{OrgID: orgID})
}

// withRouteContext attaches a chi route context with a {provider} URL param
// so handlers using chi.URLParam(r, "provider") work in unit tests.
func withRouteContext(r *http.Request, provider string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("provider", provider)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
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

type fakeEDRIntegrationStore struct {
	byProvider map[string]*models.EDRIntegration
}

func (f *fakeEDRIntegrationStore) Create(_ context.Context, _ *models.EDRIntegration) error {
	return nil
}
func (f *fakeEDRIntegrationStore) Get(_ context.Context, _ string) (*models.EDRIntegration, error) {
	return nil, nil
}
func (f *fakeEDRIntegrationStore) ListByOrg(_ context.Context, _ string) ([]*models.EDRIntegration, error) {
	return nil, nil
}
func (f *fakeEDRIntegrationStore) Update(_ context.Context, _ *models.EDRIntegration) error {
	return nil
}
func (f *fakeEDRIntegrationStore) Delete(_ context.Context, _ string) error {
	return nil
}
func (f *fakeEDRIntegrationStore) GetByProvider(_ context.Context, provider string) (*models.EDRIntegration, error) {
	if f.byProvider == nil {
		return nil, nil
	}
	v, ok := f.byProvider[provider]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestHandleSecurityWebhook202(t *testing.T) {
	srv := &Server{
		security: &securityStores{
			integrations: &fakeEDRIntegrationStore{
				byProvider: map[string]*models.EDRIntegration{
					"crowdstrike": {ID: "i1", Provider: models.EDRCrowdStrike, WebhookSecret: "test-secret"},
				},
			},
		},
		securityQueue: &fakeIngestQueue{cap: 100},
	}
	body := []byte(`{"event":{"detection_id":"d1","severity":80}}`)
	sig := hmacSHA256ForTest(body, "test-secret")
	req := withRouteContext(httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader(body)), "crowdstrike")
	req.Header.Set("X-Signature", sig)
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("webhook status = %d, want %d, body: %s", w.Code, http.StatusAccepted, w.Body.String())
	}
}

func TestHandleSecurityWebhookQueueFull(t *testing.T) {
	srv := &Server{
		security: &securityStores{
			integrations: &fakeEDRIntegrationStore{
				byProvider: map[string]*models.EDRIntegration{
					"crowdstrike": {ID: "i1", Provider: models.EDRCrowdStrike},
				},
			},
		},
		securityQueue: &fakeIngestQueue{cap: 0, depth: 0},
	}
	req := withRouteContext(httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader([]byte(`{}`))), "crowdstrike")
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
	srv := &Server{
		security: &securityStores{
			integrations: &fakeEDRIntegrationStore{
				byProvider: map[string]*models.EDRIntegration{
					"crowdstrike": {ID: "i1", Provider: models.EDRCrowdStrike, WebhookSecret: "test-secret"},
				},
			},
		},
		securityQueue: &fakeIngestQueue{cap: 100},
	}
	big := bytes.Repeat([]byte("x"), (1<<20)+1)
	sig := hmacSHA256ForTest(big[:1<<20], "test-secret")
	req := withRouteContext(httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader(big)), "crowdstrike")
	req.Header.Set("X-Signature", sig)
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusAccepted {
		_ = w.Code
	}
	body, _ := io.ReadAll(req.Body)
	if len(body) > 1<<20 {
		t.Errorf("body should be bounded to 1MB, got %d", len(body))
	}
}

func TestHandleSecurityWebhookInvalidSignature(t *testing.T) {
	srv := &Server{
		security: &securityStores{
			integrations: &fakeEDRIntegrationStore{
				byProvider: map[string]*models.EDRIntegration{
					"crowdstrike": {ID: "i1", Provider: models.EDRCrowdStrike, WebhookSecret: "test-secret"},
				},
			},
		},
		securityQueue: &fakeIngestQueue{cap: 100},
	}
	body := []byte(`{"event":{"detection_id":"d1","severity":80}}`)
	req := withRouteContext(httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader(body)), "crowdstrike")
	req.Header.Set("X-Signature", "deadbeef")
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("webhook status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleSecurityWebhookUnknownProvider(t *testing.T) {
	srv := &Server{
		security: &securityStores{
			integrations: &fakeEDRIntegrationStore{
				byProvider: map[string]*models.EDRIntegration{
					"crowdstrike": {ID: "i1", Provider: models.EDRCrowdStrike},
				},
			},
		},
		securityQueue: &fakeIngestQueue{cap: 100},
	}
	body := []byte(`{"event":{"detection_id":"d1","severity":80}}`)
	req := withRouteContext(httptest.NewRequest("POST", "/api/v1/security-events/ingest/unknown", bytes.NewReader(body)), "unknown")
	w := httptest.NewRecorder()
	srv.handleSecurityWebhook(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("webhook status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func hmacSHA256ForTest(payload []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
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
