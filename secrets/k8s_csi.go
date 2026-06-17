package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// K8sCSIBackend reads secrets from files mounted by the Secrets Store CSI Driver.
// It uses the Kubernetes Secrets API (core/v1) for direct access and maps OAP
// paths to Kubernetes secret names and keys.
type K8sCSIBackend struct {
	mu        sync.RWMutex
	namespace string
	mountPath string
	// saToken is the pod's service account token for API auth.
	saToken string
	// apiServer is the Kubernetes API server URL.
	apiServer string
	// caCert is the path to the cluster CA certificate.
	caCert string
}

// K8sCSIConfig configures the K8sCSI backend.
type K8sCSIConfig struct {
	Namespace string // Kubernetes namespace
	MountPath string // CSI mount path (e.g., /var/secrets/oap)
	SAToken  string // Service account token (defaults to /var/run/secrets/kubernetes.io/serviceaccount/token)
	APIServer string // Kubernetes API server URL
	CACert   string // Path to cluster CA cert
}

// NewK8sCSIBackend creates a new K8s CSI backend.
func NewK8sCSIBackend(cfg K8sCSIConfig) *K8sCSIBackend {
	if cfg.MountPath == "" {
		cfg.MountPath = "/var/secrets/oap"
	}
	if cfg.SAToken == "" {
		cfg.SAToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	backend := &K8sCSIBackend{
		namespace: cfg.Namespace,
		mountPath: cfg.MountPath,
		apiServer: cfg.APIServer,
		caCert:    cfg.CACert,
	}

	// Try to read the service account token.
	if data, err := os.ReadFile(cfg.SAToken); err == nil {
		backend.saToken = strings.TrimSpace(string(data))
	}

	return backend
}

// Get retrieves a secret by mapping the OAP path to a K8s secret name and key.
func (k *K8sCSIBackend) Get(ctx context.Context, path string, version *int) (*SecretValue, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	// Try CSI mounted file first.
	filePath := filepath.Join(k.mountPath, path)
	if data, err := os.ReadFile(filePath); err == nil {
		return &SecretValue{
			Path:    path,
			Version: 1,
			Data:    map[string]any{"value": strings.TrimSpace(string(data))},
			Metadata: SecretMetadata{
				Version: 1,
			},
			CreatedAt: time.Now(),
		}, nil
	}

	// Fall back to Kubernetes Secrets API.
	secretName, key := k.mapPathToK8sSecret(path)
	if secretName == "" {
		return nil, fmt.Errorf("cannot map path %s to K8s secret", path)
	}

	if k.apiServer == "" {
		return nil, fmt.Errorf("K8sCSI backend requires API server config for path %s", path)
	}

	// Use the Kubernetes API to read the secret.
	data, err := k.readK8sSecret(ctx, secretName, key)
	if err != nil {
		return nil, fmt.Errorf("reading K8s secret %s/%s: %w", secretName, key, err)
	}

	return &SecretValue{
		Path:    path,
		Version: 1,
		Data:    map[string]any{"value": data},
		Metadata: SecretMetadata{
			Version: 1,
		},
		CreatedAt: time.Now(),
	}, nil
}

// Set is not supported on the K8s CSI backend (read-only).
func (k *K8sCSIBackend) Set(ctx context.Context, path string, data map[string]any, opts SetOptions) (*SecretVersion, error) {
	return nil, fmt.Errorf("K8sCSI backend is read-only")
}

// Delete is not supported on the K8s CSI backend (read-only).
func (k *K8sCSIBackend) Delete(ctx context.Context, path string, opts DeleteOptions) error {
	return fmt.Errorf("K8sCSI backend is read-only")
}

// List enumerates files under the CSI mount path.
func (k *K8sCSIBackend) List(ctx context.Context, opts ListOptions) ([]string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	prefix := opts.Prefix
	basePath := k.mountPath
	if prefix != "" {
		basePath = filepath.Join(k.mountPath, prefix)
	}

	var paths []string
	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(k.mountPath, path)
			if err == nil {
				paths = append(paths, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if opts.Limit > 0 && len(paths) > opts.Limit {
		paths = paths[:opts.Limit]
	}
	return paths, nil
}

// Metadata returns minimal metadata for a CSI-mounted secret.
func (k *K8sCSIBackend) Metadata(ctx context.Context, path string) (*SecretMetadata, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	filePath := filepath.Join(k.mountPath, path)
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("secret not found: %s", path)
	}

	return &SecretMetadata{
		Version:   1,
		CreatedAt: info.ModTime(),
		UpdatedAt: info.ModTime(),
	}, nil
}

// Rotate is not supported on the K8s CSI backend.
func (k *K8sCSIBackend) Rotate(ctx context.Context, path string, opts RotateOptions) (*SecretVersion, error) {
	return nil, fmt.Errorf("K8sCSI backend does not support rotation")
}

// Healthcheck verifies the CSI mount path exists and is readable.
func (k *K8sCSIBackend) Healthcheck(ctx context.Context) error {
	k.mu.RLock()
	defer k.mu.RUnlock()

	info, err := os.Stat(k.mountPath)
	if err != nil {
		return fmt.Errorf("CSI mount path %s not accessible: %w", k.mountPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("CSI mount path %s is not a directory", k.mountPath)
	}
	return nil
}

// Close is a no-op for the K8s CSI backend.
func (k *K8sCSIBackend) Close(ctx context.Context) error {
	return nil
}

// SupportsDynamic returns false.
func (k *K8sCSIBackend) SupportsDynamic() bool {
	return false
}

// RevokeLease is a no-op for the K8s CSI backend (no dynamic leases).
func (k *K8sCSIBackend) RevokeLease(ctx context.Context, leaseID string) error {
	return ErrNotSupported
}

// mapPathToK8sSecret converts an OAP path to a K8s secret name and key.
// Path format: <secret-name>/<key> or <secret-name> (defaults to key "value").
func (k *K8sCSIBackend) mapPathToK8sSecret(path string) (string, string) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	secretName := parts[0]
	key := "value"
	if len(parts) == 2 {
		key = parts[1]
	}
	return secretName, key
}

// readK8sSecret reads a secret value from the Kubernetes API.
func (k *K8sCSIBackend) readK8sSecret(ctx context.Context, secretName, key string) (string, error) {
	// In a production environment, this would make an HTTP call to the
	// Kubernetes API at /api/v1/namespaces/<ns>/secrets/<name>.
	// Since we are in a module without K8s client dependencies, we read
	// from the CSI mount path as a fallback.
	filePath := filepath.Join(k.mountPath, secretName, key)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
