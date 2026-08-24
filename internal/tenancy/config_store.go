package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// TenantConfigStore manages per-tenant configuration.
type TenantConfigStore struct {
	db *sql.DB
}

// NewTenantConfigStore creates a new tenant config store.
func NewTenantConfigStore(db *sql.DB) *TenantConfigStore {
	return &TenantConfigStore{db: db}
}

// Set sets a configuration value for a tenant.
func (s *TenantConfigStore) Set(ctx context.Context, tenantID uuid.UUID, key string, value interface{}) error {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tenant_configs (tenant_id, key, value, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = $3, updated_at = NOW()
	`, tenantID, key, valueJSON)
	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}

	return nil
}

// Get gets a configuration value for a tenant.
func (s *TenantConfigStore) Get(ctx context.Context, tenantID uuid.UUID, key string) (interface{}, error) {
	var valueJSON []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT value FROM tenant_configs
		WHERE tenant_id = $1 AND key = $2
	`, tenantID, key).Scan(&valueJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	var value interface{}
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return value, nil
}

// GetAll gets all configuration for a tenant.
func (s *TenantConfigStore) GetAll(ctx context.Context, tenantID uuid.UUID) (map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value FROM tenant_configs
		WHERE tenant_id = $1
		ORDER BY key
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get configs: %w", err)
	}
	defer rows.Close()

	configs := make(map[string]interface{})
	for rows.Next() {
		var key string
		var valueJSON []byte
		if err := rows.Scan(&key, &valueJSON); err != nil {
			return nil, fmt.Errorf("failed to scan config: %w", err)
		}

		var value interface{}
		if err := json.Unmarshal(valueJSON, &value); err != nil {
			return nil, fmt.Errorf("failed to unmarshal value: %w", err)
		}

		configs[key] = value
	}
	return configs, nil
}

// Delete deletes a configuration value for a tenant.
func (s *TenantConfigStore) Delete(ctx context.Context, tenantID uuid.UUID, key string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM tenant_configs
		WHERE tenant_id = $1 AND key = $2
	`, tenantID, key)
	if err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}

	return nil
}
