package azure

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
)

func TestAzureClientImplementsInterface(t *testing.T) {
	var _ cloud.ProviderClient = (*AzureClient)(nil)
}
