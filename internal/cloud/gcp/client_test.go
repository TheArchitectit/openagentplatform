package gcp

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
)

func TestGCPClientImplementsInterface(t *testing.T) {
	var _ cloud.ProviderClient = (*GCPClient)(nil)
}
