package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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
func (v *VaultBackend) List(ctx context.Context, opts secrets.ListOptions) ([]string, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	endpoint := fmt.Sprintf("v1/%s/metadata/%s", v.config.MountPath, opts.Prefix)
	resp, err := v.readWithToken(ctx, endpoint, token)
	if err != nil {
		return nil, fmt.Errorf("vault list %s: %w", opts.Prefix, err)
	}

	if resp.Data == nil {
		return []string{}, nil
	}

	keys, ok := resp.Data["keys"].([]interface{})
	if !ok {
		return []string{}, nil
	}

	paths := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := k.(string); ok {
			paths = append(paths, s)
		}
	}

	if opts.Limit > 0 && len(paths) > opts.Limit {
		paths = paths[:opts.Limit]
	}
	return paths, nil
}

// Metadata returns metadata for a secret.
func (v *VaultBackend) Metadata(ctx context.Context, path string) (*secrets.SecretMetadata, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	endpoint := fmt.Sprintf("v1/%s/metadata/%s", v.config.MountPath, path)
	resp, err := v.readWithToken(ctx, endpoint, token)
	if err != nil {
		return nil, fmt.Errorf("vault metadata %s: %w", path, err)
	}

	md := &secrets.SecretMetadata{}
	if resp.Data != nil {
		if v, ok := resp.Data["version"].(float64); ok {
			md.Version = int(v)
		}
		if c, ok := resp.Data["created_time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, c); err == nil {
				md.CreatedAt = t
			}
		}
	}

	return md, nil
}

// Rotate creates a new version of a secret with new data.
func (v *VaultBackend) Rotate(ctx context.Context, path string, opts secrets.RotateOptions) (*secrets.SecretVersion, error) {
	if opts.NewData == nil {
		// Read current data and re-write it as a new version.
		val, err := v.Get(ctx, path, nil)
		if err != nil {
			return nil, err
		}
		opts.NewData = val.Data
	}

	return v.Set(ctx, path, opts.NewData, secrets.SetOptions{})
}

// Healthcheck verifies the Vault server is reachable.
func (v *VaultBackend) Healthcheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", v.config.Address+"/v1/sys/health", nil)
	if err != nil {
		return err
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault healthcheck: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("vault server unhealthy: %d", resp.StatusCode)
	}
	return nil
}

// Close stops the renewal loop and closes the HTTP client.
func (v *VaultBackend) Close(ctx context.Context) error {
	close(v.stopCh)
	v.client.CloseIdleConnections()
	return nil
}

// SupportsDynamic returns true.
func (v *VaultBackend) SupportsDynamic() bool {
	return true
}

// RevokeLease revokes a Vault dynamic-secret lease.
func (v *VaultBackend) RevokeLease(ctx context.Context, leaseID string) error {
	if leaseID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", v.config.Address+"/v1/sys/leases/revoke", nil)
	if err != nil {
		return fmt.Errorf("vault revoke lease: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)
	q := req.URL.Query()
	q.Set("lease_id", leaseID)
	req.URL.RawQuery = q.Encode()

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault revoke lease: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("vault revoke lease: status %d", resp.StatusCode)
	}
	return nil
}

// GetDynamic requests dynamic credentials from a Vault secrets engine.
// The mount parameter is the engine mount (e.g., "database", "aws").
// The role parameter is the role name within that engine.
func (v *VaultBackend) GetDynamic(ctx context.Context, mount string, role string) (*secrets.SecretValue, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	endpoint := fmt.Sprintf("v1/%s/creds/%s", mount, role)
	resp, err := v.readWithToken(ctx, endpoint, token)
	if err != nil {
		return nil, fmt.Errorf("vault dynamic creds %s/%s: %w", mount, role, err)
	}

	data := make(map[string]any)
	if resp.Data != nil {
		for k, val := range resp.Data {
			data[k] = val
		}
	}

	leaseDuration := time.Duration(resp.LeaseDuration) * time.Second

	metadata := secrets.SecretMetadata{
		IsDynamic:     true,
		LeaseID:       resp.LeaseID,
		LeaseDuration: leaseDuration,
	}

	return &secrets.SecretValue{
		Data:     data,
		Metadata: metadata,
		CreatedAt: time.Now(),
	}, nil
}

// RenewLease extends a dynamic secret lease.
func (v *VaultBackend) RenewLease(ctx context.Context, leaseID string, increment time.Duration) error {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	payload := map[string]interface{}{
		"lease_id": leaseID,
	}
	if increment > 0 {
		payload["increment"] = int(increment.Seconds())
	}

	endpoint := "v1/sys/leases/renew"
	_, err := v.writeWithToken(ctx, endpoint, payload, token)
	if err != nil {
		return fmt.Errorf("vault lease renew: %w", err)
	}
	return nil
}

// RevokeDynamic revokes a dynamic secret lease.
func (v *VaultBackend) RevokeDynamic(ctx context.Context, leaseID string) error {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()

	payload := map[string]interface{}{
		"lease_id": leaseID,
	}

	endpoint := "v1/sys/leases/revoke"
	_, err := v.writeWithToken(ctx, endpoint, payload, token)
	if err != nil {
		return fmt.Errorf("vault lease revoke: %w", err)
	}
	return nil
}

// vaultResponse is a generic Vault HTTP response.
type vaultResponse struct {
	Data     map[string]interface{} `json:"data"`
	Auth     *vaultAuth             `json:"auth,omitempty"`
	Version  int                    `json:"version"`
	LeaseID  string                 `json:"lease_id"`
	LeaseDuration int               `json:"lease_duration"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type vaultAuth struct {
	ClientToken   string   `json:"client_token"`
	LeaseDuration int      `json:"lease_duration"`
	Policies      []string `json:"policies"`
}

// readWithToken performs an authenticated GET request to the Vault API.
func (v *VaultBackend) readWithToken(ctx context.Context, path, token string) (*vaultResponse, error) {
	url := v.config.Address + "/" + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	if v.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.config.Namespace)
	}
	req.Header.Set("X-Vault-Request", "true")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("vault permission denied")
	}
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("secret not found")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("vault API error: %d", resp.StatusCode)
	}

	var vr vaultResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decoding vault response: %w", err)
	}
	return &vr, nil
}

// writeWithToken performs an authenticated POST request to the Vault API.
func (v *VaultBackend) writeWithToken(ctx context.Context, path string, data map[string]interface{}, token string) (*vaultResponse, error) {
	url := v.config.Address + "/" + path
	var bodyReader *jsonReader
	if data != nil {
		bodyReader = newJSONReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	if v.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.config.Namespace)
	}
	req.Header.Set("X-Vault-Request", "true")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("vault permission denied")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("vault API error: %d", resp.StatusCode)
	}

	var vr vaultResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		return nil, fmt.Errorf("decoding vault response: %w", err)
	}
	return &vr, nil
}

// write is a convenience for writeWithToken using the current token.
func (v *VaultBackend) write(ctx context.Context, path string, data map[string]interface{}) (*vaultResponse, error) {
	v.mu.RLock()
	token := v.token
	v.mu.RUnlock()
	return v.writeWithToken(ctx, path, data, token)
}

// deleteWithToken performs an authenticated DELETE request.
func (v *VaultBackend) deleteWithToken(ctx context.Context, path, token string) error {
	url := v.config.Address + "/" + path
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	if v.config.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.config.Namespace)
	}
	req.Header.Set("X-Vault-Request", "true")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return fmt.Errorf("vault permission denied")
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("secret not found")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("vault API error: %d", resp.StatusCode)
	}
	return nil
}

// jsonReader wraps a map to provide an io.Reader.
type jsonReader struct {
	data   []byte
	offset int
}

func newJSONReader(data map[string]interface{}) *jsonReader {
	b, _ := json.Marshal(data)
	return &jsonReader{data: b}
}

func (j *jsonReader) Read(p []byte) (int, error) {
	if j.offset >= len(j.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, j.data[j.offset:])
	j.offset += n
	return n, nil
}

func intSliceToAny(s []int) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}
