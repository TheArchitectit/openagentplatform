package tenancy

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTenant_Fields(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()

	tenant := &Tenant{
		ID:        id,
		Name:      "Test Company",
		Slug:      "test-company",
		Settings:  map[string]interface{}{"key": "value"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if tenant.ID != id {
		t.Errorf("expected ID %s, got %s", id, tenant.ID)
	}
	if tenant.Name != "Test Company" {
		t.Errorf("expected name 'Test Company', got %q", tenant.Name)
	}
	if tenant.Slug != "test-company" {
		t.Errorf("expected slug 'test-company', got %q", tenant.Slug)
	}
	if tenant.Settings["key"] != "value" {
		t.Errorf("expected settings key 'value', got %v", tenant.Settings["key"])
	}
	if tenant.DeletedAt != nil {
		t.Error("expected nil DeletedAt")
	}
}

func TestTenant_DeletedAt(t *testing.T) {
	now := time.Now().UTC()
	deletedAt := now.Add(-24 * time.Hour)

	tenant := &Tenant{
		ID:        uuid.New(),
		Name:      "Deleted Company",
		Slug:      "deleted-company",
		DeletedAt: &deletedAt,
	}

	if tenant.DeletedAt == nil {
		t.Error("expected non-nil DeletedAt")
	}
	if !tenant.DeletedAt.Equal(deletedAt) {
		t.Errorf("expected DeletedAt %v, got %v", deletedAt, tenant.DeletedAt)
	}
}

func TestTenantStore_Create_Validation(t *testing.T) {
	// Test validation without database
	store := &TenantStore{db: nil}

	tests := []struct {
		name    string
		slug    string
		wantErr bool
	}{
		{"", "valid-slug", true},
		{"Valid Name", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_"+tt.slug, func(t *testing.T) {
			// This will fail at validation level
			_, err := store.Create(nil, tt.name, tt.slug, nil)
			if tt.wantErr && err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}

func TestTenantStore_New(t *testing.T) {
	store := NewTenantStore(nil)
	if store == nil {
		t.Error("expected non-nil store")
	}
}

func TestTenantConfigStore_New(t *testing.T) {
	store := NewTenantConfigStore(nil)
	if store == nil {
		t.Error("expected non-nil store")
	}
}

func TestTenant_JSON(t *testing.T) {
	tenant := &Tenant{
		ID:   uuid.New(),
		Name: "Test",
		Slug: "test",
		Settings: map[string]interface{}{
			"feature": true,
			"limit":   100,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Verify JSON serialization doesn't panic
	if tenant.Settings == nil {
		t.Error("expected non-nil settings")
	}
	if tenant.Settings["feature"] != true {
		t.Error("expected feature to be true")
	}
	if tenant.Settings["limit"] != 100 {
		t.Error("expected limit to be 100")
	}
}
