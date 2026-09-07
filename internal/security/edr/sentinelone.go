package edr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type SentinelOneProvider struct {
	webhookSecret string
	apiToken      string
	baseURL       string
	httpClient    *http.Client
}

func NewSentinelOneProvider(apiToken, baseURL, webhookSecret string) *SentinelOneProvider {
	return &SentinelOneProvider{
		apiToken:      apiToken,
		baseURL:       baseURL,
		webhookSecret: webhookSecret,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *SentinelOneProvider) Name() models.EDRProviderType {
	return models.EDRSentinelOne
}

func (p *SentinelOneProvider) VerifyWebhookSignature(payload []byte, signature string) bool {
	if p.webhookSecret == "" {
		return true
	}
	return verifyHMAC(payload, signature, p.webhookSecret)
}

type sentinelOneThreat struct {
	ID         string    `json:"id"`
	Severity   string    `json:"severity"`
	Tactic     string    `json:"tactic"`
	Technique  string    `json:"technique"`
	ThreatType string    `json:"threatType"`
	AgentID    string    `json:"agentId"`
	Hostname   string    `json:"agentHostName"`
	DetectedAt time.Time `json:"detectionTime"`
}

func (p *SentinelOneProvider) ParseWebhookEvent(payload []byte) (*models.SecurityEvent, error) {
	var raw struct {
		Data sentinelOneThreat `json:"data"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	severity := "info"
	switch raw.Data.Severity {
	case "critical", "high":
		severity = "critical"
	case "medium":
		severity = "warning"
	}
	return &models.SecurityEvent{
		Provider:        models.EDRSentinelOne,
		ProviderEventID: raw.Data.ID,
		Severity:        severity,
		Tactic:          raw.Data.Tactic,
		Technique:       raw.Data.Technique,
		DetectionType:   raw.Data.ThreatType,
		OccurredAt:      raw.Data.DetectedAt,
		IngestionMethod: "webhook",
		Payload: map[string]any{
			"agent_id": raw.Data.AgentID,
			"hostname": raw.Data.Hostname,
		},
	}, nil
}

func (p *SentinelOneProvider) PullRecentEvents(ctx context.Context, since time.Time) ([]*models.SecurityEvent, error) {
	url := p.baseURL + "/web/api/v2.1/threats?createdAt__gte=" + since.UTC().Format(time.RFC3339)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "ApiToken "+p.apiToken)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sentinelone: %s: %s", resp.Status, body)
	}
	var out struct {
		Data []sentinelOneThreat `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var events []*models.SecurityEvent
	for _, t := range out.Data {
		ev := &models.SecurityEvent{
			Provider:        models.EDRSentinelOne,
			ProviderEventID: t.ID,
			Severity:        normalizeS1Severity(t.Severity),
			Tactic:          t.Tactic,
			Technique:       t.Technique,
			DetectionType:   t.ThreatType,
			OccurredAt:      t.DetectedAt,
			IngestionMethod: "pull",
			Payload: map[string]any{
				"agent_id": t.AgentID,
				"hostname": t.Hostname,
			},
		}
		events = append(events, ev)
	}
	return events, nil
}

func normalizeS1Severity(s string) string {
	switch s {
	case "critical", "high":
		return "critical"
	case "medium":
		return "warning"
	default:
		return "info"
	}
}
