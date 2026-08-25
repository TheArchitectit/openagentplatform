package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/patches"
)

func newTestServerWithDeployer(t *testing.T) (*Server, func()) {
	t.Helper()
	cfg := &config.Config{}
	log := newDiscardLogger()
	s := NewServer(cfg, log, nil, nil, nil)
	s.SetPatchStore(&fakePatchStore{})
	// Wire a deployer with nil NATS conn. CoordinateReboots will run
	// health checks + stagger but skip publish — safe for testing.
	deployer := patches.NewPatchDeployer(patches.PatchDeployerConfig{
		RebootStagger:      1 * time.Millisecond,
		HealthCheckTimeout: 1 * time.Second,
		// Nil HealthCheckFn → health checks pass.
	}, nil)
	s.SetPatchDeployer(deployer)
	return s, func() {}
}

func TestHandleScheduleReboot_NoAuth(t *testing.T) {
	s, cleanup := newTestServerWithDeployer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{"agent-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patches/reboot", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleScheduleReboot(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleScheduleReboot_EmptyAgentIDs(t *testing.T) {
	s, cleanup := newTestServerWithDeployer(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patches/reboot", bytes.NewReader(body))
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-1"}))
	rec := httptest.NewRecorder()

	s.handleScheduleReboot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleScheduleReboot_InvalidJSON(t *testing.T) {
	s, cleanup := newTestServerWithDeployer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/patches/reboot", bytes.NewReader([]byte("not json")))
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-1"}))
	rec := httptest.NewRecorder()

	s.handleScheduleReboot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleScheduleReboot_NilDeployer(t *testing.T) {
	cfg := &config.Config{}
	log := newDiscardLogger()
	s := NewServer(cfg, log, nil, nil, nil)
	s.SetPatchStore(&fakePatchStore{})
	// No deployer wired.

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{"agent-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patches/reboot", bytes.NewReader(body))
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-1"}))
	rec := httptest.NewRecorder()

	s.handleScheduleReboot(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleScheduleReboot_NilPatchStore(t *testing.T) {
	cfg := &config.Config{}
	log := newDiscardLogger()
	s := NewServer(cfg, log, nil, nil, nil)
	// No patch store wired.

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{"agent-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patches/reboot", bytes.NewReader(body))
	req = req.WithContext(auth.WithUser(context.Background(), &auth.SessionClaims{OrgID: "org-1"}))
	rec := httptest.NewRecorder()

	s.handleScheduleReboot(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}
