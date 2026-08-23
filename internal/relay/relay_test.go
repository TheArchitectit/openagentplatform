package relay

import (
	"context"
	"testing"
	"time"
)

func TestRelayService_EstablishConnection(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	conn, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if conn.TenantID != "tenant-1" {
		t.Errorf("expected tenant ID 'tenant-1', got %q", conn.TenantID)
	}
	if conn.SourceAgentID != "agent-1" {
		t.Errorf("expected source agent ID 'agent-1', got %q", conn.SourceAgentID)
	}
	if conn.TargetAgentID != "agent-2" {
		t.Errorf("expected target agent ID 'agent-2', got %q", conn.TargetAgentID)
	}
	if conn.Status != ConnectionStatusActive {
		t.Errorf("expected status 'active', got %q", conn.Status)
	}
}

func TestRelayService_EstablishConnection_Validation(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	tests := []struct {
		name          string
		tenantID      string
		sourceAgentID string
		targetAgentID string
		wantErr       bool
	}{
		{"missing tenant ID", "", "agent-1", "agent-2", true},
		{"missing source agent", "tenant-1", "", "agent-2", true},
		{"missing target agent", "tenant-1", "agent-1", "", true},
		{"valid", "tenant-1", "agent-1", "agent-2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.EstablishConnection(context.Background(), tt.tenantID, tt.sourceAgentID, tt.targetAgentID)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRelayService_EstablishConnection_MaxConnections(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 2}, nil)

	// Create max connections
	_, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.EstablishConnection(context.Background(), "tenant-1", "agent-3", "agent-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Third connection should fail
	_, err = svc.EstablishConnection(context.Background(), "tenant-1", "agent-5", "agent-6")
	if err == nil {
		t.Error("expected error for exceeding max connections")
	}
}

func TestRelayService_CloseConnection(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	conn, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.CloseConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify connection is closed
	closedConn, err := svc.GetConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if closedConn.Status != ConnectionStatusClosed {
		t.Errorf("expected status 'closed', got %q", closedConn.Status)
	}
}

func TestRelayService_CloseConnection_NotFound(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	err := svc.CloseConnection(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent connection")
	}
}

func TestRelayService_CloseConnection_AlreadyClosed(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	conn, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.CloseConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to close again
	err = svc.CloseConnection(context.Background(), conn.ID)
	if err == nil {
		t.Error("expected error for already closed connection")
	}
}

func TestRelayService_GetConnection(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	created, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conn, err := svc.GetConnection(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if conn.ID != created.ID {
		t.Errorf("expected connection ID %s, got %s", created.ID, conn.ID)
	}
}

func TestRelayService_GetConnection_NotFound(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	_, err := svc.GetConnection(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent connection")
	}
}

func TestRelayService_ListConnections(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	// Create connections for different tenants
	_, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.EstablishConnection(context.Background(), "tenant-1", "agent-3", "agent-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.EstablishConnection(context.Background(), "tenant-2", "agent-5", "agent-6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	conns := svc.ListConnections(context.Background(), "tenant-1")
	if len(conns) != 2 {
		t.Errorf("expected 2 connections for tenant-1, got %d", len(conns))
	}

	conns = svc.ListConnections(context.Background(), "tenant-2")
	if len(conns) != 1 {
		t.Errorf("expected 1 connection for tenant-2, got %d", len(conns))
	}
}

func TestRelayService_RecordBytes(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	conn, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.RecordBytes(context.Background(), conn.ID, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = svc.RecordBytes(context.Background(), conn.ID, 2048)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify bytes recorded
	updatedConn, _ := svc.GetConnection(context.Background(), conn.ID)
	if updatedConn.BytesRelayed != 3072 {
		t.Errorf("expected 3072 bytes relayed, got %d", updatedConn.BytesRelayed)
	}
}

func TestRelayService_RecordBytes_NotFound(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	err := svc.RecordBytes(context.Background(), "nonexistent", 1024)
	if err == nil {
		t.Error("expected error for nonexistent connection")
	}
}

func TestRelayService_GetMetrics(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	// Create connections
	conn1, _ := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	svc.EstablishConnection(context.Background(), "tenant-1", "agent-3", "agent-4")

	// Record bytes
	svc.RecordBytes(context.Background(), conn1.ID, 1024)

	metrics := svc.GetMetrics(context.Background(), "tenant-1")
	if metrics.TenantID != "tenant-1" {
		t.Errorf("expected tenant ID 'tenant-1', got %q", metrics.TenantID)
	}
	if metrics.ConnectionCount != 2 {
		t.Errorf("expected 2 active connections, got %d", metrics.ConnectionCount)
	}
	if metrics.TotalConnections != 2 {
		t.Errorf("expected 2 total connections, got %d", metrics.TotalConnections)
	}
	if metrics.TotalBytesRelayed != 1024 {
		t.Errorf("expected 1024 bytes relayed, got %d", metrics.TotalBytesRelayed)
	}
}

func TestRelayService_GetMetrics_Empty(t *testing.T) {
	svc := NewRelayService(RelayConfig{}, nil)

	metrics := svc.GetMetrics(context.Background(), "nonexistent")
	if metrics.TenantID != "nonexistent" {
		t.Errorf("expected tenant ID 'nonexistent', got %q", metrics.TenantID)
	}
	if metrics.ConnectionCount != 0 {
		t.Errorf("expected 0 connections, got %d", metrics.ConnectionCount)
	}
}

func TestRelayService_CleanupIdleConnections(t *testing.T) {
	svc := NewRelayService(RelayConfig{
		IdleTimeout: 1 * time.Millisecond,
	}, nil)

	// Create connection
	conn, err := svc.EstablishConnection(context.Background(), "tenant-1", "agent-1", "agent-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for idle timeout
	time.Sleep(2 * time.Millisecond)

	// Cleanup
	closed := svc.CleanupIdleConnections(context.Background())
	if closed != 1 {
		t.Errorf("expected 1 connection closed, got %d", closed)
	}

	// Verify connection is closed
	closedConn, _ := svc.GetConnection(context.Background(), conn.ID)
	if closedConn.Status != ConnectionStatusClosed {
		t.Errorf("expected status 'closed', got %q", closedConn.Status)
	}
}

func TestConnectionStatus_Constants(t *testing.T) {
	tests := []struct {
		status   ConnectionStatus
		expected string
	}{
		{ConnectionStatusActive, "active"},
		{ConnectionStatusClosed, "closed"},
		{ConnectionStatusError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.status))
			}
		})
	}
}
