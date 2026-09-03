package eve

import (
	"context"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// Compile-time check: HypervisorClient is satisfied by *fakeHypervisorClient.
var _ HypervisorClient = (*fakeHypervisorClient)(nil)

// fakeHypervisorClient is a minimal stub that satisfies the interface so
// the compile check above is meaningful.
type fakeHypervisorClient struct{}

func (fakeHypervisorClient) Name() models.HypervisorProvider {
	return models.HypervisorProxmox
}

func (fakeHypervisorClient) ListNodes(context.Context) ([]NodeInfo, error) {
	return nil, nil
}

func (fakeHypervisorClient) ListVMs(context.Context) ([]VMInfo, error) {
	return nil, nil
}

func (fakeHypervisorClient) ListContainers(context.Context) ([]ContainerInfo, error) {
	return nil, nil
}

func (fakeHypervisorClient) ListStoragePools(context.Context) ([]StoragePoolInfo, error) {
	return nil, nil
}

func (fakeHypervisorClient) ListRecentEvents(context.Context, time.Time) ([]ClusterEvent, error) {
	return nil, nil
}
