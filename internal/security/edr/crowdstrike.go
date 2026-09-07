package edr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type CrowdStrikeProvider struct {
	webhookSecret string
	clientID      string
	clientSecret  string
	baseURL       string
	httpClient    *http.Client
	token         string
	tokenExp      time.Time
}

func NewCrowdStrikeProvider(clientID, clientSecret, webhookSecret string) *CrowdStrikeProvider {
	return &CrowdStrikeProvider{
		clientID:      clientID,
		clientSecret:  clientSecret,
		webhookSecret: webhookSecret,
		baseURL:       "https://api.crowdstrike.com",
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *CrowdStrikeProvider) Name() models.EDRProviderType {
	return models.EDRCrowdStrike
}

func (p *CrowdStrikeProvider) VerifyWebhookSignature(payload []byte, signature string) bool {
	if p.webhookSecret == "" {
		return true
	}
	return verifyHMAC(payload, signature, p.webhookSecret)
}

type crowdstrikeDetection struct {
	DetectionID string `json:"detection_id"`
	DeviceID    string `json:"device_id"`
	Hostname    string `json:"hostname"`
	Severity    int    `json:"severity"`
	Tactic      string `json:"tactic"`
	Technique   string `json:"technique"`
	Type        string `json:"type"`
	Timestamp   int64  `json:"timestamp"`
}

func (p *CrowdStrikeProvider) ParseWebhookEvent(payload []byte) (*models.SecurityEvent, error) {
	var raw crowdstrikeDetection
	var envelope struct {
		Event crowdstrikeDetection `json:"event"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && envelope.Event.DetectionID != "" {
		raw = envelope.Event
	} else if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("crowdstrike: parse: %w", err)
	}
	severity := "info"
	switch {
	case raw.Severity >= 80:
		severity = "critical"
	case raw.Severity >= 50:
		severity = "warning"
	}
	return &models.SecurityEvent{
		Provider:        models.EDRCrowdStrike,
		ProviderEventID: raw.DetectionID,
		Severity:        severity,
		Tactic:          raw.Tactic,
		Technique:       raw.Technique,
		DetectionType:   raw.Type,
		OccurredAt:      time.Unix(raw.Timestamp, 0),
		IngestionMethod: "webhook",
		Payload: map[string]any{
			"device_id": raw.DeviceID,
			"hostname":  raw.Hostname,
		},
	}, nil
}

func (p *CrowdStrikeProvider) PullRecentEvents(ctx context.Context, since time.Time) ([]*models.SecurityEvent, error) {
	if err := p.refreshToken(ctx); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/detects/queries/detects/v1?filter=created_timestamp:>%d", p.baseURL, since.Unix())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("crowdstrike: %s: %s", resp.Status, body)
	}
	var out struct {
		Resources []string `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return nil, nil
}

func (p *CrowdStrikeProvider) refreshToken(ctx context.Context) error {
	if p.token != "" && time.Now().Before(p.tokenExp) {
		return nil
	}
	url := p.baseURL + "/oauth2/token"
	body := []byte("client_id=" + p.clientID + "&client_secret=" + p.clientSecret)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	p.token = out.AccessToken
	p.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return nil
}
