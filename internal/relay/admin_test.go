package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// certWith builds a x509.Certificate carrying the given role SAN (empty to omit)
// and the given tenant URI SANs. Mirrors the RELAY-02/RELAY-04 trust convention:
// `oap:role:<role>` and `oap:tenant:<tenantID>`.
func certWith(role string, tenants ...string) *x509.Certificate {
	c := &x509.Certificate{}
	if role != "" {
		c.Subject = pkix.Name{CommonName: "oap:role:" + role}
	}
	for _, t := range tenants {
		u, err := url.Parse("oap:tenant:" + t)
		if err != nil {
			panic(err)
		}
		c.URIs = append(c.URIs, u)
	}
	return c
}

// reqWithCert builds a GET request carrying the given cert (nil for no cert).
func reqWithCert(cert *x509.Certificate, target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if cert != nil {
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	}
	return r
}

// newAdminFixture returns an admin server over a relay service that has two
// tenants with established connections and recorded bytes.
func newAdminFixture() *AdminServer {
	svc := NewRelayService(RelayConfig{MaxConnections: 100}, nil)
	ctx := context.Background()
	c1, _ := svc.EstablishConnection(ctx, "tenant-a", "agent1", "agent2")
	_ = svc.RecordBytes(ctx, c1.ID, 1024)
	c2, _ := svc.EstablishConnection(ctx, "tenant-b", "agent3", "agent4")
	_ = svc.RecordBytes(ctx, c2.ID, 2048)
	return NewAdminServer(svc, nil)
}

func TestOperatorIdentity_RoleAndTenants(t *testing.T) {
	p, ok := operatorIdentity(reqWithCert(certWith("relay-operator", "acme", "globex"), "/"))
	if !ok {
		t.Fatal("expected cert to be present")
	}
	if p.Role != "relay-operator" {
		t.Errorf("Role = %q, want relay-operator", p.Role)
	}
	if !p.Tenants["acme"] || !p.Tenants["globex"] || len(p.Tenants) != 2 {
		t.Errorf("Tenants = %v, want {acme, globex}", p.Tenants)
	}
}

func TestOperatorIdentity_AdminNoTenants(t *testing.T) {
	p, ok := operatorIdentity(reqWithCert(certWith("relay-admin"), "/"))
	if !ok {
		t.Fatal("expected cert to be present")
	}
	if p.Role != "relay-admin" {
		t.Errorf("Role = %q, want relay-admin", p.Role)
	}
	if len(p.Tenants) != 0 {
		t.Errorf("Tenants = %v, want empty", p.Tenants)
	}
}

func TestOperatorIdentity_NoCert(t *testing.T) {
	if _, ok := operatorIdentity(reqWithCert(nil, "/")); ok {
		t.Fatal("expected no cert to report not-ok")
	}
}

func TestAdminHandleHealth_NoCertRejected(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleHealth(w, reqWithCert(nil, "/admin/health"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAdminHandleHealth_OK(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleHealth(w, reqWithCert(certWith("relay-operator", "tenant-a"), "/admin/health"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var h healthResponse
	if err := json.NewDecoder(w.Body).Decode(&h); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if h.Status != "ok" {
		t.Errorf("Status = %q, want ok", h.Status)
	}
	if h.ActiveConnections != 2 {
		t.Errorf("ActiveConnections = %d, want 2", h.ActiveConnections)
	}
}

func TestAdminHandleMetrics_NoCertRejected(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(nil, "/admin/metrics"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestAdminHandleMetrics_UnrecognizedRoleForbidden(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(certWith("relay-viewer", "tenant-a"), "/admin/metrics"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestAdminHandleMetrics_AdminSeesAll(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(certWith("relay-admin"), "/admin/metrics"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var m metricsResponse
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Tenants) != 2 {
		t.Fatalf("len(Tenants) = %d, want 2", len(m.Tenants))
	}
}

func TestAdminHandleMetrics_AdminFiltered(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(certWith("relay-admin"), "/admin/metrics?tenant=tenant-a"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var m metricsResponse
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Tenants) != 1 || m.Tenants[0].TenantID != "tenant-a" {
		t.Fatalf("Tenants = %+v, want only tenant-a", m.Tenants)
	}
	if m.Tenants[0].BytesRelayed != 1024 {
		t.Errorf("BytesRelayed = %d, want 1024", m.Tenants[0].BytesRelayed)
	}
}

func TestAdminHandleMetrics_OperatorScoped(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(certWith("relay-operator", "tenant-a"), "/admin/metrics"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var m metricsResponse
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Tenants) != 1 || m.Tenants[0].TenantID != "tenant-a" {
		t.Fatalf("Tenants = %+v, want only tenant-a (operator scope)", m.Tenants)
	}
}

func TestAdminHandleMetrics_OperatorCrossTenantForbidden(t *testing.T) {
	admin := newAdminFixture()
	w := httptest.NewRecorder()
	admin.handleMetrics(w, reqWithCert(certWith("relay-operator", "tenant-a"), "/admin/metrics?tenant=tenant-b"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestAdminHandleDiscovery_NilRegistry(t *testing.T) {
	admin := newAdminFixture() // no SetDiscoveryRegistry → nil
	w := httptest.NewRecorder()
	admin.handleDiscovery(w, reqWithCert(certWith("relay-admin"), "/admin/discovery"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestAdminHandleDiscovery_AdminSeesAll(t *testing.T) {
	admin := newAdminFixture()
	reg := NewDiscoveryRegistry("relay-1", nil)
	_ = reg.Publish(&DiscoveryEnvelope{
		Record:     models.AgentCard{ID: "agent-a", Name: "Agent A"},
		Provenance: Provenance{OriginRelayID: "relay-1", TenantID: "tenant-a"},
		Visibility: Visibility{Scope: VisibilityTenantPrivate},
		TTL:        time.Hour, Version: 1,
	})
	_ = reg.Publish(&DiscoveryEnvelope{
		Record:     models.AgentCard{ID: "agent-b", Name: "Agent B"},
		Provenance: Provenance{OriginRelayID: "relay-1", TenantID: "tenant-b"},
		Visibility: Visibility{Scope: VisibilityGlobalPublic},
		TTL:        time.Hour, Version: 1,
	})
	admin.SetDiscoveryRegistry(reg)

	w := httptest.NewRecorder()
	admin.handleDiscovery(w, reqWithCert(certWith("relay-admin"), "/admin/discovery"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if int(resp["total"].(float64)) != 2 {
		t.Fatalf("expected 2 records, got %v", resp["total"])
	}
}

func TestAdminHandleDiscovery_OperatorScoped(t *testing.T) {
	admin := newAdminFixture()
	reg := NewDiscoveryRegistry("relay-1", nil)
	_ = reg.Publish(&DiscoveryEnvelope{
		Record:     models.AgentCard{ID: "agent-a", Name: "Agent A"},
		Provenance: Provenance{OriginRelayID: "relay-1", TenantID: "tenant-a"},
		Visibility: Visibility{Scope: VisibilityTenantPrivate},
		TTL:        time.Hour, Version: 1,
	})
	_ = reg.Publish(&DiscoveryEnvelope{
		Record:     models.AgentCard{ID: "agent-b", Name: "Agent B"},
		Provenance: Provenance{OriginRelayID: "relay-1", TenantID: "tenant-b"},
		Visibility: Visibility{Scope: VisibilityGlobalPublic},
		TTL:        time.Hour, Version: 1,
	})
	admin.SetDiscoveryRegistry(reg)

	// Operator with tenant-a SAN: sees only tenant-a records.
	w := httptest.NewRecorder()
	admin.handleDiscovery(w, reqWithCert(certWith("relay-operator", "tenant-a"), "/admin/discovery"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if int(resp["total"].(float64)) != 1 {
		t.Fatalf("operator should see 1 record (tenant-a only), got %v", resp["total"])
	}
}
