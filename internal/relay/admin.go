package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// AdminServer serves the operator observability surface (RELAY-04). It is a
// separate mTLS listener — never mixed with the WSS data plane. Every route
// requires a verified client certificate; metrics additionally require a
// recognized role SAN and enforce per-role tenant visibility.
type AdminServer struct {
	svc       *RelayService
	log       *slog.Logger
	startedAt time.Time
}

// NewAdminServer creates an admin server bound to the given relay service.
func NewAdminServer(svc *RelayService, log *slog.Logger) *AdminServer {
	if log == nil {
		log = slog.Default()
	}
	return &AdminServer{svc: svc, log: log, startedAt: time.Now().UTC()}
}

const (
	// RoleAdmin sees every tenant's metrics.
	RoleAdmin = "relay-admin"
	// RoleOperator sees only tenants bound to its certificate SANs.
	RoleOperator = "relay-operator"
)

// adminPrincipal is the verified identity derived from a client certificate.
type adminPrincipal struct {
	Role    string
	Tenants map[string]bool
}

// operatorIdentity parses the role and tenant SANs from the TLS peer
// certificate. The trust conventions mirror RELAY-02: tokens of the form
// `oap:role:<role>` and `oap:tenant:<tenantID>` are carried in the cert's CN,
// DNS SANs, or URI SANs. Returns false if no client certificate was presented.
func operatorIdentity(r *http.Request) (adminPrincipal, bool) {
	var p adminPrincipal
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return p, false
	}
	cert := r.TLS.PeerCertificates[0]

	var tokens []string
	tokens = append(tokens, cert.Subject.CommonName)
	tokens = append(tokens, cert.DNSNames...)
	for _, u := range cert.URIs {
		tokens = append(tokens, u.String())
	}

	p.Tenants = make(map[string]bool)
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		switch {
		case strings.HasPrefix(tok, "oap:role:"):
			p.Role = strings.TrimPrefix(tok, "oap:role:")
		case strings.HasPrefix(tok, "oap:tenant:"):
			p.Tenants[strings.TrimPrefix(tok, "oap:tenant:")] = true
		}
	}
	return p, true
}

// Serve runs the admin HTTP server on the given TLS listener. It blocks until
// ctx is cancelled or the listener errors; it always closes the listener before
// returning. The caller MUST wrap ln with tls.NewListener and a config that
// requires and verifies client certificates (fail-closed).
func (a *AdminServer) Serve(ctx context.Context, ln net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/health", a.handleHealth)
	mux.HandleFunc("/admin/metrics", a.handleMetrics)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// healthResponse is the /admin/health payload. It carries no tenant data.
type healthResponse struct {
	Status            string `json:"status"`
	UptimeSeconds     int64  `json:"uptime_seconds"`
	ActiveConnections int    `json:"active_connections"`
	PendingLegs       int    `json:"pending_legs"`
}

// handleHealth serves liveness/readiness. Any verified client certificate is
// sufficient — no role is required.
func (a *AdminServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := operatorIdentity(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "client_cert_required"})
		return
	}

	active := 0
	for _, m := range a.svc.AllMetrics(r.Context()) {
		active += m.ConnectionCount
	}

	resp := healthResponse{
		Status:            "ok",
		UptimeSeconds:     int64(time.Since(a.startedAt).Seconds()),
		ActiveConnections: active,
		PendingLegs:       a.svc.MatchEngine().PendingLegCount(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// metricsResponse is the /admin/metrics payload. Numeric values come from
// existing RelayMetrics accounting; no new counters are introduced.
type metricsResponse struct {
	Tenants []tenantMetrics `json:"tenants"`
}

type tenantMetrics struct {
	TenantID         string `json:"tenant_id"`
	ConnectionCount  int    `json:"connection_count"`
	TotalConnections int64  `json:"total_connections"`
	BytesRelayed     int64  `json:"bytes_relayed"`
}

// handleMetrics serves tenant metrics scoped to the caller's role. Admin role
// sees all tenants (or a requested one); operator role sees only tenants bound
// to its cert SANs. A cross-tenant request by an operator is 403.
func (a *AdminServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	principal, ok := operatorIdentity(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "client_cert_required"})
		return
	}
	if principal.Role != RoleAdmin && principal.Role != RoleOperator {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unrecognized_role"})
		return
	}

	requested := r.URL.Query().Get("tenant")

	// Resolve the set of permitted tenant IDs for this request.
	var permitted map[string]bool
	switch principal.Role {
	case RoleAdmin:
		// No restriction: caller may query any tenant or all tenants.
		permitted = nil
	case RoleOperator:
		if requested != "" {
			if !principal.Tenants[requested] {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant_not_permitted"})
				return
			}
		}
		permitted = principal.Tenants
	}

	all := a.svc.AllMetrics(r.Context())
	resp := metricsResponse{Tenants: make([]tenantMetrics, 0, len(all))}
	for id, m := range all {
		// Skip tenants outside the permitted set.
		if permitted != nil && !permitted[id] {
			continue
		}
		// When a specific tenant is requested, skip everything else.
		if requested != "" && id != requested {
			continue
		}
		resp.Tenants = append(resp.Tenants, tenantMetrics{
			TenantID:         id,
			ConnectionCount:  m.ConnectionCount,
			TotalConnections: m.TotalConnections,
			BytesRelayed:     m.TotalBytesRelayed,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// writeJSON marshals v and writes it with the JSON content type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// AdminTLSConfig builds the TLS configuration for the admin listener: a server
// certificate plus mandatory client-certificate verification against the given
// CA pool. Passing a nil pool produces a config that requires a cert but trusts
// every cert — callers MUST supply a real platform CA pool.
func AdminTLSConfig(serverCert tls.Certificate, clientCAs *x509.CertPool) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}
}
