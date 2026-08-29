package main

import (
	"log/slog"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/gateway"
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
// escalation/reminder loop plus the manager (the A2A gateway's approval
// gate, wired later in buildA2AGateway, attaches to the manager). The
// in-memory store is the only ApprovalStore shipped today; durable
// approval persistence is a follow-on outside R1–R2 scope.
func buildHITLEngine(apiServer *api.Server, alertStore alerts.Store, notifierReg *notify.NotifierRegistry, auditSvc *audit.AuditService, cfg *config.Config, log *slog.Logger) (*hitl.EscalationEngine, *hitl.ApprovalManager) {
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
	return engine, manager
}

// wireHITLGate installs the R5 task gate on the A2A gateway: tasks that
// declare requires_approval metadata are held in input-required and the
// manager's decision hook resumes/fails them.
func wireHITLGate(a2aGw *gateway.Gateway, manager *hitl.ApprovalManager, log *slog.Logger) error {
	gate, err := gateway.NewApprovalGate(manager, a2aGw, log)
	if err != nil {
		return err
	}
	gate.Start()
	a2aGw.SetApprovalGate(gate)
	log.Info("hitl: A2A task approval gate enabled")
	return nil
}
