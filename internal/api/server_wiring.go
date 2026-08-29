package api

import (
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/a2a/hitl"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/monitoring"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/reports"
	"github.com/openagentplatform/openagentplatform/internal/resilience"
	"github.com/openagentplatform/openagentplatform/internal/scheduled"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

// SetScriptStore wires the script definition and run persistence interface
// into the server. Called from main. May be nil; script endpoints return
// 503 when unset.
func (s *Server) SetScriptStore(store scriptStore) {
	s.scriptStore = store
}

// SetSecretsResolver wires the secret resolver and the list of registered
// backend names into the API server. When resolver is nil the secrets
// endpoints return 503.
func (s *Server) SetSecretsResolver(r *resolver.SecretResolver, backendNames []string) {
	s.secretsResolver = r
	s.secretsBackends = backendNames
}

// SetReportsStore wires the reports Store into the API server.
// When nil the reports endpoints return 503.
func (s *Server) SetReportsStore(store reports.Store) {
	s.reportsStore = store
}

// SetReportsScheduler wires the reports Scheduler into the API server.
// When nil the generation/scheduling endpoints return 503.
func (s *Server) SetReportsScheduler(sched *reports.Scheduler) {
	s.reportsScheduler = sched
}

// SetScheduledStore wires the scheduled automation Store into the API server.
// When nil the scheduled automation endpoints return 503.
func (s *Server) SetScheduledStore(store scheduled.Store) {
	s.scheduledStore = store
}

// SetScheduledScheduler wires the scheduled automation Scheduler into the
// API server. When nil the scheduled generation endpoints return 503.
func (s *Server) SetScheduledScheduler(sched *scheduled.Scheduler) {
	s.scheduledScheduler = sched
}

// SetReportsDeliverer wires the report Deliverer into the API server.
// The download endpoint uses it to verify presigned download tokens.
func (s *Server) SetReportsDeliverer(d *reports.DefaultDeliverer) {
	s.reportsDeliverer = d
}

// SetBilling wires the Stripe-backed billing and metering services into the
// API server. Any of the three may be nil (billing is optional); the billing
// and usage endpoints return 503 when their service is unset. When a
// MeteringService is provided the caller is expected to have started its flush
// loop and to Flush it on shutdown.
func (s *Server) SetBilling(stripe *billing.StripeClient, billingSvc *billing.BillingService, metering *billing.MeteringService) {
	s.StripeClient = stripe
	s.BillingService = billingSvc
	s.MeteringService = metering
}

// SetA2AAdapterBridge wires the Python adapter client and the A2A gateway
// into the API server so the /api/v1/a2a/* adapter proxy routes can
// delegate to them. Either may be nil; the proxy returns 503 for the routes
// that depend on an unset dependency.
func (s *Server) SetA2AAdapterBridge(client *bridge.AdapterClient, gw *gateway.Gateway) {
	s.a2aClient = client
	s.a2aGateway = gw
}

// SetHITLManager wires the Human-in-the-Loop approval engine into the API
// server so the /api/v1/a2a/approvals routes can serve it. May be nil; the
// routes then return 503 hitl_not_configured.
func (s *Server) SetHITLManager(m *hitl.ApprovalManager) {
	s.hitlManager = m
}

// SetAdapterBreaker wires the circuit breaker that guards calls to the
// Python adapter service. May be nil; proxy calls then run unbroken.
func (s *Server) SetAdapterBreaker(cb *resilience.CircuitBreaker) {
	s.adapterBreaker = cb
}

// SetHealthChecker wires the aggregated component health checker consulted
// by /readyz. May be nil; /readyz then runs only its built-in probes.
func (s *Server) SetHealthChecker(hc *monitoring.HealthChecker) {
	s.healthChecker = hc
}

// callAdapter runs fn (an adapter-service HTTP round trip) through the
// circuit breaker when one is wired. A nil breaker runs fn directly.
func (s *Server) callAdapter(fn func() error) error {
	if s.adapterBreaker == nil {
		return fn()
	}
	return s.adapterBreaker.Execute(fn)
}

// SetPatchStore wires the patch job persistence interface into the
// server. Called from main. May be nil; patch endpoints return 503 when
// unset.
func (s *Server) SetPatchStore(store patches.Store) {
	s.patchStore = store
}

// SetPatchDeployer wires the patch deployer into the server. Called
// from main after the deployer is constructed. May be nil; the reboot
// coordination endpoint returns 503 when unset.
func (s *Server) SetPatchDeployer(d *patches.PatchDeployer) {
	s.patchDeployer = d
}

// SetPatchScanner wires the patch scan dispatcher into the server.
// Called from main after the dispatcher is constructed. May be nil;
// catalog endpoints return 503 when unset.
func (s *Server) SetPatchScanner(d *patches.PatchScanDispatcher) {
	s.patchScanner = d
}

// SetMeshAdmission wires the mesh tunnel session admission controller into
// the server. Called from main after the controller is constructed. May be
// nil; mesh endpoints return 503 when unset.
func (s *Server) SetMeshAdmission(a MeshAdmission) {
	s.meshAdmission = a
}

// SetMeshReleaseStore wires the mesh agent-release persistence interface into
// the server. Called from main after the store is constructed. May be nil;
// release endpoints return 503 when unset.
func (s *Server) SetMeshReleaseStore(store MeshReleaseStore) {
	s.meshReleaseStore = store
}
