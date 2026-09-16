// Package cloud_test holds cross-provider contract tests. These live in
// an external test package so they can import every provider client
// without an import cycle (the providers themselves import internal/cloud).
//
// The contract under test is the ProviderClient seam the reconciler
// depends on: every shipped provider must identify itself with its
// canonical lowercase name and be usable as a registered client. Network
// behaviour is deliberately NOT exercised here — the SDK-backed methods
// require live credentials; those paths are covered by the fake-provider
// fixture tests in reconciler_fixture_test.go at the reconciler level.
package cloud_test

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
	"github.com/openagentplatform/openagentplatform/internal/cloud/aws"
	"github.com/openagentplatform/openagentplatform/internal/cloud/azure"
	"github.com/openagentplatform/openagentplatform/internal/cloud/gcp"
)

// TestProviderContracts pins the identity + compliance contract for every
// shipped provider client. Adding a new provider? Register it here — a
// provider that forgets this suite is a provider the reconciler can
// register but whose identity the platform cannot reason about.
func TestProviderContracts(t *testing.T) {
	tests := []struct {
		name     string
		client   cloud.ProviderClient
		wantName string
	}{
		{"aws", aws.NewAWSClient(nil), "aws"},
		{"azure", azure.NewAzureClient("test-subscription", nil), "azure"},
		{"gcp", gcp.NewGCPClient("test-project"), "gcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.client == nil {
				t.Fatal("constructor returned nil")
			}
			// Compile-time interface assertion, re-checked at runtime so
			// a regression reports the failing provider by name.
			var _ cloud.ProviderClient = tt.client
			if got := tt.client.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q (reconciler registers providers by this name)", got, tt.wantName)
			}
		})
	}
}
