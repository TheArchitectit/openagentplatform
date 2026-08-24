package models

import (
	"encoding/json"
	"testing"
	"time"
)

// TestWinUpdateKBStateJSONRoundTrip verifies the struct's JSON tags round-
// trip correctly, including omitempty on Result.
func TestWinUpdateKBStateJSONRoundTrip(t *testing.T) {
	in := WinUpdateKBState{
		ID:        "id-1",
		OrgID:     "org-1",
		AgentID:   "agent-1",
		KB:        "KB5001234",
		State:     "approved",
		Result:    "",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC),
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Result is omitempty; with an empty string it should be absent.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["result"]; ok {
		t.Errorf("expected result to be omitted when empty, got %v", raw["result"])
	}

	var out WinUpdateKBState
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != in.ID || out.OrgID != in.OrgID || out.AgentID != in.AgentID ||
		out.KB != in.KB || out.State != in.State {
		t.Errorf("round-trip mismatch: %+v vs %+v", out, in)
	}
}

// TestWinUpdateKBStateJSONWithResult verifies a non-empty Result is
// preserved.
func TestWinUpdateKBStateJSONWithResult(t *testing.T) {
	in := WinUpdateKBState{ID: "id-1", OrgID: "org-1", AgentID: "agent-1", KB: "KB1", State: "failed", Result: "0x80070005"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out WinUpdateKBState
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Result != "0x80070005" {
		t.Errorf("result: got %q, want 0x80070005", out.Result)
	}
}
