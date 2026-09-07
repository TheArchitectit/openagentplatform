package aws

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/cloud"
)

func TestAWSClientImplementsInterface(t *testing.T) {
	var _ cloud.ProviderClient = (*AWSClient)(nil)
}

func TestAWSPeriodRange(t *testing.T) {
	cases := []struct {
		period   string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{"2026-09", "2026-09-01", "2026-10-01", false},
		{"2026-12", "2026-12-01", "2027-01-01", false},
		{"2026-01", "2026-01-01", "2026-02-01", false},
		{"invalid", "", "", true},
		{"2026-13", "", "", true},
	}
	for _, c := range cases {
		start, end, err := awsPeriodRange(c.period)
		if c.wantErr {
			if err == nil {
				t.Errorf("awsPeriodRange(%q): expected error, got nil", c.period)
			}
			continue
		}
		if err != nil {
			t.Errorf("awsPeriodRange(%q): unexpected error: %v", c.period, err)
			continue
		}
		if start != c.wantStart {
			t.Errorf("awsPeriodRange(%q) start = %q, want %q", c.period, start, c.wantStart)
		}
		if end != c.wantEnd {
			t.Errorf("awsPeriodRange(%q) end = %q, want %q", c.period, end, c.wantEnd)
		}
	}
}
