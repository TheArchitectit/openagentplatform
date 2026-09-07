package vsphere

import (
	"testing"

	"github.com/vmware/govmomi/vim25/types"

	"github.com/openagentplatform/openagentplatform/internal/eve"
)

func TestVSphereClientImplementsInterface(t *testing.T) {
	var _ eve.HypervisorClient = (*VSphereClient)(nil)
}

func TestPowerState(t *testing.T) {
	cases := []struct {
		in   types.VirtualMachinePowerState
		want string
	}{
		{types.VirtualMachinePowerStatePoweredOn, "running"},
		{types.VirtualMachinePowerStatePoweredOff, "stopped"},
		{types.VirtualMachinePowerStateSuspended, "suspended"},
		{types.VirtualMachinePowerState("nonsense"), "unknown"},
	}
	for _, c := range cases {
		if got := powerState(c.in); got != c.want {
			t.Errorf("powerState(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
