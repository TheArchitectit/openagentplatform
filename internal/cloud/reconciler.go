package cloud

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// AgentStore is the minimal agent interface needed by the reconciler for
// virtual-agent auto-enrollment. The full agent store has many more methods.
type AgentStore interface {
	GetByCloudID(ctx context.Context, provider models.CloudProvider, cloudID string) (*models.Agent, error)
	CreateVirtual(ctx context.Context, a *models.Agent) error
}

// Reconciler is the per-org reconciliation worker. It walks the cloud
// inventory via the registered provider clients, upserts resources,
// archives resources that disappeared, runs drift detection against
// policies, and (when enabled) auto-enrolls unmanaged resources as
// virtual agents.
type Reconciler struct {
	accountsStore AccountStore
	resourceStore ResourceStore
	policyStore   PolicyStore
	costStore     CostStore
	agentStore    AgentStore
	providers     map[string]ProviderClient
	driftSink     DriftAlertSink
	log           *slog.Logger
	mu            sync.Mutex
	autoEnroll    bool
}

func NewReconciler(
	as AccountStore,
	rs ResourceStore,
	ps PolicyStore,
	cs CostStore,
	as2 AgentStore,
	log *slog.Logger,
) *Reconciler {
	return &Reconciler{
		accountsStore: as,
		resourceStore: rs,
		policyStore:   ps,
		costStore:     cs,
		agentStore:    as2,
		providers:     make(map[string]ProviderClient),
		log:           log,
	}
}

func (r *Reconciler) RegisterProvider(p ProviderClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// SetAutoEnroll toggles virtual-agent auto-enrollment per process. The
// spec keeps this opt-in (§3.2): orgs without this flag see new resources
// in a "Pending Enrollment" queue (TBD in a follow-up; the implementation
// here simply skips the enrollment step).
func (r *Reconciler) SetAutoEnroll(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoEnroll = enabled
}

func (r *Reconciler) ReconcileOrg(ctx context.Context, orgID string) error {
	accounts, err := r.accountsStore.ListByOrg(ctx, orgID)
	if err != nil {
		return err
	}

	for _, acct := range accounts {
		if !acct.Enabled {
			continue
		}
		client, ok := r.lookupProvider(string(acct.Provider))
		if !ok {
			r.log.Warn("cloud: no provider client for", "provider", acct.Provider)
			continue
		}

		// Fetch all resources in the account
		resources, err := client.ListResources(ctx, "", acct.AccountID, "")
		if err != nil {
			r.log.Warn("cloud: ListResources failed", "account", acct.AccountID, "err", err)
			continue
		}

		// Upsert each resource into the table
		seen := make(map[string]bool)
		for i := range resources {
			res := &resources[i]
			res.OrgID = orgID
			res.AccountID = acct.AccountID
			if err := r.resourceStore.Upsert(ctx, res); err != nil {
				r.log.Warn("cloud: upsert resource failed", "id", res.ResourceID, "err", err)
			}
			seen[res.ResourceID] = true
		}

		// Archive resources that disappeared from the cloud API
		existing, err := r.resourceStore.ListByOrg(ctx, orgID, ResourceFilter{
			Provider:  string(acct.Provider),
			AccountID: acct.AccountID,
		})
		if err == nil {
			for _, res := range existing {
				if !seen[res.ResourceID] && res.ArchivedAt == nil {
					if err := r.resourceStore.Archive(ctx, res.ID); err != nil {
						r.log.Warn("cloud: archive failed", "id", res.ID, "err", err)
					}
				}
			}
		}

		// Auto-enroll unmanaged resources as virtual agents (spec §3).
		// Only fires when the org-level flag is set and the reconciler has
		// an AgentStore wired in. A nil AgentStore (e.g. tests) is a no-op.
		r.mu.Lock()
		autoEnroll := r.autoEnroll
		r.mu.Unlock()
		if autoEnroll && r.agentStore != nil {
			for i := range resources {
				res := &resources[i]
				agentID := "cloud-" + string(res.Provider) + "-" + res.ResourceID
				existing, err := r.agentStore.GetByCloudID(ctx, res.Provider, res.ResourceID)
				if err == nil && existing != nil {
					continue
				}
				if err := r.agentStore.CreateVirtual(ctx, &models.Agent{
					ID:              agentID,
					OrgID:           orgID,
					SiteID:          "",
					Hostname:        res.Name,
					OperatingSystem: string(res.Provider),
					Platform:        "cloud/" + string(res.Provider),
					Tags: []string{
						"cloud:enrolled",
						"cloud:provider:" + string(res.Provider),
					},
					Status: "virtual",
				}); err != nil {
					r.log.Warn("cloud: auto-enroll failed", "id", agentID, "err", err)
				}
			}
		}

		// Run drift detection against the org's policies for this account
		policies, _ := r.policyStore.ListByOrg(ctx, orgID)
		for _, pol := range policies {
			if !pol.Enabled || pol.Provider != acct.Provider || pol.AccountID != acct.AccountID {
				continue
			}
			r.checkDrift(pol, resources)
		}

		// Fetch and persist the cost snapshot for the current billing period
		period := time.Now().UTC().Format("2006-01")
		costInfo, _ := client.GetCost(ctx, "", acct.AccountID, period)
		if costInfo.TotalCostUSD > 0 {
			snapshot := &models.CostSnapshot{
				ID:            orgID + "-" + acct.AccountID + "-" + period,
				OrgID:         orgID,
				Provider:      acct.Provider,
				AccountID:     acct.AccountID,
				BillingPeriod: period,
				TotalCostUSD:  costInfo.TotalCostUSD,
				ServiceCosts:  costInfo.ServiceCosts,
				CreatedAt:     time.Now().UTC(),
			}
			if err := r.costStore.Insert(ctx, snapshot); err != nil {
				r.log.Warn("cloud: cost snapshot insert failed", "account", acct.AccountID, "err", err)
			}
		}
	}
	return nil
}

// checkDrift emits a `cloud_drift` alert (via the central AlertSink if
// configured) for every missing-required-tag and invalid-tag-value. The
// sink is optional to keep this package testable without a full alert
// pipeline; the server wiring installs the real sink.
func (r *Reconciler) checkDrift(pol *models.CloudPolicy, resources []models.CloudResource) {
	if r.driftSink == nil {
		return
	}
	for i := range resources {
		res := &resources[i]
		for _, required := range pol.RequiredTags {
			if _, ok := res.Tags[required]; !ok {
				r.driftSink.Emit(DriftAlert{
					Type:       "cloud_drift",
					Severity:   "warning",
					ResourceID: res.ID,
					Message:    "missing_required_tag: " + required,
					Details:    map[string]any{"resource": res.Name, "tag": required},
				})
			}
		}
		for tagKey, allowed := range pol.TagRules {
			if actual, ok := res.Tags[tagKey]; ok {
				allowedMap := make(map[string]bool, len(allowed))
				for _, v := range allowed {
					allowedMap[v] = true
				}
				if !allowedMap[actual] {
					r.driftSink.Emit(DriftAlert{
						Type:       "cloud_drift",
						Severity:   "warning",
						ResourceID: res.ID,
						Message:    "invalid_tag_value: " + tagKey + "=" + actual,
						Details: map[string]any{
							"resource": res.Name,
							"tag":      tagKey,
							"actual":   actual,
							"allowed":  allowed,
						},
					})
				}
			}
		}
	}
}

// DriftAlertSink is the seam the reconciler uses to emit drift events.
// The default server wiring plugs in a NATS-backed sink; tests use a
// no-op or capture sink.
type DriftAlertSink interface {
	Emit(DriftAlert)
}

// DriftAlert is the minimal shape of a cloud drift alert.
type DriftAlert struct {
	Type       string
	Severity   string
	ResourceID string
	Message    string
	Details    map[string]any
}

// SetDriftSink installs a sink for drift alerts. Without one the
// reconciler still runs but does not emit drift events.
func (r *Reconciler) SetDriftSink(s DriftAlertSink) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.driftSink = s
}

func (r *Reconciler) lookupProvider(name string) (ProviderClient, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.providers[name]
	return p, ok
}
