package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
)

const sessionCookieName = "oap_session"

// registerRoutes wires up the public auth flow and the protected API.

func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"openagentplatform","version":"1.2.0"}`))
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

	// Stripe webhook endpoint. Authentication is enforced inside the
	// handler via the Stripe-Signature header — Stripe's HMAC is the
	// only credential we accept here.
	r.Route("/api/v1/billing", func(r chi.Router) {
		r.Post("/webhook", s.handleBillingWebhook)
	})

	// Protected API.
	r.Group(func(r chi.Router) {
		r.Use(auth.VerifierMiddleware(s.sessionMinter, s.oidcVerifier, sessionCookieName))
		r.Use(orgContextMiddleware)
		r.Use(tenancy.TenantMiddleware(s.resolveOrgTier))
		r.Use(tenancy.QuotaMiddleware(s.currentOrgUsage))
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/health", s.healthz)

			// Admin diagnostics dashboard endpoints.
			r.Route("/diagnostics", func(r chi.Router) {
				r.Get("/", s.handleDiagnostics)
				r.Get("/connections", s.handleDiagnosticsConnections)
			})

			r.Route("/agents", func(r chi.Router) {
				r.Get("/", s.listAgents)
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
				// Manually creating an agent record requires an
				// elevated role.
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Post("/", s.createAgent)
			})

			// Platform-wide check-result feed. Supports agent_id,
			// check_id, status, search, limit, and offset filters.
			r.Get("/check-results", s.handleListAllCheckResults)

			r.Route("/sites", func(r chi.Router) {
				r.Get("/", s.listSites)
			})

			s.mountAPISubRoutes(r)
			r.Route("/secrets", func(r chi.Router) {
				r.Get("/health", s.handleSecretsHealth)
				r.Get("/backends", s.handleSecretsBackends)
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/resolve", s.handleSecretsResolve)
			})

			// Enterprise reporting: templates, on-demand generation,
			// run history, and cron-based schedules. Endpoints return
			// 503 when the reports Store / Scheduler is not wired.
			// Reading templates and run history is open; generating
			// reports and managing schedules requires an elevated
			// role.
			r.Route("/reports", func(r chi.Router) {
				r.Get("/templates", s.listReportTemplates)
				r.Get("/runs", s.listReportRuns)
				r.Route("/runs/{id}", func(r chi.Router) {
					r.Get("/", s.getReportRun)
					// Presigned download: token auth, not session auth
					// (links are opened outside the app).
					r.Get("/download", s.downloadReport)
				})
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
					r.Post("/generate", s.generateReport)
					r.Route("/schedules", func(r chi.Router) {
						r.Post("/", s.createReportSchedule)
						r.Get("/", s.listReportSchedules)
						r.Delete("/{id}", s.deleteReportSchedule)
					})
				})
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
