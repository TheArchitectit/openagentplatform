package relay

import (
	"context"
	"testing"
	"time"
)

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
