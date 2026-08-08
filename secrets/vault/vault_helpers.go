package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"github.com/openagentplatform/openagentplatform/secrets"
)

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
