package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
	"github.com/openagentplatform/openagentplatform/secrets"
)

// AuthMethod is the Vault authentication method.

type AuthMethod string

const (
	AuthToken       AuthMethod = "token"
	AuthAppRole     AuthMethod = "approle"
	AuthKubernetes  AuthMethod = "kubernetes"
	AuthJWT         AuthMethod = "jwt"
)

// Config configures the Vault backend.
type Config struct {
	Address       string    `json:"address"`        // https://vault.internal:8200
	AuthMethod    AuthMethod `json:"auth_method"`    // token, approle, kubernetes, jwt
	Token         string    `json:"token,omitempty"`  // for token auth
	RoleID        string    `json:"role_id,omitempty"`       // for AppRole
	SecretID      string    `json:"secret_id,omitempty"`     // for AppRole
	K8sRole       string    `json:"k8s_role,omitempty"`      // for K8s auth
	JWTRole       string    `json:"jwt_role,omitempty"`      // for JWT/OIDC auth
	JWTPath       string    `json:"jwt_path,omitempty"`      // path to JWT file
	Namespace     string    `json:"namespace,omitempty"`     // Vault Enterprise namespace
	MountPath     string    `json:"mount_path,omitempty"`    // KV v2 mount (default: "secret")
	CACert        string    `json:"ca_cert,omitempty"`       // CA cert path or PEM content
	TokenTTL      time.Duration `json:"token_ttl,omitempty"`
}

// VaultBackend implements the SecretBackend interface for HashiCorp Vault.
type VaultBackend struct {
	mu        sync.RWMutex
	config    Config
	client    *http.Client
	token     string
	tokenTTL  time.Duration
	stopCh    chan struct{}
}

// New creates a new Vault backend.
func New(ctx context.Context, config Config) (*VaultBackend, error) {
	if config.Address == "" {
		return nil, fmt.Errorf("vault address is required")
	}
	if config.MountPath == "" {
		config.MountPath = "secret"
	}

	tlsConfig := &tls.Config{}
	if config.CACert != "" {
		// Try to load as a file path first, then as PEM content.
		if cert, err := os.ReadFile(config.CACert); err == nil {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(cert) {
				return nil, fmt.Errorf("failed to parse CA cert from file %s", config.CACert)
			}
			tlsConfig.RootCAs = pool
		} else {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(config.CACert)) {
				return nil, fmt.Errorf("failed to parse CA cert PEM content")
			}
			tlsConfig.RootCAs = pool
		}
	}

	v := &VaultBackend{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		stopCh: make(chan struct{}),
	}

	// Authenticate based on the configured method.
	if err := v.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("vault authentication failed: %w", err)
	}

	// Start the background token renewal goroutine.
	go v.tokenRenewalLoop(ctx)

	return v, nil
}

// authenticate performs the initial Vault authentication.
func (v *VaultBackend) authenticate(ctx context.Context) error {
	switch v.config.AuthMethod {
	case AuthToken:
		v.token = v.config.Token
		v.tokenTTL = v.config.TokenTTL
	case AuthAppRole:
		return v.loginAppRole(ctx)
	case AuthKubernetes:
		return v.loginKubernetes(ctx)
	case AuthJWT:
		return v.loginJWT(ctx)
	default:
		return fmt.Errorf("unsupported auth method: %s", v.config.AuthMethod)
	}
	return nil
}

// loginAppRole authenticates using the AppRole method.
func (v *VaultBackend) loginAppRole(ctx context.Context) error {
	if v.config.RoleID == "" || v.config.SecretID == "" {
		return fmt.Errorf("AppRole requires role_id and secret_id")
	}

	data := map[string]interface{}{
		"role_id":   v.config.RoleID,
		"secret_id": v.config.SecretID,
	}

	resp, err := v.write(ctx, "auth/approle/login", data)
	if err != nil {
		return fmt.Errorf("AppRole login: %w", err)
	}

	return v.parseTokenResponse(resp)
}

// loginKubernetes authenticates using Kubernetes service account JWT.
func (v *VaultBackend) loginKubernetes(ctx context.Context) error {
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if v.config.JWTPath != "" {
		tokenPath = v.config.JWTPath
	}

	jwtData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("reading SA token: %w", err)
	}

	data := map[string]interface{}{
		"jwt":  string(jwtData),
		"role": v.config.K8sRole,
	}

	resp, err := v.write(ctx, "auth/kubernetes/login", data)
	if err != nil {
		return fmt.Errorf("Kubernetes login: %w", err)
	}

	return v.parseTokenResponse(resp)
}

// loginJWT authenticates using JWT/OIDC.
func (v *VaultBackend) loginJWT(ctx context.Context) error {
	tokenPath := v.config.JWTPath
	if tokenPath == "" {
		return fmt.Errorf("JWT auth requires jwt_path configuration")
	}

	jwtData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("reading JWT: %w", err)
	}

	data := map[string]interface{}{
		"jwt":  string(jwtData),
		"role": v.config.JWTRole,
	}

	resp, err := v.write(ctx, "auth/jwt/login", data)
	if err != nil {
		return fmt.Errorf("JWT login: %w", err)
	}

	return v.parseTokenResponse(resp)
}

// parseTokenResponse extracts the token and TTL from a Vault auth response.
func (v *VaultBackend) parseTokenResponse(resp *vaultResponse) error {
	if resp.Auth == nil {
		return fmt.Errorf("no auth data in response")
	}
	v.mu.Lock()
	v.token = resp.Auth.ClientToken
	v.tokenTTL = time.Duration(resp.Auth.LeaseDuration) * time.Second
	v.mu.Unlock()
	return nil
}

// tokenRenewalLoop renews the Vault token at 70% of its TTL.
func (v *VaultBackend) tokenRenewalLoop(ctx context.Context) {
	for {
		v.mu.RLock()
		ttl := v.tokenTTL
		v.mu.RUnlock()

		if ttl <= 0 {
			ttl = 1 * time.Hour
		}

		renewalInterval := time.Duration(float64(ttl) * 0.7)

		select {
		case <-ctx.Done():
			return
		case <-v.stopCh:
			return
		case <-time.After(renewalInterval):
			if err := v.renewToken(ctx); err != nil {
				// If renewal fails, re-authenticate.
				if reauthErr := v.authenticate(ctx); reauthErr != nil {
					// Log and continue; will retry on next interval.
					continue
				}
			}
		}
	}
}

// renewToken renews the current token.
func (v *VaultBackend) renewToken(ctx context.Context) error {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	resp, err := v.writeWithToken(ctx, "auth/token/renew-self", nil, token)
	if err != nil {
		return err
	}

	if resp.Auth != nil {
		v.mu.Lock()
		v.tokenTTL = time.Duration(resp.Auth.LeaseDuration) * time.Second
		v.mu.Unlock()
	}
	return nil
}

// Get reads a secret from KV v2.
func (v *VaultBackend) Get(ctx context.Context, path string, version *int) (*secrets.SecretValue, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	endpoint := fmt.Sprintf("v1/%s/data/%s", v.config.MountPath, path)
	if version != nil {
		endpoint = fmt.Sprintf("v1/%s/data/%s?version=%d", v.config.MountPath, path, *version)
	}

	resp, err := v.readWithToken(ctx, endpoint, token)
	if err != nil {
		return nil, fmt.Errorf("vault get %s: %w", path, err)
	}

	if resp.Data == nil {
		return nil, fmt.Errorf("secret not found: %s", path)
	}

	kvData, ok := resp.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response format for %s", path)
	}

	data := make(map[string]any, len(kvData))
	for k, val := range kvData {
		data[k] = val
	}

	metadata := secrets.SecretMetadata{
		Version: resp.Version,
	}

	if resp.Metadata != nil {
		if created, ok := resp.Metadata["created_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, created); err == nil {
				metadata.CreatedAt = t
			}
		}
	}

	return &secrets.SecretValue{
		Path:      path,
		Version:   resp.Version,
		Data:      data,
		Metadata:  metadata,
		CreatedAt: metadata.CreatedAt,
	}, nil
}

// Set writes a secret to KV v2.
func (v *VaultBackend) Set(ctx context.Context, path string, data map[string]any, opts secrets.SetOptions) (*secrets.SecretVersion, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	payload := map[string]interface{}{
		"data": data,
	}
	if opts.CAS > 0 {
		payload["options"] = map[string]interface{}{
			"cas": opts.CAS,
		}
	}

	endpoint := fmt.Sprintf("v1/%s/data/%s", v.config.MountPath, path)
	resp, err := v.writeWithToken(ctx, endpoint, payload, token)
	if err != nil {
		return nil, fmt.Errorf("vault set %s: %w", path, err)
	}

	return &secrets.SecretVersion{
		Path:    path,
		Version: resp.Version,
	}, nil
}

// Delete deletes a secret or specific versions.
func (v *VaultBackend) Delete(ctx context.Context, path string, opts secrets.DeleteOptions) error {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	endpoint := fmt.Sprintf("v1/%s/data/%s", v.config.MountPath, path)

	if len(opts.Versions) > 0 {
		payload := map[string]interface{}{
			"versions": intSliceToAny(opts.Versions),
		}
		_, err := v.writeWithToken(ctx, endpoint, payload, token)
		if err != nil {
			return fmt.Errorf("vault delete versions for %s: %w", path, err)
		}
	} else {
		err := v.deleteWithToken(ctx, endpoint, token)
		if err != nil {
			return fmt.Errorf("vault delete %s: %w", path, err)
		}
	}

	return nil
}

// List lists secret paths under a prefix.
