package main

import (
	"log/slog"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/alerts"
	"github.com/openagentplatform/openagentplatform/internal/api"
	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/internal/config"
	"github.com/openagentplatform/openagentplatform/internal/notify"
)

// hitlCheckInterval is how often the approval engine scans for timeouts,
// escalations, and due re-notifications (R2.4 / R3). 30s keeps the
// worst-case reminder/timeout slip well under a minute of the configured
// per-type durations (30m–8h).
const hitlCheckInterval = 30 * time.Second

// buildHITLEngine constructs the Human-in-the-Loop approval engine
// (hitl-approval spec), wires its notification delivery through the
// platform's existing channel infrastructure, and returns the
// escalation/reminder loop that Run() starts. The in-memory store is the
// only ApprovalStore shipped today; durable approval persistence is a
// follow-on outside R1–R2 scope.
func buildHITLEngine(apiServer *api.Server, alertStore alerts.Store, notifierReg *notify.NotifierRegistry, auditSvc *audit.AuditService, cfg *config.Config, log *slog.Logger) *hitl.EscalationEngine {
	typeCfgs := hitl.DefaultApprovalTypes()
	manager := hitl.NewApprovalManager(typeCfgs)
	manager.SetStore(hitl.NewMemStore())
	// Default re-notification delay between create and the first reminder
	// for types without a per-type override (spec R2.4).
	manager.SetDefaultReminderInterval(30 * time.Minute)

	notifier := api.NewApprovalNotifier(alertStore, notifierReg, typeCfgs, cfg.PublicBaseURL, log)
	manager.SetNotifier(notifier)
	// R4.4: mirror the approval lifecycle into the tamper-evident audit trail.
	api.WireHITLAudit(manager, auditSvc, log)
	apiServer.SetHITLManager(manager)

	engine := hitl.NewEscalationEngine(manager, hitlCheckInterval)
	log.Info("hitl: approval engine enabled",
		"types", len(typeCfgs), "check_interval", hitlCheckInterval)
	return engine
}
