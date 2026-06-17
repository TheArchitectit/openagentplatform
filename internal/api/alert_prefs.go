package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/notify"
)

// getUserAlertPreferences returns the authenticated user's alert
// preferences. If the user has no preferences row yet, the global
// defaults for the org are returned (with the user_id set to the
// current subject) so the client can render a complete form.
func (s *Server) getUserAlertPreferences(w http.ResponseWriter, r *http.Request) {
	if s.prefStore == nil {
		http.Error(w, `{"error":"preference_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	prefs, err := s.prefStore.GetUserPreferences(r.Context(), claims.Subject, claims.OrgID)
	if err != nil {
		if !errors.Is(err, alerts.ErrPreferencesNotFound) {
			s.log.Error("get user preferences failed", "err", err)
			http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
			return
		}
		// No row yet: synthesise from global defaults.
		prefs = s.defaultUserPreferences(r, claims)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs)
}

// putUserAlertPreferences upserts the authenticated user's alert
// preferences.
func (s *Server) putUserAlertPreferences(w http.ResponseWriter, r *http.Request) {
	if s.prefStore == nil {
		http.Error(w, `{"error":"preference_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var prefs alerts.UserAlertPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}

	// Server-assigned fields. The user cannot impersonate another user
	// or a different org.
	prefs.UserID = claims.Subject
	prefs.OrgID = claims.OrgID
	prefs.UpdatedAt = time.Now().UTC()

	if err := s.prefStore.UpsertUserPreferences(r.Context(), &prefs); err != nil {
		s.log.Error("upsert user preferences failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs)
}

// getGlobalAlertPreferences returns the org-wide global preferences.
// Admin only.
func (s *Server) getGlobalAlertPreferences(w http.ResponseWriter, r *http.Request) {
	if s.prefStore == nil {
		http.Error(w, `{"error":"preference_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil || claims.Role != auth.RoleAdmin {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	prefs, err := s.prefStore.GetGlobalPreferences(r.Context(), claims.OrgID)
	if err != nil {
		if !errors.Is(err, alerts.ErrPreferencesNotFound) {
			s.log.Error("get global preferences failed", "err", err)
			http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
			return
		}
		prefs = s.defaultGlobalPreferences(claims)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs)
}

// putGlobalAlertPreferences upserts the org-wide global preferences.
// Admin only.
func (s *Server) putGlobalAlertPreferences(w http.ResponseWriter, r *http.Request) {
	if s.prefStore == nil {
		http.Error(w, `{"error":"preference_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil || claims.Role != auth.RoleAdmin {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}

	var prefs alerts.GlobalAlertPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	prefs.OrgID = claims.OrgID
	prefs.UpdatedAt = time.Now().UTC()

	if err := s.prefStore.UpsertGlobalPreferences(r.Context(), &prefs); err != nil {
		s.log.Error("upsert global preferences failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs)
}

// getAlertRuleChannels returns the channel IDs associated with the
// given alert rule via the alert_rule_channels junction.
func (s *Server) getAlertRuleChannels(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}

	// Verify the rule exists and the caller has access.
	rules, err := s.alertStore.GetAlertRules(r.Context(), orgID)
	if err != nil {
		s.log.Error("list alert rules for channel lookup failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	var found bool
	for _, rl := range rules {
		if rl.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
		return
	}

	channelIDs, err := s.routingLinker.GetAlertRuleChannels(r.Context(), id)
	if err != nil {
		s.log.Error("get alert rule channels failed", "rule_id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	if channelIDs == nil {
		channelIDs = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"rule_id":     id,
		"channel_ids": channelIDs,
	})
}

// putAlertRuleChannels sets the channel set for the given alert rule.
// Body is a JSON object with "channel_ids": [...].
func (s *Server) putAlertRuleChannels(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	if s.routingLinker == nil {
		http.Error(w, `{"error":"routing_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")

	var body struct {
		ChannelIDs []string `json:"channel_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}

	// Verify rule existence.
	rules, err := s.alertStore.GetAlertRules(r.Context(), "")
	if err != nil {
		s.log.Error("list alert rules for channel set failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	var found bool
	for _, rl := range rules {
		if rl.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
		return
	}

	if err := s.routingLinker.SetAlertRuleChannels(r.Context(), id, body.ChannelIDs); err != nil {
		s.log.Error("set alert rule channels failed", "rule_id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}

	channelIDs := uniqSortedStrings(body.ChannelIDs)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"rule_id":     id,
		"channel_ids": channelIDs,
	})
}

// noopLinker is unused; kept to silence the linter if the helper
// changes shape later.
// (removed; the helper it was paired with was rewritten to operate
//  directly on the interface without an extra wrapper)

// uniqSortedStrings deduplicates and sorts a string slice.
func uniqSortedStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	// Simple insertion sort; inputs are small.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// defaultUserPreferences builds a default UserAlertPreferences using
// the org's global preferences (if available) and the registered
// channel types. The returned struct is never nil.
func (s *Server) defaultUserPreferences(r *http.Request, claims *auth.SessionClaims) *alerts.UserAlertPreferences {
	prefs := &alerts.UserAlertPreferences{
		UserID:             claims.Subject,
		OrgID:              claims.OrgID,
		SeverityThreshold:  alerts.SeverityWarning,
		ChannelPreferences: make(map[string]bool),
		UpdatedAt:          time.Now().UTC(),
	}
	// Try to inherit quiet hours from global defaults.
	if s.prefStore != nil {
		if gp, err := s.prefStore.GetGlobalPreferences(r.Context(), claims.OrgID); err == nil && gp != nil {
			prefs.QuietHours = gp.DefaultQuietHours
		}
	}
	// Seed channel preferences with the supported types, all enabled.
	if reg := s.notifierRegistry(); reg != nil {
		for _, t := range reg.SupportedTypes() {
			prefs.ChannelPreferences[t] = true
		}
	}
	return prefs
}

// defaultGlobalPreferences builds a default GlobalAlertPreferences.
func (s *Server) defaultGlobalPreferences(claims *auth.SessionClaims) *alerts.GlobalAlertPreferences {
	return &alerts.GlobalAlertPreferences{
		OrgID:              claims.OrgID,
		RetentionDays:      30,
		MaxAlertsPerAgent:  100,
		AutoResolveSeconds: 0,
		UpdatedAt:          time.Now().UTC(),
	}
}

// keep references for type-checking.
var _ = notify.NotificationChannel{}
