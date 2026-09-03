// Reconciler — per-cluster reconciliation worker (EVE spec §1.2).
//
// Walks the cluster API via a registered HypervisorClient, upserts VM/CT
// resources, archives resources that disappeared, and emits transition
// alerts (vm_down, ha_failover, storage_warning) through the DriftAlertSink.
package eve

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// ClusterStore is the persistence seam for hypervisor_clusters rows.
type ClusterStore interface {
	Get(ctx context.Context, id string) (*models.HypervisorCluster, error)
	ListByOrg(ctx context.Context, orgID string) ([]*models.HypervisorCluster, error)
	UpdateLastSeen(ctx context.Context, id string, at time.Time) error
}

// ResourceStore is the persistence seam for hypervisor_resources rows.
type ResourceStore interface {
	Upsert(ctx context.Context, r *models.HypervisorResource) error
	ListByCluster(ctx context.Context, clusterID string) ([]*models.HypervisorResource, error)
}

// EventStore is the persistence seam for hypervisor_events rows.
type EventStore interface {
	Insert(ctx context.Context, e *models.HypervisorEvent) error
}

// DriftAlertSink is the alert emission seam. The server wires a NATS-backed
// implementation; tests can use a recorder to assert alert traffic.
type DriftAlertSink interface {
	Emit(ctx context.Context, alertType, resourceID string, details map[string]any)
}

type logSlogSink struct {
	log *slog.Logger
}

func (s *logSlogSink) Emit(ctx context.Context, alertType, resourceID string, details map[string]any) {
	s.log.Info("eve: alert", "type", alertType, "resource", resourceID, "details", details)
}

type Reconciler struct {
	store     ClusterStore
	resources ResourceStore
	events    EventStore
	clients   map[string]HypervisorClient
	sink      DriftAlertSink
	log       *slog.Logger
	mu        sync.Mutex
}

func NewReconciler(s ClusterStore, r ResourceStore, e EventStore, log *slog.Logger) *Reconciler {
	return &Reconciler{
		store:     s,
		resources: r,
		events:    e,
		clients:   make(map[string]HypervisorClient),
		sink:      &logSlogSink{log: log},
		log:       log,
	}
}

// SetSink replaces the default log sink with an injected DriftAlertSink.
// Used by server wiring to plug in the NATS-backed alert publisher.
func (r *Reconciler) SetSink(sink DriftAlertSink) { r.sink = sink }

func (r *Reconciler) RegisterClient(provider models.HypervisorProvider, client HypervisorClient) {
	r.mu.Lock()
	r.clients[provider.String()] = client
	r.mu.Unlock()
}

func (r *Reconciler) ReconcileCluster(ctx context.Context, clusterID string) error {
	cluster, err := r.store.Get(ctx, clusterID)
	if err != nil {
		return err
	}
	if cluster == nil || !cluster.Enabled {
		return nil
	}

	r.mu.Lock()
	client, ok := r.clients[cluster.Provider.String()]
	r.mu.Unlock()
	if !ok {
		r.log.Warn("eve: no client for provider", "provider", cluster.Provider)
		return nil
	}

	now := time.Now().UTC()
	prevStatuses := make(map[string]string)

	// Snapshot prior VM statuses for transition detection
	existing, _ := r.resources.ListByCluster(ctx, clusterID)
	for _, res := range existing {
		prevStatuses[res.ResourceID] = res.Status
	}

	// Fetch + upsert VMs
	if vms, err := client.ListVMs(ctx); err == nil {
		for _, vm := range vms {
			res := resourceFromVM(vm, cluster)
			if err := r.resources.Upsert(ctx, res); err != nil {
				r.log.Warn("eve: upsert VM failed", "id", vm.ID, "err", err)
			}
			// Transition: running → stopped
			if prev, ok := prevStatuses[vm.ID]; ok && prev == "running" && vm.Status == "stopped" {
				r.sink.Emit(ctx, "hypervisor_vm_down", vm.ID, map[string]any{
					"cluster_id": cluster.ID,
					"vm_name":    vm.Name,
					"prev":       prev,
					"current":    vm.Status,
				})
			}
		}
	} else {
		r.log.Warn("eve: ListVMs failed", "cluster", clusterID, "err", err)
	}

	// Fetch + upsert LXC containers (Proxmox)
	if cts, err := client.ListContainers(ctx); err == nil {
		for _, ct := range cts {
			res := resourceFromContainer(ct, cluster)
			if err := r.resources.Upsert(ctx, res); err != nil {
				r.log.Warn("eve: upsert CT failed", "id", ct.ID, "err", err)
			}
		}
	}

	// Storage pool utilization alert (>85% by default)
	if pools, err := client.ListStoragePools(ctx); err == nil {
		for _, p := range pools {
			if p.TotalGB > 0 && float64(p.UsedGB)/float64(p.TotalGB) > 0.85 {
				r.sink.Emit(ctx, "hypervisor_storage_warning", p.Name, map[string]any{
					"cluster_id": cluster.ID,
					"pool":       p.Name,
					"used_pct":   float64(p.UsedGB) * 100 / float64(p.TotalGB),
				})
			}
		}
	}

	// Cluster events
	if evs, err := client.ListRecentEvents(ctx, cluster.LastSeen); err == nil {
		for _, ev := range evs {
			he := &models.HypervisorEvent{
				ID:         clusterID + "-" + ev.Timestamp.Format(time.RFC3339Nano),
				OrgID:      cluster.OrgID,
				ClusterID:  clusterID,
				EventType:  ev.Type,
				Payload:    ev.Payload,
				OccurredAt: ev.Timestamp,
				IngestedAt: now,
			}
			if err := r.events.Insert(ctx, he); err != nil {
				r.log.Warn("eve: insert event failed", "cluster", clusterID, "err", err)
			}
			if ev.Type == "ha_failover" {
				r.sink.Emit(ctx, "hypervisor_ha_failover", clusterID, ev.Payload)
			}
		}
	}

	cluster.LastSeen = now
	if err := r.store.UpdateLastSeen(ctx, cluster.ID, now); err != nil {
		r.log.Warn("eve: update cluster last_seen failed", "cluster", clusterID, "err", err)
	}
	return nil
}

func resourceFromVM(vm VMInfo, cluster *models.HypervisorCluster) *models.HypervisorResource {
	return &models.HypervisorResource{
		ID:           cluster.ID + "-vm-" + vm.ID,
		OrgID:        cluster.OrgID,
		ClusterID:    cluster.ID,
		ResourceID:   vm.ID,
		ResourceType: "vm",
		Name:         vm.Name,
		Status:       vm.Status,
		CPUCount:     vm.CPUCount,
		MemoryMB:     vm.MemoryMB,
		DiskGB:       vm.DiskGB,
		LastSeen:     time.Now().UTC(),
	}
}

func resourceFromContainer(ct ContainerInfo, cluster *models.HypervisorCluster) *models.HypervisorResource {
	return &models.HypervisorResource{
		ID:           cluster.ID + "-ct-" + ct.ID,
		OrgID:        cluster.OrgID,
		ClusterID:    cluster.ID,
		ResourceID:   ct.ID,
		ResourceType: "ct",
		Name:         ct.Name,
		Status:       ct.Status,
		CPUCount:     ct.CPUCount,
		MemoryMB:     ct.MemoryMB,
		DiskGB:       ct.DiskGB,
		LastSeen:     time.Now().UTC(),
	}
}
