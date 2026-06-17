package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// agentToken is the per-site registration token used by an agent to prove it
// is authorised to register against a particular site. The DB column
// sites.registration_token stores the bcrypt/argon2 hash; for the initial
// implementation we store a plaintext token and compare with constant-time
// equality. A future migration should switch this to a hashed comparison.
type siteTokenLookup interface {
	GetSiteRegistrationToken(ctx context.Context, siteID string) (token string, orgID string, err error)
}

type agentStore interface {
	siteTokenLookup

	UpsertAgent(ctx context.Context, a *models.Agent) error
	GetAgent(ctx context.Context, orgID, id string) (*models.Agent, error)
	ListAgents(ctx context.Context, filter AgentListFilter) ([]models.Agent, int, error)
	ListCheckResultsByAgent(ctx context.Context, agentID string, limit int) ([]models.CheckResult, error)
	ListCheckResultsByAgentPaged(ctx context.Context, agentID string, limit, offset int) ([]models.CheckResult, int, error)
	ListCheckResultsPaged(ctx context.Context, agentID, checkID, status, search string, limit, offset int) ([]models.CheckResult, int, error)
}

// AgentListFilter is the filter applied to GET /api/v1/agents.
type AgentListFilter struct {
	OrgID  string
	SiteID string
	Status string
	Search string
	Limit  int
	Offset int
}

// handleRegisterAgent validates the per-site registration token, upserts the
// agent record, and returns an agent JWT plus NATS subject names.
func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var req struct {
		AgentToken    string   `json:"agent_token"`
		SiteID        string   `json:"site_id"`
		Hostname      string   `json:"hostname"`
		OS            string   `json:"os"`
		Arch          string   `json:"arch"`
		Platform      string   `json:"platform"`
		CPUCount      int      `json:"cpu_count"`
		TotalMemoryMB int64    `json:"total_memory_mb"`
		TotalDiskGB   int64    `json:"total_disk_gb"`
		AgentVersion  string   `json:"agent_version"`
		Tags          []string `json:"tags,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if req.AgentToken == "" || req.SiteID == "" || req.Hostname == "" {
		http.Error(w, `{"error":"missing_required_fields"}`, http.StatusBadRequest)
		return
	}

	store := s.agentStore()
	regToken, orgID, err := store.GetSiteRegistrationToken(r.Context(), req.SiteID)
	if err != nil {
		s.log.Warn("site lookup failed", "site_id", req.SiteID, "err", err)
		http.Error(w, `{"error":"invalid_site"}`, http.StatusUnauthorized)
		return
	}
	if subtleCompare(regToken, req.AgentToken) != 0 {
		s.log.Warn("agent_token mismatch", "site_id", req.SiteID)
		http.Error(w, `{"error":"invalid_agent_token"}`, http.StatusUnauthorized)
		return
	}

	agentID := uuid.NewString()
	now := time.Now().UTC()
	agent := &models.Agent{
		ID:            agentID,
		SiteID:        req.SiteID,
		OrgID:         orgID,
		Hostname:      req.Hostname,
		OperatingSystem: req.OS,
		Arch:          req.Arch,
		Platform:      req.Platform,
		CPUCount:      req.CPUCount,
		TotalMemoryMB: req.TotalMemoryMB,
		TotalDiskGB:   req.TotalDiskGB,
		AgentVersion:  req.AgentVersion,
		Version:       req.AgentVersion,
		Status:        "online",
		LastSeen:      now,
		Tags:          req.Tags,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := store.UpsertAgent(r.Context(), agent); err != nil {
		s.log.Error("agent upsert failed", "err", err)
		http.Error(w, `{"error":"agent_upsert_failed"}`, http.StatusInternalServerError)
		return
	}

	// Mint agent JWT (24h).
	token, err := s.mintAgentToken(agentID, req.SiteID, orgID, 24*time.Hour)
	if err != nil {
		s.log.Error("agent token mint failed", "err", err)
		http.Error(w, `{"error":"token_mint_failed"}`, http.StatusInternalServerError)
		return
	}

	subjects := NATSSubjectsForAgent(agentID)

	resp := map[string]any{
		"agent_id":      agentID,
		"token":         token,
		"expires_in":    int((24 * time.Hour).Seconds()),
		"nats_subjects": subjects,
		"agent":         agent,
	}
	if s.eventBus != nil {
		evt := map[string]any{
			"type":      "AgentOnline",
			"agent_id":  agentID,
			"site_id":   req.SiteID,
			"timestamp": now.Unix(),
		}
		if data, err := json.Marshal(evt); err == nil {
			if perr := s.eventBus.Publish(r.Context(), "oap.events.agent.online", data); perr != nil {
				s.log.Warn("publish AgentOnline failed", "err", perr)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// handleListAgents returns the paginated agent list with optional filters.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := atoiDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	filter := AgentListFilter{
		SiteID: q.Get("site_id"),
		Status: q.Get("status"),
		Search: q.Get("search"),
		Limit:  limit,
		Offset: offset,
	}
	if claims, ok := authFromCtx(r); ok && claims != nil {
		filter.OrgID = claims.OrgID
	}

	agents, total, err := s.agentStore().ListAgents(r.Context(), filter)
	if err != nil {
		s.log.Error("list agents failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if agents == nil {
		agents = []models.Agent{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agents": agents,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// handleGetAgent returns one agent plus the most recent check results.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}

	orgID := ""
	if claims, ok := authFromCtx(r); ok && claims != nil {
		orgID = claims.OrgID
	}
	agent, err := s.agentStore().GetAgent(r.Context(), orgID, id)
	if err != nil {
		s.log.Warn("agent lookup failed", "id", id, "err", err)
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		return
	}

	limit := atoiDefault(r.URL.Query().Get("check_limit"), 25)
	results, err := s.agentStore().ListCheckResultsByAgent(r.Context(), id, limit)
	if err != nil {
		s.log.Warn("list check results failed", "id", id, "err", err)
		results = nil
	}
	if results == nil {
		results = []models.CheckResult{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agent":         agent,
		"check_results": results,
	})
}

// NATSSubjectsForAgent returns the canonical NATS subject names an agent
// should subscribe to / publish on.
func NATSSubjectsForAgent(agentID string) map[string]string {
	return map[string]string{
		"heartbeat":  fmt.Sprintf("oap.agents.%s.heartbeat", agentID),
		"commands":   fmt.Sprintf("oap.agents.%s.commands", agentID),
		"checks":     fmt.Sprintf("oap.agents.%s.checks", agentID),
		"results":    fmt.Sprintf("oap.agents.%s.results", agentID),
	}
}

// handleListAgentCheckResults returns the paginated check-result history
// for a single agent. Optional query parameters: limit (default 50,
// max 500) and offset (default 0). The response shape mirrors
// listAgents: { results: [...], total, limit, offset }.
func (s *Server) handleListAgentCheckResults(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"missing_id"}`, http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := atoiDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	checkID := q.Get("check_id")
	status := q.Get("status")

	// We delegate the underlying list to the store; the check_id and
	// status filters require a more specific query, so we use the
	// platform-wide ListCheckResultsPaged and post-filter by agent_id.
	results, total, err := s.agentStore().ListCheckResultsPaged(r.Context(), id, checkID, status, "", limit, offset)
	if err != nil {
		s.log.Error("list check results failed",
			"agent_id", id, "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []models.CheckResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agent_id": id,
		"results":  results,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// handleListAllCheckResults returns a filtered, paginated list of check
// results across all agents. Optional query parameters: agent_id,
// check_id, status, search, limit, offset.
func (s *Server) handleListAllCheckResults(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"db_unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := atoiDefault(q.Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}

	results, total, err := s.agentStore().ListCheckResultsPaged(
		r.Context(),
		q.Get("agent_id"),
		q.Get("check_id"),
		q.Get("status"),
		q.Get("search"),
		limit,
		offset,
	)
	if err != nil {
		s.log.Error("list all check results failed", "err", err)
		http.Error(w, `{"error":"list_failed"}`, http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []models.CheckResult{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"results": results,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// agentStore builds the agentStore backed by s.db. In production this should
// be replaced with a dedicated repository type. Kept inline so the API
// package has a single, testable persistence seam.
func (s *Server) agentStore() agentStore {
	return &pgAgentStore{pool: s.db}
}

// mintAgentToken uses the session minter to mint an EdDSA-signed agent JWT.
func (s *Server) mintAgentToken(agentID, siteID, orgID string, ttl time.Duration) (string, error) {
	if s.sessionMinter == nil {
		return "", errors.New("api: session minter not configured")
	}
	return s.sessionMinter.MintAgentToken(agentID, siteID, orgID, ttl)
}

// subtleCompare returns 0 if a and b are equal in length and content.
// Uses constant-time comparison to avoid leaking timing information on
// registration-token checks. Returns 0 on equal, non-zero otherwise.
func subtleCompare(a, b string) int {
	if len(a) != len(b) {
		return 1
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	if v == 0 {
		return 0
	}
	return 1
}

func atoiDefault(s string, d int) int {
	if s == "" {
		return d
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return d
	}
	return n
}
