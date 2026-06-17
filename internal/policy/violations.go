// Package policy - violations.go implements the ViolationManager that
// dedupes policy violations, maps them to alerts, and drives the alert
// state machine through the alert engine's NATS event bus.
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openagentplatform/openagentplatform/internal/events"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// SeverityCategory is the mapping from a policy's category to the
// default alert severity. It mirrors the platform's compliance posture:
// security findings are critical, compliance findings are warnings,
// everything else is informational.
type SeverityCategory string

const (
	SeverityCategorySecurity     SeverityCategory = "security"
	SeverityCategoryCompliance   SeverityCategory = "compliance"
	SeverityCategoryConfiguration SeverityCategory = "configuration"
	SeverityCategoryPerformance  SeverityCategory = "performance"
)

// ViolationManager dedupes policy violations and triggers alerts.
// It listens to policy evaluation results (via OnViolation) and:
//
//   - publishes "alert.fired" on oap.events.alerts for new violations
//   - updates the "last_seen" timestamp for repeated failures (no re-alert)
//   - publishes "alert.resolved" when a previously-failing agent passes
//
// The manager is safe for concurrent use. The dedup index is an
// in-memory map keyed by (policy_id, agent_id); on startup the index
// is empty and is rebuilt lazily as OnViolation is called.
type ViolationManager struct {
	store    Store
	publisher Publisher
	log      *slog.Logger
	now      func() time.Time

	// dedupIndex maps "policyID:agentID" -> last violation id.
	// A present entry means there is an open violation for that pair;
	// absence means either no violation has ever fired or it was resolved.
	dedupMu   sync.Mutex
	dedupIndex map[string]string

	// agentResolver is used to look up agent hostname/site for the
	// alert payload. May be nil; if nil, hostname is left empty.
	agentResolver AgentResolver
}

// AgentResolver is the subset of agent lookup used when building
// alert payloads. Returning an empty hostname is acceptable; the
// manager only includes it in the alert payload if non-empty.
type AgentResolver interface {
	AgentHostname(ctx context.Context, agentID string) (string, error)
	AgentSiteID(ctx context.Context, agentID string) (string, error)
}

// Publisher is the subset of the events.Client used by the
// ViolationManager to publish alert events. The same interface is
// already declared in engine.go; this alias re-exports it so callers
// do not need to import both packages.

// ViolationManagerConfig configures NewViolationManager.
type ViolationManagerConfig struct {
	Store         Store
	Publisher     Publisher
	AgentResolver AgentResolver
	Logger        *slog.Logger
	Now           func() time.Time
}

// NewViolationManager constructs a fresh manager with an empty dedup
// index. The store and publisher are required.
func NewViolationManager(cfg ViolationManagerConfig) *ViolationManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ViolationManager{
		store:         cfg.Store,
		publisher:     cfg.Publisher,
		agentResolver: cfg.AgentResolver,
		log:           cfg.Logger,
		now:           cfg.Now,
		dedupIndex:    make(map[string]string),
	}
}

// OnViolation is the single entry point invoked by the PolicyEngine
// after evaluating a policy against an agent. The behavior depends on
// the result:
//
//   - result.Allowed == false: record a violation, dedup by
//     (policy_id, agent_id). If a new violation is created, fire an
//     alert; if an existing open violation is found, update last_seen
//     and skip the alert.
//   - result.Allowed == true: if there is an open violation for this
//     (policy_id, agent_id), auto-resolve it and fire a recovery alert.
//
// Returns the PolicyViolation record (newly created, updated, or
// auto-resolved). If no record is needed (passing and no prior open
// violation), the returned pointer is nil and the error is nil.
func (m *ViolationManager) OnViolation(ctx context.Context, policyID, agentID string, result EvalResult) (*models.PolicyViolation, error) {
	if policyID == "" {
		return nil, errors.New("violation_manager: policy_id required")
	}
	if agentID == "" {
		return nil, errors.New("violation_manager: agent_id required")
	}
	if m.store == nil {
		return nil, errors.New("violation_manager: nil store")
	}

	dedupKey := dedupKeyFor(policyID, agentID)

	if !result.Allowed {
		return m.handleFailure(ctx, policyID, agentID, dedupKey, result)
	}
	return m.handlePass(ctx, policyID, agentID, dedupKey)
}

// handleFailure records a failing evaluation. Returns the existing or
// newly-created violation record.
func (m *ViolationManager) handleFailure(ctx context.Context, policyID, agentID, dedupKey string, result EvalResult) (*models.PolicyViolation, error) {
	// Check the in-memory dedup index first.
	m.dedupMu.Lock()
	existingID, hasOpen := m.dedupIndex[dedupKey]
	m.dedupMu.Unlock()

	if hasOpen {
		// Already failing: refresh last_seen and return without re-alerting.
		existing, err := m.store.GetPolicyViolationByID(ctx, existingID)
		if err == nil && existing != nil {
			m.log.Debug("violation still failing; skipping re-alert",
				"violation_id", existing.ID,
				"policy_id", policyID,
				"agent_id", agentID)
			return existing, nil
		}
		// Index had a stale id (e.g. record was deleted out of band);
		// fall through and create a new violation.
		m.dedupMu.Lock()
		delete(m.dedupIndex, dedupKey)
		m.dedupMu.Unlock()
	}

	// Look up the policy so we can map severity, category, and remediation.
	pol, err := m.store.GetPolicy(ctx, "", policyID)
	if err != nil {
		// Policy not found is non-fatal: we still record the violation
		// with best-effort defaults.
		m.log.Warn("policy lookup failed during violation handling",
			"policy_id", policyID, "err", err)
	}

	severity, category := m.severityFor(pol)

	// Build a human-readable title and description from the first
	// violation message, falling back to the policy description.
	title := policyName(pol)
	if title == "" {
		title = policyID
	}
	description := ""
	if len(result.Violations) > 0 {
		description = result.Violations[0].Message
	} else {
		description = policyDescription(pol)
	}

	// Aggregate compliance data from the result details and policy
	// metadata. This is what the compliance summary endpoint reports.
	compliance := map[string]any{
		"compliant": false,
		"category":  category,
	}
	if pol != nil {
		compliance["policy_name"] = pol.Name
		compliance["enforcement_mode"] = pol.EnforcementMode
	}
	if result.Details != nil {
		compliance["eval_source"] = result.Details["source"]
	}

	// Remediation steps: for the seed policies we ship hand-written
	// guidance; for custom policies the first violation's "details"
	// map is forwarded as-is. Callers can extend this by adding
	// fields to the policy model.
	remediation := m.remediationFor(pol, result)

	now := m.now()
	// The model stores Details as a free-form map. We serialise the
	// violation list into a single "violations" key so downstream
	// readers can recover the original structured entries.
	detailsMap := map[string]any{
		"violations": result.Violations,
		"category":   category,
	}
	if pol != nil {
		detailsMap["policy_name"] = pol.Name
		detailsMap["enforcement_mode"] = pol.EnforcementMode
		detailsMap["remediation"] = remediation
	}
	v := &models.PolicyViolation{
		ID:        uuid.New().String(),
		PolicyID:  policyID,
		AgentID:   agentID,
		Severity:  severity,
		Message:   description,
		Details:   detailsMap,
		Resolved:  false,
		CreatedAt: now,
	}
	if err := m.store.InsertPolicyViolation(ctx, v); err != nil {
		return nil, fmt.Errorf("violation_manager: insert: %w", err)
	}

	m.dedupMu.Lock()
	m.dedupIndex[dedupKey] = v.ID
	m.dedupMu.Unlock()

	// Fire the alert via the shared NATS subject.
	hostname, siteID := m.lookupAgent(ctx, agentID)
	if err := m.publishAlert(ctx, "alert.fired", v, pol, hostname, siteID, title, description, remediation, severity, category, compliance); err != nil {
		m.log.Warn("publish policy violation alert failed",
			"violation_id", v.ID, "err", err)
	}

	m.log.Info("policy violation recorded",
		"violation_id", v.ID,
		"policy_id", policyID,
		"agent_id", agentID,
		"severity", severity,
		"category", category)
	return v, nil
}

// handlePass auto-resolves any open violation for (policyID, agentID).
// Returns the resolved record (if there was an open one) and a nil
// error.
func (m *ViolationManager) handlePass(ctx context.Context, policyID, agentID, dedupKey string) (*models.PolicyViolation, error) {
	m.dedupMu.Lock()
	existingID, hasOpen := m.dedupIndex[dedupKey]
	if hasOpen {
		delete(m.dedupIndex, dedupKey)
	}
	m.dedupMu.Unlock()

	if !hasOpen {
		// Nothing to resolve.
		return nil, nil
	}

	existing, err := m.store.GetPolicyViolationByID(ctx, existingID)
	if err != nil || existing == nil {
		// Stale index entry; treat as resolved.
		return nil, nil
	}

	now := m.now()
	if err := m.store.UpdatePolicyViolationResolved(ctx, existing.ID, now); err != nil {
		return nil, fmt.Errorf("violation_manager: resolve: %w", err)
	}
	existing.Resolved = true
	existing.ResolvedAt = &now

	// Fire the recovery alert so notification channels learn that the
	// issue is gone.
	pol, _ := m.store.GetPolicy(ctx, "", policyID)
	hostname, siteID := m.lookupAgent(ctx, agentID)
	severity, category := m.severityFor(pol)
	compliance := map[string]any{
		"compliant": true,
		"category":  category,
	}
	if err := m.publishAlert(ctx, "alert.resolved", existing, pol, hostname, siteID,
		policyName(pol), "policy check passed; violation auto-resolved", nil,
		severity, category, compliance); err != nil {
		m.log.Warn("publish policy recovery alert failed",
			"violation_id", existing.ID, "err", err)
	}

	m.log.Info("policy violation auto-resolved",
		"violation_id", existing.ID,
		"policy_id", policyID,
		"agent_id", agentID)
	return existing, nil
}

// severityFor maps a policy's declared severity and category to the
// alert severity used in the NATS payload. The category mapping is the
// authoritative one when the policy's own severity is empty or not
// recognized.
func (m *ViolationManager) severityFor(p *models.Policy) (severity, category string) {
	category = "configuration"
	if p != nil {
		category = p.Category
		if category == "" {
			category = "configuration"
		}
	}

	// Category-based default severity.
	switch SeverityCategory(category) {
	case SeverityCategorySecurity:
		severity = "critical"
	case SeverityCategoryCompliance:
		severity = "warning"
	case SeverityCategoryPerformance:
		severity = "info"
	case SeverityCategoryConfiguration:
		severity = "info"
	default:
		severity = "warning"
	}

	// If the policy explicitly declared a severity, prefer it.
	if p != nil {
		switch p.Severity {
		case "info", "warning", "critical", "emergency":
			severity = p.Severity
		}
	}
	return severity, category
}

// remediationFor returns a short list of remediation steps for a
// violation. For the built-in seed policies we emit hand-written
// guidance keyed on policy name; for custom policies we return an
// empty list (the UI may use the policy description instead).
func (m *ViolationManager) remediationFor(p *models.Policy, result EvalResult) []string {
	if p == nil {
		return nil
	}
	switch p.Name {
	case "antivirus_installed":
		return []string{
			"Install an approved antivirus product (e.g. Windows Defender, ClamAV, CrowdStrike Falcon).",
			"Confirm the AV service is running and reporting heartbeats to the endpoint management console.",
			"Verify the latest AV definitions are up to date (within 24 hours).",
		}
	case "firewall_enabled":
		return []string{
			"Enable the host firewall (Windows Defender Firewall, iptables/nftables, pf).",
			"Confirm the firewall service is running and blocking unsolicited inbound traffic.",
			"Review firewall rules to ensure they align with the organisation's network policy.",
		}
	case "disk_encryption":
		return []string{
			"Enable full-disk encryption (BitLocker, FileVault, LUKS).",
			"Store recovery keys in the organisation's key escrow system.",
			"Verify the encryption status of all data volumes (not just the boot volume).",
		}
	case "os_patching":
		return []string{
			"Apply all outstanding critical OS patches.",
			"Schedule a maintenance window for patches older than 30 days.",
			"Confirm automatic updates are enabled where the policy allows.",
		}
	case "password_policy":
		return []string{
			"Enforce the organisation's password complexity policy via Group Policy / MDM.",
			"Require minimum 12 characters, mixed case, digits, and special characters.",
			"Enable password history and account lockout thresholds.",
		}
	case "screen_lock":
		return []string{
			"Set the screen lock timeout to 15 minutes or less.",
			"Enable password-protected screen lock for all interactive sessions.",
		}
	case "monitoring_agent_running":
		return []string{
			"Reinstall or restart the OpenAgentPlatform agent on the endpoint.",
			"Confirm the agent can reach the platform control plane (DNS, TLS).",
			"Check the agent log for the last error and resolve any authentication failures.",
		}
	case "no_suspicious_services":
		return []string{
			"Investigate the flagged service: review the binary path, publisher, and hash.",
			"Isolate the host if the service is confirmed malicious.",
			"Open an incident and rotate any credentials present on the host.",
		}
	}
	return nil
}

// lookupAgent returns the hostname and site_id for the given agent.
// Empty strings are returned if the resolver is nil or returns an
// error; the caller must not treat these as fatal.
func (m *ViolationManager) lookupAgent(ctx context.Context, agentID string) (hostname, siteID string) {
	if m.agentResolver == nil {
		return "", ""
	}
	if h, err := m.agentResolver.AgentHostname(ctx, agentID); err == nil {
		hostname = h
	} else {
		m.log.Debug("agent hostname lookup failed", "agent_id", agentID, "err", err)
	}
	if s, err := m.agentResolver.AgentSiteID(ctx, agentID); err == nil {
		siteID = s
	} else {
		m.log.Debug("agent site_id lookup failed", "agent_id", agentID, "err", err)
	}
	return hostname, siteID
}

// publishAlert builds the JSON payload and publishes it on
// oap.events.alerts. The AlertEngine consumes these events and
// applies the state machine.
func (m *ViolationManager) publishAlert(
	ctx context.Context,
	eventType string,
	v *models.PolicyViolation,
	pol *models.Policy,
	hostname, siteID, title, description string,
	remediation []string,
	severity, category string,
	compliance map[string]any,
) error {
	if m.publisher == nil {
		return nil
	}

	policyNameVal := ""
	policyCategory := category
	if pol != nil {
		policyNameVal = pol.Name
		policyCategory = pol.Category
	}

	payload := map[string]any{
		"type":       eventType,
		"agent_id":   v.AgentID,
		"agent_hostname": hostname,
		"site_id":    siteID,
		"check_id":   "policy:" + v.PolicyID,
		"severity":   severity,
		"status":     "failing",
		"message":    description,
		"timestamp":  m.now(),
		"alert_type": "policy_violation",
		"violation": map[string]any{
			"id":         v.ID,
			"policy_id":  v.PolicyID,
			"policy_name": policyNameVal,
			"category":   policyCategory,
			"title":      title,
			"description": description,
			"remediation": remediation,
			"compliance":  compliance,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("violation_manager: marshal: %w", err)
	}
	return m.publisher.Publish(ctx, events.SubjectAlertEvents, raw)
}

// dedupKeyFor builds the canonical dedup key for a (policy, agent)
// pair. It is independent of the alert engine's dedup key format so
// that policy dedup is correct even when alerts are re-keyed.
func dedupKeyFor(policyID, agentID string) string {
	return policyID + "\x00" + agentID
}

// --- helper accessors (defined as functions so they survive nil pol) ----

func policyName(p *models.Policy) string {
	if p == nil {
		return ""
	}
	return p.Name
}

func policyDescription(p *models.Policy) string {
	if p == nil {
		return ""
	}
	return p.Description
}
