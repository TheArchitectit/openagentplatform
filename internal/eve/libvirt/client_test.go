package libvirt

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/eve"
)

func TestLibvirtClientImplementsInterface(t *testing.T) {
	var _ eve.HypervisorClient = (*LibvirtClient)(nil)
}
