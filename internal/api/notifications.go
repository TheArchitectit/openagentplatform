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

// notifierRegistry is the registry used to validate channel
// configurations on create/update. It is set on the Server at startup
// (see SetNotifierRegistry).
func (s *Server) notifierRegistry() *notify.NotifierRegistry {
	if s.notifierReg != nil {
		return s.notifierReg
	}
	return notify.InitDefaultRegistry()
}

// listNotificationChannels returns the channels visible to the
// authenticated user. Org-wide channels (user_id == "") and
// user-owned channels (user_id == claims.Subject) are both included.
func (s *Server) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	orgID := ""
	userID := ""
	if claims != nil {
		orgID = claims.OrgID
		userID = claims.Subject
	}
	channels, err := s.alertStore.ListNotificationChannels(r.Context(), orgID, userID)
	if err != nil {
		s.log.Error("list notification channels failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(channels)
}

// createNotificationChannel inserts a new channel. The body must
// include type, name, and a type-specific config object.
func (s *Server) createNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var body struct {
		Name    string          `json:"name"`
		Type    string          `json:"type"`
		Config  json.RawMessage `json:"config"`
		Enabled *bool           `json:"enabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, `{"error":"name_required"}`, http.StatusBadRequest)
		return
	}
	if body.Type == "" {
		http.Error(w, `{"error":"type_required"}`, http.StatusBadRequest)
		return
	}
	registry := s.notifierRegistry()
	notifier := registry.Get(body.Type)
	if notifier == nil {
		http.Error(w, `{"error":"unsupported_channel_type"}`, http.StatusBadRequest)
		return
	}
	if err := notifier.ValidateConfig(body.Config); err != nil {
		http.Error(w, `{"error":"invalid_config","detail":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	now := time.Now().UTC()
	channel := &notify.NotificationChannel{
		ID:        uuidNew(),
		OrgID:     claims.OrgID,
		UserID:    claims.Subject,
		Name:      body.Name,
		Type:      body.Type,
		Enabled:   enabled,
		Config:    body.Config,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.alertStore.CreateNotificationChannel(r.Context(), channel); err != nil {
		s.log.Error("create notification channel failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(channel)
}

// getNotificationChannel returns a single channel by id. The user must
// belong to the channel's org.
func (s *Server) getNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	channel, err := s.alertStore.GetNotificationChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, alerts.ErrChannelNotFound) {
			http.Error(w, `{"error":"channel_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get notification channel failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	claims, _ := auth.UserFromContext(r.Context())
	if claims == nil || channel.OrgID != claims.OrgID {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(channel)
}
