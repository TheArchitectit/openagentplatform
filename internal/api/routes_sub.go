package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/internal/checklib"
	"github.com/openagentplatform/openagentplatform/internal/licensing"
)

// mountAPISubRoutes registers the protected /api/v1 sub-routes (everything
// under the auth-verified tenant group except /secrets and /reports, which
// remain in registerRoutes). Extracted from registerRoutes so that file
// stays under the file-size soft limit.
func (s *Server) mountAPISubRoutes(r chi.Router) {
	s.mountCloudRoutes(r)
	s.mountEveRoutes(r)
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

	// Fleet-level alert-suppression windows (RMM-02). Distinct from
	// per-user quiet hours and patch-deploy windows. The feature is
	// commercial-tier gated (Fail-closed to Community per the licensing
	// gater) and requires an elevated role for mutation.
	r.Route("/alert-suppression-windows", func(r chi.Router) {
		r.Use(s.licenseContextMiddleware)
		r.Use(s.gater.RequireFeature(licensing.FeatureAlertSuppressionWindows))
		r.Get("/", s.listSuppressionWindows)
		elevated := auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)
		r.With(elevated).Route("/{id}", func(r chi.Router) {
			r.Put("/", s.updateSuppressionWindow)
			r.Delete("/", s.deleteSuppressionWindow)
		})
		r.With(elevated).Post("/", s.createSuppressionWindow)
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
		// Audit reads are privileged: the log contains actor identities,
		// IPs, and resource references across every org.
		r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician))
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
		// Per-KB WinUpdate state (RMM-03). Read-only; available to any
		// authenticated org member. No licensing gate, no role gate.
		r.Get("/kb", s.handleGetKBBatch)
		// CVE↔KB correlation (RMM-05). Read-only; available to any
		// authenticated org member. No licensing gate, no role gate.
		// Query: ?kb=KB123456 (CVEs for KB) or ?cve=CVE-2024-12345
		// (KBs that fix the CVE).
		r.Get("/cve", s.handleLookupCVE)
		// Reboot coordination (RMM-04). Enqueues a staggered reboot
		// directive for the listed agents. Elevated role required.
		// Mounted before the /{id} group so chi matches the static
		// path before the wildcard.
		r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician)).Post("/reboot", s.handleScheduleReboot)
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

	// Mesh tunnel fabric (RMM-09). Operator tunnel session admission.
	// Org-scoping is enforced inside the handlers from the authenticated
	// session; the mesh endpoints return 503 when meshAdmission is unset.
	r.Route("/mesh", func(r chi.Router) {
		elevated := auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator)
		r.With(elevated).Post("/session", s.handleMeshSessionCreate)
		r.With(elevated).Get("/session", s.handleMeshSessionList)
		r.With(elevated).Post("/session/{id}/close", s.handleMeshSessionClose)
	})

	// Mesh agent self-update releases (RMM-09). Ed25519-signed agent
	// binaries are recorded here as attestation records; the agent verifies
	// the signature at apply time. Org-scoped; returns 503 when
	// meshReleaseStore is unset.
	r.Route("/agents/{id}/releases", func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleTechnician, auth.RoleOperator))
		r.Post("/", s.handleMeshReleaseCreate)
		r.Get("/", s.handleMeshReleaseList)
		r.With(auth.RequireRole(auth.RoleAdmin)).Post("/{version}/pin", s.handleMeshReleasePin)
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
		r.Get("/adapters/{name}/models", s.handleA2AAdapterModels)
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

		// HITL approval queue (hitl-approval spec R1). RegisterHITLRoutes
		// applies its own role gate to the mutating endpoints.
		RegisterHITLRoutes(r, s)
	})

	// Secrets management endpoints. When no resolver is
	// configured these return 503. Resolving a secret is
	// admin-only.

	// Scheduled automation (RMM-06). When no scheduled store is
	// configured these return 503.
	s.mountScheduled(r)
}
