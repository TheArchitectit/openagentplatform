package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRegisterContract verifies the enrollment wire contract between the
// agent and the server's handleRegisterAgent:
//   - the request body carries agent_token (the per-site registration
//     token) plus cpu_count / total_memory_mb / total_disk_gb field names,
//   - the response is accepted when the server returns the token under
//     its canonical "token" key,
//   - a missing registration token fails before any HTTP call.
func TestRegisterContract(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("server could not decode register body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		// Server-shaped response: canonical "token" key (plus agent_id).
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"agent_id":"agent-123","token":"sess.jwt","expires_in":86400}`))
	}))
	defer srv.Close()

	cfg := &Config{
		SiteID:            "site-1",
		RegistrationToken: "site-reg-token",
	}
	api := NewAPIClient(srv.URL, "", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hi := &HostInfo{
		Hostname:    "host-1",
		OS:          "linux",
		NumCPU:      4,
		TotalMemory: 8 * 1024 * 1024 * 1024,   // 8 GiB in bytes
		TotalDisk:   512 * 1024 * 1024 * 1024, // 512 GiB in bytes
	}
	if err := RegisterAgent(context.Background(), cfg, api, hi, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Request contract.
	if gotBody["agent_token"] != "site-reg-token" {
		t.Errorf("agent_token = %v, want site-reg-token", gotBody["agent_token"])
	}
	if gotBody["site_id"] != "site-1" {
		t.Errorf("site_id = %v, want site-1", gotBody["site_id"])
	}
	if gotBody["cpu_count"] != float64(4) {
		t.Errorf("cpu_count = %v, want 4 (server contract field name)", gotBody["cpu_count"])
	}
	if gotBody["total_memory_mb"] != float64(8192) {
		t.Errorf("total_memory_mb = %v, want 8192 (bytes converted)", gotBody["total_memory_mb"])
	}
	if gotBody["total_disk_gb"] != float64(512) {
		t.Errorf("total_disk_gb = %v, want 512 (bytes converted)", gotBody["total_disk_gb"])
	}

	// Response contract: the server-issued session token must land in the
	// config's AuthToken (parsed from "token").
	if cfg.AgentID != "agent-123" {
		t.Errorf("cfg.AgentID = %q, want agent-123", cfg.AgentID)
	}
	if cfg.AuthToken != "sess.jwt" {
		t.Errorf("cfg.AuthToken = %q, want sess.jwt", cfg.AuthToken)
	}
}

// TestRegisterRequiresRegistrationToken verifies fail-closed behaviour when
// no registration token is configured.
func TestRegisterRequiresRegistrationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("HTTP call must not be made without a registration token")
	}))
	defer srv.Close()

	cfg := &Config{SiteID: "site-1"}
	api := NewAPIClient(srv.URL, "", false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := RegisterAgent(context.Background(), cfg, api, &HostInfo{Hostname: "h"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("RegisterAgent without registration token = nil error, want error")
	}
}
