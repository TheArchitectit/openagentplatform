package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func TestHandleLookupCVE_ByKB(t *testing.T) {
	score := 9.8
	store := &fakePatchStore{
		cves: []models.CVEEnrichment{
			{CVEID: "CVE-2024-12345", CvssV3Score: &score, CvssV3Severity: "CRITICAL"},
		},
	}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/cve?kb=KB5001234", nil)
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-a"}))
	rec := httptest.NewRecorder()

	s.handleLookupCVE(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CVEs  []models.CVEEnrichment `json:"cves"`
		Total int                    `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total: got %d, want 1", resp.Total)
	}
	if len(resp.CVEs) != 1 || resp.CVEs[0].CVEID != "CVE-2024-12345" {
		t.Errorf("unexpected CVEs: %+v", resp.CVEs)
	}
}

func TestHandleLookupCVE_ByCVE(t *testing.T) {
	score := 9.1
	store := &fakePatchStore{
		kbMatches: []patches.CVEKBMatch{
			{KB: "KB5001234", Title: "Security Update", Severity: "critical", CvssScore: &score},
		},
	}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/cve?cve=CVE-2024-12345", nil)
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-a"}))
	rec := httptest.NewRecorder()

	s.handleLookupCVE(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Patches []patches.CVEKBMatch `json:"patches"`
		Total   int                  `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("total: got %d, want 1", resp.Total)
	}
	if len(resp.Patches) != 1 || resp.Patches[0].KB != "KB5001234" {
		t.Errorf("unexpected patches: %+v", resp.Patches)
	}
}

func TestHandleLookupCVE_NoParams(t *testing.T) {
	store := &fakePatchStore{}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/cve", nil)
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-a"}))
	rec := httptest.NewRecorder()

	s.handleLookupCVE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLookupCVE_BothParams(t *testing.T) {
	store := &fakePatchStore{}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/cve?kb=KB123&cve=CVE-2024-1", nil)
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-a"}))
	rec := httptest.NewRecorder()

	s.handleLookupCVE(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLookupCVE_NoAuth(t *testing.T) {
	store := &fakePatchStore{}
	s, closeFn := newTestServerWithPatchStore(t, store)
	defer closeFn()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/cve?kb=KB123", nil)
	rec := httptest.NewRecorder()

	s.handleLookupCVE(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}
