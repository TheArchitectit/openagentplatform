package audit

import "testing"

// TestOutcomeFromStatus verifies the audit-trail status mapping. In
// particular, a status of 0 (WriteHeader never called, no body written) must
// be recorded as an error rather than silently as a success — otherwise a
// handler that bailed without responding would look like a healthy request in
// the audit trail.
func TestOutcomeFromStatus(t *testing.T) {
	cases := []struct {
		status int
		want   Outcome
	}{
		{0, OutcomeError},          // never responded — previously OutcomeSuccess (bug)
		{200, OutcomeSuccess},
		{201, OutcomeSuccess},
		{204, OutcomeSuccess},
		{301, OutcomeError},        // 3xx not explicitly success
		{401, OutcomeDenied},
		{403, OutcomeDenied},
		{400, OutcomeFailure},
		{404, OutcomeFailure},
		{422, OutcomeFailure},
		{500, OutcomeError},
		{502, OutcomeError},
	}
	for _, tc := range cases {
		if got := outcomeFromStatus(tc.status); got != tc.want {
			t.Errorf("outcomeFromStatus(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
