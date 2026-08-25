package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/mesh"
)

// meshReleaseCreateReq is the JSON body for POST /agents/{id}/releases.
type meshReleaseCreateReq struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	SHA256   string `json:"sha256"`
	Signature string `json:"signature"`
	Pinned   bool   `json:"pinned"`
}

// meshReleasePinReq is the JSON body for POST /agents/{id}/releases/{version}/pin.
type meshReleasePinReq struct {
	Pinned bool `json:"pinned"`
}

// handleMeshReleaseCreate registers a new Ed25519-signed agent release for the
// caller's org. The signature is NOT verified here — verification happens on the
// agent at apply time (defense in depth), and the build pipeline that signs the
// binary is itself a trusted component. We only persist the attestation record.
func (s *Server) handleMeshReleaseCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}
	if s.meshReleaseStore == nil {
		http.Error(w, `{"error":"mesh release store unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, `{"error":"agent id required"}`, http.StatusBadRequest)
		return
	}

	var req meshReleaseCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Version == "" || req.Platform == "" || req.SHA256 == "" || req.Signature == "" {
		http.Error(w, `{"error":"version, platform, sha256, signature required"}`, http.StatusBadRequest)
		return
	}

	rel, err := s.meshReleaseStore.InsertAgentRelease(
		r.Context(), orgID, req.Version, req.Platform, req.SHA256, req.Signature, req.Pinned,
	)
	if err != nil {
		s.log.Warn("mesh release create failed", "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rel)
}

// handleMeshReleaseList returns the org's agent release attestation records.
// ?pinned=true restricts to pinned releases (operator-gated rollout set).
func (s *Server) handleMeshReleaseList(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}
	if s.meshReleaseStore == nil {
		http.Error(w, `{"error":"mesh release store unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, `{"error":"agent id required"}`, http.StatusBadRequest)
		return
	}

	onlyPinned := r.URL.Query().Get("pinned") == "true"
	releases, err := s.meshReleaseStore.ListAgentReleases(r.Context(), orgID, onlyPinned)
	if err != nil {
		s.log.Warn("mesh release list failed", "err", err)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(releases)
}

// handleMeshReleasePin toggles the pinned state of a release (operator-gated
// rollout). Only admins may pin — pinning directly controls what agents will
// accept via self-update, so it is an elevated, audited action.
func (s *Server) handleMeshReleasePin(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// Pinning is admin-only.
	if user.Role != auth.RoleAdmin {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	orgID := user.OrgID
	if orgID == "" {
		http.Error(w, `{"error":"org_id missing from session"}`, http.StatusForbidden)
		return
	}
	if s.meshReleaseStore == nil {
		http.Error(w, `{"error":"mesh release store unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	agentID := chi.URLParam(r, "id")
	version := chi.URLParam(r, "version")
	if agentID == "" || version == "" {
		http.Error(w, `{"error":"agent id and version required"}`, http.StatusBadRequest)
		return
	}

	var req meshReleasePinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	if err := s.meshReleaseStore.PinAgentRelease(r.Context(), orgID, version, req.Pinned); err != nil {
		if errors.Is(err, mesh.ErrReleaseNotFound) {
			http.Error(w, `{"error":"release not found"}`, http.StatusNotFound)
		} else {
			s.log.Warn("mesh release pin failed", "err", err)
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
