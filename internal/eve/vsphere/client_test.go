package vsphere

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/eve"
)

func TestVSphereClientImplementsInterface(t *testing.T) {
	var _ eve.HypervisorClient = (*VSphereClient)(nil)
}
