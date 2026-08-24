package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/audit"
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
		s.log.Error("list alerts failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
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
	if err := validateAlertRule(&rule); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	orgID := orgIDFromContext(r)
	if orgID == "" {
		http.Error(w, `{"error":"org context required"}`, http.StatusBadRequest)
		return
	}
	rule.OrgID = orgID
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
	if err := validateAlertRule(&rule); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
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

// maxOfflineSilenceSeconds bounds the offline-sla silence window to 30 days.
const maxOfflineSilenceSeconds = 30 * 24 * 60 * 60

// validateAlertRule applies input bounds to the rule. The offline-silence
// condition, when present, must be positive and at most 30 days — an absent
// or zero value means the rule has no silence condition (backward compatible).
func validateAlertRule(rule *models.AlertRule) error {
	if rule.OfflineSilenceSeconds == nil {
		return nil
	}
	v := *rule.OfflineSilenceSeconds
	if v <= 0 {
		return errors.New("offline_silence_seconds must be positive")
	}
	if v > maxOfflineSilenceSeconds {
		return errors.New("offline_silence_seconds exceeds maximum (30 days)")
	}
	return nil
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

// --- Fleet alert-suppression windows (RMM-02) ---------------------------

// listSuppressionWindows returns all alert-suppression windows for the org.
func (s *Server) listSuppressionWindows(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	orgID := orgIDFromContext(r)
	windows, err := s.alertStore.GetAlertSuppressionWindows(r.Context(), orgID)
	if err != nil {
		s.log.Error("list suppression windows failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	if windows == nil {
		windows = []models.AlertSuppressionWindow{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(windows)
}

// createSuppressionWindow creates a new alert-suppression window.
func (s *Server) createSuppressionWindow(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	var w2 models.AlertSuppressionWindow
	if err := json.NewDecoder(r.Body).Decode(&w2); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if err := validateSuppressionWindow(&w2); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	orgID := orgIDFromContext(r)
	now := time.Now().UTC()
	w2.OrgID = orgID
	if w2.ID == "" {
		w2.ID = uuidNew()
	}
	w2.CreatedAt = now
	w2.UpdatedAt = now
	if err := s.alertStore.CreateAlertSuppressionWindow(r.Context(), &w2); err != nil {
		s.log.Error("create suppression window failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	s.recordSuppressionWindowAudit(r, "alert_suppression_window.create", w2.ID, map[string]any{
		"name":      w2.Name,
		"org_id":    w2.OrgID,
		"client_id": w2.ClientID,
		"site_id":   w2.SiteID,
		"recurring": w2.Recurring,
	})
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(w2)
}

// updateSuppressionWindow updates an existing alert-suppression window.
func (s *Server) updateSuppressionWindow(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	var w2 models.AlertSuppressionWindow
	if err := json.NewDecoder(r.Body).Decode(&w2); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, http.StatusBadRequest)
		return
	}
	if err := validateSuppressionWindow(&w2); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	orgID := orgIDFromContext(r)
	w2.ID = id
	w2.OrgID = orgID
	w2.UpdatedAt = time.Now().UTC()
	if err := s.alertStore.UpdateAlertSuppressionWindow(r.Context(), &w2); err != nil {
		if errors.Is(err, alerts.ErrAlertSuppressionWindowNotFound) {
			http.Error(w, `{"error":"window_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("update suppression window failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	s.recordSuppressionWindowAudit(r, "alert_suppression_window.update", w2.ID, map[string]any{
		"name":      w2.Name,
		"org_id":    w2.OrgID,
		"client_id": w2.ClientID,
		"site_id":   w2.SiteID,
		"recurring": w2.Recurring,
	})
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(w2)
}

// deleteSuppressionWindow deletes an alert-suppression window by id.
func (s *Server) deleteSuppressionWindow(w http.ResponseWriter, r *http.Request) {
	if s.alertStore == nil {
		http.Error(w, `{"error":"alert_store_not_configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := chi.URLParam(r, "id")
	orgID := orgIDFromContext(r)
	if err := s.alertStore.DeleteAlertSuppressionWindow(r.Context(), orgID, id); err != nil {
		if errors.Is(err, alerts.ErrAlertSuppressionWindowNotFound) {
			http.Error(w, `{"error":"window_not_found"}`, http.StatusNotFound)
			return
		}
		s.log.Error("delete suppression window failed", "err", err)
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	s.recordSuppressionWindowAudit(r, "alert_suppression_window.delete", id, map[string]any{
		"org_id": orgID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// maxSuppressionWindowDuration bounds a non-recurring window to 30 days.
const maxSuppressionWindowDuration = 30 * 24 * time.Hour

// maxSuppressionWindowNameLen bounds the window name length. It matches the
// alert-rule name bound (see validateAlertRule / alert_rules.name).
const maxSuppressionWindowNameLen = 120

// validateSuppressionWindow applies input bounds to the window. The window
// must have a non-empty bounded name, a non-zero start/end, and — for
// non-recurring windows — end after start within the 30-day cap. Recurring
// windows MAY be overnight (end time-of-day earlier than start is valid);
// their Weekdays must each be valid time.Weekday values (0-6), de-duplicated.
func validateSuppressionWindow(w *models.AlertSuppressionWindow) error {
	if w.Name == "" {
		return errors.New("name is required")
	}
	if len(w.Name) > maxSuppressionWindowNameLen {
		return errors.New("name exceeds maximum length")
	}
	if w.Start.IsZero() || w.End.IsZero() {
		return errors.New("start and end are required")
	}
	if w.Recurring {
		// A recurring window evaluates weekday and time-of-day in its
		// timezone, so an explicit non-empty timezone must be a valid IANA
		// location. Empty is allowed (store falls back to UTC); an invalid
		// non-empty string is a client error, not a server-side default.
		if w.Timezone != "" {
			if _, err := time.LoadLocation(w.Timezone); err != nil {
				return errors.New("invalid timezone")
			}
		}
		// Validate and de-duplicate Weekdays. Each must be a valid
		// time.Weekday (0-6). Order is preserved for stable storage.
		seen := make(map[time.Weekday]bool, len(w.Weekdays))
		valid := make([]time.Weekday, 0, len(w.Weekdays))
		for _, d := range w.Weekdays {
			if d < time.Sunday || d > time.Saturday {
				return errors.New("weekdays must be valid day values (0-6)")
			}
			if !seen[d] {
				seen[d] = true
				valid = append(valid, d)
			}
		}
		w.Weekdays = valid
		// Overnight recurring windows (end time-of-day before start) are
		// valid; the model evaluates them across midnight.
		return nil
	}
	if !w.End.After(w.Start) {
		return errors.New("end must be after start")
	}
	if w.End.Sub(w.Start) > maxSuppressionWindowDuration {
		return errors.New("window duration exceeds maximum (30 days)")
	}
	return nil
}

// recordSuppressionWindowAudit writes an audit event for a fleet-level
// alert-suppression-window mutation. It reuses the alert-change event type
// (the closest semantic fit in internal/audit/audit.go) so window
// lifecycle changes are hash-chained alongside other alert-related actions.
func (s *Server) recordSuppressionWindowAudit(r *http.Request, action, resourceID string, details map[string]any) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       action,
		ResourceType: "alert_suppression_window",
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
	}); err != nil {
		s.log.Error("audit: suppression window event failed", "action", action, "err", err)
	}
}
