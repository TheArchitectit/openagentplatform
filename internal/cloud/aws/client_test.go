package aws

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
)

func TestAWSClientImplementsInterface(t *testing.T) {
	var _ cloud.ProviderClient = (*AWSClient)(nil)
}
