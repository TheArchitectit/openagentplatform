package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/checklib"
	"github.com/openagentplatform/openagentplatform/internal/telemetry"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

const sessionCookieName = "oap_session"

// registerRoutes wires up the public auth flow and the protected API.
func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"openagentplatform","version":"0.1.0"}`))
	})

	// Prometheus scrape and JSON summary endpoints.  These are mounted
	// before auth so scrapers do not need credentials; restrict them at
	// the network layer in production.
	s.metricsRouter(r)

	// Health, readiness, and version probes.  These are mounted
	// before auth so Kubernetes, load balancers, and CI smoke tests
	// can reach them without credentials.  The /debug/* routes are
	// only mounted when DEBUG_MODE is enabled.
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/status", s.handleStatus)
	r.Get("/version", s.handleVersion)
	if s.cfg != nil && s.cfg.DebugMode {
		r.Get("/debug/config", s.handleDebugConfig)
		s.mountPprofRoutes(r)
	}

	// Public auth endpoints.
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", s.handleLogin)
		r.Get("/callback", s.handleCallback)
		r.Post("/logout", s.handleLogout)
		r.Get("/me", s.handleMe)
	})

	// WebSocket upgrade endpoint. Authentication is enforced inside
	// the handler (cookie or ?token=) because WebSocket clients cannot
	// use the same Authorization-header flow as REST calls.
	r.Get("/ws", s.handleWebSocket)

	// Protected API.
	r.Group(func(r chi.Router) {
		r.Use(auth.VerifierMiddleware(s.sessionMinter, s.oidcVerifier, sessionCookieName))
		r.Use(orgContextMiddleware)
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/health", s.healthz)

			// Admin diagnostics dashboard endpoints.
			r.Route("/diagnostics", func(r chi.Router) {
				r.Get("/", s.handleDiagnostics)
				r.Get("/connections", s.handleDiagnosticsConnections)
			})

			r.Route("/agents", func(r chi.Router) {
				r.Get("/", s.listAgents)
				r.Post("/", s.createAgent)
				// Agent registration is mounted here for routing
				// convenience, but it does its own auth via the
				// per-site registration token in the request body
				// (see handleRegisterAgent). The session-cookie
				// verifier middleware will be invoked, but the
				// handler accepts requests without a cookie as long
				// as the registration token validates.
				r.Post("/register", s.handleRegisterAgent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetAgent)
					// Per-agent check-result history. Supports limit,
					// offset, check_id, and status query parameters.
					r.Get("/check-results", s.handleListAgentCheckResults)
				})
			})

			// Platform-wide check-result feed. Supports agent_id,
			// check_id, status, search, limit, and offset filters.
			r.Get("/check-results", s.handleListAllCheckResults)

			r.Route("/sites", func(r chi.Router) {
				r.Get("/", s.listSites)
			})

			r.Route("/checks", func(r chi.Router) {
				r.Get("/", s.handleListChecks)
				r.Post("/", s.handleCreateCheck)
				r.Post("/assign-bulk", s.handleBulkAssign)
				// Built-in check library: catalog + instantiate from template.
				checklib.NewLibrary(s.db).RegisterRoutes(r)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetCheck)
					r.Put("/", s.handleUpdateCheck)
					r.Delete("/", s.handleDeleteCheck)
					r.Post("/run-now", s.handleRunCheckNow)
					r.Post("/assign", s.handleAssignCheck)
					r.Delete("/assign/{agent_id}", s.handleUnassignCheck)
					r.Get("/assignments", s.handleListCheckAssignments)
				})
			})

			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", s.listAlerts)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getAlert)
					r.Post("/acknowledge", s.acknowledgeAlert)
					r.Post("/snooze", s.snoozeAlert)
					r.Post("/resolve", s.resolveAlert)
					r.Post("/close", s.closeAlert)
				})
			})

			r.Route("/alert-rules", func(r chi.Router) {
				r.Get("/", s.listAlertRules)
				r.Post("/", s.createAlertRule)
				r.Route("/{id}", func(r chi.Router) {
					r.Put("/", s.updateAlertRule)
					r.Delete("/", s.deleteAlertRule)
					// Channel mapping for an individual alert rule
					// (alert_rule_channels junction).
					r.Get("/channels", s.getAlertRuleChannels)
					r.Put("/channels", s.putAlertRuleChannels)
				})
			})

			// User-level alert preferences (quiet hours, severity
			// threshold, channel toggles, mute).
			r.Route("/alert-preferences", func(r chi.Router) {
				r.Get("/", s.getUserAlertPreferences)
				r.Put("/", s.putUserAlertPreferences)
				// Global (org-level, admin-only) preferences.
				r.Route("/global", func(r chi.Router) {
					r.Get("/", s.getGlobalAlertPreferences)
					r.Put("/", s.putGlobalAlertPreferences)
				})
			})

			r.Route("/notification-channels", func(r chi.Router) {
				r.Get("/", s.listNotificationChannels)
				r.Post("/", s.createNotificationChannel)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getNotificationChannel)
					r.Put("/", s.updateNotificationChannel)
					r.Delete("/", s.deleteNotificationChannel)
					r.Post("/test", s.testNotificationChannel)
				})
			})

			// Policy engine: Rego-based compliance checks.
			// The /evaluate-site route is mounted first because
			// chi's path matching is order-independent for non-
			// overlapping paths, but we keep it ahead of the
			// /{id} group for readability.
			r.Route("/policies", func(r chi.Router) {
				r.Get("/", s.listPolicies)
				r.Post("/", s.createPolicy)
				r.Post("/evaluate-site", s.evaluateSite)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getPolicy)
					r.Put("/", s.updatePolicy)
					r.Delete("/", s.deletePolicy)
					r.Post("/evaluate", s.evaluatePolicy)
					r.Post("/assign", s.assignPolicy)
					// Per-policy violation feed. Supports resolved
					// and status filters plus pagination.
					r.Get("/violations", s.listViolationsByPolicy)
				})
			})

			// Per-agent violation feed. Lives at /agents/{id}/violations
			// (not under /policies) because it is the agent-centric
			// view used by the endpoint detail page.
			r.Route("/agents/{id}/violations", func(r chi.Router) {
				r.Get("/", s.listViolationsByAgent)
			})

			// Violation lifecycle endpoints (dismiss, remediate).
			r.Route("/violations/{id}", func(r chi.Router) {
				r.Post("/dismiss", s.dismissViolation)
				r.Post("/remediate", s.remediateViolation)
			})

			// Org-level compliance summary used by the dashboard.
			r.Get("/compliance/summary", s.complianceSummary)

			r.Route("/audit", func(r chi.Router) {
				r.Get("/events", s.listAuditEvents)
				r.Route("/events/{id}", func(r chi.Router) {
					r.Get("/", s.getAuditEvent)
				})
				r.Route("/chain/{resource_id}", func(r chi.Router) {
					r.Get("/", s.getAuditChain)
				})
			})

			// Script library: reusable scripts that can be enqueued for
			// execution on one or more agents. The /runs sub-route is
			// mounted before the /{id} group so chi can match
			// /scripts/runs/{run_id} without falling through to the
			// {id} parameter.
			r.Route("/scripts", func(r chi.Router) {
				r.Get("/", s.handleListScripts)
				r.Post("/", s.handleCreateScript)
				// Per-run detail mounted at /scripts/runs/{run_id}
				// so it doesn't collide with /scripts/{id}.
				r.Get("/runs/{run_id}", s.handleGetScriptRun)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetScript)
					r.Put("/", s.handleUpdateScript)
					r.Delete("/", s.handleDeleteScript)
					r.Post("/run", s.handleRunScript)
					r.Get("/runs", s.handleListScriptRuns)
				})
			})

			// Patch approval workflow with RBAC.
			r.Route("/patches", func(r chi.Router) {
				r.Get("/", s.listPatches)
				r.Get("/stats", s.getPatchStats)
				r.Post("/jobs", s.createPatchJob)
				// Catalog: aggregated view of available patches
				// across all agents, plus on-demand scan triggers.
				r.Route("/catalog", func(r chi.Router) {
					r.Get("/", s.listPatchCatalog)
					r.Post("/scan", s.triggerScanAll)
					r.Post("/scan/site/{siteId}", s.triggerScanSite)
				})
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getPatch)
					r.Post("/approve", s.approvePatch)
					r.Post("/reject", s.rejectPatch)
					r.Post("/schedule", s.schedulePatch)
					r.Post("/cancel", s.cancelPatch)
					r.Post("/rollback", s.rollbackPatch)
				})
			})

			// Per-agent patch feed (the agent's own available
			// patches, from the most recent scan). Mounted under
			// /agents/{id}/patches so the endpoint detail page can
			// link directly to it.
			r.Route("/agents/{id}/patches", func(r chi.Router) {
				r.Get("/", s.getAgentPatches)
				r.Post("/scan", s.triggerScanAgent)
			})

			// Remote shell: list/get/kill shell sessions and
			// manage stored credentials. The WebSocket bridge is
			// mounted in the public group below because it does
			// its own authentication (cookie or ?token=).
			r.Route("/shell", func(r chi.Router) {
				r.Get("/sessions", s.handleRemoteListSessions)
				r.Get("/{session_id}", s.handleRemoteGetSession)
				r.Post("/{session_id}/kill", s.handleRemoteKillSession)
				r.Post("/credentials", s.handleRemoteStoreCredential)
				r.Get("/credentials", s.handleRemoteListCredentials)
				r.Delete("/credentials/{id}", s.handleRemoteDeleteCredential)
				// Recorded shell sessions: list, metadata,
				// SSE playback, export, and hard delete.
				// Playback supports speed + from query params;
				// export emits an asciinema v2 .cast file.
				r.Route("/recordings", func(r chi.Router) {
					r.Get("/", s.handleListRecordings)
					r.Route("/{session_id}", func(r chi.Router) {
						r.Get("/", s.handleGetRecording)
						r.Get("/play", s.handlePlayRecording)
						r.Get("/export", s.handleExportRecording)
						r.Delete("/", s.handleDeleteRecording)
					})
				})
			})
			r.Route("/agents/{id}/shell", func(r chi.Router) {
				r.Post("/", s.handleRemoteCreateSession)
			})

			// A2A (Agent-to-Agent) proxy routes. These forward requests
			// from the frontend to the Python adapter service and the
			// A2A gateway, so the UI can discover agents, inspect
			// cards, check health, list tasks, and view cost summaries
			// without needing a direct connection to the adapter
			// service.
			r.Route("/a2a", func(r chi.Router) {
				// Adapter discovery and inspection.
				r.Get("/adapters", s.handleA2AListAdapters)
				r.Get("/adapters/{name}/card", s.handleA2AAdapterCard)
				r.Get("/adapters/{name}/health", s.handleA2AAdapterHealth)

				// A2A task operations.
				r.Get("/tasks", s.handleA2AListTasks)
				r.Get("/tasks/{id}", s.handleA2AGetTask)
				r.Post("/tasks/{id}/cancel", s.handleA2ACancelTask)
				r.Get("/tasks/events", s.handleA2ATaskEvents)
				r.Post("/invoke", s.handleA2AInvoke)
				r.Post("/stream", s.handleA2AStream)

				// Cost and budget summary.
				r.Get("/costs/summary", s.handleA2ACostSummary)
			})

			// Secrets management endpoints. When no resolver is
			// configured these return 503.
			r.Route("/secrets", func(r chi.Router) {
				r.Get("/health", s.handleSecretsHealth)
				r.Post("/resolve", s.handleSecretsResolve)
				r.Get("/backends", s.handleSecretsBackends)
			})
		})
	})

	// Public WebSocket endpoint for shell sessions. Authentication
	// (cookie or ?token=) is enforced inside the handler. We mount
	// this outside the verifier group because the WebSocket upgrade
	// cannot use the standard middleware flow.
	if s.remote != nil {
		r.Get("/api/v1/shell/{session_id}/ws", s.remote.HandleShellWebSocket)
	}

	// Public agent-side endpoint: registration. This is mounted inside
	// the protected group above (see /api/v1/agents/register) because
	// chi does not allow two Route() calls to register the same prefix
	// on the same mux. The registration handler performs its own auth
	// via the per-site registration token in the request body.
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, `{"error":"oidc_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	state, err := randomState()
	if err != nil {
		http.Error(w, `{"error":"state_generation_failed"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oap_oauth_state",
		Value:    state,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	authURL := s.oidcAuthURL(state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback exchanges the OIDC code for an ID token, verifies it, mints
// an internal session JWT, and sets the session cookie.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidcVerifier == nil {
		http.Error(w, `{"error":"oidc_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	stateCookie, err := r.Cookie("oap_oauth_state")
	if err != nil || stateCookie.Value == "" {
		http.Error(w, `{"error":"missing_state_cookie"}`, http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, `{"error":"state_mismatch"}`, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, `{"error":"missing_code"}`, http.StatusBadRequest)
		return
	}

	idToken, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		s.log.Error("oidc code exchange failed", "err", err)
		http.Error(w, `{"error":"code_exchange_failed"}`, http.StatusBadGateway)
		return
	}

	claims, err := s.oidcVerifier.Verify(r.Context(), idToken)
	if err != nil {
		s.log.Error("oidc verify failed", "err", err)
		http.Error(w, `{"error":"id_token_invalid"}`, http.StatusUnauthorized)
		return
	}

	sessionTok, err := s.sessionMinter.Mint(claims)
	if err != nil {
		s.log.Error("session mint failed", "err", err)
		http.Error(w, `{"error":"session_mint_failed"}`, http.StatusInternalServerError)
		return
	}

	s.recordLogin(r, claims)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionTok,
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((1 * time.Hour).Seconds()),
	})
	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "oap_oauth_state",
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// Redirect back to the web UI.
	http.Redirect(w, r, s.cfg.OIDCRedirectURL, http.StatusFound)
}

// handleLogout clears the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.recordLogout(r)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   s.cfg.CookieDomain,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"logged_out"}`))
}

// handleMe returns the authenticated user from the session.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	// Allow either middleware-authenticated requests or direct cookie reads
	// for the browser flow.
	sm := s.sessionMinter
	if sm == nil {
		http.Error(w, `{"error":"session_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	tok := bearerOrCookie(r, sessionCookieName)
	if tok == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	claims, err := sm.Parse(tok)
	if err != nil {
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":    claims.Subject,
		"email":  claims.Email,
		"name":   claims.Name,
		"role":   claims.Role,
		"org_id": claims.OrgID,
	})
}

func (s *Server) listAgents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

func (s *Server) createAgent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{}`))
}

func (s *Server) listSites(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[]`))
}

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
	if err := s.alertEngine.Acknowledge(r.Context(), id, actor); err != nil {
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
	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := s.alertEngine.Snooze(r.Context(), id, actor, duration); err != nil {
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
	if err := s.alertEngine.Resolve(r.Context(), id, actor); err != nil {
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
	if err := s.alertEngine.Close(r.Context(), id, actor); err != nil {
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
	orgID := r.URL.Query().Get("org_id")
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
	if err := s.alertStore.DeleteAlertRule(r.Context(), id); err != nil {
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

// actorFromContext extracts the actor identifier (user subject or "system")
// from the request context. Returns "unknown" if no auth claims are present.
func actorFromContext(r *http.Request) string {
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		if claims.Subject != "" {
			return claims.Subject
		}
	}
	return "unknown"
}

// uuidNew returns a new UUID v4 string. Wrapped here so callers don't
// need to import the uuid package directly.
func uuidNew() string {
	return uuid.New().String()
}

// --- Remote shell route shims -----------------------------------------
//
// These methods all forward to the *RemoteHandler if one is
// configured; otherwise they return 503. Keeping them on Server
// preserves the existing pattern (every other route in this file
// is a method on Server).

func (s *Server) handleRemoteListSessions(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleListShellSessions(w, r)
}

func (s *Server) handleRemoteGetSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleGetShellSession(w, r)
}

func (s *Server) handleRemoteKillSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleKillShellSession(w, r)
}

func (s *Server) handleRemoteCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleCreateShellSession(w, r)
}

func (s *Server) handleRemoteStoreCredential(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleStoreCredential(w, r)
}

func (s *Server) handleRemoteListCredentials(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleListCredentials(w, r)
}

func (s *Server) handleRemoteDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if s.remote == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "remote_not_configured")
		return
	}
	s.remote.HandleDeleteCredential(w, r)
}

// bearerOrCookie extracts a token from Authorization header or session cookie.
func bearerOrCookie(r *http.Request, cookieName string) string {
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// oidcAuthURL builds the authorization URL against the OIDC issuer.
func (s *Server) oidcAuthURL(state string) string {
	u, _ := url.Parse(s.cfg.OIDCIssuerURL + "/auth")
	q := u.Query()
	q.Set("client_id", s.cfg.OIDCClientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile groups")
	q.Set("redirect_uri", s.cfg.OIDCRedirectURL)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// exchangeCode performs the OIDC token exchange using client credentials.
func (s *Server) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {s.cfg.OIDCClientID},
		"client_secret": {s.cfg.OIDCClientSecret},
		"redirect_uri":  {s.cfg.OIDCRedirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.OIDCIssuerURL+"/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Body = http.NoBody

	// Use the stdlib client with a form body.
	client := &http.Client{Timeout: 10 * time.Second}
	req.Body = nil
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.OIDCIssuerURL+"/token", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// Encode form into body using a buffer-backed reader.
	req.Body = formBody(form)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpErr{Status: resp.StatusCode, URL: req.URL.String()}
	}
	var tokenResp struct {
		IDToken string `json:"id_token"`
		Token   string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	if tokenResp.IDToken == "" {
		return "", errors.New("oidc: empty id_token in token response")
	}
	return tokenResp.IDToken, nil
}

type httpErr struct {
	Status int
	URL    string
}

func (e httpErr) Error() string {
	return "oidc: token endpoint returned status " + itoa(e.Status)
}

func itoa(i int) string {
	// minimal alloc-free path
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// randomState returns a random URL-safe string.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	return base64URL(b), nil
}

// recordLogin writes a "login" audit event for a successful OIDC callback.
// Failures are logged but do not block the response.
func (s *Server) recordLogin(r *http.Request, claims *auth.Claims) {
	if s.audit == nil || claims == nil {
		return
	}
	// Use a detached context so the audit write survives the request
	// being cancelled by the browser navigating away.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      claims.Subject,
		Action:       string(audit.EventLogin),
		ResourceType: "session",
		ResourceID:   claims.Subject,
		Details: map[string]any{
			"email": claims.Email,
			"role":  auth.MapGroupsToRole(claims.Groups),
		},
		Outcome:   audit.OutcomeSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		OrgID:     claims.OrgID,
		SiteID:    claims.SiteID,
	})
	if err != nil {
		s.log.Error("audit: login record failed", "err", err)
	}
}

// recordLogout writes a "logout" audit event. We try to attribute the event
// to the authenticated user, but fall back to "unknown" if the session has
// already been invalidated.
func (s *Server) recordLogout(r *http.Request) {
	if s.audit == nil {
		return
	}
	actorID := ""
	orgID := ""
	siteID := ""
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil {
		actorID = claims.Subject
		orgID = claims.OrgID
		siteID = claims.SiteID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorUser,
		ActorID:      actorID,
		Action:       string(audit.EventLogout),
		ResourceType: "session",
		ResourceID:   actorID,
		Outcome:      audit.OutcomeSuccess,
		IP:           clientIP(r),
		UserAgent:    r.UserAgent(),
		OrgID:        orgID,
		SiteID:       siteID,
	})
	if err != nil {
		s.log.Error("audit: logout record failed", "err", err)
	}
}

// clientIP is duplicated from the audit middleware so the auth handlers
// (which run before middleware-injected request IDs) can still attribute
// the event to a client. chi's RealIP middleware sets X-Forwarded-For /
// X-Real-IP, so we honour those here too.
func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		if comma := strings.Index(h, ","); comma >= 0 {
			return strings.TrimSpace(h[:comma])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return strings.TrimSpace(h)
	}
	return r.RemoteAddr
}

// orgContextMiddleware ensures every authenticated request carries an OrgID
// in its session claims. If no org context is present, the request is
// rejected with 400. This enforces multi-tenant isolation: every API call
// must be scoped to the caller's organization.
func orgContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.UserFromContext(r.Context())
		if !ok || claims == nil {
			// No claims means the request is unauthenticated; the auth
			// middleware should have already rejected it.
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if claims.OrgID == "" {
			http.Error(w, `{"error":"org context required"}`, http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps http.ResponseWriter so we can capture the status
// code for metrics emission.  The default http.ResponseWriter does not
// expose the status once it has been written.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// routeLabel returns the chi route pattern for the current request, or
// "unmatched" when the request did not match a registered route.  This is
// what we expose as the "path" label on api_requests_total so we avoid
// high-cardinality URL explosions.
func routeLabel(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}

// metricsMiddleware records request count and duration for every request
// handled by the API.  It should be installed near the top of the
// middleware stack so it captures all responses, including 401s and
// 500s.  The /metrics endpoint itself is excluded to keep the scrape
// from polluting the request rate.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't count scrapes of the metrics endpoint itself.
		if r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/api/v1/metrics") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		path := routeLabel(r)
		status := strconv.Itoa(rec.status)
		telemetry.RecordAPIRequest(r.Method, path, status)
		telemetry.ObserveHTTPRequestDuration(r.Method, path, time.Since(start).Seconds())
	})
}
