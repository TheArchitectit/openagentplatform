package proxmox

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/eve"
)

func TestProxmoxClientImplementsInterface(t *testing.T) {
	var _ eve.HypervisorClient = (*ProxmoxClient)(nil)
}
