package models

import (
	"encoding/json"
	"testing"
	"time"
)

// Regression: pkg/agent publishes Timestamp as int64 unix seconds
// (time.Now().Unix()), which json.Unmarshal rejects for time.Time. Every
// agent heartbeat was failing to decode server-side (event-bus spec,
// Known Limitations #1; wiring plan W1).
func TestHeartbeatUnmarshalUnixSeconds(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	payload := `{"agent_id":"a-1","timestamp":` +
		strconvFormatInt(now.Unix()) +
		`,"cpu_percent":12.5,"mem_percent":40.0,"disk_percent":60.5,"uptime_secs":3600,"version":"1.2.3"}`

	var hb Heartbeat
	if err := json.Unmarshal([]byte(payload), &hb); err != nil {
		t.Fatalf("decode int64 timestamp: %v", err)
	}
	if !hb.Timestamp.Equal(now) {
		t.Errorf("timestamp = %v, want %v", hb.Timestamp, now)
	}
	if hb.AgentID != "a-1" || hb.CPUPercent != 12.5 || hb.Version != "1.2.3" || hb.UptimeSecs != 3600 {
		t.Errorf("sibling fields not decoded correctly: %+v", hb)
	}
}

func TestHeartbeatUnmarshalRFC3339(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	payload := `{"agent_id":"a-2","timestamp":"` + now.Format(time.RFC3339) + `"}`

	var hb Heartbeat
	if err := json.Unmarshal([]byte(payload), &hb); err != nil {
		t.Fatalf("decode RFC3339 timestamp: %v", err)
	}
	if !hb.Timestamp.Equal(now) {
		t.Errorf("timestamp = %v, want %v", hb.Timestamp, now)
	}
}

func TestHeartbeatUnmarshalTimestampMagnitudes(t *testing.T) {
	base := time.Unix(1755936000, 0).UTC() // plausible epoch second
	cases := []struct {
		name string
		raw  string
		want time.Time
	}{
		{"seconds", "1755936000", base},
		{"milliseconds", "1755936000000", base},
		{"microseconds", "1755936000000000", base},
		{"nanoseconds", "1755936000000000000", base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hb Heartbeat
			if err := json.Unmarshal([]byte(`{"timestamp":`+tc.raw+`}`), &hb); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !hb.Timestamp.Equal(tc.want) {
				t.Errorf("timestamp = %v, want %v", hb.Timestamp, tc.want)
			}
		})
	}
}

func TestHeartbeatUnmarshalEdgeCases(t *testing.T) {
	t.Run("missing timestamp stays zero", func(t *testing.T) {
		var hb Heartbeat
		if err := json.Unmarshal([]byte(`{"agent_id":"a-3"}`), &hb); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !hb.Timestamp.IsZero() {
			t.Errorf("timestamp = %v, want zero", hb.Timestamp)
		}
	})
	t.Run("garbage timestamp stays zero without erroring", func(t *testing.T) {
		var hb Heartbeat
		if err := json.Unmarshal([]byte(`{"timestamp":"not-a-time","agent_id":"a-4"}`), &hb); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !hb.Timestamp.IsZero() {
			t.Errorf("timestamp = %v, want zero", hb.Timestamp)
		}
		if hb.AgentID != "a-4" {
			t.Errorf("agent_id = %q, want a-4", hb.AgentID)
		}
	})
	t.Run("malformed JSON still errors", func(t *testing.T) {
		var hb Heartbeat
		if err := json.Unmarshal([]byte(`{not json`), &hb); err == nil {
			t.Error("expected error for malformed payload")
		}
	})
}

func TestUnixSecondsToTimeBoundaries(t *testing.T) {
	sec := int64(1755936000)
	if got := unixSecondsToTime(sec); got.Unix() != sec {
		t.Errorf("seconds: got %v", got)
	}
	if got := unixSecondsToTime(0); !got.IsZero() {
		t.Errorf("zero: got %v, want zero time", got)
	}
}

func strconvFormatInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
