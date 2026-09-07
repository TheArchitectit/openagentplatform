package ingest

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func TestEventHostID(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"device_id": "dev-123"},
	}
	if id := eventHostID(ev); id != "dev-123" {
		t.Errorf("eventHostID = %q, want dev-123", id)
	}
}

func TestEventHostIDMachineID(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"machine_id": "m-abc"},
	}
	if id := eventHostID(ev); id != "m-abc" {
		t.Errorf("eventHostID = %q, want m-abc", id)
	}
}

func TestEventHostIDAgentID(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"agent_id": "s1-agent-7"},
	}
	if id := eventHostID(ev); id != "s1-agent-7" {
		t.Errorf("eventHostID = %q, want s1-agent-7", id)
	}
}

func TestEventHostIDEmpty(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"unrelated": "x"},
	}
	if id := eventHostID(ev); id != "" {
		t.Errorf("eventHostID = %q, want empty", id)
	}
}

func TestEventHostname(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"hostname": "host1"},
	}
	if h := eventHostname(ev); h != "host1" {
		t.Errorf("eventHostname = %q, want host1", h)
	}
}

func TestEventHostnameAgentHostName(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"agentHostName": "sentinel-host"},
	}
	if h := eventHostname(ev); h != "sentinel-host" {
		t.Errorf("eventHostname = %q, want sentinel-host", h)
	}
}

func TestEventHostnameEmpty(t *testing.T) {
	ev := &models.SecurityEvent{
		Payload: map[string]interface{}{"unrelated": "x"},
	}
	if h := eventHostname(ev); h != "" {
		t.Errorf("eventHostname = %q, want empty", h)
	}
}
