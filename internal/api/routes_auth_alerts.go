package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
		return
	}
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	alerts, _, err := s.alertStore.ListAlerts(r.Context(), alerts.AlertFilter{OrgID: orgID, Limit: 50})
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
		return
	}
	if alerts == nil {
		alerts = []models.Alert{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(alerts)
}

// getAlert returns a single alert by id, including its state history.
func (s *Server) getAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		orgID = claims.OrgID
	}
	alert, err := s.alertStore.GetAlert(r.Context(), orgID, id)
	if err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("get alert failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	history, _ := s.alertStore.GetStateHistory(r.Context(), id)
	notifs, _ := s.alertStore.GetNotificationHistory(r.Context(), id)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"alert":                alert,
		"state_history":        history,
		"notification_history": notifs,
	})
}

// acknowledgeAlert transitions an alert to acknowledged.
func (s *Server) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Acknowledge(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("acknowledge failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"acknowledged"}`))
}

// snoozeAlert transitions an alert to snoozed with a duration from the body.
func (s *Server) snoozeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var body struct {
		DurationMinutes int `json:"duration_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if body.DurationMinutes <= 0 {
		http.Error(w, `{"error":"duration_minutes_required"}`, http.StatusBadRequest)
		return
	}
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := s.alertEngine.Snooze(r.Context(), orgID, id, actor, duration); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("snooze failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"snoozed"}`))
}

// resolveAlert transitions an alert to resolved.
func (s *Server) resolveAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Resolve(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("resolve failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"resolved"}`))
}

// closeAlert transitions an alert to closed.
func (s *Server) closeAlert(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, `{"error":"alert_engine_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	actor := actorFromContext(r)
	orgID := orgIDFromContext(r)
	if err := s.alertEngine.Close(r.Context(), orgID, id, actor); err != nil {
		if errors.Is(err, alerts.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("close failed", "id", id, "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"closed"}`))
}

// listAlertRules returns all alert rules, optionally filtered by org_id.
func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := orgIDFromContext(r)
	rules, err := s.alertStore.GetAlertRules(r.Context(), orgID)
	if err != nil {
		s.log.Error("list alert rules failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rules)
}

// createAlertRule creates a new alert rule.
func (s *Server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if rule.ID == "" {
		rule.ID = uuidNew()
	}
	now := time.Now().UTC()
	rule.CreatedAt = now
	rule.UpdatedAt = now
	if err := s.alertStore.CreateAlertRule(r.Context(), &rule); err != nil {
		s.log.Error("create alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rule)
}

// updateAlertRule updates an existing alert rule.
func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	rule.ID = id
	rule.UpdatedAt = time.Now().UTC()
	if err := s.alertStore.UpdateAlertRule(r.Context(), &rule); err != nil {
		if errors.Is(err, alerts.ErrAlertRuleNotFound) {
			http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("update alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rule)
}

// deleteAlertRule deletes an alert rule by id.
func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := orgIDFromContext(r)
	if err := s.alertStore.DeleteAlertRule(r.Context(), orgID, id); err != nil {
		if errors.Is(err, alerts.ErrAlertRuleNotFound) {
			http.Error(w, `{"error":"rule_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("delete alert rule failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
