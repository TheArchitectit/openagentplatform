package infisical

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/secrets"
)

// AuthMethod is the Infisical authentication method.
type AuthMethod string

const (
	AuthUniversal  AuthMethod = "universal"
	AuthKubernetes AuthMethod = "kubernetes"
)

// Config configures the Infisical backend.
type Config struct {
	SiteURL      string     `json:"site_url"` // https://app.infisical.com
	ProjectID    string     `json:"project_id"`
	Environment  string     `json:"environment"`
	AuthMethod   AuthMethod `json:"auth_method"`
	ClientID     string     `json:"client_id,omitempty"`
	ClientSecret string     `json:"client_secret,omitempty"`
	K8sRole      string     `json:"k8s_role,omitempty"`
	JWTPath      string     `json:"jwt_path,omitempty"`
	FolderPrefix string     `json:"folder_prefix,omitempty"`
}

// InfisicalBackend implements the SecretBackend interface for Infisical.
type InfisicalBackend struct {
	mu          sync.RWMutex
	config      Config
	client      *http.Client
	accessToken string
	tokenExpiry time.Time
}

// New creates a new Infisical backend.
func New(ctx context.Context, config Config) (*InfisicalBackend, error) {
	if config.SiteURL == "" {
		config.SiteURL = "https://app.infisical.com"
	}
	if config.Environment == "" {
		config.Environment = "prod"
	}
	if config.FolderPrefix == "" {
		config.FolderPrefix = os.Getenv("OAP_INFISICAL_FOLDER_PREFIX")
	}

	b := &InfisicalBackend{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	if err := b.authenticate(ctx); err != nil {
		return nil, fmt.Errorf("infisical authentication failed: %w", err)
	}

	return b, nil
}

// authenticate performs the initial Infisical authentication.
func (b *InfisicalBackend) authenticate(ctx context.Context) error {
	switch b.config.AuthMethod {
	case AuthUniversal:
		return b.universalAuthLogin(ctx)
	case AuthKubernetes:
		return b.kubernetesAuthLogin(ctx)
	default:
		return fmt.Errorf("unsupported auth method: %s", b.config.AuthMethod)
	}
}

// universalAuthLogin authenticates using Universal Auth.
func (b *InfisicalBackend) universalAuthLogin(ctx context.Context) error {
	if b.config.ClientID == "" || b.config.ClientSecret == "" {
		return fmt.Errorf("Universal Auth requires client_id and client_secret")
	}

	payload := map[string]string{
		"clientId":     b.config.ClientID,
		"clientSecret": b.config.ClientSecret,
	}

	resp, err := b.postJSON(ctx, b.config.SiteURL+"/api/v1/auth/universal-auth/login", stringMapToAny(payload), false)
	if err != nil {
		return fmt.Errorf("universal auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("universal auth failed: %d", resp.StatusCode)
	}

	var authResp struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("decoding auth response: %w", err)
	}

	b.mu.Lock()
	b.accessToken = authResp.AccessToken
	b.tokenExpiry = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	b.mu.Unlock()

	return nil
}

// kubernetesAuthLogin authenticates using a Kubernetes service account JWT.
func (b *InfisicalBackend) kubernetesAuthLogin(ctx context.Context) error {
	tokenPath := b.config.JWTPath
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	jwtData, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("reading SA token: %w", err)
	}

	payload := map[string]string{
		"jwt":  string(jwtData),
		"role": b.config.K8sRole,
	}

	resp, err := b.postJSON(ctx, b.config.SiteURL+"/api/v1/auth/kubernetes-auth/login", stringMapToAny(payload), false)
	if err != nil {
		return fmt.Errorf("kubernetes auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("kubernetes auth failed: %d", resp.StatusCode)
	}

	var authResp struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("decoding auth response: %w", err)
	}

	b.mu.Lock()
	b.accessToken = authResp.AccessToken
	b.tokenExpiry = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	b.mu.Unlock()

	return nil
}

// Get retrieves a secret from Infisical.
// OAP path: <workspace>/<client>/<site>/<agent>/<key>
// Maps to Infisical: folder=<workspace>/<client>/<site>/<agent>, key=<key>
func (b *InfisicalBackend) Get(ctx context.Context, path string, version *int) (*secrets.SecretValue, error) {
	folder, key := b.mapPath(path)
	apiURL := fmt.Sprintf("%s/api/v3/secrets/raw/%s",
		b.config.SiteURL, url.PathEscape(key))

	resp, err := b.authedRequest(ctx, "GET", apiURL, nil, func(req *http.Request) {
		req.Header.Set("X-Native-Project-ID", b.config.ProjectID)
		req.Header.Set("X-Native-Environment", b.config.Environment)
		req.Header.Set("X-Native-Folder-Path", folder)
	})
	if err != nil {
		return nil, fmt.Errorf("infisical get %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("secret not found: %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("infisical API error: %d: %s", resp.StatusCode, string(body))
	}

	var secretResp struct {
		SecretValue string `json:"secretValue"`
		Version     int    `json:"version"`
		CreatedAt   string `json:"createdAt"`
		UpdatedAt   string `json:"updatedAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&secretResp); err != nil {
		return nil, fmt.Errorf("decoding infisical response: %w", err)
	}

	metadata := secrets.SecretMetadata{
		Version: secretResp.Version,
	}

	if secretResp.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, secretResp.CreatedAt); err == nil {
			metadata.CreatedAt = t
		}
	}
	if secretResp.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, secretResp.UpdatedAt); err == nil {
			metadata.UpdatedAt = t
		}
	}

	return &secrets.SecretValue{
		Path:      path,
		Version:   secretResp.Version,
		Data:      map[string]any{"value": secretResp.SecretValue},
		Metadata:  metadata,
		CreatedAt: metadata.CreatedAt,
	}, nil
}

// Set writes a secret to Infisical.
func (b *InfisicalBackend) Set(ctx context.Context, path string, data map[string]any, opts secrets.SetOptions) (*secrets.SecretVersion, error) {
	folder, key := b.mapPath(path)

	value, ok := data["value"].(string)
	if !ok {
		return nil, fmt.Errorf("infisical backend requires string value in data[\"value\"]")
	}

	payload := map[string]interface{}{
		"projectId":   b.config.ProjectID,
		"environment": b.config.Environment,
		"folderPath":  folder,
		"secretKey":   key,
		"secretValue": value,
	}

	resp, err := b.authedRequest(ctx, "POST", b.config.SiteURL+"/api/v3/secrets/raw", payload, nil)
	if err != nil {
		return nil, fmt.Errorf("infisical set %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("infisical set failed: %d: %s", resp.StatusCode, string(body))
	}

	var setResp struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		return nil, err
	}

	return &secrets.SecretVersion{
		Path:    path,
		Version: setResp.Version,
	}, nil
}

// Delete removes a secret from Infisical.
func (b *InfisicalBackend) Delete(ctx context.Context, path string, opts secrets.DeleteOptions) error {
	folder, key := b.mapPath(path)

	payload := map[string]interface{}{
		"projectId":   b.config.ProjectID,
		"environment": b.config.Environment,
		"folderPath":  folder,
		"secretKey":   key,
	}

	resp, err := b.authedRequest(ctx, "DELETE", b.config.SiteURL+"/api/v3/secrets/raw", payload, nil)
	if err != nil {
		return fmt.Errorf("infisical delete %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("secret not found: %s", path)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("infisical delete failed: %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// List enumerates secrets in a folder.
func (b *InfisicalBackend) List(ctx context.Context, opts secrets.ListOptions) ([]string, error) {
	folder := opts.Prefix
	if folder == "" {
		folder = "/"
	}

	apiURL := fmt.Sprintf("%s/api/v3/secrets?folderPath=%s&environment=%s&projectId=%s",
		b.config.SiteURL,
		url.QueryEscape(folder),
		url.QueryEscape(b.config.Environment),
		url.QueryEscape(b.config.ProjectID))

	resp, err := b.authedRequest(ctx, "GET", apiURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("infisical list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("infisical list failed: %d: %s", resp.StatusCode, string(body))
	}

	var listResp struct {
		Secrets []struct {
			SecretKey string `json:"secretKey"`
		} `json:"secrets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(listResp.Secrets))
	for _, s := range listResp.Secrets {
		paths = append(paths, s.SecretKey)
	}

	if opts.Limit > 0 && len(paths) > opts.Limit {
		paths = paths[:opts.Limit]
	}
	return paths, nil
}

// Metadata returns metadata for a secret.
func (b *InfisicalBackend) Metadata(ctx context.Context, path string) (*secrets.SecretMetadata, error) {
	val, err := b.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	return &val.Metadata, nil
}

// Rotate creates a new version with the provided data.
func (b *InfisicalBackend) Rotate(ctx context.Context, path string, opts secrets.RotateOptions) (*secrets.SecretVersion, error) {
	data := opts.NewData
	if data == nil {
		val, err := b.Get(ctx, path, nil)
		if err != nil {
			return nil, err
		}
		data = val.Data
	}
	return b.Set(ctx, path, data, secrets.SetOptions{})
}

// Healthcheck verifies the Infisical API is reachable.
func (b *InfisicalBackend) Healthcheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", b.config.SiteURL+"/api/status", nil)
	if err != nil {
		return err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("infisical healthcheck: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("infisical unhealthy: %d", resp.StatusCode)
	}
	return nil
}

// Close is a no-op.
func (b *InfisicalBackend) Close(ctx context.Context) error {
	b.client.CloseIdleConnections()
	return nil
}

// SupportsDynamic returns false.
func (b *InfisicalBackend) SupportsDynamic() bool {
	return false
}

// RevokeLease is a no-op for the Infisical backend (no dynamic leases).
func (b *InfisicalBackend) RevokeLease(ctx context.Context, leaseID string) error {
	return secrets.ErrNotSupported
}

// mapPath converts an OAP path to Infisical folder + key.
// OAP path: <workspace>/<client>/<site>/<agent>/<key>
// Infisical folder: <prefix>/<workspace>/<client>/<site>/<agent>
// Infisical key: <key>
func (b *InfisicalBackend) mapPath(path string) (string, string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "/", path
	}

	folder := strings.Join(parts[:len(parts)-1], "/")
	key := parts[len(parts)-1]

	if b.config.FolderPrefix != "" {
		folder = b.config.FolderPrefix + "/" + folder
	}

	return folder, key
}

// stringMapToAny converts map[string]string to map[string]interface{}.
func stringMapToAny(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// postJSON performs a POST with JSON body. If authed is false, no auth header is set.
func (b *InfisicalBackend) postJSON(ctx context.Context, url string, payload map[string]interface{}, authed bool) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authed {
		req.Header.Set("Authorization", "Bearer "+b.accessToken)
	}
	return b.client.Do(req)
}

// authedRequest performs an HTTP request with authentication and auto-refresh on 401.
func (b *InfisicalBackend) authedRequest(ctx context.Context, method, url string, body map[string]interface{}, extraHeaders func(*http.Request)) (*http.Response, error) {
	resp, err := b.doRequest(ctx, method, url, body, extraHeaders)
	if err != nil {
		return nil, err
	}

	// On 401, re-authenticate and retry once.
	if resp.StatusCode == 401 {
		resp.Body.Close()
		if err := b.authenticate(ctx); err != nil {
			return nil, fmt.Errorf("re-authentication: %w", err)
		}
		return b.doRequest(ctx, method, url, body, extraHeaders)
	}

	return resp, nil
}

// doRequest executes a single HTTP request with auth headers.
func (b *InfisicalBackend) doRequest(ctx context.Context, method, url string, body map[string]interface{}, extraHeaders func(*http.Request)) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	b.mu.RLock()
	token := b.accessToken
	b.mu.RUnlock()

	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if extraHeaders != nil {
		extraHeaders(req)
	}

	return b.client.Do(req)
}
