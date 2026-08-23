package tenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Tenant represents a tenant in the system.
type Tenant struct {
	// ID is the unique tenant identifier.
	ID uuid.UUID `json:"id"`
	// Name is the tenant's display name.
	Name string `json:"name"`
	// Slug is the URL-friendly tenant identifier.
	Slug string `json:"slug"`
	// Settings contains tenant-specific configuration.
	Settings map[string]interface{} `json:"settings,omitempty"`
	// CreatedAt is when the tenant was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the tenant was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt is when the tenant was soft-deleted (nil if active).
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// TenantStore manages tenant persistence.
type TenantStore struct {
	db *sql.DB
}

// NewTenantStore creates a new tenant store.
func NewTenantStore(db *sql.DB) *TenantStore {
	return &TenantStore{db: db}
}

// Create creates a new tenant.
func (s *TenantStore) Create(ctx context.Context, name, slug string, settings map[string]interface{}) (*Tenant, error) {
	// Validate inputs
	if name == "" {
		return nil, fmt.Errorf("tenant name is required")
	}
	if slug == "" {
		return nil, fmt.Errorf("tenant slug is required")
	}

	// Check slug uniqueness
	exists, err := s.slugExists(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to check slug uniqueness: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("tenant slug %q already exists", slug)
	}

	// Create tenant
	id := uuid.New()
	now := time.Now().UTC()

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tenants (id, name, slug, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, name, slug, settingsJSON, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return &Tenant{
		ID:        id,
		Name:      name,
		Slug:      slug,
		Settings:  settings,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Get retrieves a tenant by ID.
func (s *TenantStore) Get(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	var tenant Tenant
	var settingsJSON []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, settings, created_at, updated_at, deleted_at
		FROM tenants
		WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Slug,
		&settingsJSON,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tenant %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	if settingsJSON != nil {
		if err := json.Unmarshal(settingsJSON, &tenant.Settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
		}
	}

	return &tenant, nil
}

// GetBySlug retrieves a tenant by slug.
func (s *TenantStore) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	var tenant Tenant
	var settingsJSON []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, slug, settings, created_at, updated_at, deleted_at
		FROM tenants
		WHERE slug = $1 AND deleted_at IS NULL
	`, slug).Scan(
		&tenant.ID,
		&tenant.Name,
		&tenant.Slug,
		&settingsJSON,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&tenant.DeletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tenant %q not found", slug)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	if settingsJSON != nil {
		if err := json.Unmarshal(settingsJSON, &tenant.Settings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
		}
	}

	return &tenant, nil
}

// List retrieves all active tenants.
func (s *TenantStore) List(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, slug, settings, created_at, updated_at, deleted_at
		FROM tenants
		WHERE deleted_at IS NULL
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var tenant Tenant
		var settingsJSON []byte
		if err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.Slug,
			&settingsJSON,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&tenant.DeletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		if settingsJSON != nil {
			if err := json.Unmarshal(settingsJSON, &tenant.Settings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
			}
		}

		tenants = append(tenants, &tenant)
	}
	return tenants, nil
}

// Update updates a tenant.
func (s *TenantStore) Update(ctx context.Context, id uuid.UUID, name string, settings map[string]interface{}) (*Tenant, error) {
	now := time.Now().UTC()

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings: %w", err)
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE tenants
		SET name = $1, settings = $2, updated_at = $3
		WHERE id = $4 AND deleted_at IS NULL
	`, name, settingsJSON, now, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update tenant: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return nil, fmt.Errorf("tenant %s not found", id)
	}

	return s.Get(ctx, id)
}

// Delete soft-deletes a tenant.
func (s *TenantStore) Delete(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE tenants
		SET deleted_at = $1, updated_at = $1
		WHERE id = $2 AND deleted_at IS NULL
	`, now, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("tenant %s not found", id)
	}

	return nil
}

// slugExists checks if a tenant slug already exists.
func (s *TenantStore) slugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM tenants WHERE slug = $1 AND deleted_at IS NULL)
	`, slug).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

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
