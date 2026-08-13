package api

import (
	"github.com/openagentplatform/openagentplatform/a2a/bridge"
	"github.com/openagentplatform/openagentplatform/a2a/gateway"
	"github.com/openagentplatform/openagentplatform/internal/billing"
	"github.com/openagentplatform/openagentplatform/internal/patches"
	"github.com/openagentplatform/openagentplatform/internal/reports"
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

// SetPatchStore wires the patch job persistence interface into the
// server. Called from main. May be nil; patch endpoints return 503 when
// unset.
func (s *Server) SetPatchStore(store patches.Store) {
	s.patchStore = store
}

// SetPatchScanner wires the patch scan dispatcher into the server.
// Called from main after the dispatcher is constructed. May be nil;
// catalog endpoints return 503 when unset.
func (s *Server) SetPatchScanner(d *patches.PatchScanDispatcher) {
	s.patchScanner = d
}
