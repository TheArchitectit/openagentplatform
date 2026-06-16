// Package api - patch_catalog.go implements the HTTP handlers for the
// server-side patch catalog and on-demand scan triggers. All
// endpoints are mounted under /api/v1/patches/catalog in routes.go.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/patches"
)

// listPatchCatalog returns the aggregated patch catalog, optionally
// filtered by minimum agent count, severity, or package manager.
func (s *Server) listPatchCatalog(w http.ResponseWriter, r *http.Request) {
	if s.patchScanner == nil {
		http.Error(w, `{"error":"patch_scanner_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	filter := patches.CatalogFilter{
		Severity:       q.Get("severity"),
		PackageManager: q.Get("package_manager"),
	}
	if v := q.Get("min_agent_count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.MinAgentCount = n
		}
	}
	entries := s.patchScanner.FilteredCatalog(filter)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"total":   len(entries),
	})
}

// getAgentPatches returns the latest available patches for a
// single agent (from the most recent scan).
func (s *Server) getAgentPatches(w http.ResponseWriter, r *http.Request) {
	if s.patchScanner == nil {
		http.Error(w, `{"error":"patch_scanner_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	agentPatches := s.patchScanner.AgentPatches(id)
	// Ensure the response is a JSON array even when empty.
	if len(agentPatches) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agent_id": id,
		"patches":  agentPatches,
	})
}

// triggerScanAll triggers a platform-wide patch scan. The scan runs
// asynchronously in the background; the response returns 202 with
// the number of agents that will be contacted.
func (s *Server) triggerScanAll(w http.ResponseWriter, r *http.Request) {
	if s.patchScanner == nil {
		http.Error(w, `{"error":"patch_scanner_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	go func() {
		// Use a detached context so the scan continues even if the
		// HTTP request that triggered it is cancelled.
		if _, err := s.patchScanner.ScanAll(r.Context()); err != nil {
			s.log.Warn("patch scan: ScanAll failed", "err", err)
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"scan_triggered"}`))
}

// triggerScanSite triggers a patch scan for all agents in a site.
func (s *Server) triggerScanSite(w http.ResponseWriter, r *http.Request) {
	if s.patchScanner == nil {
		http.Error(w, `{"error":"patch_scanner_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	siteID := chi.URLParam(r, "siteId")
	if siteID == "" {
		http.Error(w, `{"error":"missing_site_id"}`, http.StatusBadRequest)
		return
	}
	go func() {
		if _, err := s.patchScanner.ScanSite(r.Context(), siteID); err != nil {
			s.log.Warn("patch scan: ScanSite failed", "site_id", siteID, "err", err)
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"scan_triggered","site_id":"` + siteID + `"}`))
}

// triggerScanAgent triggers a patch scan for a single agent and
// returns the result synchronously.
func (s *Server) triggerScanAgent(w http.ResponseWriter, r *http.Request) {
	if s.patchScanner == nil {
		http.Error(w, `{"error":"patch_scanner_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	result, err := s.patchScanner.ScanAgent(r.Context(), id)
	if err != nil {
		s.log.Warn("patch scan: ScanAgent failed", "agent_id", id, "err", err)
		http.Error(w, `{"error":"scan_failed","message":"`+err.Error()+`"}`, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
