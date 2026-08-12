package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/checklib"
	"github.com/openagentplatform/openagentplatform/internal/tenancy"
)

const sessionCookieName = "oap_session"

// registerRoutes wires up the public auth flow and the protected API.

func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"openagentplatform","version":"1.1.0"}`))
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

			r.Route("/checks", func(r chi.Router) {
				r.Get("/", s.handleListChecks)
				// Built-in check library: read-only catalog is available
				// to any org member; instantiating a check from a
				// template is mutating and is gated below.
				lib := checklib.NewLibrary(s.db)
				lib.RegisterReadRoutes(r)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetCheck)
					r.Get("/assignments", s.handleListCheckAssignments)
					// Mutating per-check routes require an elevated role.
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Put("/", s.handleUpdateCheck)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Delete("/", s.handleDeleteCheck)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/run-now", s.handleRunCheckNow)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/assign", s.handleAssignCheck)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Delete("/assign/{agent_id}", s.handleUnassignCheck)
				})
				// Collection-level mutations and template instantiation
				// require an elevated role.
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/", s.handleCreateCheck)
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/assign-bulk", s.handleBulkAssign)
				// Register on a fresh elevated subrouter to keep the
				// POST /library/{id}/create registration isolated.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
					lib.RegisterMutatingRoutes(r)
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
				r.Route("/{id}", func(r chi.Router) {
					// Channel mapping for an individual alert rule
					// (alert_rule_channels junction).
					r.Get("/channels", s.getAlertRuleChannels)
					// Mutating alert-rule configuration requires an
					// elevated role.
					elevated := auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)
					r.With(elevated).Put("/", s.updateAlertRule)
					r.With(elevated).Delete("/", s.deleteAlertRule)
					r.With(elevated).Put("/channels", s.putAlertRuleChannels)
				})
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/", s.createAlertRule)
			})

			// User-level alert preferences (quiet hours, severity
			// threshold, channel toggles, mute).
			r.Route("/alert-preferences", func(r chi.Router) {
				r.Get("/", s.getUserAlertPreferences)
				r.Put("/", s.putUserAlertPreferences)
				// Global (org-level, admin-only) preferences.
				r.Route("/global", func(r chi.Router) {
					r.Get("/", s.getGlobalAlertPreferences)
					r.With(auth.RequireRole(auth.RoleAdmin)).Put("/", s.putGlobalAlertPreferences)
				})
			})

			r.Route("/notification-channels", func(r chi.Router) {
				r.Get("/", s.listNotificationChannels)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getNotificationChannel)
					// Channel management and the /test endpoint (which
					// performs an outbound HTTP fetch to the configured
					// webhook URL) require an elevated role.
					elevated := auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)
					r.With(elevated).Put("/", s.updateNotificationChannel)
					r.With(elevated).Delete("/", s.deleteNotificationChannel)
					r.With(elevated).Post("/test", s.testNotificationChannel)
				})
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/", s.createNotificationChannel)
			})

			// Policy engine: Rego-based compliance checks.
			// The /evaluate-site route is mounted first because
			// chi's path matching is order-independent for non-
			// overlapping paths, but we keep it ahead of the
			// /{id} group for readability.
			r.Route("/policies", func(r chi.Router) {
				r.Get("/", s.listPolicies)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getPolicy)
					// Per-policy violation feed. Supports resolved
					// and status filters plus pagination.
					r.Get("/violations", s.listViolationsByPolicy)
					// Creating or modifying a policy compiles/stores a
					// Rego module, and evaluation/assignment runs it
					// against live resources, so these are admin-only.
					r.With(auth.RequireRole(auth.RoleAdmin)).Put("/", s.updatePolicy)
					r.With(auth.RequireRole(auth.RoleAdmin)).Delete("/", s.deletePolicy)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/evaluate", s.evaluatePolicy)
					r.With(auth.RequireRole(auth.RoleAdmin)).Post("/assign", s.assignPolicy)
				})
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/", s.createPolicy)
				r.With(auth.RequireRole(auth.RoleAdmin)).Post("/evaluate-site", s.evaluateSite)
			})

			// Per-agent violation feed. Lives at /agents/{id}/violations
			// (not under /policies) because it is the agent-centric
			// view used by the endpoint detail page.
			r.Route("/agents/{id}/violations", func(r chi.Router) {
				r.Get("/", s.listViolationsByAgent)
			})

			// Violation lifecycle endpoints (dismiss, remediate). These
			// change compliance state and require an elevated role.
			r.Route("/violations/{id}", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
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

			// Billing: commercial-tier management (customers, subscriptions,
			// invoices, usage). All routes return 503 if the BillingService
			// or StripeClient is not wired into the Server. Billing actions
			// are admin-only.
			r.Route("/billing", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin))
				r.Post("/create-customer", s.handleCreateCustomer)
				r.Post("/create-subscription", s.handleCreateSubscription)
				r.Get("/subscription", s.handleGetSubscription)
				r.Post("/cancel", s.handleCancelSubscription)
				r.Get("/invoices", s.handleGetInvoices)
				r.Get("/usage", s.handleGetUsage)
			})

			// Script library: reusable scripts that can be enqueued for
			// execution on one or more agents. The /runs sub-route is
			// mounted before the /{id} group so chi can match
			// /scripts/runs/{run_id} without falling through to the
			// {id} parameter.
			//
			// Creating, modifying, or running a script publishes a
			// script_body that is executed on an endpoint agent, so all
			// mutating routes require an elevated role. Read-only list
			// and detail endpoints remain available to any authenticated
			// member of the org.
			r.Route("/scripts", func(r chi.Router) {
				r.Get("/", s.handleListScripts)
				// Per-run detail mounted at /scripts/runs/{run_id}
				// so it doesn't collide with /scripts/{id}.
				r.Get("/runs/{run_id}", s.handleGetScriptRun)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetScript)
					r.Get("/runs", s.handleListScriptRuns)
					// Mutating + execution routes require an elevated
					// role (POST /run publishes script_body to agents).
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Put("/", s.handleUpdateScript)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Delete("/", s.handleDeleteScript)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Post("/run", s.handleRunScript)
				})
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)).Post("/", s.handleCreateScript)
			})

			// Patch approval workflow with RBAC. Read-only catalog and
			// per-patch views are available to any authenticated org
			// member; creating jobs, triggering scans, and the approval
			// lifecycle (approve/reject/schedule/cancel/rollback) require
			// an elevated role because they drive patch deployment on
			// endpoints.
			r.Route("/patches", func(r chi.Router) {
				r.Get("/", s.listPatches)
				r.Get("/stats", s.getPatchStats)
				r.Route("/catalog", func(r chi.Router) {
					r.Get("/", s.listPatchCatalog)
					// Scan triggers require an elevated role.
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/scan", s.triggerScanAll)
					r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/scan/site/{siteId}", s.triggerScanSite)
				})
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.getPatch)
					// Approval lifecycle requires an elevated role.
					elevated := auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)
					r.With(elevated).Post("/approve", s.approvePatch)
					r.With(elevated).Post("/reject", s.rejectPatch)
					r.With(elevated).Post("/schedule", s.schedulePatch)
					r.With(elevated).Post("/cancel", s.cancelPatch)
					r.With(elevated).Post("/rollback", s.rollbackPatch)
				})
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/jobs", s.createPatchJob)
			})

			// Per-agent patch feed (the agent's own available
			// patches, from the most recent scan). Mounted under
			// /agents/{id}/patches so the endpoint detail page can
			// link directly to it.
			r.Route("/agents/{id}/patches", func(r chi.Router) {
				r.Get("/", s.getAgentPatches)
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/scan", s.triggerScanAgent)
			})

			// Remote shell: list/get/kill shell sessions and
			// manage stored credentials. The WebSocket bridge is
			// mounted in the public group below because it does
			// its own authentication (cookie or ?token=). All
			// remote-shell access requires an elevated role.
			r.Route("/shell", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
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
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
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

				// A2A task read operations + cost summary.
				r.Get("/tasks", s.handleA2AListTasks)
				r.Get("/tasks/{id}", s.handleA2AGetTask)
				r.Get("/tasks/events", s.handleA2ATaskEvents)
				r.Get("/costs/summary", s.handleA2ACostSummary)
				// Invoking, streaming, and cancelling A2A tasks drives
				// agent work and spend, so they require an elevated
				// role.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
					r.Post("/tasks/{id}/cancel", s.handleA2ACancelTask)
					r.Post("/invoke", s.handleA2AInvoke)
					r.Post("/stream", s.handleA2AStream)
				})
			})

			// Secrets management endpoints. When no resolver is
			// configured these return 503. Resolving a secret is
			// admin-only.
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
