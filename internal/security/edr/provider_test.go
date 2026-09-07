package edr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/security"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func TestCrowdStrikeProviderImplementsInterface(t *testing.T) {
	p := NewCrowdStrikeProvider("id", "secret", "whsec")
	var _ security.EDRProvider = p
}

func TestDefenderProviderImplementsInterface(t *testing.T) {
	p := NewDefenderProvider("tenant", "id", "secret", "whsec")
	var _ security.EDRProvider = p
}

func TestSentinelOneProviderImplementsInterface(t *testing.T) {
	p := NewSentinelOneProvider("token", "https://usea1.sentinelone.net", "whsec")
	var _ security.EDRProvider = p
}

func TestCrowdStrikeParseEvent(t *testing.T) {
	p := NewCrowdStrikeProvider("id", "secret", "whsec")
	payload := []byte(`{"detection_id":"det-123","device_id":"dev-abc","hostname":"host1","severity":85,"tactic":"TA0001","technique":"T1078","type":"process","timestamp":1700000000}`)
	ev, err := p.ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if ev.Severity != "critical" {
		t.Errorf("Severity = %q, want critical", ev.Severity)
	}
	if ev.Tactic != "TA0001" {
		t.Errorf("Tactic = %q, want TA0001", ev.Tactic)
	}
	if ev.Provider != models.EDRCrowdStrike {
		t.Errorf("Provider = %q, want crowdstrike", ev.Provider)
	}
}

func TestCrowdStrikeParseEventEnvelope(t *testing.T) {
	p := NewCrowdStrikeProvider("id", "secret", "whsec")
	payload := []byte(`{"event":{"detection_id":"det-456","device_id":"dev-def","hostname":"host2","severity":60,"tactic":"TA0002","technique":"T1059","type":"network","timestamp":1700000001}}`)
	ev, err := p.ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if ev.Severity != "warning" {
		t.Errorf("Severity = %q, want warning", ev.Severity)
	}
}

func TestDefenderParseEvent(t *testing.T) {
	p := NewDefenderProvider("tenant", "id", "secret", "whsec")
	payload := []byte(`{"value":[{"id":"alert-1","title":"Suspicious","severity":"High","category":"Execution","mitreTechniques":{"tactic":"TA0002","technique":"T1059"},"machineId":"machine-abc","detectionDateTime":"2026-01-01T00:00:00Z"}]}`)
	ev, err := p.ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if ev.Severity != "critical" {
		t.Errorf("Severity = %q, want critical", ev.Severity)
	}
	if ev.Provider != models.EDRDefender {
		t.Errorf("Provider = %q, want defender", ev.Provider)
	}
}

func TestSentinelOneParseEvent(t *testing.T) {
	p := NewSentinelOneProvider("token", "https://usea1.sentinelone.net", "whsec")
	payload := []byte(`{"data":{"id":"threat-1","severity":"high","tactic":"TA0001","technique":"T1078","threatType":"malware","agentId":"agent-xyz","agentHostName":"workstation1","detectionTime":"2026-01-01T00:00:00Z"}}`)
	ev, err := p.ParseWebhookEvent(payload)
	if err != nil {
		t.Fatalf("ParseWebhookEvent: %v", err)
	}
	if ev.Severity != "critical" {
		t.Errorf("Severity = %q, want critical", ev.Severity)
	}
	if ev.Provider != models.EDRSentinelOne {
		t.Errorf("Provider = %q, want sentinelone", ev.Provider)
	}
}

func TestVerifyHMAC(t *testing.T) {
	secret := "my-webhook-secret"
	payload := []byte(`{"test":true}`)
	mac := hmacSHA256(payload, secret)
	if !verifyHMAC(payload, mac, secret) {
		t.Error("verifyHMAC should return true for a valid signature")
	}
	if verifyHMAC(payload, "bad-signature", secret) {
		t.Error("verifyHMAC should return false for an invalid signature")
	}
}

func hmacSHA256(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
