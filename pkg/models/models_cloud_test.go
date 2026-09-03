package models

import "testing"

func TestCloudProviderConstants(t *testing.T) {
	if CloudProviderAWS != "aws" {
		t.Errorf("CloudProviderAWS = %q, want aws", CloudProviderAWS)
	}
	if CloudProviderAzure != "azure" {
		t.Errorf("CloudProviderAzure = %q, want azure", CloudProviderAzure)
	}
	if CloudProviderGCP != "gcp" {
		t.Errorf("CloudProviderGCP = %q, want gcp", CloudProviderGCP)
	}
}

func TestCloudResourceTags(t *testing.T) {
	r := &CloudResource{
		ID:           "r1",
		OrgID:        "org1",
		Provider:     CloudProviderAWS,
		ResourceID:   "i-abc123",
		ResourceType: "ec2_instance",
		Tags:         map[string]string{"env": "prod", "owner": "sre"},
	}
	if r.Tags["env"] != "prod" {
		t.Errorf("r.Tags[env] = %q, want prod", r.Tags["env"])
	}
}
