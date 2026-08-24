package oauth

import (
	"log/slog"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
)

// AuthorizationServer is the main MCP OAuth 2.1 authorization server.
type AuthorizationServer struct {
	mu sync.RWMutex

	// issuer is the authorization server's issuer URL.
	issuer string
	// resource is the resource server URL this authz server protects.
	resource string
	// registrationEndpoint is the full URL for client registration.
	registrationEndpoint string

	clients map[string]*RegisteredClient // clientID -> client
	codes   map[string]*AuthCode         // code -> auth code
	tokens  map[string]*AccessToken      // access token -> info
	refresh map[string]*RefreshToken     // refresh token -> info
	nonces  map[string]time.Time         // nonce -> expiry

	audit  *audit.AuditService
	logger *slog.Logger

	// cleanup goroutine control
	cleanupStop chan struct{}
	cleanupDone chan struct{}
}

// NewAuthorizationServer creates a new OAuth 2.1 authorization server.
// issuer is the authz server URL (e.g. "https://oap.example.com").
// resource is the resource server URL (e.g. "https://api.oap.example.com").
// registrationEndpoint is the full URL for /.well-known/oauth-protected-resource/register.
func NewAuthorizationServer(
	issuer string,
	resource string,
	registrationEndpoint string,
	auditSvc *audit.AuditService,
	logger *slog.Logger,
) *AuthorizationServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthorizationServer{
		issuer:               issuer,
		resource:             resource,
		registrationEndpoint: registrationEndpoint,
		clients:              make(map[string]*RegisteredClient),
		codes:                make(map[string]*AuthCode),
		tokens:               make(map[string]*AccessToken),
		refresh:              make(map[string]*RefreshToken),
		nonces:               make(map[string]time.Time),
		audit:                auditSvc,
		logger:               logger,
	}
}

// cleanupInterval is how often the background goroutine purges expired tokens.
const cleanupInterval = 5 * time.Minute

// StartCleanup launches a background goroutine that periodically purges
// expired authorization codes, access tokens, refresh tokens, and nonces.
// Call StopCleanup to terminate the goroutine.
func (a *AuthorizationServer) StartCleanup() {
	a.mu.Lock()
	if a.cleanupStop != nil {
		a.mu.Unlock()
		return // already running
	}
	a.cleanupStop = make(chan struct{})
	a.cleanupDone = make(chan struct{})
	a.mu.Unlock()

	go func() {
		defer close(a.cleanupDone)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.purgeExpired()
			case <-a.cleanupStop:
				return
			}
		}
	}()
}

// StopCleanup terminates the background cleanup goroutine and waits for it to exit.
func (a *AuthorizationServer) StopCleanup() {
	a.mu.Lock()
	stop := a.cleanupStop
	done := a.cleanupDone
	a.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

// purgeExpired removes all expired tokens, codes, and nonces from memory.
func (a *AuthorizationServer) purgeExpired() {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()

	// Purge expired auth codes.
	for k, code := range a.codes {
		if code.Used || now.After(code.ExpiresAt) {
			delete(a.codes, k)
		}
	}
	// Purge expired access tokens.
	for k, tok := range a.tokens {
		if tok.Revoked || now.After(tok.ExpiresAt) {
			delete(a.tokens, k)
		}
	}
	// Purge expired refresh tokens.
	for k, rt := range a.refresh {
		if rt.Revoked || now.After(rt.ExpiresAt) {
			delete(a.refresh, k)
		}
	}
	// Purge expired nonces.
	for k, exp := range a.nonces {
		if now.After(exp) {
			delete(a.nonces, k)
		}
	}
}
